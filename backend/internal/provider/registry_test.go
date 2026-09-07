package provider_test

import (
	"context"
	"testing"

	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

type dummyMediaProvider struct {
	name string
}

func (d *dummyMediaProvider) Name() string { return d.name }
func (d *dummyMediaProvider) Search(ctx context.Context, track music.Track) ([]provider.MediaCandidate, error) {
	return nil, nil
}
func (d *dummyMediaProvider) Resolve(ctx context.Context, candidate provider.MediaCandidate) (*provider.MediaSource, error) {
	return nil, nil
}

func TestRegistry_MediaChain_RegistrationOrder(t *testing.T) {
	r := provider.NewRegistry()

	// Register in order: ytmusic, youtube, soundcloud
	ytm := &dummyMediaProvider{name: "ytmusic"}
	yt := &dummyMediaProvider{name: "youtube"}
	sc := &dummyMediaProvider{name: "soundcloud"}

	r.RegisterMedia(ytm)
	r.RegisterMedia(yt)
	r.RegisterMedia(sc)

	// 1. Default (preferred: ytmusic) -> ytmusic, youtube, soundcloud
	chain1 := r.MediaChain("ytmusic")
	if len(chain1) != 3 {
		t.Fatalf("expected 3 providers, got %d", len(chain1))
	}
	expected1 := []string{"ytmusic", "youtube", "soundcloud"}
	for i, exp := range expected1 {
		if chain1[i].Name() != exp {
			t.Errorf("chain1[%d] = %q, want %q", i, chain1[i].Name(), exp)
		}
	}

	// 2. Preferred: youtube -> youtube, ytmusic, soundcloud
	chain2 := r.MediaChain("youtube")
	expected2 := []string{"youtube", "ytmusic", "soundcloud"}
	for i, exp := range expected2 {
		if chain2[i].Name() != exp {
			t.Errorf("chain2[%d] = %q, want %q", i, chain2[i].Name(), exp)
		}
	}

	// 3. Preferred: soundcloud -> soundcloud, ytmusic, youtube
	chain3 := r.MediaChain("soundcloud")
	expected3 := []string{"soundcloud", "ytmusic", "youtube"}
	for i, exp := range expected3 {
		if chain3[i].Name() != exp {
			t.Errorf("chain3[%d] = %q, want %q", i, chain3[i].Name(), exp)
		}
	}
}
