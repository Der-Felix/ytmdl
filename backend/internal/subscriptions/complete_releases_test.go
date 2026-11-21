package subscriptions

import (
	"context"
	"testing"

	"ytdm/backend/internal/music"
)

// singleProvider lists Discovery and the single "One More Time", which
// carries the album track again.
func singleProvider() *fakeProvider {
	p := discoveryProvider()
	p.releases = append(p.releases, music.Release{
		Title: "One More Time", ReleaseType: music.ReleaseSingle, Year: 2000,
		Provider: "fake", SourceID: "999",
	})
	p.tracks["999"] = []music.Track{track("One More Time", 320_000, "GBDUW0000059")}
	return p
}

// withDiscoveryDownloaded stores Discovery with both its recordings as files.
func withDiscoveryDownloaded(h *harness, p *fakeProvider) {
	h.catalog.addRelease("fake", "302127")
	for i, tr := range p.tracks["302127"] {
		id := "track-" + string(rune('a'+i))
		h.catalog.addTrack(id, tr)
		h.files.markDownloaded(id)
	}
}

// A release the library does not hold arrives complete: a recording that is
// already downloaded is queued with it as a copy, counted apart from the
// recordings to fetch.
func TestSyncQueuesACopyForAReleaseTheLibraryDoesNotHold(t *testing.T) {
	p := singleProvider()
	h := newHarness(t, p)
	sub := subscribe(t, h, true)
	withDiscoveryDownloaded(h, p)

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	t.Logf("result: new_releases=%d new_tracks=%d queued=%d skipped=%d copies=%d",
		result.NewReleases, result.NewTracks, result.QueuedTracks, result.SkippedTracks, result.CopiesQueued)

	if result.CopiesQueued != 1 {
		t.Fatalf("copies_queued = %d, want 1", result.CopiesQueued)
	}
	// The other numbers keep their meaning: no recording is new or fetched,
	// both are held as files, and the single brings no unknown track.
	if result.NewTracks != 0 || result.QueuedTracks != 0 || result.SkippedTracks != 2 || result.NewReleases != 0 {
		t.Fatalf("result = %+v", result)
	}
	if queued := h.downloader.queued(); len(queued) != 1 || queued[0] != "999" {
		t.Fatalf("queued releases = %v, want the single", queued)
	}
}

// The backlog of releases that are already in the library but incomplete is
// left to an explicit backfill; a routine sync does not grow them.
func TestSyncLeavesAReleaseTheLibraryAlreadyHoldsAlone(t *testing.T) {
	p := singleProvider()
	h := newHarness(t, p)
	sub := subscribe(t, h, true)
	withDiscoveryDownloaded(h, p)
	h.catalog.addRelease("fake", "999")

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.CopiesQueued != 0 || result.QueuedTracks != 0 {
		t.Fatalf("result = %+v, want nothing queued", result)
	}
	if queued := h.downloader.queued(); len(queued) != 0 {
		t.Fatalf("queued releases = %v, want none", queued)
	}
}

func TestSyncQueuesNoCopyWhenCompletingReleasesIsOff(t *testing.T) {
	p := singleProvider()
	h := newHarness(t, p)
	h.completeReleases.Store(false)
	sub := subscribe(t, h, true)
	withDiscoveryDownloaded(h, p)

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.CopiesQueued != 0 || result.SkippedTracks != 2 {
		t.Fatalf("result = %+v", result)
	}
	if queued := h.downloader.queued(); len(queued) != 0 {
		t.Fatalf("queued releases = %v, want none", queued)
	}
}

// A release that is queued for a new recording anyway carries its copies in
// the same job; both are counted, each under its own number.
func TestSyncCountsCopiesAndNewRecordingsOfOneReleaseApart(t *testing.T) {
	p := singleProvider()
	p.tracks["999"] = append(p.tracks["999"], track("Ouverture", 180_000, "GBDUW0300001"))
	h := newHarness(t, p)
	sub := subscribe(t, h, true)
	withDiscoveryDownloaded(h, p)

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.QueuedTracks != 1 || result.CopiesQueued != 1 || result.NewTracks != 1 || result.NewReleases != 1 {
		t.Fatalf("result = %+v", result)
	}
	if queued := h.downloader.queued(); len(queued) != 1 || queued[0] != "999" {
		t.Fatalf("queued releases = %v, want one job for the single", queued)
	}
}

// Without auto download nothing is handed over, copies included.
func TestSyncQueuesNoCopyWithoutAutoDownload(t *testing.T) {
	p := singleProvider()
	h := newHarness(t, p)
	sub := subscribe(t, h, false)
	withDiscoveryDownloaded(h, p)

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.CopiesQueued != 0 || len(h.downloader.queued()) != 0 {
		t.Fatalf("result = %+v, queued %v", result, h.downloader.queued())
	}
}
