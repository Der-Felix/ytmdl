package discography

import (
	"context"
	"log/slog"
	"strings"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

// Ownership says whether a release belongs to an artist or is one the artist
// only appears on.
type Ownership int

const (
	// OwnershipUnknown means the release names no artist to compare with.
	OwnershipUnknown Ownership = iota
	// OwnershipOwn means the artist is the release's (or one of its joint)
	// primary artists.
	OwnershipOwn
	// OwnershipAppearance means the release belongs to someone else.
	OwnershipAppearance
)

// ReleaseOwnership decides whether release belongs to the artist identified by
// artistID (the provider's id) and artistName.
//
// Provider ids win: when the release carries the ids of its own artists
// (music.Release.ArtistIDs) and an artist id is given, the answer is whether
// that id is among them. Otherwise the normalised names are compared — the
// release's credits and its album artist against artistName, with the
// normalisation the matcher uses for artist identity. A joined album artist
// ("A & B") is split for that comparison, so a joint release counts as the
// artist's own.
//
// A release that names no artist at all, or an artist without a name, is
// OwnershipUnknown. Callers keep such a release: missing data must never make
// a release disappear.
func ReleaseOwnership(release music.Release, artistID, artistName string) Ownership {
	if id := strings.TrimSpace(artistID); id != "" && len(release.ArtistIDs) > 0 {
		for _, own := range release.ArtistIDs {
			if strings.TrimSpace(own) == id {
				return OwnershipOwn
			}
		}
		return OwnershipAppearance
	}

	wanted := matcher.NormalizeArtist(artistName)
	if wanted == "" {
		return OwnershipUnknown
	}

	names := make([]string, 0, len(release.Artists)+2)
	names = append(names, release.Artists...)
	if album := strings.TrimSpace(release.AlbumArtist); album != "" && album != music.UnknownArtist {
		names = append(names, album)
		names = append(names, music.SplitCredit(album)...)
	}

	compared := false
	for _, name := range names {
		key := matcher.NormalizeArtist(name)
		if key == "" || name == music.UnknownArtist {
			continue
		}
		compared = true
		if key == wanted {
			return OwnershipOwn
		}
	}
	if !compared {
		return OwnershipUnknown
	}
	return OwnershipAppearance
}

// ownReleases drops the releases the artist only appears on. A release whose
// listing does not say who it belongs to is looked up on its own (Deezer's
// artist album listing names no artist); when that lookup fails as well, the
// release is kept.
func ownReleases(ctx context.Context, metadata provider.MetadataProvider, releases []music.Release,
	artistID, artistName string, logger *slog.Logger) (kept []music.Release, skipped int) {
	kept = make([]music.Release, 0, len(releases))
	for _, release := range releases {
		ownership := ReleaseOwnership(release, artistID, artistName)
		if ownership == OwnershipUnknown && ctx.Err() == nil {
			detail, err := metadata.GetRelease(ctx, release.SourceID)
			switch {
			case err != nil:
				logger.Warn("release owner could not be read; the release is kept",
					logging.KeyRelease, release.DisplayTitle(),
					logging.KeyErrorCode, string(apperr.CodeOf(err)),
					logging.KeyError, err.Error())
			case detail != nil:
				ownership = ReleaseOwnership(*detail, artistID, artistName)
			}
		}
		if ownership == OwnershipAppearance {
			skipped++
			logger.Info("release skipped: the artist only appears on it",
				logging.KeyRelease, release.DisplayTitle())
			continue
		}
		kept = append(kept, release)
	}
	return kept, skipped
}
