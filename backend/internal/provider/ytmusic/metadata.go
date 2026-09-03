package ytmusic

import (
	"context"
	"net/http"
	"strings"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/httpx"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

// Limits that keep a single catalogue request bounded.
const (
	maxSearchResults = 20
	maxReleases      = 400
	maxTracks        = 300
)

// Config configures the metadata provider.
type Config struct {
	BaseURL    string
	Language   string
	Region     string
	HTTPClient *http.Client
}

// MetadataProvider reads the YouTube Music catalogue.
type MetadataProvider struct {
	api *innerTube
}

var (
	_ provider.MetadataProvider = (*MetadataProvider)(nil)
	_ provider.Availability     = (*MetadataProvider)(nil)
)

// NewMetadataProvider builds the YouTube Music metadata provider.
func NewMetadataProvider(cfg Config) *MetadataProvider {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = "https://music.youtube.com"
	}
	language := strings.TrimSpace(cfg.Language)
	if language == "" {
		language = "en"
	}
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "US"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = httpx.New(httpx.DefaultTimeout)
	}
	return &MetadataProvider{api: &innerTube{
		httpClient: client,
		baseURL:    baseURL,
		language:   language,
		region:     region,
	}}
}

// Name returns the provider identifier.
func (p *MetadataProvider) Name() string { return ProviderName }

// Available reports whether the InnerTube API answers.
func (p *MetadataProvider) Available(ctx context.Context) error {
	_, err := p.api.search(ctx, "test", filterArtists)
	return err
}

// SearchArtists looks artists up by name.
func (p *MetadataProvider) SearchArtists(ctx context.Context, query string) ([]music.Artist, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "The search query must not be empty.")
	}

	response, err := p.api.search(ctx, trimmed, filterArtists)
	if err != nil {
		return nil, err
	}
	return extractArtists(response, maxSearchResults), nil
}

// GetArtist resolves a YouTube Music channel id.
func (p *MetadataProvider) GetArtist(ctx context.Context, id string) (*music.Artist, error) {
	browseID, err := validateBrowseID(id, "UC", "MPLA")
	if err != nil {
		return nil, err
	}

	response, err := p.api.browse(ctx, browseID, "")
	if err != nil {
		if apperr.CodeOf(err) == apperr.CodeProviderNotFound {
			return nil, apperr.Newf(apperr.CodeArtistNotFound, "YouTube Music does not know the artist %q.", id)
		}
		return nil, err
	}

	artist := extractArtistHeader(response, browseID)
	if artist == nil {
		return nil, apperr.Newf(apperr.CodeArtistNotFound, "YouTube Music does not know the artist %q.", id)
	}
	return artist, nil
}

// GetDiscography returns the releases of an artist.
//
// The artist page shows only a preview of each shelf, so every shelf that
// offers a "show all" navigation is followed to collect the complete list.
func (p *MetadataProvider) GetDiscography(ctx context.Context, artistID string) ([]music.Release, error) {
	browseID, err := validateBrowseID(artistID, "UC", "MPLA")
	if err != nil {
		return nil, err
	}

	response, err := p.api.browse(ctx, browseID, "")
	if err != nil {
		if apperr.CodeOf(err) == apperr.CodeProviderNotFound {
			return nil, apperr.Newf(apperr.CodeArtistNotFound, "YouTube Music does not know the artist %q.", artistID)
		}
		return nil, err
	}

	artistName := ""
	if artist := extractArtistHeader(response, browseID); artist != nil {
		artistName = artist.Name
	}

	releases := make([]music.Release, 0, 32)
	seen := make(map[string]struct{}, 32)
	collect := func(source node) {
		for _, release := range extractReleases(source, artistName) {
			if _, duplicate := seen[release.SourceID]; duplicate {
				continue
			}
			if len(releases) >= maxReleases {
				return
			}
			seen[release.SourceID] = struct{}{}
			releases = append(releases, release)
		}
	}

	collect(response)

	for _, target := range extractShelfContinuations(response) {
		if len(releases) >= maxReleases {
			break
		}
		shelf, err := p.api.browse(ctx, target.browseID, target.params)
		if err != nil {
			// A single unavailable shelf must not fail the whole discography.
			continue
		}
		collect(shelf)
	}

	return releases, nil
}

// GetRelease resolves a YouTube Music album id.
func (p *MetadataProvider) GetRelease(ctx context.Context, releaseID string) (*music.Release, error) {
	browseID, err := validateBrowseID(releaseID, "MPRE", "OLAK")
	if err != nil {
		return nil, err
	}

	response, err := p.api.browse(ctx, browseID, "")
	if err != nil {
		if apperr.CodeOf(err) == apperr.CodeProviderNotFound {
			return nil, apperr.Newf(apperr.CodeReleaseNotFound, "YouTube Music does not know the release %q.", releaseID)
		}
		return nil, err
	}

	release := extractReleaseHeader(response, browseID, "")
	if release == nil {
		return nil, apperr.Newf(apperr.CodeReleaseNotFound, "YouTube Music does not know the release %q.", releaseID)
	}
	return release, nil
}

// GetReleaseTracks returns the tracks of a release.
func (p *MetadataProvider) GetReleaseTracks(ctx context.Context, releaseID string) ([]music.Track, error) {
	browseID, err := validateBrowseID(releaseID, "MPRE", "OLAK")
	if err != nil {
		return nil, err
	}

	response, err := p.api.browse(ctx, browseID, "")
	if err != nil {
		if apperr.CodeOf(err) == apperr.CodeProviderNotFound {
			return nil, apperr.Newf(apperr.CodeReleaseNotFound, "YouTube Music does not know the release %q.", releaseID)
		}
		return nil, err
	}

	release := extractReleaseHeader(response, browseID, "")
	if release == nil {
		return nil, apperr.Newf(apperr.CodeReleaseNotFound, "YouTube Music does not know the release %q.", releaseID)
	}

	tracks := extractTracks(response, *release, maxTracks)
	if len(tracks) == 0 {
		// The page describes a release, so it has tracks. Finding none means
		// the response no longer matches the renderers this parser knows, and
		// reporting that is better than silently downloading an empty album.
		return nil, apperr.Newf(apperr.CodeProviderUnavailable,
			"No track list could be read from the YouTube Music page of %q.", release.DisplayTitle())
	}
	return tracks, nil
}

// validateBrowseID checks that an id looks like one of the expected InnerTube
// identifiers. Rejecting anything else keeps arbitrary strings out of the
// request body.
func validateBrowseID(id string, prefixes ...string) (string, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return "", apperr.New(apperr.CodeInvalidRequest, "The YouTube Music id must not be empty.")
	}
	if len(trimmed) > 128 {
		return "", apperr.New(apperr.CodeInvalidRequest, "The YouTube Music id is too long.")
	}
	for _, r := range trimmed {
		isAllowed := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-'
		if !isAllowed {
			return "", apperr.Newf(apperr.CodeInvalidRequest, "%q is not a valid YouTube Music id.", id)
		}
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return trimmed, nil
		}
	}
	return "", apperr.Newf(apperr.CodeInvalidRequest,
		"%q is not a YouTube Music id of the expected kind (%s).", id, strings.Join(prefixes, ", "))
}
