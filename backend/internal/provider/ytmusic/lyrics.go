package ytmusic

import (
	"context"
	"strings"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

// lyricsPageType marks the tab of a watch context that carries the lyrics.
const lyricsPageType = "MUSIC_PAGE_TYPE_TRACK_LYRICS"

// LyricsProvider reads the lyrics YouTube Music shows next to a track.
//
// It is the fallback behind LRCLIB and delivers plain lyrics only. Timed
// lyrics are served exclusively to an authenticated mobile music client, which
// this backend does not impersonate, so the limitation is by construction
// rather than by accident.
//
// The two endpoints are the ones the web player itself uses and are not a
// documented API. Every failure here is therefore treated as a miss or as a
// transient error, never as an answer about the track.
type LyricsProvider struct {
	api *innerTube
}

var _ provider.LyricsProvider = (*LyricsProvider)(nil)

// NewLyricsProvider builds the provider from the same configuration the
// metadata provider uses.
func NewLyricsProvider(cfg Config) *LyricsProvider {
	return &LyricsProvider{api: NewMetadataProvider(cfg).api}
}

// Name returns the provider identifier.
func (p *LyricsProvider) Name() string { return ProviderName }

// Lyrics returns the plain lyrics of a track, or (nil, nil) when YouTube Music
// has none.
//
// mediaID is the YouTube video id the audio was actually downloaded from. It
// is used verbatim: searching for the title and artist again could easily land
// on a different recording than the one in the library, and lyrics that belong
// to another take are worse than no lyrics at all. A track that was not
// matched on YouTube therefore has no fallback here.
func (p *LyricsProvider) Lyrics(ctx context.Context, _ music.Track, mediaID string) (*music.Lyrics, error) {
	videoID := strings.TrimSpace(mediaID)
	if videoID == "" {
		return nil, nil
	}

	watch, err := p.api.next(ctx, videoID)
	if err != nil {
		if apperr.CodeOf(err) == apperr.CodeProviderNotFound {
			return nil, nil
		}
		return nil, err
	}

	browseID := lyricsBrowseID(watch)
	if browseID == "" {
		return nil, nil
	}

	page, err := p.api.browse(ctx, browseID, "")
	if err != nil {
		if apperr.CodeOf(err) == apperr.CodeProviderNotFound {
			return nil, nil
		}
		return nil, err
	}

	shelf := page.findFirst("musicDescriptionShelfRenderer")
	if !shelf.exists() {
		return nil, nil
	}
	body := strings.TrimSpace(shelf.get("description").text())
	if body == "" {
		return nil, nil
	}
	// The footer names the licensor. It is kept so that the sidecar says where
	// the text came from.
	if source := strings.TrimSpace(shelf.get("footer").text()); source != "" {
		body += "\n\n" + source
	}

	return &music.Lyrics{
		Provider:  ProviderName,
		SourceID:  videoID,
		PlainText: body,
	}, nil
}

// lyricsBrowseID finds the browse id of the lyrics tab in a watch response.
func lyricsBrowseID(watch node) string {
	for _, tab := range watch.findAll("tabRenderer") {
		endpoint := tab.get("endpoint", "browseEndpoint")
		if !endpoint.exists() {
			continue
		}
		pageType := endpoint.get("browseEndpointContextSupportedConfigs",
			"browseEndpointContextMusicConfig", "pageType").str()
		if pageType != lyricsPageType {
			continue
		}
		if id := endpoint.get("browseId").str(); strings.HasPrefix(id, "MPLY") {
			return id
		}
	}
	return ""
}
