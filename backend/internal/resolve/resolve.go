// Package resolve turns a pasted address into the provider identity the rest
// of the backend already works with.
//
// It exists so that a client can hand over whatever the user pasted and get
// back something addressable, instead of having to know each provider's id
// formats itself. Understanding an address is provider knowledge, and provider
// knowledge belongs behind the API.
package resolve

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"ytdm/backend/internal/apperr"
)

// Kind names what an address points at.
type Kind string

const (
	KindArtist  Kind = "artist"
	KindRelease Kind = "release"
	KindTrack   Kind = "track"
)

// Ref is a resolved address: what it is, whose it is, and under which id.
type Ref struct {
	Kind     Kind   `json:"kind"`
	Provider string `json:"provider"`
	ID       string `json:"id"`
	// ReleaseID is set for a track whose release is known from the address.
	// The metadata providers that cannot resolve a bare track id need it.
	ReleaseID string `json:"release_id,omitempty"`
}

// ChannelResolver resolves a YouTube channel address to its canonical id.
// yt-dlp implements it; the interface keeps this package testable without it.
type ChannelResolver interface {
	ChannelID(ctx context.Context, target string) (string, error)
}

// Service resolves addresses.
type Service struct {
	channels ChannelResolver
}

// NewService builds the resolver. channels may be nil, in which case handle
// addresses are reported as unresolvable rather than silently mishandled.
func NewService(channels ChannelResolver) *Service {
	return &Service{channels: channels}
}

const (
	providerYTMusic = "ytmusic"
	providerSpotify = "spotify"
	providerDeezer  = "deezer"
)

