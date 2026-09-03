package deezer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"ytdm/backend/internal/music"
)

var (
	liveMarkers  = regexp.MustCompile(`(?i)(\blive\b|\bunplugged\b|\bin concert\b|\bconcert\b)`)
	remixMarkers = regexp.MustCompile(`(?i)(\bremix(es|ed)?\b|\bthe remixes\b)`)
	epMarkers    = regexp.MustCompile(`(?i)(\bep\b|\(\s*ep\s*\)|\[\s*ep\s*\]| - ep$)`)
)

type apiArtistRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Link string `json:"link"`
}

type apiContributor struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type apiArtist struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Link          string `json:"link"`
	PictureSmall  string `json:"picture_small"`
	PictureMedium string `json:"picture_medium"`
	PictureBig    string `json:"picture_big"`
	PictureXL     string `json:"picture_xl"`
	NbAlbum       int    `json:"nb_album"`
	NbFan         int    `json:"nb_fan"`
}

type apiArtistList struct {
	Data  []apiArtist `json:"data"`
	Total int         `json:"total"`
	Next  string      `json:"next"`
}

type apiAlbumRef struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	CoverSmall  string `json:"cover_small"`
	CoverMedium string `json:"cover_medium"`
	CoverBig    string `json:"cover_big"`
	CoverXL     string `json:"cover_xl"`
	ReleaseDate string `json:"release_date"`
}

type apiAlbum struct {
	ID           int64            `json:"id"`
	Title        string           `json:"title"`
	Link         string           `json:"link"`
	CoverSmall   string           `json:"cover_small"`
	CoverMedium  string           `json:"cover_medium"`
	CoverBig     string           `json:"cover_big"`
	CoverXL      string           `json:"cover_xl"`
	ReleaseDate  string           `json:"release_date"`
	RecordType   string           `json:"record_type"`
	NbTracks     int              `json:"nb_tracks"`
	Artist       *apiArtistRef    `json:"artist"`
	Contributors []apiContributor `json:"contributors"`
	Tracks       *apiTrackList    `json:"tracks"`
}

type apiAlbumList struct {
	Data  []apiAlbum `json:"data"`
	Total int        `json:"total"`
	Next  string     `json:"next"`
}

type apiTrack struct {
	ID            int64            `json:"id"`
	Title         string           `json:"title"`
	TitleShort    string           `json:"title_short"`
	TitleVersion  string           `json:"title_version"`
	ISRC          string           `json:"isrc"`
	Link          string           `json:"link"`
	Duration      int              `json:"duration"` // in seconds
	TrackPosition int              `json:"track_position"`
	DiskNumber    int              `json:"disk_number"`
	ReleaseDate   string           `json:"release_date"`
	Artist        *apiArtistRef    `json:"artist"`
	Contributors  []apiContributor `json:"contributors"`
	Album         *apiAlbumRef     `json:"album"`
}

type apiTrackList struct {
	Data  []apiTrack `json:"data"`
	Total int        `json:"total"`
	Next  string     `json:"next"`
}

func toArtist(a *apiArtist) music.Artist {
	idStr := strconv.FormatInt(a.ID, 10)
	link := a.Link
	if link == "" {
		link = fmt.Sprintf("https://www.deezer.com/artist/%s", idStr)
	}
	return music.Artist{
		ID:        idStr,
		Name:      strings.TrimSpace(a.Name),
		Provider:  providerName,
		SourceID:  idStr,
		SourceURL: link,
		ImageURL:  bestImage(a.PictureXL, a.PictureBig, a.PictureMedium, a.PictureSmall),
	}
}

func toRelease(a *apiAlbum) music.Release {
	idStr := strconv.FormatInt(a.ID, 10)
	link := a.Link
	if link == "" {
		link = fmt.Sprintf("https://www.deezer.com/album/%s", idStr)
	}

	// Deezer's structured data is the album's own artist plus the contributor
	// list. The album artist is normally a single clean name, but a
	// collaboration can arrive as a joined display string; the contributor
	// list is what decides whether that string names one artist or several.
	artists := extractArtists(a.Artist, a.Contributors)
	providerArtist := ""
	if a.Artist != nil {
		providerArtist = strings.TrimSpace(a.Artist.Name)
	}
	albumArtist := music.ResolveAlbumArtist(providerArtist, artists)

	year := parseYear(a.ReleaseDate)
	relType := classifyRelease(a.RecordType, a.Title, a.NbTracks)

	return music.Release{
		ID:          idStr,
		Title:       strings.TrimSpace(a.Title),
		Artists:     artists,
		AlbumArtist: albumArtist,
		ReleaseType: relType,
		Year:        year,
		TrackCount:  a.NbTracks,
		CoverURL:    bestImage(a.CoverXL, a.CoverBig, a.CoverMedium, a.CoverSmall),
		Provider:    providerName,
		SourceID:    idStr,
		SourceURL:   link,
	}
}

