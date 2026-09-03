package ytmusic

import (
	"regexp"
	"strings"

	"ytdm/backend/internal/music"
)

// The extractors below read the renderers YouTube Music uses. They search the
// response tree for the renderer they need instead of following a fixed path,
// so that a rearranged page layout does not break the provider.

// artistURL builds the public page URL of an artist channel.
func artistURL(browseID string) string {
	if browseID == "" {
		return ""
	}
	return "https://music.youtube.com/channel/" + browseID
}

// releaseURL builds the public page URL of a release.
func releaseURL(browseID string) string {
	if browseID == "" {
		return ""
	}
	return "https://music.youtube.com/browse/" + browseID
}

// trackURL builds the watch URL of a track.
func trackURL(videoID string) string {
	if videoID == "" {
		return ""
	}
	return "https://music.youtube.com/watch?v=" + videoID
}

// extractArtists reads the artist entries of a search response.
func extractArtists(response node, limit int) []music.Artist {
	out := make([]music.Artist, 0, limit)
	seen := make(map[string]struct{}, limit)

	for _, item := range response.findAll("musicResponsiveListItemRenderer") {
		browseID := item.browseID()
		if !strings.HasPrefix(browseID, "UC") {
			continue
		}
		if _, duplicate := seen[browseID]; duplicate {
			continue
		}
		name := firstColumnText(item)
		if name == "" {
			continue
		}
		seen[browseID] = struct{}{}
		out = append(out, music.Artist{
			ID:        browseID,
			Name:      name,
			Provider:  ProviderName,
			SourceID:  browseID,
			SourceURL: artistURL(browseID),
			ImageURL:  item.thumbnailURL(),
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

// extractArtistHeader reads the artist description from a browse response.
func extractArtistHeader(response node, browseID string) *music.Artist {
	for _, key := range []string{
		"musicImmersiveHeaderRenderer",
		"musicVisualHeaderRenderer",
		"musicDetailHeaderRenderer",
		"musicResponsiveHeaderRenderer",
		"musicHeaderRenderer",
	} {
		header := response.findFirst(key)
		if !header.exists() {
			continue
		}
		name := header.get("title").text()
		if name == "" {
			continue
		}
		return &music.Artist{
			ID:        browseID,
			Name:      name,
			Provider:  ProviderName,
			SourceID:  browseID,
			SourceURL: artistURL(browseID),
			ImageURL:  header.thumbnailURL(),
		}
	}
	return nil
}

// shelfTarget is a "show all" navigation of a carousel shelf.
type shelfTarget struct {
	browseID string
	params   string
}

// extractShelfContinuations collects the navigation targets that lead to the
// complete list behind a preview shelf.
func extractShelfContinuations(response node) []shelfTarget {
	var out []shelfTarget
	seen := make(map[shelfTarget]struct{})

	for _, header := range response.findAll("musicCarouselShelfBasicHeaderRenderer") {
		endpoint := header.findFirst("browseEndpoint")
		if !endpoint.exists() {
			continue
		}
		target := shelfTarget{
			browseID: endpoint.get("browseId").str(),
			params:   endpoint.get("params").str(),
		}
		if target.browseID == "" || target.params == "" {
			continue
		}
		if _, duplicate := seen[target]; duplicate {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}
	return out
}

// extractReleases reads the release entries of a browse response.
func extractReleases(response node, albumArtist string) []music.Release {
	var out []music.Release
	seen := make(map[string]struct{})

	appendRelease := func(browseID, title string, subtitle node, thumbnail string) {
		if browseID == "" || title == "" {
			return
		}
		if _, duplicate := seen[browseID]; duplicate {
			return
		}
		seen[browseID] = struct{}{}

		artists := releaseArtists(subtitle, albumArtist)
		out = append(out, music.Release{
			ID:          browseID,
			Title:       title,
			Artists:     artists,
			AlbumArtist: music.ResolveAlbumArtist(firstOf(artists), artists),
			ReleaseType: classifyRelease(subtitle.text(), title),
			Year:        parseYear(subtitle.text()),
			CoverURL:    thumbnail,
			Provider:    ProviderName,
			SourceID:    browseID,
			SourceURL:   releaseURL(browseID),
		})
	}

	// Carousel and grid entries.
	for _, item := range response.findAll("musicTwoRowItemRenderer") {
		browseID := item.browseID()
		if !isReleaseBrowseID(browseID) {
			continue
		}
		appendRelease(browseID, item.get("title").text(), item.get("subtitle"), item.thumbnailURL())
	}

	// List entries, used by the "show all" pages.
	for _, item := range response.findAll("musicResponsiveListItemRenderer") {
		browseID := item.browseID()
		if !isReleaseBrowseID(browseID) {
			continue
		}
		appendRelease(browseID, firstColumnText(item), columnTextNode(item, 1), item.thumbnailURL())
	}
	return out
}

// isReleaseBrowseID reports whether a browse id addresses a release.
func isReleaseBrowseID(browseID string) bool {
	return strings.HasPrefix(browseID, "MPRE") || strings.HasPrefix(browseID, "OLAK")
}

// extractReleaseHeader reads the release description from a browse response.
func extractReleaseHeader(response node, browseID, contextArtist string) *music.Release {
	for _, key := range []string{
		"musicDetailHeaderRenderer",
		"musicResponsiveHeaderRenderer",
		"musicImmersiveHeaderRenderer",
		"musicHeaderRenderer",
	} {
		header := response.findFirst(key)
		if !header.exists() {
			continue
		}
		title := header.get("title").text()
		if title == "" {
			continue
		}

		subtitle := header.get("subtitle").text()

		// The strapline is where a release page names its artists, and it
		// names them as individual runs with channel ids. The subtitle of an
		// album page usually holds only the type and the year.
		artists := runArtists(header.get("straplineTextOne"))
		if len(artists) == 0 {
			artists = runArtists(header.get("subtitle"))
		}
		if len(artists) == 0 {
			artists = subtitleArtists(subtitle, contextArtist)
		}
		if len(artists) == 0 {
			artists = subtitleArtists(header.get("straplineTextOne").text(), contextArtist)
		}

		release := &music.Release{
			ID:          browseID,
			Title:       title,
			Artists:     artists,
			AlbumArtist: music.ResolveAlbumArtist(firstOf(artists), artists),
			ReleaseType: classifyRelease(subtitle, title),
			Year:        parseYear(subtitle),
			TrackCount:  trackCountFrom(header),
			CoverURL:    extractReleaseCoverURL(header),
			Provider:    ProviderName,
			SourceID:    browseID,
			SourceURL:   releaseURL(browseID),
		}
		return release
	}
	return nil
}

// extractReleaseCoverURL extracts the release cover image URL specifically from the
// release's thumbnail node, ensuring that artist avatars (such as straplineThumbnail)
// are never selected as release artwork.
func extractReleaseCoverURL(header node) string {
	if thumb := header.get("thumbnail"); thumb.exists() {
		if u := thumb.thumbnailURL(); u != "" {
			return u
		}
	}
	if thumb := header.get("thumbnailRenderer"); thumb.exists() {
		if u := thumb.thumbnailURL(); u != "" {
			return u
		}
	}
	return ""
}

// trackCountFrom reads the track count out of the second description line,
// which reads like "12 songs • 45 minutes".
func trackCountFrom(header node) int {
	for _, key := range []string{"secondSubtitle", "subtitle"} {
		text := header.get(key).text()
		for _, part := range strings.Split(text, "•") {
			part = strings.TrimSpace(part)
			fields := strings.Fields(part)
			if len(fields) < 2 {
				continue
			}
			unit := strings.ToLower(fields[1])
			if !strings.HasPrefix(unit, "song") && !strings.HasPrefix(unit, "track") &&
				!strings.HasPrefix(unit, "titel") && !strings.HasPrefix(unit, "lied") {
				continue
			}
			if count := wrap(fields[0]).int(); count > 0 {
				return count
			}
		}
	}
	return 0
}

// extractTracks reads the track list of a release browse response.
func extractTracks(response node, release music.Release, limit int) []music.Track {
	out := make([]music.Track, 0, 16)
	seen := make(map[string]struct{}, 16)

	for _, item := range response.findAll("musicResponsiveListItemRenderer") {
		videoID := item.videoID()
		if videoID == "" {
			continue
		}
		if _, duplicate := seen[videoID]; duplicate {
			continue
		}
		title := firstColumnText(item)
		if title == "" {
			continue
		}
		seen[videoID] = struct{}{}

		artists := columnArtists(item, 1)
		if len(artists) == 0 {
			artists = release.Artists
		}

		track := music.Track{
			ID:             videoID,
			Title:          title,
			Artists:        artists,
			Album:          release.Title,
			AlbumArtist:    release.DisplayAlbumArtist(),
			TrackNumber:    trackIndex(item, len(out)+1),
			TrackTotal:     release.TrackCount,
			DiscNumber:     1,
			DiscTotal:      1,
			DurationMS:     trackDuration(item),
			Year:           release.Year,
			CoverURL:       release.CoverURL,
			SourceProvider: ProviderName,
			SourceID:       videoID,
			SourceURL:      trackURL(videoID),
			ReleaseID:      release.ID,
			ReleaseType:    release.ReleaseType,
		}
		out = append(out, track)
		if len(out) >= limit {
			break
		}
	}

	if release.TrackCount == 0 {
		for i := range out {
			out[i].TrackTotal = len(out)
		}
	}
	return out
}

// trackIndex reads the position of a track, falling back to its position in
// the list.
func trackIndex(item node, fallback int) int {
	if index := item.get("index").text(); index != "" {
		if value := wrap(index).int(); value > 0 {
			return value
		}
	}
	return fallback
}

// trackDuration reads the runtime of a track from its fixed column or from the
// accessibility label of the play button.
func trackDuration(item node) int {
	for _, column := range item.get("fixedColumns").array() {
		text := column.findFirst("text").text()
		if ms := parseDuration(text); ms > 0 {
			return ms
		}
	}
	for _, column := range item.get("flexColumns").array() {
		for _, run := range column.findFirst("text").runs() {
			if ms := parseDuration(run.get("text").str()); ms > 0 {
				return ms
			}
		}
	}
	return 0
}

// firstColumnText returns the text of the first flexible column, which holds
// the primary label of a list item.
func firstColumnText(item node) string { return columnText(item, 0) }

// columnText returns the text of the n-th flexible column.
func columnText(item node, n int) string {
	return columnTextNode(item, n).text()
}

// columnTextNode returns the text object of the n-th flexible column, so that
// callers can read its structured runs rather than only its rendering.
func columnTextNode(item node, n int) node {
	columns := item.get("flexColumns").array()
	if n < 0 || n >= len(columns) {
		return node{}
	}
	return columns[n].findFirst("text")
}

// columnArtists reads the artist names out of a column, using the navigation
// targets to tell artists apart from the other facts in the same line.
func columnArtists(item node, n int) []string {
	columns := item.get("flexColumns").array()
	if n < 0 || n >= len(columns) {
		return nil
	}
	text := columns[n].findFirst("text")

	if out := runArtists(text); len(out) > 0 {
		return out
	}
	return subtitleArtists(text.text(), "")
}

// runArtists reads the artists out of a YouTube Music text object by their
// channel ids.
//
// This is the structured form and it is always preferred: YouTube Music
// renders "Capital Bra & Samra" as two runs carrying two channel ids with a
// separator run between them. Flattening that to a string and taking it apart
// again would throw away the only information that distinguishes two artists
// from one artist whose name contains an ampersand — which is how a library
// ends up with a directory called "Simon & Garfunkel" *and* one called
// "Simon".
func runArtists(text node) []string {
	var out []string
	seen := make(map[string]struct{}, 4)
	for _, run := range text.runs() {
		name := strings.TrimSpace(run.get("text").str())
		if name == "" || isSeparator(name) {
			continue
		}
		if browseID := run.browseID(); !strings.HasPrefix(browseID, "UC") {
			continue
		}
		key := strings.ToLower(name)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	return out
}

// subtitleArtists reads the artist names out of a rendered subtitle such as
// "Album • Artist • 2001". Parts that describe the release type or the year
// are skipped.
//
// It is the fallback for text that carries no channel ids at all, and it
// deliberately does not take a name apart any further: an entry here is one
// artist as far as this provider is concerned, and only structured data may
// contradict that.
func subtitleArtists(subtitle, fallback string) []string {
	var out []string
	for _, part := range strings.Split(subtitle, "•") {
		part = strings.TrimSpace(part)
		if part == "" || isReleaseTypeWord(part) || parseYear(part) > 0 {
			continue
		}
		out = append(out, part)
	}
	if len(out) == 0 && strings.TrimSpace(fallback) != "" {
		return []string{strings.TrimSpace(fallback)}
	}
	return out
}

// releaseArtists reads the artists of a release from a subtitle or strapline,
// preferring the structured runs and falling back to the rendered text.
func releaseArtists(text node, fallback string) []string {
	if out := runArtists(text); len(out) > 0 {
		return out
	}
	return subtitleArtists(text.text(), fallback)
}

func isSeparator(text string) bool {
	switch strings.TrimSpace(text) {
	case "•", "·", "&", ",", "and", "-":
		return true
	default:
		return false
	}
}

// releaseTypeWords maps the words YouTube Music uses for a release type onto
// the internal types. English and German are covered because those are the
// interface languages the backend requests.
var releaseTypeWords = map[string]music.ReleaseType{
	"album":        music.ReleaseAlbum,
	"single":       music.ReleaseSingle,
	"ep":           music.ReleaseEP,
	"compilation":  music.ReleaseCompilation,
	"compilations": music.ReleaseCompilation,
	"sampler":      music.ReleaseCompilation,
}

func isReleaseTypeWord(text string) bool {
	_, ok := releaseTypeWords[strings.ToLower(strings.TrimSpace(text))]
	return ok
}

// liveTitleRe matches the ways a live release names itself. A bare "live"
// anywhere in the title is deliberately not enough, so that an album called
// "Long Live" keeps its type.
var liveTitleRe = regexp.MustCompile(`(?i)(\(live\b|\blive (at|in|from|on|aus|im)\b|\blive album\b|\bunplugged\b| - live\b)`)

// classifyRelease determines the release type from the subtitle and the title.
func classifyRelease(subtitle, title string) music.ReleaseType {
	lowerTitle := strings.ToLower(title)
	switch {
	case liveTitleRe.MatchString(title):
		return music.ReleaseLive
	case strings.Contains(lowerTitle, "remix"):
		return music.ReleaseRemix
	}

	for _, part := range strings.Split(subtitle, "•") {
		if kind, ok := releaseTypeWords[strings.ToLower(strings.TrimSpace(part))]; ok {
			return kind
		}
	}
	if strings.HasSuffix(strings.TrimSpace(lowerTitle), " ep") || strings.Contains(lowerTitle, "(ep)") {
		return music.ReleaseEP
	}
	return music.ReleaseAlbum
}

// firstOf returns the first element of a credit list, or "".
func firstOf(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return names[0]
}
