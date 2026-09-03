package resolve

import (
	"context"
	"errors"
	"testing"

	"ytdm/backend/internal/apperr"
)

// fakeChannels stands in for yt-dlp. Resolving a handle must never need the
// network in a unit test.
type fakeChannels struct {
	id     string
	err    error
	target string
	calls  int
}

func (f *fakeChannels) ChannelID(_ context.Context, target string) (string, error) {
	f.calls++
	f.target = target
	return f.id, f.err
}

func TestResolveAddresses(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Ref
	}{
		{
			name:  "youtube music channel",
			input: "https://music.youtube.com/channel/UCxgN32UVVztKAQd2HkXzBtw",
			want:  Ref{Kind: KindArtist, Provider: providerYTMusic, ID: "UCxgN32UVVztKAQd2HkXzBtw"},
		},
		{
			name:  "youtube channel",
			input: "https://www.youtube.com/channel/UCxgN32UVVztKAQd2HkXzBtw",
			want:  Ref{Kind: KindArtist, Provider: providerYTMusic, ID: "UCxgN32UVVztKAQd2HkXzBtw"},
		},
		{
			name:  "youtube music browse album",
			input: "https://music.youtube.com/browse/MPREb_d1UkStdzUrN",
			want:  Ref{Kind: KindRelease, Provider: providerYTMusic, ID: "MPREb_d1UkStdzUrN"},
		},
		{
			name:  "youtube music album playlist",
			input: "https://music.youtube.com/playlist?list=OLAK5uy_abcdefghijkl",
			want:  Ref{Kind: KindRelease, Provider: providerYTMusic, ID: "OLAK5uy_abcdefghijkl"},
		},
		{
			name:  "spotify artist",
			input: "https://open.spotify.com/artist/6XyY86QOPPrYVGvF9ch6wz",
			want:  Ref{Kind: KindArtist, Provider: providerSpotify, ID: "6XyY86QOPPrYVGvF9ch6wz"},
		},
		{
			name:  "spotify album with locale prefix",
			input: "https://open.spotify.com/intl-de/album/6XyY86QOPPrYVGvF9ch6wz",
			want:  Ref{Kind: KindRelease, Provider: providerSpotify, ID: "6XyY86QOPPrYVGvF9ch6wz"},
		},
		{
			name:  "spotify artist with locale prefix and query",
			input: "https://open.spotify.com/intl-de/artist/6XyY86QOPPrYVGvF9ch6wz?si=test12345",
			want:  Ref{Kind: KindArtist, Provider: providerSpotify, ID: "6XyY86QOPPrYVGvF9ch6wz"},
		},
		{
			name:  "spotify track with locale prefix",
			input: "https://open.spotify.com/intl-de/track/6XyY86QOPPrYVGvF9ch6wz",
			want:  Ref{Kind: KindTrack, Provider: providerSpotify, ID: "6XyY86QOPPrYVGvF9ch6wz"},
		},
		{
			name:  "spotify track without locale prefix",
			input: "https://open.spotify.com/track/6XyY86QOPPrYVGvF9ch6wz",
			want:  Ref{Kind: KindTrack, Provider: providerSpotify, ID: "6XyY86QOPPrYVGvF9ch6wz"},
		},
		{
			name:  "play.spotify.com track",
			input: "https://play.spotify.com/track/6XyY86QOPPrYVGvF9ch6wz",
			want:  Ref{Kind: KindTrack, Provider: providerSpotify, ID: "6XyY86QOPPrYVGvF9ch6wz"},
		},
		{
			name:  "spotify artist uri",
			input: "spotify:artist:6XyY86QOPPrYVGvF9ch6wz",
			want:  Ref{Kind: KindArtist, Provider: providerSpotify, ID: "6XyY86QOPPrYVGvF9ch6wz"},
		},
		{
			name:  "spotify album uri",
			input: "spotify:album:6XyY86QOPPrYVGvF9ch6wz",
			want:  Ref{Kind: KindRelease, Provider: providerSpotify, ID: "6XyY86QOPPrYVGvF9ch6wz"},
		},
		{
			name:  "spotify track uri",
			input: "spotify:track:6XyY86QOPPrYVGvF9ch6wz",
			want:  Ref{Kind: KindTrack, Provider: providerSpotify, ID: "6XyY86QOPPrYVGvF9ch6wz"},
		},
		{
			name:  "deezer artist url",
			input: "https://www.deezer.com/artist/27",
			want:  Ref{Kind: KindArtist, Provider: providerDeezer, ID: "27"},
		},
		{
			name:  "deezer album url with language and query",
			input: "https://www.deezer.com/de/album/6575789?utm_source=test",
			want:  Ref{Kind: KindRelease, Provider: providerDeezer, ID: "6575789"},
		},
		{
			name:  "deezer track url without www and with language",
			input: "https://deezer.com/en/track/67238732",
			want:  Ref{Kind: KindTrack, Provider: providerDeezer, ID: "67238732"},
		},
		{
			name:  "deezer artist uri",
			input: "deezer:artist:27",
			want:  Ref{Kind: KindArtist, Provider: providerDeezer, ID: "27"},
		},
		{
			name:  "deezer album uri",
			input: "deezer:album:6575789",
			want:  Ref{Kind: KindRelease, Provider: providerDeezer, ID: "6575789"},
		},
		{
			name:  "deezer track uri",
			input: "deezer:track:67238732",
			want:  Ref{Kind: KindTrack, Provider: providerDeezer, ID: "67238732"},
		},
		{
			name:  "bare channel id",
			input: "UCxgN32UVVztKAQd2HkXzBtw",
			want:  Ref{Kind: KindArtist, Provider: providerYTMusic, ID: "UCxgN32UVVztKAQd2HkXzBtw"},
		},
		{
			name:  "bare release id",
			input: "MPREb_d1UkStdzUrN",
			want:  Ref{Kind: KindRelease, Provider: providerYTMusic, ID: "MPREb_d1UkStdzUrN"},
		},
		{
			name:  "address without a scheme",
			input: "music.youtube.com/channel/UCxgN32UVVztKAQd2HkXzBtw",
			want:  Ref{Kind: KindArtist, Provider: providerYTMusic, ID: "UCxgN32UVVztKAQd2HkXzBtw"},
		},
	}

	service := NewService(&fakeChannels{id: "UCmustNotBeUsed"})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := service.Resolve(context.Background(), test.input)
			if err != nil {
				t.Fatalf("Resolve(%q) failed: %v", test.input, err)
			}
			if *got != test.want {
				t.Errorf("Resolve(%q) = %+v, want %+v", test.input, *got, test.want)
			}
		})
	}
}

