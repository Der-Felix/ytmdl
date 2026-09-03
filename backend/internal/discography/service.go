package discography

import (
	"context"
	"log/slog"
	"strings"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

// Stage names a step of the resolution. The job manager maps these onto job
// states so that the progress a client sees follows the actual work.
type Stage string

const (
	StageArtist   Stage = "resolving_artist"
	StageReleases Stage = "resolving_releases"
	StageTracks   Stage = "resolving_tracks"
	StageDedup    Stage = "deduplicating"
)

// ProgressFunc reports how far the resolution has come. current and total refer
// to the releases of the tracks stage and are zero for the other stages.
type ProgressFunc func(stage Stage, current, total int)

// Options configures the service.
type Options struct {
	Registry            *provider.Registry
	DurationToleranceMS int
	Logger              *slog.Logger
}

// Service resolves the catalogue of an artist and reduces it to the distinct
// recordings that should be downloaded.
type Service struct {
	registry    *provider.Registry
	toleranceMS int
	logger      *slog.Logger
}

// NewService builds the discography service.
func NewService(opts Options) (*Service, error) {
	if opts.Registry == nil {
		return nil, apperr.New(apperr.CodeInternal, "The discography service needs a provider registry.")
	}
	tolerance := opts.DurationToleranceMS
	if tolerance <= 0 {
		tolerance = DefaultDurationToleranceMS
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{registry: opts.Registry, toleranceMS: tolerance, logger: logger}, nil
}

// ArtistRequest asks for the catalogue of one artist.
type ArtistRequest struct {
	Provider string
	ArtistID string
	Filter   music.ReleaseFilter
}

// Result is the resolved catalogue.
type Result struct {
	Artist   music.Artist    `json:"artist"`
	Releases []music.Release `json:"releases"`
	// Groups holds one entry per distinct recording, together with the
	// duplicates that were folded into it.
	Groups []Group `json:"groups"`
	// TotalTracks is the number of tracks before deduplication.
	TotalTracks int `json:"total_tracks"`
	// Warnings records releases that could not be read. They do not fail the
	// resolution, because one unavailable release must not cost the whole
	// artist.
	Warnings []string `json:"warnings,omitempty"`
	// TransientWarnings counts how many of those failures could plausibly
	// succeed on a later attempt — a rate limit or an outage rather than a
	// release that is simply gone. A subscription uses it to decide whether
	// to come back early or to wait for its normal interval.
	TransientWarnings int `json:"transient_warnings,omitempty"`
}

// Tracks returns the representative track of every group.
func (r Result) Tracks() []music.Track {
	out := make([]music.Track, 0, len(r.Groups))
	for _, group := range r.Groups {
		out = append(out, group.Track)
	}
	return out
}

// DuplicatesRemoved returns how many tracks were folded into another one.
func (r Result) DuplicatesRemoved() int {
	return r.TotalTracks - len(r.Groups)
}

// ResolveArtist walks the catalogue of an artist: the artist itself, the
// releases the filter selects, their tracks, and finally the deduplication.
func (s *Service) ResolveArtist(ctx context.Context, req ArtistRequest, progress ProgressFunc) (*Result, error) {
	metadata, err := s.registry.Metadata(req.Provider)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ArtistID) == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "An artist id is required.")
	}
	filter := req.Filter
	if !filter.Any() {
		filter = music.DefaultReleaseFilter()
	}

	logger := s.logger.With(
		logging.KeyProvider, metadata.Name(),
		logging.KeyOperation, "resolve_artist",
		"artist_id", req.ArtistID,
	)

	report(progress, StageArtist, 0, 0)
	artist, err := metadata.GetArtist(ctx, req.ArtistID)
	if err != nil {
		return nil, err
	}
	logger = logger.With(logging.KeyArtist, artist.Name)

	report(progress, StageReleases, 0, 0)
	discography, err := metadata.GetDiscography(ctx, req.ArtistID)
	if err != nil {
		return nil, err
	}
	releases := music.FilterReleases(discography, filter)
	logger.Info("discography resolved",
		"releases_total", len(discography), "releases_selected", len(releases))

	result := &Result{Artist: *artist, Releases: releases}

	tracks := make([]music.Track, 0, len(releases)*10)
	for i, release := range releases {
		if err := ctx.Err(); err != nil {
			return nil, apperr.Wrap(apperr.CodeJobCancelled, "The resolution was cancelled.", err)
		}
		report(progress, StageTracks, i, len(releases))

		releaseTracks, err := metadata.GetReleaseTracks(ctx, release.SourceID)
		if err != nil {
			warning := release.DisplayTitle() + ": " + apperr.MessageOf(err)
			result.Warnings = append(result.Warnings, warning)
			if apperr.Retryable(err) {
				result.TransientWarnings++
			}
			logger.Warn("release could not be read",
				logging.KeyRelease, release.DisplayTitle(),
				logging.KeyErrorCode, string(apperr.CodeOf(err)),
				logging.KeyError, err.Error())
			continue
		}
		// The release the track was resolved from decides where it is filed
		// later and who its album artist is, so the whole context is applied
		// here, in the one place every download path goes through.
		music.ApplyReleaseContext(&releases[i], releaseTracks, artist.DisplayName())
		tracks = append(tracks, releaseTracks...)
	}
	report(progress, StageTracks, len(releases), len(releases))

	if len(releases) > 0 && len(tracks) == 0 && len(result.Warnings) == len(releases) {
		return nil, apperr.Newf(apperr.CodeProviderUnavailable,
			"None of the %d releases of %q could be read.", len(releases), artist.DisplayName())
	}

	report(progress, StageDedup, 0, 0)
	result.TotalTracks = len(tracks)
	result.Groups = Deduplicate(tracks, DedupOptions{DurationToleranceMS: s.toleranceMS})
	SortGroups(result.Groups)

	logger.Info("catalogue resolved",
		"tracks_total", result.TotalTracks,
		"tracks_distinct", len(result.Groups),
		"duplicates_removed", result.DuplicatesRemoved(),
		"warnings", len(result.Warnings),
		"warnings_transient", result.TransientWarnings)

	return result, nil
}

