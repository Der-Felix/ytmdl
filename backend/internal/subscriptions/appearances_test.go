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

func boolPtr(v bool) *bool { return &v }

func syncBebe(t *testing.T, h *harness, id string) *SyncResult {
	t.Helper()
	result, err := h.service.Sync(context.Background(), id)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	return result
}

func TestResolveIncludeAppearances(t *testing.T) {
	cases := []struct {
		own    *bool
		global bool
		want   bool
	}{
		{nil, true, true},
		{nil, false, false},
		{boolPtr(true), false, true},
		{boolPtr(false), true, false},
		{boolPtr(true), true, true},
		{boolPtr(false), false, false},
	}
	for _, c := range cases {
		if got := ResolveIncludeAppearances(c.own, c.global); got != c.want {
			t.Errorf("ResolveIncludeAppearances(%v, %v) = %v, want %v", c.own, c.global, got, c.want)
		}
	}
}

func TestNullableBoolTellsAbsentNullAndValueApart(t *testing.T) {
	var body struct {
		Field NullableBool `json:"include_appearances"`
	}
	for raw, want := range map[string]struct {
		set   bool
		value *bool
	}{
		`{}`:                            {false, nil},
		`{"include_appearances":null}`:  {true, nil},
		`{"include_appearances":true}`:  {true, boolPtr(true)},
		`{"include_appearances":false}`: {true, boolPtr(false)},
	} {
		body.Field = NullableBool{}
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if body.Field.Set != want.set || !sameOptionalBool(body.Field.Value, want.value) {
			t.Errorf("%s: got set=%v value=%v", raw, body.Field.Set, body.Field.Value)
		}
	}
	if err := json.Unmarshal([]byte(`{"include_appearances":"yes"}`), &body); err == nil {
		t.Fatal("a string must be rejected")
	}
}

func TestNewSubscriptionFollowsTheSettingByDefault(t *testing.T) {
	h := newHarness(t, appearanceCatalogue())
	sub := subscribeBebe(t, h, nil)
	if sub.IncludeAppearances != nil {
		t.Fatalf("a new subscription must have no choice of its own, got %v", *sub.IncludeAppearances)
	}
	if !sub.EffectiveIncludeAppearances {
		t.Fatal("with the setting on, the effective value must be true")
	}

	result := syncBebe(t, h, sub.ID)
	if result.ReleasesSeen != 2 || result.AppearancesSkipped != 0 {
		t.Fatalf("releases seen %d, appearances skipped %d; want 2 and 0",
			result.ReleasesSeen, result.AppearancesSkipped)
	}
}

// TestSyncFollowsAChangeOfTheGlobalSetting is the point of the design: the
// setting is read at sync time, so turning it off reaches every subscription
// that follows it without touching any of them, and an exception stays put.
func TestSyncFollowsAChangeOfTheGlobalSetting(t *testing.T) {
	h := newHarness(t, appearanceCatalogue())
	follows := subscribeBebe(t, h, nil)

	h.appearances.Store(false)

	result := syncBebe(t, h, follows.ID)
	if result.ReleasesSeen != 1 || result.NewReleases != 1 || result.AppearancesSkipped != 1 {
		t.Fatalf("releases seen %d, new %d, appearances skipped %d; want 1, 1, 1",
			result.ReleasesSeen, result.NewReleases, result.AppearancesSkipped)
	}
	if result.TracksSeen != 1 {
		t.Fatalf("only the own release's track may be seen, got %d", result.TracksSeen)
	}
	got, err := h.service.Get(context.Background(), follows.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.IncludeAppearances != nil || got.EffectiveIncludeAppearances {
		t.Fatalf("own %v, effective %v; want nil and false", got.IncludeAppearances, got.EffectiveIncludeAppearances)
	}
	list, err := h.service.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].EffectiveIncludeAppearances {
		t.Fatalf("the listing must report the effective value false: %+v", list)
	}

	// Back on: the next sync sees the appearance again.
	h.appearances.Store(true)
	result = syncBebe(t, h, follows.ID)
	if result.ReleasesSeen != 2 || result.AppearancesSkipped != 0 {
		t.Fatalf("after switching on: releases seen %d, skipped %d", result.ReleasesSeen, result.AppearancesSkipped)
	}
}

func TestOwnChoiceOverridesTheSettingBothWays(t *testing.T) {
	for _, global := range []bool{true, false} {
		h := newHarness(t, appearanceCatalogue())
		h.appearances.Store(global)

		sub := subscribeBebe(t, h, boolPtr(!global))
		if sub.IncludeAppearances == nil || *sub.IncludeAppearances == global {
			t.Fatalf("global %v: the own choice was not stored", global)
		}
		if sub.EffectiveIncludeAppearances != !global {
			t.Fatalf("global %v: effective %v, want %v", global, sub.EffectiveIncludeAppearances, !global)
		}

		result := syncBebe(t, h, sub.ID)
		wantSeen, wantSkipped := 2, 0
		if global { // the exception says "never"
			wantSeen, wantSkipped = 1, 1
		}
		if result.ReleasesSeen != wantSeen || result.AppearancesSkipped != wantSkipped {
			t.Fatalf("global %v: releases seen %d, skipped %d; want %d, %d",
				global, result.ReleasesSeen, result.AppearancesSkipped, wantSeen, wantSkipped)
		}
	}
}

