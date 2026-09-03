package deezer

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

var idRegexp = regexp.MustCompile(`^[0-9]{1,32}$`)

// Provider implements provider.MetadataProvider for Deezer.
type Provider struct {
	client *client
}

// New creates a new Deezer metadata provider.
func New(cfg Config) *Provider {
	return &Provider{
		client: newClient(cfg),
	}
}

// Name returns the provider's unique identifier.
func (p *Provider) Name() string {
	return providerName
}

// Available checks whether Deezer is reachable.
//
// The check goes out unpaced and unretried: it reports what Deezer answers
// right now, rather than queueing behind whatever catalogue walk is in
// progress. See client.probe.
func (p *Provider) Available(ctx context.Context) error {
	var reachable struct {
		Data []any `json:"data"`
	}
	return p.client.probe(ctx, "/genre", &reachable)
}

// SearchArtists searches for artists matching the given query.
func (p *Provider) SearchArtists(ctx context.Context, query string) ([]music.Artist, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Search query must not be empty.")
	}

	var list apiArtistList
	endpoint := fmt.Sprintf("/search/artist?q=%s", url.QueryEscape(q))
	if err := p.client.get(ctx, endpoint, &list); err != nil {
		return nil, err
	}

	out := make([]music.Artist, 0, len(list.Data))
	for i := range list.Data {
		out = append(out, toArtist(&list.Data[i]))
	}
	return out, nil
}

// GetArtist resolves artist metadata by Deezer artist ID.
func (p *Provider) GetArtist(ctx context.Context, id string) (*music.Artist, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}

	var artist apiArtist
	if err := p.client.get(ctx, fmt.Sprintf("/artist/%s", id), &artist); err != nil {
		if apperr.CodeOf(err) == apperr.CodeProviderNotFound {
			return nil, apperr.Newf(apperr.CodeArtistNotFound, "Deezer artist %q not found", id)
		}
		return nil, err
	}

	res := toArtist(&artist)
	return &res, nil
}

// GetDiscography retrieves every release for the given artist ID, paginating
// until complete.
func (p *Provider) GetDiscography(ctx context.Context, artistID string) ([]music.Release, error) {
	if err := validateID(artistID); err != nil {
		return nil, err
	}

	nextURL := fmt.Sprintf("/artist/%s/albums?limit=50", artistID)
	seen := make(map[string]bool)
	var releases []music.Release

	for nextURL != "" {
		if err := ctx.Err(); err != nil {
			return nil, apperr.Wrap(apperr.CodeJobCancelled, "Discography resolution cancelled", err)
		}

		var page apiAlbumList
		if err := p.client.get(ctx, nextURL, &page); err != nil {
			if apperr.CodeOf(err) == apperr.CodeProviderNotFound {
				return nil, apperr.Newf(apperr.CodeArtistNotFound, "Deezer artist %q not found", artistID)
			}
			return nil, err
		}

		for i := range page.Data {
			rel := toRelease(&page.Data[i])
			if !seen[rel.ID] {
				seen[rel.ID] = true
				releases = append(releases, rel)
			}
		}

		if page.Next == "" || len(page.Data) == 0 {
			break
		}
		nextURL = page.Next
	}

	return releases, nil
}

// GetRelease resolves release metadata by Deezer album ID.
func (p *Provider) GetRelease(ctx context.Context, releaseID string) (*music.Release, error) {
	if err := validateID(releaseID); err != nil {
		return nil, err
	}

	var album apiAlbum
	if err := p.client.get(ctx, fmt.Sprintf("/album/%s", releaseID), &album); err != nil {
		if apperr.CodeOf(err) == apperr.CodeProviderNotFound {
			return nil, apperr.Newf(apperr.CodeReleaseNotFound, "Deezer release %q not found", releaseID)
		}
		return nil, err
	}

	rel := toRelease(&album)
	return &rel, nil
}

// GetReleaseTracks retrieves the tracklist of a Deezer album, paginating
// through all tracks and applying per-disc totals.
func (p *Provider) GetReleaseTracks(ctx context.Context, releaseID string) ([]music.Track, error) {
	if err := validateID(releaseID); err != nil {
		return nil, err
	}

	release, err := p.GetRelease(ctx, releaseID)
	if err != nil {
		return nil, err
	}

	nextURL := fmt.Sprintf("/album/%s/tracks?limit=50", releaseID)
	var tracks []music.Track

	for nextURL != "" {
		if err := ctx.Err(); err != nil {
			return nil, apperr.Wrap(apperr.CodeJobCancelled, "Release tracks resolution cancelled", err)
		}

		var page apiTrackList
		if err := p.client.get(ctx, nextURL, &page); err != nil {
			if apperr.CodeOf(err) == apperr.CodeProviderNotFound {
				return nil, apperr.Newf(apperr.CodeReleaseNotFound, "Deezer release %q not found", releaseID)
			}
			return nil, err
		}

		for i := range page.Data {
			track := toTrack(&page.Data[i], release)
			tracks = append(tracks, track)
		}

		if page.Next == "" || len(page.Data) == 0 {
			break
		}
		nextURL = page.Next
	}

	applyDiscTotals(tracks)
	return tracks, nil
}

// GetTrack resolves a single track directly by its Deezer ID.
func (p *Provider) GetTrack(ctx context.Context, id string) (*music.Track, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}

	var track apiTrack
	if err := p.client.get(ctx, fmt.Sprintf("/track/%s", id), &track); err != nil {
		if apperr.CodeOf(err) == apperr.CodeProviderNotFound {
			return nil, apperr.Newf(apperr.CodeTrackNotFound, "Deezer track %q not found", id)
		}
		return nil, err
	}

	tr := toTrack(&track, nil)
	return &tr, nil
}

func validateID(id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" || !idRegexp.MatchString(trimmed) {
		return apperr.Newf(apperr.CodeInvalidRequest, "Invalid Deezer ID %q: must be a positive integer", id)
	}
	if _, err := strconv.ParseInt(trimmed, 10, 64); err != nil {
		return apperr.Newf(apperr.CodeInvalidRequest, "Invalid Deezer ID %q: out of range", id)
	}
	return nil
}

// Compile-time interface checks.
var (
	_ provider.MetadataProvider = (*Provider)(nil)
	_ provider.Availability     = (*Provider)(nil)
	_ provider.TrackResolver    = (*Provider)(nil)
)
