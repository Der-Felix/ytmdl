package jobs

import (
	"context"
	"testing"

	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

type provenanceRecordingCatalog struct {
	fakeCatalog
	lastEntry music.LibraryEntry
}

func (p *provenanceRecordingCatalog) PersistDownload(_ context.Context, entry music.LibraryEntry, _ int) (music.StoredEntry, error) {
	p.lastEntry = entry
	return music.StoredEntry{
		ArtistID:  entry.Artist.ID,
		ReleaseID: "rel-1",
		TrackID:   "tr-1",
	}, nil
}

func (p *provenanceRecordingCatalog) FindArtistBySource(_ context.Context, provider, sourceID string) (*music.Artist, error) {
	if provider == "deezer" && sourceID == "288164" {
		return &music.Artist{
			ID:       "canonical-alan-walker-id",
			Name:     "Alan Walker",
			Provider: "deezer",
			SourceID: "288164",
		}, nil
	}
	return nil, nil
}

func TestWorker_PersistCarriesCanonicalArtistProvenance(t *testing.T) {
	cat := &provenanceRecordingCatalog{}
	mgr := &Manager{
		catalog: cat,
	}

	track := music.Track{
		Title:          "Faded",
		Album:          "Different World",
		AlbumArtist:    "Alan Walker",
		SourceProvider: "deezer",
		SourceID:       "tr_faded",
	}

	release := music.Release{
		Title:       "Different World",
		AlbumArtist: "Alan Walker",
		Provider:    "ytmusic", // e.g. audio resolved from YouTube Music
		SourceID:    "yt_rel_123",
	}

	source := provider.MediaSource{
		Provider: "ytmusic",
		ID:       "yt_video_123",
	}

	file := music.File{
		Path: "Alan Walker/Different World/01 - Faded.opus",
	}

	// 1. When explicit CanonicalArtistID is provided:
	_, err := mgr.persist(context.Background(), track, release, source, file, "canonical-alan-walker-id")
	if err != nil {
		t.Fatalf("persist failed: %v", err)
	}

	if cat.lastEntry.Artist == nil {
		t.Fatalf("expected Artist to be set in LibraryEntry")
	}
	if cat.lastEntry.Artist.ID != "canonical-alan-walker-id" {
		t.Fatalf("expected Artist.ID to be %q, got %q", "canonical-alan-walker-id", cat.lastEntry.Artist.ID)
	}
	if cat.lastEntry.Artist.Name != "Alan Walker" {
		t.Fatalf("expected Artist.Name to be %q, got %q", "Alan Walker", cat.lastEntry.Artist.Name)
	}
}
