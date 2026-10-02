package repository_test

import (
	"context"
	"testing"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/playlist"
)

func TestLibraryToolsUserIsolationAtomicityAndDynamicRules(t *testing.T) {
	db, ps, u1, u2, ids := setupPlaylistTest(t)
	ctx := context.Background()
	c := repository.NewCatalog(db)
	// Idempotent event retries and counts are private to each listener.
	for _, id := range []string{"event-fixture-00000001", "event-fixture-00000001", "event-fixture-00000002"} {
		if err := c.RecordPlayback(ctx, u1, id, ids[0]); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.RecordPlayback(ctx, u2, "event-fixture-00000001", ids[1]); err != nil {
		t.Fatal(err)
	}
	h, err := c.ListeningHistory(ctx, u1, "frequent")
	if err != nil || len(h) != 1 || h[0].PlayCount != 2 {
		t.Fatalf("history count: %#v %v", h, err)
	}
	other, err := c.ListeningHistory(ctx, u2, "recent")
	if err != nil || len(other) != 1 || other[0].ID != ids[1] {
		t.Fatal("history leaked")
	}
	// Playlist resolves favorites afresh, and mutations cannot bypass rules or ownership.
	svc, _ := playlist.New(playlist.Options{Store: ps})
	rules := &repository.SmartRules{Favorites: true, Limit: 100, Sort: "frequent"}
	p, err := svc.CreatePlaylist(ctx, u1, "Smart", "", rules)
	if err != nil {
		t.Fatal(err)
	}
	if err = ps.FavoriteTrack(ctx, u1, ids[0]); err != nil {
		t.Fatal(err)
	}
	detail, err := svc.GetPlaylist(ctx, u1, p.ID)
	if err != nil || detail.TrackCount != 1 || detail.Tracks[0].ID != ids[0] {
		t.Fatalf("smart: %#v %v", detail, err)
	}
	if err = ps.FavoriteTrack(ctx, u1, ids[1]); err != nil {
		t.Fatal(err)
	}
	detail, err = svc.GetPlaylist(ctx, u1, p.ID)
	if err != nil || len(detail.Tracks) != 2 {
		t.Fatal("smart playlist stale", err)
	}
	if _, err = ps.AddTracks(ctx, u1, p.ID, ids); err == nil {
		t.Fatal("smart membership mutation accepted")
	}
	if err = svc.SetSmartRules(ctx, u2, p.ID, nil); err == nil {
		t.Fatal("foreign rules mutation accepted")
	}
	manual, err := ps.CreatePlaylist(ctx, u1, "Bulk", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ps.AddTracks(ctx, u1, manual.ID, []string{ids[0], "missing"}); err == nil {
		t.Fatal("invalid bulk accepted")
	}
	empty, _ := ps.GetPlaylistForUser(ctx, u1, manual.ID)
	if len(empty.Tracks) != 0 {
		t.Fatal("bulk write partially applied")
	}
	detail, err = ps.AddTracks(ctx, u1, manual.ID, []string{ids[1], ids[0], ids[1]})
	if err != nil || len(detail.Tracks) != 2 || detail.Tracks[0].ID != ids[1] || detail.Tracks[1].Position != 2 {
		t.Fatalf("bulk order/idempotency: %#v %v", detail, err)
	}
	// Manual overrides survive ordinary provider writes and never touch audio file records.
	album := "User album"
	year := 2026
	patch := repository.MetadataPatch{Album: &album, Year: &year}
	if err = c.UpdateTrackMetadata(ctx, []string{ids[0], "missing"}, patch); err == nil {
		t.Fatal("invalid patch accepted")
	}
	before, _ := c.GetLibraryTrackDetail(ctx, ids[0])
	if before.Track.Album == album {
		t.Fatal("partial metadata patch")
	}
	if err = c.UpdateTrackMetadata(ctx, []string{ids[0]}, patch); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "UPDATE tracks SET album='Provider album' WHERE id=$1", ids[0]); err != nil {
		t.Fatal(err)
	}
	tracks, _, err := c.ListTracksFiltered(ctx, repository.TrackListFilter{IDs: []string{ids[0]}})
	if err != nil || len(tracks) != 1 || tracks[0].Album != album || tracks[0].Year != 2026 {
		t.Fatal("override lost", err)
	}
	favorite, err := ps.ListFavoriteTracks(ctx, u1)
	if err != nil || favorite[0].Album != album && favorite[1].Album != album {
		t.Fatal("favorite metadata inconsistent", err)
	}
	// Duplicate candidates include recording versions but only tracks with a physical file.
	if _, err = db.ExecContext(ctx, "UPDATE tracks SET title='Same',album_artist='Same artist' WHERE id IN ($1,$2)", ids[0], ids[1]); err != nil {
		t.Fatal(err)
	}
	groups, err := c.DuplicateGroups(ctx, 0)
	if err != nil || len(groups) != 1 || groups[0].Count != 2 || len(groups[0].Tracks) != 2 {
		t.Fatalf("duplicates: %#v %v", groups, err)
	}
	// Artwork/loudness persist and stale file measurements do not apply.
	if err = c.SetCustomArtwork(ctx, "artists", "art_pl", []byte("image fixture")); err != nil {
		t.Fatal(err)
	}
	data, _, err := c.CustomArtwork(ctx, "artists", "art_pl")
	if err != nil || len(data) == 0 {
		t.Fatal("artwork lost")
	}
	l := repository.Loudness{GainDB: -5, IntegratedLUFS: -11, TruePeakDB: -2}
	if err = c.SetLoudness(ctx, ids[0], "fil_"+ids[0], 100, 42, l); err != nil {
		t.Fatal(err)
	}
	cached, _ := c.Loudness(ctx, ids[0], "fil_"+ids[0], 100, 42)
	stale, _ := c.Loudness(ctx, ids[0], "fil_"+ids[0], 100, 43)
	if cached == nil || stale != nil {
		t.Fatal("stale loudness applied")
	}
	if err = c.ClearListeningHistory(ctx, u1); err != nil {
		t.Fatal(err)
	}
	h, _ = c.ListeningHistory(ctx, u1, "")
	other, _ = c.ListeningHistory(ctx, u2, "")
	if len(h) != 0 || len(other) != 1 {
		t.Fatal("history clear affected other user")
	}
}
