package provider_test

import (
	"testing"

	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/provider/youtube"
	"ytdm/backend/internal/provider/ytmusic"
	"ytdm/backend/internal/ytdlp"
)

func TestFamilyOf(t *testing.T) {
	tests := []struct {
		provider string
		want     provider.Family
	}{
		{"youtube", provider.FamilyYouTube},
		{"YouTube", provider.FamilyYouTube},
		{"ytmusic", provider.FamilyYouTube},
		{"YTMusic", provider.FamilyYouTube},
		{"  youtube  ", provider.FamilyYouTube},
		{"spotify", provider.Family("spotify")},
		{"deezer", provider.Family("deezer")},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := provider.FamilyOf(tt.provider)
			if got != tt.want {
				t.Errorf("FamilyOf(%q) = %q, want %q", tt.provider, got, tt.want)
			}
		})
	}
}

func TestProvidersImplementFamily(t *testing.T) {
	client := ytdlp.New(ytdlp.Options{})
	ytProv, err := youtube.New(youtube.Config{Name: "youtube", Client: client})
	if err != nil {
		t.Fatalf("youtube.New: %v", err)
	}
	if ytProv.Family() != provider.FamilyYouTube {
		t.Errorf("expected YouTube provider family to be %s, got %s", provider.FamilyYouTube, ytProv.Family())
	}

	ytmProv, err := ytmusic.NewMediaProvider(ytmusic.MediaConfig{Client: client})
	if err != nil {
		t.Fatalf("ytmusic.NewMediaProvider: %v", err)
	}
	if ytmProv.Family() != provider.FamilyYouTube {
		t.Errorf("expected YTMusic provider family to be %s, got %s", provider.FamilyYouTube, ytmProv.Family())
	}

	var _ provider.FamilyProvider = ytProv
	var _ provider.FamilyProvider = ytmProv
}
