package subscriptions

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ytdm/backend/internal/music"
)

// appearanceCatalogue lists one release of the artist and one the artist only
// appears on, the way a provider listing with release artists does.
func appearanceCatalogue() *fakeProvider {
	return &fakeProvider{
		artist: &music.Artist{Name: "Bebe Rexha", Provider: "fake", SourceID: "4492087"},
		releases: []music.Release{
			{
				Title: "Expectations", ReleaseType: music.ReleaseAlbum, Year: 2018,
				Provider: "fake", SourceID: "own", AlbumArtist: "Bebe Rexha",
				ArtistIDs: []string{"4492087"},
			},
			{
				Title: "I'm Good (Blue)", ReleaseType: music.ReleaseSingle, Year: 2022,
				Provider: "fake", SourceID: "guest", AlbumArtist: "David Guetta",
				Artists: []string{"David Guetta", "Bebe Rexha"}, ArtistIDs: []string{"542"},
			},
		},
		tracks: map[string][]music.Track{
			"own":   {track("Meant To Be", 163_000, "USWB11704431")},
			"guest": {track("I'm Good (Blue)", 175_000, "UKWLG2200018")},
		},
	}
}

func subscribeBebe(t *testing.T, h *harness, include *bool) *Subscription {
	t.Helper()
	sub, err := h.service.Create(context.Background(), NewSubscription{
		Provider: "fake", ArtistSourceID: "4492087", ArtistName: "Bebe Rexha",
		IncludeAppearances: include,
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	return sub
}

func TestSyncIncludesAppearancesByDefault(t *testing.T) {
	h := newHarness(t, appearanceCatalogue())
	sub := subscribeBebe(t, h, nil)
	if !sub.IncludeAppearances {
		t.Fatal("a new subscription must include appearances")
	}

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.ReleasesSeen != 2 || result.AppearancesSkipped != 0 {
		t.Fatalf("releases seen %d, appearances skipped %d; want 2 and 0",
			result.ReleasesSeen, result.AppearancesSkipped)
	}
}

func TestSyncSkipsAppearancesWhenSwitchedOff(t *testing.T) {
	h := newHarness(t, appearanceCatalogue())
	off := false
	sub := subscribeBebe(t, h, &off)

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.ReleasesSeen != 1 || result.NewReleases != 1 || result.AppearancesSkipped != 1 {
		t.Fatalf("releases seen %d, new %d, appearances skipped %d; want 1, 1, 1",
			result.ReleasesSeen, result.NewReleases, result.AppearancesSkipped)
	}
	if result.TracksSeen != 1 {
		t.Fatalf("only the own release's track may be seen, got %d", result.TracksSeen)
	}

	// Switching it back on brings the appearance back.
	on := true
	if _, err := h.service.Update(context.Background(), sub.ID, Update{IncludeAppearances: &on}); err != nil {
		t.Fatalf("update: %v", err)
	}
	result, err = h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if result.ReleasesSeen != 2 || result.AppearancesSkipped != 0 {
		t.Fatalf("after switching on: releases seen %d, skipped %d", result.ReleasesSeen, result.AppearancesSkipped)
	}
}

func TestUpdateAcceptsTheAppearanceSwitchAlone(t *testing.T) {
	h := newHarness(t, appearanceCatalogue())
	sub := subscribeBebe(t, h, nil)
	off := false
	updated, err := h.service.Update(context.Background(), sub.ID, Update{IncludeAppearances: &off})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.IncludeAppearances {
		t.Fatal("the switch was not turned off")
	}
}

func TestExportWritesTheAppearanceSwitch(t *testing.T) {
	h := newHarness(t, appearanceCatalogue())
	off := false
	subscribeBebe(t, h, &off)

	payload, err := h.service.Export(context.Background())
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if payload.Version != ExportFormatVersion || ExportFormatVersion != 1 {
		t.Fatalf("the export format version must stay 1, got %d", payload.Version)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(string(raw), `"include_appearances":false`) {
		t.Fatalf("the export does not carry the switch: %s", raw)
	}
}

func TestImportReadsTheAppearanceSwitchWithAndWithoutTheField(t *testing.T) {
	h := newHarness(t, appearanceCatalogue())

	// The first item comes from a file written before the field existed.
	raw := `{"format":"ytmdl-subscriptions","version":1,"exported_at":"2026-01-01T00:00:00Z",
		"subscriptions":[
			{"artist_name":"Old","provider":"fake","artist_source_id":"1","enabled":true,
			 "release_filter":{"albums":true},"download_priority":"low"},
			{"artist_name":"New","provider":"fake","artist_source_id":"2","enabled":true,
			 "release_filter":{"albums":true},"download_priority":"low","include_appearances":false}]}`
	var payload ExportPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}

	preview, err := h.service.PreviewImport(context.Background(), payload)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !preview.Items[0].IncludeAppearances || preview.Items[1].IncludeAppearances {
		t.Fatalf("preview read the switch wrongly: %+v", preview.Items)
	}

	if _, err := h.service.ApplyImport(context.Background(), payload); err != nil {
		t.Fatalf("apply: %v", err)
	}
	old, _ := h.store.FindBySource(context.Background(), "fake", "1")
	newer, _ := h.store.FindBySource(context.Background(), "fake", "2")
	if old == nil || newer == nil {
		t.Fatal("the import did not create both subscriptions")
	}
	if !old.IncludeAppearances {
		t.Fatal("a file without the field must import as include_appearances=true")
	}
	if newer.IncludeAppearances {
		t.Fatal("include_appearances=false was not imported")
	}

	// Re-importing the old file over a subscription that turned the switch
	// off is an update, and says so.
	off := false
	if _, err := h.service.Update(context.Background(), old.ID, Update{IncludeAppearances: &off}); err != nil {
		t.Fatalf("update: %v", err)
	}
	preview, err = h.service.PreviewImport(context.Background(), payload)
	if err != nil {
		t.Fatalf("second preview: %v", err)
	}
	if preview.Items[0].Status != ImportStatusWouldUpdate {
		t.Fatalf("the changed switch was not detected: %+v", preview.Items[0])
	}
	if preview.Items[1].Status != ImportStatusUnchanged {
		t.Fatalf("an unchanged subscription was reported as %q", preview.Items[1].Status)
	}
}
