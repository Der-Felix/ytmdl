package spotify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/httpx"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

// Limits for the paged endpoints. The hard limits keep a single request from
// walking an unbounded catalogue.
const (
	pageSize         = 50
	maxReleases      = 1000
	maxAlbumTracks   = 300
	maxSearchResults = 20
	trackBatchSize   = 50
)

// Config holds everything the provider needs.
type Config struct {
	ClientID     string
	ClientSecret string
	Market       string
	APIBaseURL   string
	AuthURL      string
	HTTPClient   *http.Client
}

// Provider is the Spotify metadata provider.
type Provider struct {
	client *client
}

// Compile time checks that the provider fulfils the interfaces.
var (
	_ provider.MetadataProvider = (*Provider)(nil)
	_ provider.Availability     = (*Provider)(nil)
)

// New builds the Spotify provider. Credentials are required; without them the
// provider cannot be used at all.
func New(cfg Config) (*Provider, error) {
	if strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.ClientSecret) == "" {
		return nil, apperr.New(apperr.CodeProviderUnavailable,
			"Spotify needs a client id and a client secret.")
	}

	apiBaseURL := strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/")
	if apiBaseURL == "" {
		apiBaseURL = "https://api.spotify.com/v1"
	}
	authURL := strings.TrimSpace(cfg.AuthURL)
	if authURL == "" {
		authURL = "https://accounts.spotify.com/api/token"
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = httpx.New(httpx.DefaultTimeout)
	}

	return &Provider{client: &client{
		httpClient: httpClient,
		apiBaseURL: apiBaseURL,
		authURL:    authURL,
		clientID:   cfg.ClientID,
		secret:     cfg.ClientSecret,
		market:     strings.ToUpper(strings.TrimSpace(cfg.Market)),
	}}, nil
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return ProviderName }

// Available checks that the configured credentials work.
func (p *Provider) Available(ctx context.Context) error {
	_, err := p.client.accessToken(ctx)
	return err
}

// marketQuery returns the market parameter when one is configured.
func (p *Provider) marketQuery() url.Values {
	query := url.Values{}
	if p.client.market != "" {
		query.Set("market", p.client.market)
	}
	return query
}

// SearchArtists looks up artists by name.
func (p *Provider) SearchArtists(ctx context.Context, query string) ([]music.Artist, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "The search query must not be empty.")
	}

	params := p.marketQuery()
	params.Set("q", trimmed)
	params.Set("type", "artist")
	params.Set("limit", "20")

	var payload struct {
		Artists struct {
			Items []apiArtist `json:"items"`
		} `json:"artists"`
	}
	if err := p.client.get(ctx, "/search", params, &payload); err != nil {
		return nil, err
	}

	out := make([]music.Artist, 0, len(payload.Artists.Items))
	for _, item := range payload.Artists.Items {
		if item.ID == "" {
			continue
		}
		out = append(out, toArtist(item))
		if len(out) >= maxSearchResults {
			break
		}
	}
	return out, nil
}

// GetArtist resolves a Spotify artist id.
func (p *Provider) GetArtist(ctx context.Context, id string) (*music.Artist, error) {
	path, err := pathFor("/artists/%s", id)
	if err != nil {
		return nil, err
	}

	var payload apiArtist
	if err := p.client.get(ctx, path, nil, &payload); err != nil {
		if apperr.CodeOf(err) == apperr.CodeProviderNotFound {
			return nil, apperr.Newf(apperr.CodeArtistNotFound, "Spotify does not know the artist %q.", id)
		}
		return nil, err
	}
	artist := toArtist(payload)
	return &artist, nil
}

// GetDiscography returns every release of an artist. Releases the artist only
// appears on are skipped: the discography is about their own catalogue.
func (p *Provider) GetDiscography(ctx context.Context, artistID string) ([]music.Release, error) {
	path, err := pathFor("/artists/%s/albums", artistID)
	if err != nil {
		return nil, err
	}

	params := p.marketQuery()
	params.Set("include_groups", "album,single,compilation")

	releases := make([]music.Release, 0, 64)
	seen := make(map[string]struct{}, 64)

	err = paginate(ctx, p.client, path, params, pageSize, maxReleases, func(raw json.RawMessage) (int, error) {
		var items []apiAlbum
		if err := json.Unmarshal(raw, &items); err != nil {
			return 0, apperr.Wrap(apperr.CodeProviderUnavailable, "The Spotify album list could not be decoded.", err)
		}
		for _, item := range items {
			if item.ID == "" {
				continue
			}
			if _, duplicate := seen[item.ID]; duplicate {
				continue
			}
			seen[item.ID] = struct{}{}
			releases = append(releases, toRelease(item))
		}
		return len(items), nil
	})
	if err != nil {
		if apperr.CodeOf(err) == apperr.CodeProviderNotFound {
			return nil, apperr.Newf(apperr.CodeArtistNotFound, "Spotify does not know the artist %q.", artistID)
		}
		return nil, err
	}
	return releases, nil
}