// ResolveRelease resolves a single release and its deduplicated tracks.
func (s *Service) ResolveRelease(ctx context.Context, providerName, releaseID string) (*Result, error) {
	metadata, err := s.registry.Metadata(providerName)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(releaseID) == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "A release id is required.")
	}

	release, err := metadata.GetRelease(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	tracks, err := metadata.GetReleaseTracks(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	music.ApplyReleaseContext(release, tracks, "")

	result := &Result{
		Artist: music.Artist{
			Name:     release.DisplayAlbumArtist(),
			Provider: metadata.Name(),
		},
		Releases:    []music.Release{*release},
		TotalTracks: len(tracks),
		Groups:      Deduplicate(tracks, DedupOptions{DurationToleranceMS: s.toleranceMS}),
	}
	SortGroups(result.Groups)
	return result, nil
}

// ResolveTrack resolves a single track. Providers that can resolve a track id
// directly are used for that; for the others the release the track belongs to
// is read and the track picked out of it.
func (s *Service) ResolveTrack(ctx context.Context, providerName, trackID, releaseID string) (*music.Track, error) {
	metadata, err := s.registry.Metadata(providerName)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(trackID) == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "A track id is required.")
	}

	if resolver, ok := metadata.(provider.TrackResolver); ok {
		track, err := resolver.GetTrack(ctx, trackID)
		if err == nil {
			return track, nil
		}
		if releaseID == "" {
			return nil, err
		}
	}

	if strings.TrimSpace(releaseID) == "" {
		return nil, apperr.Newf(apperr.CodeInvalidRequest,
			"The provider %q cannot resolve a single track; a release id is required.", metadata.Name())
	}

	tracks, err := metadata.GetReleaseTracks(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	// A single track download has to be filed exactly like the same track
	// coming from a discography run, so it goes through the same release
	// normalisation. A release the provider cannot describe is not fatal here:
	// the track list alone still identifies the recording.
	if release, relErr := metadata.GetRelease(ctx, releaseID); relErr == nil && release != nil {
		music.ApplyReleaseContext(release, tracks, "")
	}
	for _, track := range tracks {
		if track.SourceID == trackID || track.ID == trackID {
			return &track, nil
		}
	}
	return nil, apperr.Newf(apperr.CodeTrackNotFound,
		"The track %q is not part of the release %q.", trackID, releaseID)
}

// SearchArtists forwards an artist search to a metadata provider.
// When the requested provider suffers an outage (PROVIDER_UNAVAILABLE / rate limited),
// it tries the fallback providers in the metadata chain.
func (s *Service) SearchArtists(ctx context.Context, providerName, query string) ([]music.Artist, error) {
	chain := s.registry.MetadataChain(providerName)
	if len(chain) == 0 {
		return nil, apperr.New(apperr.CodeProviderNotFound, "No metadata provider is configured.")
	}

	var lastErr error
	for i, metadata := range chain {
		artists, err := metadata.SearchArtists(ctx, query)
		if err == nil {
			return artists, nil
		}
		lastErr = err
		if apperr.CodeOf(err) != apperr.CodeProviderUnavailable && apperr.CodeOf(err) != apperr.CodeProviderRateLimited {
			return nil, err
		}
		if i+1 < len(chain) {
			s.logger.Warn("metadata provider unavailable for search, falling back",
				logging.KeyProvider, metadata.Name(),
				"fallback", chain[i+1].Name(),
				logging.KeyError, err.Error(),
			)
		}
	}
	return nil, lastErr
}

// Artist forwards an artist lookup to a metadata provider.
func (s *Service) Artist(ctx context.Context, providerName, artistID string) (*music.Artist, error) {
	metadata, err := s.registry.Metadata(providerName)
	if err != nil {
		return nil, err
	}
	return metadata.GetArtist(ctx, artistID)
}

// Discography returns the releases of an artist after applying the filter.
func (s *Service) Discography(ctx context.Context, providerName, artistID string, filter music.ReleaseFilter) ([]music.Release, error) {
	metadata, err := s.registry.Metadata(providerName)
	if err != nil {
		return nil, err
	}
	releases, err := metadata.GetDiscography(ctx, artistID)
	if err != nil {
		return nil, err
	}
	if !filter.Any() {
		return releases, nil
	}
	return music.FilterReleases(releases, filter), nil
}

// Release returns a single release together with its tracks.
func (s *Service) Release(ctx context.Context, providerName, releaseID string) (*music.Release, []music.Track, error) {
	metadata, err := s.registry.Metadata(providerName)
	if err != nil {
		return nil, nil, err
	}
	release, err := metadata.GetRelease(ctx, releaseID)
	if err != nil {
		return nil, nil, err
	}
	tracks, err := metadata.GetReleaseTracks(ctx, releaseID)
	if err != nil {
		return nil, nil, err
	}
	music.ApplyReleaseContext(release, tracks, "")
	return release, tracks, nil
}

func report(progress ProgressFunc, stage Stage, current, total int) {
	if progress != nil {
		progress(stage, current, total)
	}
}
