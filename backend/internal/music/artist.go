// Package music contains the internal domain model. Provider specific
// structures are normalised into these types immediately behind the provider
// boundary and never leak further into the backend.
package music

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// Artist is a normalised artist as delivered by a metadata provider.
type Artist struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	SourceID  string `json:"source_id"`
	SourceURL string `json:"source_url"`
	ImageURL  string `json:"image_url,omitempty"`

	// Genres and Popularity are optional enrichments; providers that do not
	// deliver them leave them empty.
	Genres     []string `json:"genres,omitempty"`
	Popularity int      `json:"popularity,omitempty"`
}

// DisplayName returns the trimmed artist name, falling back to "Unknown
// Artist" so that library paths never end up empty.
func (a Artist) DisplayName() string {
	name := strings.TrimSpace(a.Name)
	if name == "" {
		return UnknownArtist
	}
	return name
}

// UnknownArtist is the placeholder used when a provider delivers no usable
// artist name.
const UnknownArtist = "Unknown Artist"

// JoinArtists renders a list of artist names for tagging and display.
func JoinArtists(names []string) string {
	cleaned := make([]string, 0, len(names))
	for _, n := range names {
		if t := strings.TrimSpace(n); t != "" {
			cleaned = append(cleaned, t)
		}
	}
	if len(cleaned) == 0 {
		return UnknownArtist
	}
	return strings.Join(cleaned, "; ")
}

// PrimaryArtist returns the first non empty artist name.
func PrimaryArtist(names []string) string {
	for _, n := range names {
		if t := strings.TrimSpace(n); t != "" {
			return t
		}
	}
	return UnknownArtist
}

// NewID returns a random, URL safe identifier for a domain object. The
// backend uses opaque ids so that provider ids never become primary keys.
func NewID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand only fails when the system entropy source is broken; a
		// music downloader cannot sensibly continue in that state.
		panic("music: cannot read random bytes: " + err.Error())
	}
	return hex.EncodeToString(buf[:])
}
