package discography

import (
	"testing"

	"ytdm/backend/internal/music"
)

func TestRereadRemixTitleKey(t *testing.T) {
	remix := track("Blinding Lights Major Lazer Remix")
	current := IdentityKey(remix)
	older := olderSchemeKey(remix)
	if current == older {
		t.Fatal("the fixture must have an older key that differs from the current one")
	}

	onLive := track("Song", withRelease(music.ReleaseLive, "Live in Paris", 2012))
	studio := track("Song")

	cases := []struct {
		name    string
		track   music.Track
		stored  string
		want    string
		verdict KeyVerdict
	}{
		{"current key", remix, current, current, KeyCurrent},
		{"key of the earlier reading", remix, older, current, KeyRewrite},
		// A live release row written before the release counted, and one
		// written after: both already have the current title reading.
		{"live release, studio key", onLive, IdentityKey(studio), IdentityKey(studio), KeyCurrent},
		{"live release, live key", onLive, IdentityKey(onLive), IdentityKey(onLive), KeyCurrent},
		// The release's live marker is not added by this step: a row of a
		// live release that carries the earlier reading gets the current
		// reading without it, the form the lookup's live branch knows.
		{"live release, earlier reading", track("Blinding Lights Major Lazer Remix", withRelease(music.ReleaseLive, "Live", 2012)),
			older, current, KeyRewrite},
		{"key of another title", remix, IdentityKey(track("Other Song")), IdentityKey(track("Other Song")), KeyNotDerivable},
		{"key of another artist", track("Song"), IdentityKey(music.Track{Title: "Song", Artists: []string{"Somebody Else"}}),
			IdentityKey(music.Track{Title: "Song", Artists: []string{"Somebody Else"}}), KeyNotDerivable},
		// A track without a title is stored as "Unknown Title" under the key
		// of the empty title.
		{"stored without a title", music.Track{Title: music.Track{}.DisplayTitle(), Artists: []string{"Artist"}},
			IdentityKey(music.Track{Artists: []string{"Artist"}}), IdentityKey(music.Track{Artists: []string{"Artist"}}), KeyCurrent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, verdict := RereadRemixTitleKey(tc.track, tc.stored)
			if got != tc.want || verdict != tc.verdict {
				t.Errorf("got %q (%d), want %q (%d)", got, verdict, tc.want, tc.verdict)
			}
			// Idempotent: the result is current for the same row.
			if verdict == KeyRewrite {
				if again, v := RereadRemixTitleKey(tc.track, got); again != got || v != KeyCurrent {
					t.Errorf("second pass gave %q (%d), want the key unchanged and current", again, v)
				}
			}
		})
	}
}

func TestIdentitySchemesAreOrdered(t *testing.T) {
	if !(IdentitySchemeUnrecorded < RemixReadingScheme && RemixReadingScheme <= IdentityScheme) {
		t.Fatalf("schemes out of order: unrecorded %d, remix reading %d, current %d",
			IdentitySchemeUnrecorded, RemixReadingScheme, IdentityScheme)
	}
}