func TestUpdateSetsAndClearsTheOwnChoice(t *testing.T) {
	h := newHarness(t, appearanceCatalogue())
	sub := subscribeBebe(t, h, nil)
	ctx := context.Background()

	updated, err := h.service.Update(ctx, sub.ID,
		Update{IncludeAppearances: NullableBool{Set: true, Value: boolPtr(false)}})
	if err != nil {
		t.Fatalf("update to false: %v", err)
	}
	if updated.IncludeAppearances == nil || *updated.IncludeAppearances || updated.EffectiveIncludeAppearances {
		t.Fatalf("false was not stored: own %v, effective %v", updated.IncludeAppearances, updated.EffectiveIncludeAppearances)
	}

	// An update that does not name the field leaves the choice alone.
	updated, err = h.service.Update(ctx, sub.ID, Update{AutoDownload: boolPtr(true)})
	if err != nil {
		t.Fatalf("update without the field: %v", err)
	}
	if updated.IncludeAppearances == nil || *updated.IncludeAppearances {
		t.Fatal("an update without include_appearances changed it")
	}

	// Null puts it back to following the setting.
	updated, err = h.service.Update(ctx, sub.ID, Update{IncludeAppearances: NullableBool{Set: true}})
	if err != nil {
		t.Fatalf("update to null: %v", err)
	}
	if updated.IncludeAppearances != nil || !updated.EffectiveIncludeAppearances {
		t.Fatalf("null did not clear the choice: own %v, effective %v", updated.IncludeAppearances, updated.EffectiveIncludeAppearances)
	}

	// A PATCH that names only include_appearances (even as null) is not empty.
	if (Update{IncludeAppearances: NullableBool{Set: true}}).Empty() {
		t.Fatal("an explicit null must count as a change")
	}
	if !(Update{}).Empty() {
		t.Fatal("an update naming nothing must be empty")
	}
}

func TestExportWritesTheOwnChoiceOnly(t *testing.T) {
	h := newHarness(t, appearanceCatalogue())
	subscribeBebe(t, h, boolPtr(false))
	if _, err := h.service.Create(context.Background(), NewSubscription{
		Provider: "fake", ArtistSourceID: "542", ArtistName: "David Guetta",
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	payload, err := h.service.Export(context.Background())
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if payload.Version != ExportFormatVersion || ExportFormatVersion != 1 {
		t.Fatalf("the export format version must stay 1, got %d", payload.Version)
	}
	for _, item := range payload.Subscriptions {
		raw, err := json.Marshal(item)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		switch item.ArtistSourceID {
		case "4492087":
			if !strings.Contains(string(raw), `"include_appearances":false`) {
				t.Fatalf("the export does not carry the own choice: %s", raw)
			}
		case "542":
			if strings.Contains(string(raw), `include_appearances`) {
				t.Fatalf("a subscription following the setting must not write the field: %s", raw)
			}
		}
	}
}

func TestImportReadsTheOwnChoiceWithAndWithoutTheField(t *testing.T) {
	h := newHarness(t, appearanceCatalogue())

	// The first item comes from a file written before the field existed, the
	// second carries an explicit null, the third an own choice.
	raw := `{"format":"ytmdl-subscriptions","version":1,"exported_at":"2026-01-01T00:00:00Z",
		"subscriptions":[
			{"artist_name":"Old","provider":"fake","artist_source_id":"1","enabled":true,
			 "release_filter":{"albums":true},"download_priority":"low"},
			{"artist_name":"Null","provider":"fake","artist_source_id":"3","enabled":true,
			 "release_filter":{"albums":true},"download_priority":"low","include_appearances":null},
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
	if preview.Items[0].IncludeAppearances != nil || preview.Items[1].IncludeAppearances != nil ||
		!sameOptionalBool(preview.Items[2].IncludeAppearances, boolPtr(false)) {
		t.Fatalf("preview read the field wrongly: %+v", preview.Items)
	}

	if _, err := h.service.ApplyImport(context.Background(), payload); err != nil {
		t.Fatalf("apply: %v", err)
	}
	old, _ := h.store.FindBySource(context.Background(), "fake", "1")
	null, _ := h.store.FindBySource(context.Background(), "fake", "3")
	newer, _ := h.store.FindBySource(context.Background(), "fake", "2")
	if old == nil || null == nil || newer == nil {
		t.Fatal("the import did not create all subscriptions")
	}
	if old.IncludeAppearances != nil || null.IncludeAppearances != nil {
		t.Fatal("absent and null must import as following the setting")
	}
	if !sameOptionalBool(newer.IncludeAppearances, boolPtr(false)) {
		t.Fatal("include_appearances=false was not imported")
	}

	// Re-importing the old file over a subscription that set its own choice
	// is an update, and says so.
	if _, err := h.service.Update(context.Background(), old.ID,
		Update{IncludeAppearances: NullableBool{Set: true, Value: boolPtr(false)}}); err != nil {
		t.Fatalf("update: %v", err)
	}
	preview, err = h.service.PreviewImport(context.Background(), payload)
	if err != nil {
		t.Fatalf("second preview: %v", err)
	}
	if preview.Items[0].Status != ImportStatusWouldUpdate {
		t.Fatalf("the changed choice was not detected: %+v", preview.Items[0])
	}
	if got := strings.Join(preview.Items[0].Changes, ","); !strings.Contains(got, "include_appearances: false -> setting") {
		t.Fatalf("the change is not described: %q", got)
	}
	if preview.Items[1].Status != ImportStatusUnchanged || preview.Items[2].Status != ImportStatusUnchanged {
		t.Fatalf("unchanged subscriptions were reported as %q and %q", preview.Items[1].Status, preview.Items[2].Status)
	}

	if _, err := h.service.ApplyImport(context.Background(), payload); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	old, _ = h.store.FindBySource(context.Background(), "fake", "1")
	if old.IncludeAppearances != nil {
		t.Fatal("the import must put the subscription back to following the setting")
	}
}
