package discography

import (
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/music"
)

// A release that its provider calls an album is a live release when its own
// track titles say so for nearly the whole track list. The providers have no
// release type for live recordings — Deezer's record_type knows album, single,
// ep and compile, Spotify's album_type album, single and compilation, YouTube
// Music's subtitle Album, Single and EP — so a live album is only recognised
// by a title. When the release title carries no marker ("Rock in Rio", a tour
// or venue name), the track titles usually do ("The Wicker Man - Live"), and
// the few tracks without one ("Intro", a medley, a track the label forgot to
// mark) are just as live as the rest.
//
// The thresholds are deliberately high. A deluxe edition whose bonus disc is a
// concert stays an album: its studio tracks must keep the studio identity.
const (
	// LiveReleaseMinMarkedTracks is the minimum number of tracks with a live
	// marker in their own title.
	LiveReleaseMinMarkedTracks = 3
	// LiveReleaseMarkedNumerator / LiveReleaseMarkedDenominator is the share of
	// the track list that has to carry the marker.
	LiveReleaseMarkedNumerator   = 3
	LiveReleaseMarkedDenominator = 4
)

// InferLiveRelease retypes an album as a live release when its track titles
// mark it as one (see LiveReleaseMinMarkedTracks). It reports whether it did.
// Only albums are considered: a compilation or a single keeps its type, since
// a live track on a best-of is no evidence about the others.
func InferLiveRelease(release *music.Release, tracks []music.Track) bool {
	if release == nil || release.ReleaseType != music.ReleaseAlbum || len(tracks) == 0 {
		return false
	}
	marked := 0
	for _, t := range tracks {
		if matcher.Analyze(t.Title).Versions.Has(matcher.VersionLive) {
			marked++
		}
	}
	if !liveShare(marked, len(tracks)) {
		return false
	}
	release.ReleaseType = music.ReleaseLive
	return true
}

func liveShare(marked, total int) bool {
	return marked >= LiveReleaseMinMarkedTracks &&
		LiveReleaseMarkedDenominator*marked >= LiveReleaseMarkedNumerator*total
}

// applyReleaseContext decides the release type from the track list and then
// applies the release context to the tracks, which carries the type over to
// every track and with it into the track identity.
func applyReleaseContext(release *music.Release, tracks []music.Track, contextArtist string) {
	InferLiveRelease(release, tracks)
	music.ApplyReleaseContext(release, tracks, contextArtist)
}
