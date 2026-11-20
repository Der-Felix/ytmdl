package discography

import (
	"context"
	"sort"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

func TestReleaseOwnership(t *testing.T) {
	tests := []struct {
		name     string
		release  music.Release
		artistID string
		artist   string
		want     Ownership
	}{
		{
			name:     "own release by provider id",
			release:  music.Release{ArtistIDs: []string{"4492087"}, AlbumArtist: "Bebe Rexha"},
			artistID: "4492087", artist: "Bebe Rexha",
			want: OwnershipOwn,
		},
		{
			name: "appearance by provider id although the name is credited",
			release: music.Release{
				ArtistIDs: []string{"542"}, AlbumArtist: "David Guetta",
				Artists: []string{"David Guetta", "Bebe Rexha"},
			},
			artistID: "4492087", artist: "Bebe Rexha",
			want: OwnershipAppearance,
		},
		{
			name:     "joint release by provider id",
			release:  music.Release{ArtistIDs: []string{"111", "222"}},
			artistID: "222", artist: "Bushido",
			want: OwnershipOwn,
		},
		{
			name:     "own release by normalised name",
			release:  music.Release{Artists: []string{"BEYONCÉ"}},
			artistID: "", artist: "Beyonce",
			want: OwnershipOwn,
		},
		{
			name:     "own release by name with a leading article",
			release:  music.Release{AlbumArtist: "The Weeknd"},
			artistID: "UC123", artist: "Weeknd",
			want: OwnershipOwn,
		},
		{
			name:     "joint release by name",
			release:  music.Release{AlbumArtist: "LACAZETTE & Bushido"},
			artistID: "", artist: "Bushido",
			want: OwnershipOwn,
		},
		{
			name:     "appearance by name",
			release:  music.Release{AlbumArtist: "David Guetta", Artists: []string{"David Guetta"}},
			artistID: "", artist: "Bebe Rexha",
			want: OwnershipAppearance,
		},
		{
			name:     "no artist on the release",
			release:  music.Release{Title: "I'm Good (Blue)"},
			artistID: "4492087", artist: "Bebe Rexha",
			want: OwnershipUnknown,
		},
		{
			name:     "only the placeholder artist",
			release:  music.Release{AlbumArtist: music.UnknownArtist},
			artistID: "4492087", artist: "Bebe Rexha",
			want: OwnershipUnknown,
		},
		{
			name:     "subscribed artist without a name and no ids",
			release:  music.Release{AlbumArtist: "David Guetta"},
			artistID: "", artist: "",
			want: OwnershipUnknown,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReleaseOwnership(tc.release, tc.artistID, tc.artist); got != tc.want {
				t.Fatalf("ReleaseOwnership = %v, want %v", got, tc.want)
			}
		})
	}
}

// appearanceProvider models Deezer: the artist listing names no artist, the
// album itself does.
func appearanceProvider(detailErr map[string]error) *mockMetadataProvider {
	details := map[string]music.Release{
		"own":     {SourceID: "own", Title: "Own Single", ArtistIDs: []string{"4492087"}, AlbumArtist: "Bebe Rexha"},
		"joint":   {SourceID: "joint", Title: "Joint Single", ArtistIDs: []string{"9", "4492087"}, AlbumArtist: "Someone & Bebe Rexha"},
		"guest":   {SourceID: "guest", Title: "I'm Good (Blue)", ArtistIDs: []string{"542"}, AlbumArtist: "David Guetta", Artists: []string{"David Guetta", "Bebe Rexha"}},
		"unknown": {SourceID: "unknown", Title: "Nobody Knows"},
	}
	return &mockMetadataProvider{
		name: "deezer",
		getArtist: func(_ context.Context, id string) (*music.Artist, error) {
			return &music.Artist{ID: id, Name: "Bebe Rexha", Provider: "deezer", SourceID: id}, nil
		},
		getDisco: func(context.Context, string) ([]music.Release, error) {
			var out []music.Release
			for _, id := range []string{"own", "joint", "guest", "unknown", "broken"} {
				out = append(out, music.Release{ID: id, SourceID: id, Title: id, ReleaseType: music.ReleaseSingle, Provider: "deezer"})
			}
			return out, nil
		},
		getRelease: func(_ context.Context, id string) (*music.Release, error) {
			if err := detailErr[id]; err != nil {
				return nil, err
			}
			if r, ok := details[id]; ok {
				return &r, nil
			}
			return nil, apperr.Newf(apperr.CodeReleaseNotFound, "no release %q", id)
		},
		getTracks: func(_ context.Context, id string) ([]music.Track, error) {
			return []music.Track{{ID: "t-" + id, Title: "Song " + id, DurationMS: 200_000, Artists: []string{"Bebe Rexha"}}}, nil
		},
	}
}

func resolveWithAppearanceSwitch(t *testing.T, exclude bool) *Result {
	t.Helper()
	reg := provider.NewRegistry()
	reg.RegisterMetadata(appearanceProvider(map[string]error{
		"broken": apperr.New(apperr.CodeProviderUnavailable, "down"),
	}))
	service, err := NewService(Options{Registry: reg, Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	res, err := service.ResolveArtist(context.Background(), ArtistRequest{
		Provider: "deezer", ArtistID: "4492087",
		Filter:             music.DefaultReleaseFilter(),
		ExcludeAppearances: exclude,
	}, nil)
	if err != nil {
		t.Fatalf("ResolveArtist: %v", err)
	}
	return res
}

func releaseIDs(releases []music.Release) []string {
	ids := make([]string, 0, len(releases))
	for _, r := range releases {
		ids = append(ids, r.SourceID)
	}
	sort.Strings(ids)
	return ids
}

func TestResolveArtistExcludesAppearancesOnlyWhenAsked(t *testing.T) {
	all := resolveWithAppearanceSwitch(t, false)
	if got := releaseIDs(all.Releases); len(got) != 5 {
		t.Fatalf("with appearances every listed release must stay, got %v", got)
	}
	if all.AppearancesSkipped != 0 {
		t.Fatalf("nothing may be skipped with appearances on, got %d", all.AppearancesSkipped)
	}

	own := resolveWithAppearanceSwitch(t, true)
	got := releaseIDs(own.Releases)
	// own and joint are the artist's; unknown names nobody and broken could
	// not be read, so both are kept rather than dropped.
	want := []string{"broken", "joint", "own", "unknown"}
	if len(got) != len(want) {
		t.Fatalf("releases = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("releases = %v, want %v", got, want)
		}
	}
	if own.AppearancesSkipped != 1 {
		t.Fatalf("appearances skipped = %d, want 1", own.AppearancesSkipped)
	}
}
