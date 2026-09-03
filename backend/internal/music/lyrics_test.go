package music_test

import (
	"testing"

	"ytdm/backend/internal/music"
)

func TestLyricsState(t *testing.T) {
	cases := []struct {
		name string
		in   music.Lyrics
		want music.LyricsState
	}{
		{"synced", music.Lyrics{Provider: "lrclib", Synced: true, LRC: "[00:01.00]a", PlainText: "a"}, music.LyricsAvailableSynced},
		{"plain", music.Lyrics{Provider: "ytmusic", PlainText: "a"}, music.LyricsAvailablePlain},
		{"instrumental", music.Lyrics{Provider: "lrclib", Instrumental: true}, music.LyricsInstrumental},
		{"empty", music.Lyrics{Provider: "lrclib"}, music.LyricsNotFound},
		{"synced flag without a body is not synced", music.Lyrics{Provider: "lrclib", Synced: true, PlainText: "a"}, music.LyricsAvailablePlain},
	}
	for _, c := range cases {
		if got := c.in.State(); got != c.want {
			t.Errorf("%s: State() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLyricsExtensionFollowsTheState(t *testing.T) {
	cases := []struct {
		in   music.Lyrics
		want string
	}{
		{music.Lyrics{Synced: true, LRC: "[00:01.00]a"}, ".lrc"},
		{music.Lyrics{PlainText: "a"}, ".txt"},
		{music.Lyrics{Instrumental: true}, ""},
		{music.Lyrics{}, ""},
	}
	for _, c := range cases {
		if got := c.in.Extension(); got != c.want {
			t.Errorf("Extension() = %q, want %q for %+v", got, c.want, c.in)
		}
	}
}

func TestLyricsBody(t *testing.T) {
	synced := music.Lyrics{Synced: true, LRC: "[00:01.00]a", PlainText: "a"}
	if got := synced.Body(); got != "[00:01.00]a" {
		t.Errorf("synced Body() = %q", got)
	}
	plain := music.Lyrics{PlainText: "a"}
	if got := plain.Body(); got != "a" {
		t.Errorf("plain Body() = %q", got)
	}
}

func TestValidLyricsState(t *testing.T) {
	for _, state := range music.AllLyricsStates() {
		if !music.ValidLyricsState(string(state)) {
			t.Errorf("%q must be valid", state)
		}
	}
	if music.ValidLyricsState("nonsense") {
		t.Error("an unknown value must not validate")
	}
	// There is deliberately no "error" state: a transient failure leaves the
	// stored state alone rather than recording a negative result.
	if music.ValidLyricsState("error") {
		t.Error("there must be no error state")
	}
}

func TestLyricsStateHasSidecar(t *testing.T) {
	want := map[music.LyricsState]bool{
		music.LyricsUnknown:         false,
		music.LyricsAvailableSynced: true,
		music.LyricsAvailablePlain:  true,
		music.LyricsInstrumental:    false,
		music.LyricsNotFound:        false,
	}
	for state, expected := range want {
		if got := state.HasSidecar(); got != expected {
			t.Errorf("%q.HasSidecar() = %v, want %v", state, got, expected)
		}
	}
}