// GetRelease resolves a Spotify album id.
func (p *Provider) GetRelease(ctx context.Context, releaseID string) (*music.Release, error) {
	path, err := pathFor("/albums/%s", releaseID)
	if err != nil {
		return nil, err
	}

	var payload apiAlbum
	if err := p.client.get(ctx, path, p.marketQuery(), &payload); err != nil {
		if apperr.CodeOf(err) == apperr.CodeProviderNotFound {
			return nil, apperr.Newf(apperr.CodeReleaseNotFound, "Spotify does not know the release %q.", releaseID)
		}
		return nil, err
	}
	release := toRelease(payload)
	return &release, nil
}

// GetReleaseTracks returns the tracks of a release.
//
// The album track listing does not carry ISRCs, so the full track objects are
// fetched in batches afterwards. The ISRC is what makes deduplication and
// matching reliable, which is worth the extra request per fifty tracks.
func (p *Provider) GetReleaseTracks(ctx context.Context, releaseID string) ([]music.Track, error) {
	release, err := p.GetRelease(ctx, releaseID)
	if err != nil {
		return nil, err
	}

	path, err := pathFor("/albums/%s/tracks", releaseID)
	if err != nil {
		return nil, err
	}

	tracks := make([]music.Track, 0, release.TrackCount)
	err = paginate(ctx, p.client, path, p.marketQuery(), pageSize, maxAlbumTracks, func(raw json.RawMessage) (int, error) {
		var items []apiTrack
		if err := json.Unmarshal(raw, &items); err != nil {
			return 0, apperr.Wrap(apperr.CodeProviderUnavailable, "The Spotify track list could not be decoded.", err)
		}
		for _, item := range items {
			if item.ID == "" || item.IsLocal {
				continue
			}
			tracks = append(tracks, toTrack(item, *release))
		}
		return len(items), nil
	})
	if err != nil {
		return nil, err
	}

	if err := p.fillISRCs(ctx, tracks); err != nil {
		return nil, err
	}
	applyDiscTotals(tracks)
	return tracks, nil
}

// fillISRCs loads the ISRCs of the given tracks in batches and writes them
// back into the slice.
func (p *Provider) fillISRCs(ctx context.Context, tracks []music.Track) error {
	ids := make([]string, 0, len(tracks))
	index := make(map[string]int, len(tracks))
	for i, track := range tracks {
		if track.SourceID == "" || track.ISRC != "" {
			continue
		}
		ids = append(ids, track.SourceID)
		index[track.SourceID] = i
	}
	if len(ids) == 0 {
		return nil
	}

	for start := 0; start < len(ids); start += trackBatchSize {
		end := min(start+trackBatchSize, len(ids))

		params := p.marketQuery()
		params.Set("ids", strings.Join(ids[start:end], ","))

		var payload struct {
			Tracks []apiTrack `json:"tracks"`
		}
		if err := p.client.get(ctx, "/tracks", params, &payload); err != nil {
			return err
		}
		for _, full := range payload.Tracks {
			i, ok := index[full.ID]
			if !ok {
				continue
			}
			if isrc := strings.TrimSpace(full.ExternalIDs.ISRC); isrc != "" {
				tracks[i].ISRC = isrc
			}
		}
	}
	return nil
}

// GetTrack resolves a single Spotify track id. It implements the optional
// provider.TrackResolver capability.
func (p *Provider) GetTrack(ctx context.Context, id string) (*music.Track, error) {
	path, err := pathFor("/tracks/%s", id)
	if err != nil {
		return nil, err
	}

	var payload apiTrack
	if err := p.client.get(ctx, path, p.marketQuery(), &payload); err != nil {
		if apperr.CodeOf(err) == apperr.CodeProviderNotFound {
			return nil, apperr.Newf(apperr.CodeTrackNotFound, "Spotify does not know the track %q.", id)
		}
		return nil, err
	}
	if payload.ID == "" {
		return nil, apperr.Newf(apperr.CodeTrackNotFound, "Spotify does not know the track %q.", id)
	}

	release := music.Release{}
	if payload.Album != nil {
		release = toRelease(*payload.Album)
	}
	track := toTrack(payload, release)
	if track.DiscTotal <= 0 {
		track.DiscTotal = 1
	}
	return &track, nil
}

// Compile time check for the optional capability.
var _ provider.TrackResolver = (*Provider)(nil)