var (
	// YouTube ids are URL-safe base64-ish tokens.
	youtubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{2,128}$`)
	// Spotify ids are base62 and always 22 characters.
	spotifyID = regexp.MustCompile(`^[A-Za-z0-9]{22}$`)
	// Deezer ids are positive integers.
	deezerID = regexp.MustCompile(`^[0-9]{1,32}$`)
	// A channel handle: @ followed by the handle characters YouTube allows.
	youtubeHandle = regexp.MustCompile(`^@[A-Za-z0-9._-]{1,64}$`)
)

var youtubeHosts = map[string]struct{}{
	"music.youtube.com": {},
	"www.youtube.com":   {},
	"youtube.com":       {},
	"m.youtube.com":     {},
	"youtu.be":          {},
}

var spotifyHosts = map[string]struct{}{
	"open.spotify.com": {},
	"play.spotify.com": {},
}

var deezerHosts = map[string]struct{}{
	"deezer.com":     {},
	"www.deezer.com": {},
}

// Resolve reads an address. Input that is not an address at all yields a
// CodeInvalidRequest error, which is how a caller learns to treat it as a
// search query instead.
func (s *Service) Resolve(ctx context.Context, input string) (*Ref, error) {
	text := strings.TrimSpace(input)
	if text == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "No address was given.")
	}
	if len(text) > 2048 {
		return nil, apperr.New(apperr.CodeInvalidRequest, "The address is too long.")
	}

	if ref := bareID(text); ref != nil {
		return ref, nil
	}

	parsed, err := parseURL(text)
	if err != nil {
		return nil, err
	}

	host := strings.ToLower(parsed.Hostname())
	if _, ok := youtubeHosts[host]; ok {
		return s.youtube(ctx, parsed)
	}
	if _, ok := spotifyHosts[host]; ok {
		return spotify(parsed)
	}
	if _, ok := deezerHosts[host]; ok {
		return deezer(parsed)
	}
	return nil, apperr.Newf(apperr.CodeInvalidRequest,
		"%s is not supported; use a Deezer, Spotify, YouTube or YouTube Music address.", parsed.Hostname())
}

// parseURL accepts an address with or without a scheme. A host without a dot
// is not an address but a search term, and is rejected as such.
func parseURL(text string) (*url.URL, error) {
	candidate := text
	if !strings.Contains(text, "://") {
		candidate = "https://" + text
	}
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Hostname() == "" || !strings.Contains(parsed.Hostname(), ".") {
		return nil, apperr.New(apperr.CodeInvalidRequest, "That is not a valid address.")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Only http and https addresses are supported.")
	}
	return parsed, nil
}

// bareID accepts an id pasted on its own, which is what the provider APIs use.
func bareID(text string) *Ref {
	if match := regexp.MustCompile(`^spotify:(artist|album|track):([A-Za-z0-9]{22})$`).
		FindStringSubmatch(text); match != nil {
		switch match[1] {
		case "artist":
			return &Ref{Kind: KindArtist, Provider: providerSpotify, ID: match[2]}
		case "album":
			return &Ref{Kind: KindRelease, Provider: providerSpotify, ID: match[2]}
		default:
			return &Ref{Kind: KindTrack, Provider: providerSpotify, ID: match[2]}
		}
	}

	if match := regexp.MustCompile(`^deezer:(artist|album|track):([0-9]{1,32})$`).
		FindStringSubmatch(text); match != nil {
		switch match[1] {
		case "artist":
			return &Ref{Kind: KindArtist, Provider: providerDeezer, ID: match[2]}
		case "album":
			return &Ref{Kind: KindRelease, Provider: providerDeezer, ID: match[2]}
		default:
			return &Ref{Kind: KindTrack, Provider: providerDeezer, ID: match[2]}
		}
	}

	if !youtubeID.MatchString(text) {
		return nil
	}
	switch {
	case strings.HasPrefix(text, "UC"), strings.HasPrefix(text, "MPLA"):
		return &Ref{Kind: KindArtist, Provider: providerYTMusic, ID: text}
	case strings.HasPrefix(text, "MPRE"), strings.HasPrefix(text, "OLAK"):
		return &Ref{Kind: KindRelease, Provider: providerYTMusic, ID: text}
	}
	return nil
}

// youtube reads a YouTube or YouTube Music address.
func (s *Service) youtube(ctx context.Context, parsed *url.URL) (*Ref, error) {
	segments := pathSegments(parsed)
	list := parsed.Query().Get("list")
	video := parsed.Query().Get("v")

	// A playlist id starting with OLAK is an album.
	if strings.HasPrefix(list, "OLAK") && youtubeID.MatchString(list) {
		return &Ref{Kind: KindRelease, Provider: providerYTMusic, ID: list}, nil
	}

	var first, second string
	if len(segments) > 0 {
		first = segments[0]
	}
	if len(segments) > 1 {
		second = segments[1]
	}

	// A handle carries no id, so the canonical channel id has to be looked up.
	// YouTube Music resolves a YouTube channel id to the same artist, which is
	// what makes this work without a second lookup.
	if youtubeHandle.MatchString(first) {
		return s.resolveHandle(ctx, parsed, first)
	}

	switch first {
	case "channel":
		if youtubeID.MatchString(second) {
			return &Ref{Kind: KindArtist, Provider: providerYTMusic, ID: second}, nil
		}
	case "browse":
		if youtubeID.MatchString(second) {
			if strings.HasPrefix(second, "MPRE") {
				return &Ref{Kind: KindRelease, Provider: providerYTMusic, ID: second}, nil
			}
			if strings.HasPrefix(second, "UC") || strings.HasPrefix(second, "MPLA") {
				return &Ref{Kind: KindArtist, Provider: providerYTMusic, ID: second}, nil
			}
		}
	case "c", "user":
		// Legacy channel addresses carry no id either, and yt-dlp resolves
		// them the same way a handle is resolved.
		if second != "" {
			return s.resolveHandle(ctx, parsed, second)
		}
	case "playlist":
		return nil, apperr.New(apperr.CodeInvalidRequest,
			"This playlist is not an album; only albums, singles and EPs can be downloaded.")
	}

	if video != "" || first == "watch" || parsed.Hostname() == "youtu.be" {
		// Only Spotify implements resolving a bare track id, so a YouTube
		// video link has no release to file the track under.
		return nil, apperr.New(apperr.CodeInvalidRequest,
			"A single track link cannot be resolved through YouTube Music; "+
				"use the link of the album or the artist.")
	}

	return nil, apperr.New(apperr.CodeInvalidRequest,
		"No artist or album id could be read from this YouTube link.")
}

// resolveHandle looks up the canonical channel id behind a handle or a legacy
// channel name.
func (s *Service) resolveHandle(ctx context.Context, parsed *url.URL, label string) (*Ref, error) {
	if s.channels == nil {
		return nil, apperr.New(apperr.CodeToolUnavailable,
			"Channel addresses cannot be resolved because yt-dlp is unavailable.")
	}

	// The address is rebuilt from the parsed parts rather than passed through,
	// so that nothing beyond scheme, host and path reaches the process.
	target := (&url.URL{
		Scheme: "https",
		Host:   parsed.Hostname(),
		Path:   parsed.EscapedPath(),
	}).String()

	id, err := s.channels.ChannelID(ctx, target)
	if err != nil {
		if apperr.CodeOf(err) == apperr.CodeArtistNotFound {
			return nil, apperr.Newf(apperr.CodeArtistNotFound,
				"No YouTube channel was found for %q.", label)
		}
		return nil, err
	}
	if !youtubeID.MatchString(id) {
		return nil, apperr.Newf(apperr.CodeArtistNotFound,
			"No valid channel id was found for %q.", label)
	}
	return &Ref{Kind: KindArtist, Provider: providerYTMusic, ID: id}, nil
}

// spotify reads a Spotify address.
func spotify(parsed *url.URL) (*Ref, error) {
	segments := pathSegments(parsed)
	// Localised links carry a market prefix: /intl-de/artist/{id}
	if len(segments) > 0 && strings.HasPrefix(segments[0], "intl-") {
		segments = segments[1:]
	}
	if len(segments) < 2 {
		return nil, apperr.New(apperr.CodeInvalidRequest,
			"No valid id could be read from this Spotify link.")
	}

	kind, id := segments[0], segments[1]
	if !spotifyID.MatchString(id) {
		return nil, apperr.New(apperr.CodeInvalidRequest,
			"No valid id could be read from this Spotify link.")
	}

	switch kind {
	case "artist":
		return &Ref{Kind: KindArtist, Provider: providerSpotify, ID: id}, nil
	case "album":
		return &Ref{Kind: KindRelease, Provider: providerSpotify, ID: id}, nil
	case "track":
		return &Ref{Kind: KindTrack, Provider: providerSpotify, ID: id}, nil
	}
	return nil, apperr.Newf(apperr.CodeInvalidRequest,
		"Spotify links of type %q are not supported.", kind)
}

// deezer reads a Deezer address.
func deezer(parsed *url.URL) (*Ref, error) {
	segments := pathSegments(parsed)
	// Localised links carry a language prefix: /de/album/{id} or /en/track/{id}
	if len(segments) > 0 && len(segments[0]) == 2 {
		segments = segments[1:]
	}
	if len(segments) < 2 {
		return nil, apperr.New(apperr.CodeInvalidRequest,
			"No valid id could be read from this Deezer link.")
	}

	kind, id := segments[0], segments[1]
	if !deezerID.MatchString(id) {
		return nil, apperr.New(apperr.CodeInvalidRequest,
			"No valid id could be read from this Deezer link.")
	}

	switch kind {
	case "artist":
		return &Ref{Kind: KindArtist, Provider: providerDeezer, ID: id}, nil
	case "album":
		return &Ref{Kind: KindRelease, Provider: providerDeezer, ID: id}, nil
	case "track":
		return &Ref{Kind: KindTrack, Provider: providerDeezer, ID: id}, nil
	}
	return nil, apperr.Newf(apperr.CodeInvalidRequest,
		"Deezer links of type %q are not supported.", kind)
}

func pathSegments(parsed *url.URL) []string {
	out := make([]string, 0, 4)
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment != "" {
			out = append(out, segment)
		}
	}
	return out
}
