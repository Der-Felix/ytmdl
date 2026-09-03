package spotify

import (
	"regexp"
	"strconv"
	"strings"

	"ytdm/backend/internal/music"
)

// The wire types below mirror the parts of the Spotify Web API the backend
// reads. They never leave this package: everything is mapped onto the internal
// domain model before it is returned.

type apiImage struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type apiArtist struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Images       []apiImage `json:"images"`
	Genres       []string   `json:"genres"`
	Popularity   int        `json:"popularity"`
	ExternalURLs struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
}

type apiAlbum struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	AlbumType    string      `json:"album_type"`
	AlbumGroup   string      `json:"album_group"`
	ReleaseDate  string      `json:"release_date"`
	TotalTracks  int         `json:"total_tracks"`
	Images       []apiImage  `json:"images"`
	Artists      []apiArtist `json:"artists"`
	ExternalURLs struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
}

type apiTrack struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	TrackNumber int         `json:"track_number"`
	DiscNumber  int         `json:"disc_number"`
	DurationMS  int         `json:"duration_ms"`
	Artists     []apiArtist `json:"artists"`
	IsLocal     bool        `json:"is_local"`
	ExternalIDs struct {
		ISRC string `json:"isrc"`
	} `json:"external_ids"`
	ExternalURLs struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
	Album *apiAlbum `json:"album"`
}

// ProviderName is the stable identifier of this provider.
const ProviderName = "spotify"

// largestImage returns the URL of the biggest image in the list. Spotify sorts
// its images from large to small, but the order is not part of the contract.
func largestImage(images []apiImage) string {
	var (
		best  string
		width int
	)
	for _, image := range images {
		if image.URL == "" {
			continue
		}
		if best == "" || image.Width > width {
			best, width = image.URL, image.Width
		}
	}
	return best
}

func artistNames(artists []apiArtist) []string {
	out := make([]string, 0, len(artists))
	for _, a := range artists {
		if name := strings.TrimSpace(a.Name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// yearFromReleaseDate reads the year out of Spotify's release date, which is
// either "YYYY", "YYYY-MM" or "YYYY-MM-DD".
func yearFromReleaseDate(date string) int {
	trimmed := strings.TrimSpace(date)
	if len(trimmed) < 4 {
		return 0
	}
	year, err := strconv.Atoi(trimmed[:4])
	if err != nil || year < 1000 || year > 3000 {
		return 0
	}
	return year
}

var (
	epTitleRe    = regexp.MustCompile(`(?i)(\bep\b|\(\s*ep\s*\)|\[\s*ep\s*\]| - ep$)`)
	liveTitleRe  = regexp.MustCompile(`(?i)(\blive\b|\bunplugged\b|\bin concert\b|\bconcert\b)`)
	remixTitleRe = regexp.MustCompile(`(?i)(\bremix(es|ed)?\b|\bthe remixes\b)`)
)

// classifyRelease maps a Spotify album onto an internal release type.
//
// Spotify only knows "album", "single" and "compilation". Titles carry the
// remaining information, so explicit markers in the title win. Releases that
// Spotify calls a single but that carry four or more tracks are treated as EPs,
// which is how the industry uses the two terms.
func classifyRelease(albumType string, totalTracks int, title string) music.ReleaseType {
	switch {
	case liveTitleRe.MatchString(title):
		return music.ReleaseLive
	case remixTitleRe.MatchString(title):
		return music.ReleaseRemix
	case epTitleRe.MatchString(title):
		return music.ReleaseEP
	}

	switch strings.ToLower(strings.TrimSpace(albumType)) {
	case "compilation":
		return music.ReleaseCompilation
	case "single":
		if totalTracks >= 4 {
			return music.ReleaseEP
		}
		return music.ReleaseSingle
	default:
		return music.ReleaseAlbum
	}
}

// toArtist maps a Spotify artist onto the domain model.
func toArtist(a apiArtist) music.Artist {
	return music.Artist{
		ID:         a.ID,
		Name:       a.Name,
		Provider:   ProviderName,
		SourceID:   a.ID,
		SourceURL:  a.ExternalURLs.Spotify,
		ImageURL:   largestImage(a.Images),
		Genres:     a.Genres,
		Popularity: a.Popularity,
	}
}

// toRelease maps a Spotify album onto the domain model.
func toRelease(a apiAlbum) music.Release {
	names := artistNames(a.Artists)
	albumType := a.AlbumType
	if albumType == "" {
		albumType = a.AlbumGroup
	}
	return music.Release{
		ID:          a.ID,
		Title:       a.Name,
		Artists:     names,
		AlbumArtist: music.ResolveAlbumArtist(firstName(names), names),
		ReleaseType: classifyRelease(albumType, a.TotalTracks, a.Name),
		Year:        yearFromReleaseDate(a.ReleaseDate),
		ReleaseDate: a.ReleaseDate,
		TrackCount:  a.TotalTracks,
		CoverURL:    largestImage(a.Images),
		Provider:    ProviderName,
		SourceID:    a.ID,
		SourceURL:   a.ExternalURLs.Spotify,
	}
}

// toTrack maps a Spotify track onto the domain model. release supplies the
// album context, which the album track listing omits.
func toTrack(t apiTrack, release music.Release) music.Track {
	names := artistNames(t.Artists)
	track := music.Track{
		ID:             t.ID,
		Title:          t.Name,
		Artists:        names,
		Album:          release.Title,
		AlbumArtist:    release.DisplayAlbumArtist(),
		TrackNumber:    t.TrackNumber,
		TrackTotal:     release.TrackCount,
		DiscNumber:     t.DiscNumber,
		DurationMS:     t.DurationMS,
		Year:           release.Year,
		ISRC:           strings.TrimSpace(t.ExternalIDs.ISRC),
		CoverURL:       release.CoverURL,
		SourceProvider: ProviderName,
		SourceID:       t.ID,
		SourceURL:      t.ExternalURLs.Spotify,
		ReleaseID:      release.ID,
		ReleaseType:    release.ReleaseType,
	}
	if track.DiscNumber <= 0 {
		track.DiscNumber = 1
	}
	return track
}

// applyDiscTotals fills in the total disc count, which Spotify does not report
// directly.
func applyDiscTotals(tracks []music.Track) {
	discTotal := 0
	for _, t := range tracks {
		if t.DiscNumber > discTotal {
			discTotal = t.DiscNumber
		}
	}
	if discTotal <= 0 {
		discTotal = 1
	}

	// The track total is the number of tracks on the track's own disc. It used
	// to be filled only for multi disc releases, which left TRACKTOTAL — and
	// with it DISCTOTAL — missing from every single disc album, although all
	// three media servers display both.
	perDisc := make(map[int]int, discTotal)
	counts := make(map[int]int, discTotal)
	for _, t := range tracks {
		if t.TrackNumber > perDisc[t.DiscNumber] {
			perDisc[t.DiscNumber] = t.TrackNumber
		}
		counts[t.DiscNumber]++
	}
	for i := range tracks {
		tracks[i].DiscTotal = discTotal
		total := perDisc[tracks[i].DiscNumber]
		if count := counts[tracks[i].DiscNumber]; count > total {
			total = count
		}
		if total > 0 {
			tracks[i].TrackTotal = total
		}
	}
}

// firstName returns the first entry of an artist list, or "".
func firstName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return names[0]
}
