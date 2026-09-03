package provider

import (
	"context"

	"ytdm/backend/internal/music"
)

// LyricsProvider looks up the lyrics of a track.
//
// It is an optional capability in the same way TrackResolver is: a provider
// that has no lyrics source simply does not implement it, and the
// MetadataProvider contract is untouched.
type LyricsProvider interface {
	// Name returns the stable provider identifier, e.g. "lrclib".
	Name() string

	// Lyrics returns the lyrics of a track.
	//
	// A provider that has no entry for the track returns (nil, nil): a miss is
	// a normal outcome, not an error, because a track without lyrics must
	// never fail a download. An error means the provider could not answer at
	// all, which is a different thing and is treated as transient.
	//
	// mediaID is the identifier of the audio source the track was downloaded
	// from. A provider that keys on the media platform's own id — YouTube
	// Music does — needs it; the others ignore it.
	Lyrics(ctx context.Context, track music.Track, mediaID string) (*music.Lyrics, error)
}