// TestResolveHandleUsesChannelLookup covers the addresses that carry no id at
// all: the canonical channel id has to be looked up for them.
func TestResolveHandleUsesChannelLookup(t *testing.T) {
	const resolved = "UCZU9T1ceaOgwfLRq7OKFU4Q"

	tests := []struct {
		input      string
		wantTarget string
	}{
		{"https://youtube.com/@LinkinPark", "https://youtube.com/@LinkinPark"},
		{"https://www.youtube.com/@LinkinPark", "https://www.youtube.com/@LinkinPark"},
		{"https://www.youtube.com/@LinkinPark/videos", "https://www.youtube.com/@LinkinPark/videos"},
		{"youtube.com/@LinkinPark", "https://youtube.com/@LinkinPark"},
		{"https://www.youtube.com/c/LinkinPark", "https://www.youtube.com/c/LinkinPark"},
		{"https://www.youtube.com/user/LinkinPark", "https://www.youtube.com/user/LinkinPark"},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			channels := &fakeChannels{id: resolved}
			got, err := NewService(channels).Resolve(context.Background(), test.input)
			if err != nil {
				t.Fatalf("Resolve(%q) failed: %v", test.input, err)
			}

			want := Ref{Kind: KindArtist, Provider: providerYTMusic, ID: resolved}
			if *got != want {
				t.Errorf("Resolve(%q) = %+v, want %+v", test.input, *got, want)
			}
			if channels.calls != 1 {
				t.Errorf("channel lookups = %d, want 1", channels.calls)
			}
			if channels.target != test.wantTarget {
				t.Errorf("looked up %q, want %q", channels.target, test.wantTarget)
			}
		})
	}
}

// TestResolveHandleWithoutResolver reports a usable error instead of pretending
// the address is malformed when yt-dlp is not configured.
func TestResolveHandleWithoutResolver(t *testing.T) {
	_, err := NewService(nil).Resolve(context.Background(), "https://youtube.com/@LinkinPark")
	if got := apperr.CodeOf(err); got != apperr.CodeToolUnavailable {
		t.Errorf("error code = %q, want %q", got, apperr.CodeToolUnavailable)
	}
}

// TestResolveHandleFailurePropagates keeps a lookup failure distinguishable
// from an unreadable address.
func TestResolveHandleFailurePropagates(t *testing.T) {
	channels := &fakeChannels{err: apperr.New(apperr.CodeArtistNotFound, "no channel")}
	_, err := NewService(channels).Resolve(context.Background(), "https://youtube.com/@nope")

	if got := apperr.CodeOf(err); got != apperr.CodeArtistNotFound {
		t.Errorf("error code = %q, want %q", got, apperr.CodeArtistNotFound)
	}
}

// TestResolveRejections covers everything a caller has to treat as a search
// query or explain to the user.
func TestResolveRejections(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"plain search term", "Linkin Park"},
		{"empty", "   "},
		{"unknown host", "https://soundcloud.com/artist"},
		{"youtube video", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{"youtu.be short link", "https://youtu.be/dQw4w9WgXcQ"},
		{"ordinary playlist", "https://music.youtube.com/playlist?list=PL123456"},
		{"spotify with a bad id", "https://open.spotify.com/artist/tooshort"},
		{"non http scheme", "ftp://youtube.com/channel/UCabc"},
	}

	service := NewService(&fakeChannels{id: "UCmustNotBeUsed"})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ref, err := service.Resolve(context.Background(), test.input)
			if err == nil {
				t.Fatalf("Resolve(%q) unexpectedly succeeded: %+v", test.input, ref)
			}
			var appErr *apperr.Error
			if !errors.As(err, &appErr) {
				t.Fatalf("Resolve(%q) returned a plain error: %v", test.input, err)
			}
		})
	}
}

// TestResolveHandleIsNotCalledForAddressesThatCarryAnID keeps the lookup off
// the fast path: it costs a yt-dlp process and must only run when needed.
func TestResolveHandleIsNotCalledForAddressesThatCarryAnID(t *testing.T) {
	channels := &fakeChannels{id: "UCshouldNotHappen"}
	service := NewService(channels)

	for _, input := range []string{
		"https://music.youtube.com/channel/UCxgN32UVVztKAQd2HkXzBtw",
		"https://open.spotify.com/artist/6XyY86QOPPrYVGvF9ch6wz",
		"MPREb_d1UkStdzUrN",
	} {
		if _, err := service.Resolve(context.Background(), input); err != nil {
			t.Fatalf("Resolve(%q) failed: %v", input, err)
		}
	}
	if channels.calls != 0 {
		t.Errorf("channel lookups = %d, want 0", channels.calls)
	}
}
