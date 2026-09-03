// Package provider defines the boundary between the backend and external
// services. Implementations translate their own payloads into the internal
// music domain model; provider specific structures never leave this package
// tree.
package provider

import (
	"context"

	"ytdm/backend/internal/music"
)

// MetadataProvider supplies catalogue information: artists, their discography
// and the tracks of a release.
type MetadataProvider interface {
	// Name returns the stable provider identifier, e.g. "spotify".
	Name() string

	// SearchArtists looks up artists by free text.
	SearchArtists(ctx context.Context, query string) ([]music.Artist, error)

	// GetArtist resolves a provider specific artist id.
	GetArtist(ctx context.Context, id string) (*music.Artist, error)

	// GetDiscography returns every release of an artist. Filtering by release
	// type is the caller's responsibility.
	GetDiscography(ctx context.Context, artistID string) ([]music.Release, error)

	// GetRelease resolves a provider specific release id.
	GetRelease(ctx context.Context, releaseID string) (*music.Release, error)

	// GetReleaseTracks returns the track list of a release.
	GetReleaseTracks(ctx context.Context, releaseID string) ([]music.Track, error)
}

// Availability is implemented by providers that can report whether they are
// currently usable, for example because credentials are configured.
type Availability interface {
	// Available returns nil when the provider can serve requests.
	Available(ctx context.Context) error
}

// TrackResolver is an optional capability of a metadata provider: resolving a
// single track by its provider id. Providers that cannot do this simply do not
// implement it; callers fall back to resolving the release a track belongs to.
type TrackResolver interface {
	GetTrack(ctx context.Context, id string) (*music.Track, error)
}
