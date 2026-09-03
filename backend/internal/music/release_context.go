package music

import "strings"

// ApplyReleaseContext normalises a resolved release and its track list into
// the shape the library layout and the tagger expect.
//
// It is the single place where the album artist of a release is decided, so a
// manual download, a discography run and a subscription sync all file the same
// release identically. It is deliberately total and side effect free apart
// from the values it writes onto the two arguments.
//
// contextArtist is the artist the release was reached through — an artist page
// or a subscription — and is used only where the provider itself supplied
// nothing.
func ApplyReleaseContext(release *Release, tracks []Track, contextArtist string) {
	if release == nil {
		return
	}

	release.Artists = NormalizeCredits(release.Artists)

	albumArtist := ResolveAlbumArtist(release.AlbumArtist, release.Artists)
	if albumArtist == UnknownArtist && strings.TrimSpace(contextArtist) != "" {
		albumArtist = strings.TrimSpace(contextArtist)
	}
	if isVariousArtistsRelease(*release, tracks) {
		albumArtist = VariousArtists
		release.Compilation = true
	}
	release.AlbumArtist = albumArtist

	discTotal, perDisc := discLayout(tracks)

	for i := range tracks {
		tracks[i].ReleaseID = release.SourceID
		tracks[i].ReleaseType = release.ReleaseType
		tracks[i].Compilation = release.Compilation
		if strings.TrimSpace(tracks[i].Album) == "" {
			tracks[i].Album = release.DisplayTitle()
		}
		if tracks[i].Year == 0 {
			tracks[i].Year = release.Year
		}
		if tracks[i].CoverURL == "" {
			tracks[i].CoverURL = release.CoverURL
		}

		tracks[i].Artists = NormalizeCredits(tracks[i].Artists)
		if len(tracks[i].Artists) == 0 {
			tracks[i].Artists = append([]string(nil), release.Artists...)
		}
		// ARTIST stays the performers of this recording; ALBUMARTIST is the
		// release artist for every track of the release, so that all three
		// media servers keep the album together under one artist.
		tracks[i].AlbumArtist = release.AlbumArtist

		if tracks[i].DiscNumber <= 0 {
			tracks[i].DiscNumber = 1
		}
		tracks[i].DiscTotal = discTotal

		// TRACKTOTAL is derived from what the provider actually delivered: the
		// count it reported for the release, or the number of tracks on this
		// track's disc in the list we resolved. Nothing is invented — a track
		// list we could not read leaves the total at zero.
		if tracks[i].TrackTotal <= 0 {
			tracks[i].TrackTotal = trackTotalFor(*release, discTotal, perDisc, tracks[i].DiscNumber)
		}
	}
}

// discLayout reports how many discs the track list spans and how many tracks
// each disc holds.
func discLayout(tracks []Track) (int, map[int]int) {
	perDisc := make(map[int]int, 2)
	discTotal := 0
	for _, track := range tracks {
		disc := track.DiscNumber
		if disc <= 0 {
			disc = 1
		}
		if disc > discTotal {
			discTotal = disc
		}
		perDisc[disc]++
	}
	if discTotal <= 0 {
		discTotal = 1
	}
	return discTotal, perDisc
}

// trackTotalFor returns the number of tracks on one disc of a release.
func trackTotalFor(release Release, discTotal int, perDisc map[int]int, disc int) int {
	// A single disc release is fully described by the count the provider
	// reported, which is authoritative even when the resolved list was capped.
	if discTotal == 1 && release.TrackCount > 0 {
		return release.TrackCount
	}
	if count := perDisc[disc]; count > 0 {
		return count
	}
	return release.TrackCount
}

// isVariousArtistsRelease reports whether a release is a genuine compilation.
//
// Two signals are accepted, and "several credited artists" is not one of them:
// a collaboration album stays a collaboration album.
//
//  1. The provider filed the release under its own "various artists"
//     placeholder. This is the reliable signal, because release *types* are
//     not: Deezer reports Various-Artists soundtracks as record_type "album",
//     and YouTube Music derives the type from a subtitle that usually reads
//     "Album" as well.
//  2. The provider explicitly typed the release as a compilation *and* its
//     tracks really are by different artists — which excludes a "Best of X"
//     that is typed as a compilation but performed throughout by X.
func isVariousArtistsRelease(release Release, tracks []Track) bool {
	if IsVariousArtistsName(release.AlbumArtist) {
		return true
	}
	for _, artist := range release.Artists {
		if IsVariousArtistsName(artist) {
			return true
		}
	}
	if release.ReleaseType != ReleaseCompilation {
		return false
	}
	return distinctPrimaryArtists(tracks) >= 2
}

// distinctPrimaryArtists counts how many different artists lead the tracks of
// a release.
func distinctPrimaryArtists(tracks []Track) int {
	distinct := make(map[string]struct{}, len(tracks))
	for _, track := range tracks {
		credits := NormalizeCredits(track.Artists)
		if len(credits) == 0 {
			continue
		}
		distinct[strings.ToLower(credits[0])] = struct{}{}
	}
	return len(distinct)
}