func toTrack(t *apiTrack, album *music.Release) music.Track {
	idStr := strconv.FormatInt(t.ID, 10)
	link := t.Link
	if link == "" {
		link = fmt.Sprintf("https://www.deezer.com/track/%s", idStr)
	}

	artists := extractArtists(t.Artist, t.Contributors)

	albumTitle := ""
	albumArtist := ""
	var year int
	coverURL := ""
	releaseID := ""
	var relType music.ReleaseType

	if album != nil {
		albumTitle = album.Title
		albumArtist = album.AlbumArtist
		year = album.Year
		coverURL = album.CoverURL
		releaseID = album.SourceID
		relType = album.ReleaseType
	} else if t.Album != nil {
		albumTitle = t.Album.Title
		releaseID = strconv.FormatInt(t.Album.ID, 10)
		coverURL = bestImage(t.Album.CoverXL, t.Album.CoverBig, t.Album.CoverMedium, t.Album.CoverSmall)
		year = parseYear(t.Album.ReleaseDate)
	}

	if year == 0 && t.ReleaseDate != "" {
		year = parseYear(t.ReleaseDate)
	}
	if albumArtist == "" && len(artists) > 0 {
		albumArtist = artists[0]
	}

	trackNum := t.TrackPosition
	if trackNum == 0 {
		trackNum = 1
	}
	discNum := t.DiskNumber
	if discNum == 0 {
		discNum = 1
	}

	return music.Track{
		ID:             idStr,
		Title:          strings.TrimSpace(t.Title),
		Artists:        artists,
		Album:          albumTitle,
		AlbumArtist:    albumArtist,
		TrackNumber:    trackNum,
		DiscNumber:     discNum,
		DurationMS:     t.Duration * 1000,
		Year:           year,
		ISRC:           cleanISRC(t.ISRC),
		CoverURL:       coverURL,
		SourceProvider: providerName,
		SourceID:       idStr,
		SourceURL:      link,
		ReleaseID:      releaseID,
		ReleaseType:    relType,
	}
}

// extractArtists returns the structured credit list of an album or a track.
//
// The contributor list is the structured form: every entry is an artist Deezer
// identified separately, with its own id. The album's own artist field is a
// display string that may join several of them — "LACAZETTE & Bushido" — so it
// is only used when there is no contributor list to read instead. Mixing the
// two would put the joined string into the credits and make it look like a
// third artist.
func extractArtists(mainArtist *apiArtistRef, contributors []apiContributor) []string {
	var names []string
	seen := make(map[string]bool)

	add := func(name string) {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" && !seen[strings.ToLower(trimmed)] {
			seen[strings.ToLower(trimmed)] = true
			names = append(names, trimmed)
		}
	}

	if len(contributors) > 0 {
		for _, c := range contributors {
			add(c.Name)
		}
		return names
	}
	if mainArtist != nil {
		add(mainArtist.Name)
	}
	return names
}

func classifyRelease(recordType, title string, trackCount int) music.ReleaseType {
	recType := strings.ToLower(strings.TrimSpace(recordType))
	switch recType {
	case "compile", "compilation":
		return music.ReleaseCompilation
	case "ep":
		return music.ReleaseEP
	case "single":
		if trackCount >= 4 {
			return music.ReleaseEP
		}
		return music.ReleaseSingle
	}

	// For general albums, inspect title heuristics for Live, Remix, EP markers
	switch {
	case liveMarkers.MatchString(title):
		return music.ReleaseLive
	case remixMarkers.MatchString(title):
		return music.ReleaseRemix
	case epMarkers.MatchString(title):
		return music.ReleaseEP
	default:
		return music.ReleaseAlbum
	}
}

func parseYear(dateStr string) int {
	parts := strings.Split(dateStr, "-")
	if len(parts) > 0 && len(parts[0]) == 4 {
		if y, err := strconv.Atoi(parts[0]); err == nil && y > 0 {
			return y
		}
	}
	return 0
}

func bestImage(xl, big, med, small string) string {
	for _, img := range []string{xl, big, med, small} {
		if img != "" && !strings.HasSuffix(img, "/image") && !strings.Contains(img, "images/artist//") {
			return img
		}
	}
	if xl != "" {
		return xl
	}
	return ""
}

func cleanISRC(isrc string) string {
	clean := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(isrc, "-", ""), " ", ""))
	return strings.TrimSpace(clean)
}

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
