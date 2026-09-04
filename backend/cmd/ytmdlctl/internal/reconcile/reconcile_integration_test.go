package reconcile_test

import (
	"context"
	"testing"
	"time"

	"ytdm/backend/cmd/ytmdlctl/internal/reconcile"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/music"
)

func TestReconcile_RealDuplicateFixtures_E2E(t *testing.T) {
	db := dbtest.Open(t)
	catalog := repository.NewCatalog(db)
	ctx := context.Background()
	sqlDB := db.DB

	// =========================================================================
	// 1. SEED REAL DUPLICATE SCENARIOS
	// =========================================================================

	// --- A. Alan Walker: Proved duplicate (Real Deezer ID + Synthetic Worker Row) ---
	alanCanonical, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:      "Alan Walker",
		Provider:  "deezer",
		SourceID:  "288164",
		SourceURL: "https://www.deezer.com/artist/288164",
		ImageURL:  "https://e-cdns-images.dzcdn.net/images/artist/alan-canonical.jpg",
	})
	if err != nil {
		t.Fatalf("upsert alanCanonical failed: %v", err)
	}
	// Add subscription
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO artist_subscriptions (id, provider, artist_source_id, artist_name, artist_image_url, next_sync_at, enabled, created_at, updated_at) VALUES ('sub_alan', 'deezer', '288164', 'Alan Walker', 'https://e-cdns-images.dzcdn.net/images/artist/alan-canonical.jpg', NOW(), true, NOW(), NOW())`); err != nil {
		t.Fatalf("insert sub_alan failed: %v", err)
	}
	// Add release & tracks to canonical
	relAlan1, err := catalog.UpsertRelease(ctx, music.Release{
		Title:    "Faded (Album)",
		Provider: "deezer",
		SourceID: "rel_alan_1",
	}, alanCanonical.ID)
	if err != nil {
		t.Fatalf("upsert relAlan1 failed: %v", err)
	}
	_, _ = catalog.UpsertTrack(ctx, music.Track{
		Title:          "Faded",
		SourceProvider: "deezer",
		SourceID:       "trk_alan_1",
	}, relAlan1.ID, alanCanonical.ID, 0)
	_, _ = catalog.UpsertTrack(ctx, music.Track{
		Title:          "Sing Me to Sleep",
		SourceProvider: "deezer",
		SourceID:       "trk_alan_2",
	}, relAlan1.ID, alanCanonical.ID, 0)

	// 2. Duplicate Deezer Alan Walker (Synthetic worker row, split release, 1 track)
	now := time.Now().UTC()
	var alanDupID string
	err = sqlDB.QueryRowContext(ctx, `
		INSERT INTO artists (id, name, provider, source_id, source_url, image_url, created_at, updated_at)
		VALUES ('art_alan_dup', 'Alan Walker', 'deezer', 'artist:alan-walker', '', 'https://img/alt-alan.jpg', $1, $1)
		RETURNING id`, now.Add(time.Hour)).Scan(&alanDupID)
	if err != nil {
		t.Fatalf("insert alan duplicate failed: %v", err)
	}
	relAlanDup, err := catalog.UpsertRelease(ctx, music.Release{
		Title:    "Alone (Single)",
		Provider: "deezer",
		SourceID: "rel_alan_dup",
	}, alanDupID)
	if err != nil {
		t.Fatalf("upsert relAlanDup failed: %v", err)
	}
	_, _ = catalog.UpsertTrack(ctx, music.Track{
		Title:          "Alone",
		SourceProvider: "deezer",
		SourceID:       "trk_alan_dup",
	}, relAlanDup.ID, alanDupID, 0)

	// --- B. Apache 207: Proved duplicate (Real Deezer ID + Synthetic Worker Row) ---
	apacheCanonical, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:      "Apache 207",
		Provider:  "deezer",
		SourceID:  "14878271",
		SourceURL: "https://www.deezer.com/artist/14878271",
		ImageURL:  "https://e-cdns-images.dzcdn.net/images/artist/apache.jpg",
	})
	if err != nil {
		t.Fatalf("upsert apacheCanonical failed: %v", err)
	}
	relApache1, _ := catalog.UpsertRelease(ctx, music.Release{
		Title:    "Platte",
		Provider: "deezer",
		SourceID: "rel_apache_1",
	}, apacheCanonical.ID)
	_, _ = catalog.UpsertTrack(ctx, music.Track{
		Title:          "Roller",
		SourceProvider: "deezer",
		SourceID:       "trk_apache_1",
	}, relApache1.ID, apacheCanonical.ID, 0)

	// Duplicate Apache 207 synthetic row
	var apacheDupID string
	err = sqlDB.QueryRowContext(ctx, `
		INSERT INTO artists (id, name, provider, source_id, source_url, image_url, created_at, updated_at)
		VALUES ('art_apache_dup', 'Apache 207', 'deezer', 'artist:apache-207', '', '', $1, $1)
		RETURNING id`, now.Add(2*time.Hour)).Scan(&apacheDupID)
	if err != nil {
		t.Fatalf("insert apache duplicate failed: %v", err)
	}
	relApacheDup, _ := catalog.UpsertRelease(ctx, music.Release{
		Title:    "200 km/h",
		Provider: "deezer",
		SourceID: "rel_apache_dup",
	}, apacheDupID)
	_, _ = catalog.UpsertTrack(ctx, music.Track{
		Title:          "200 km/h",
		SourceProvider: "deezer",
		SourceID:       "trk_apache_dup",
	}, relApacheDup.ID, apacheDupID, 0)

	// --- C. Ambiguous John Williams (Distinct real IDs on Deezer: Composer 1158 vs Guitarist 8740) ---
	jwComposer, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:      "John Williams",
		Provider:  "deezer",
		SourceID:  "1158",
		SourceURL: "https://www.deezer.com/artist/1158",
		ImageURL:  "https://img/jw-composer.jpg",
	})
	if err != nil {
		t.Fatalf("upsert jwComposer failed: %v", err)
	}
	jwGuitarist, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:      "John Williams",
		Provider:  "deezer",
		SourceID:  "8740",
		SourceURL: "https://www.deezer.com/artist/8740",
		ImageURL:  "https://img/jw-guitarist.jpg",
	})
	if err != nil {
		t.Fatalf("upsert jwGuitarist failed: %v", err)
	}

	// --- D. Ambiguous Cross-Provider Match (Taylor Swift on Deezer vs Spotify) ---
	tsDeezer, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Taylor Swift",
		Provider: "deezer",
		SourceID: "12246",
	})
	if err != nil {
		t.Fatalf("upsert tsDeezer failed: %v", err)
	}
	tsSpotify, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Taylor Swift",
		Provider: "spotify",
		SourceID: "06HL4z0CvFAxyc27GXpf02",
	})
	if err != nil {
		t.Fatalf("upsert tsSpotify failed: %v", err)
	}

	exec := &reconcile.SQLExecutor{DB: sqlDB}

	// Count initial rows
	var initialArtistCount, initialReleaseCount, initialTrackCount int
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists`).Scan(&initialArtistCount)
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM releases`).Scan(&initialReleaseCount)
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracks`).Scan(&initialTrackCount)

	// =========================================================================
	// 2. DRY RUN VERIFICATION (ZERO WRITES)
	// =========================================================================
	dryReport, err := reconcile.Run(ctx, exec, reconcile.Options{
		DryRun: true,
		Apply:  false,
	})
	if err != nil {
		t.Fatalf("Dry-run failed: %v", err)
	}

	if !dryReport.DryRun {
		t.Errorf("expected dryReport.DryRun = true")
	}
	if dryReport.ProvedClusters != 2 {
		t.Errorf("expected 2 proved clusters (Alan Walker, Apache 207), got %d", dryReport.ProvedClusters)
	}
	if dryReport.ProvedDups != 2 {
		t.Errorf("expected 2 proved duplicate rows, got %d", dryReport.ProvedDups)
	}
	if dryReport.AmbiguousClusters != 2 {
		t.Errorf("expected 2 ambiguous clusters (John Williams, Taylor Swift), got %d", dryReport.AmbiguousClusters)
	}
	if dryReport.MergedRows != 0 {
		t.Errorf("expected 0 merged rows in dry-run, got %d", dryReport.MergedRows)
	}
	if dryReport.ReassignedReleases != 2 {
		t.Errorf("expected 2 planned release reassignments, got %d", dryReport.ReassignedReleases)
	}
	if dryReport.ReassignedTracks != 2 {
		t.Errorf("expected 2 planned track reassignments, got %d", dryReport.ReassignedTracks)
	}

	// Assert EXACT ZERO WRITES in database after dry run
	var postDryArtistCount, postDryReleaseCount, postDryTrackCount int
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists`).Scan(&postDryArtistCount)
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM releases`).Scan(&postDryReleaseCount)
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracks`).Scan(&postDryTrackCount)

	if postDryArtistCount != initialArtistCount {
		t.Fatalf("DRY RUN MUTATED ARTISTS: before %d, after %d", initialArtistCount, postDryArtistCount)
	}
	if postDryReleaseCount != initialReleaseCount {
		t.Fatalf("DRY RUN MUTATED RELEASES: before %d, after %d", initialReleaseCount, postDryReleaseCount)
	}
	if postDryTrackCount != initialTrackCount {
		t.Fatalf("DRY RUN MUTATED TRACKS: before %d, after %d", initialTrackCount, postDryTrackCount)
	}

	// =========================================================================
	// 3. ACTUAL RECONCILIATION EXECUTION
	// =========================================================================
	mutatingReport, err := reconcile.Run(ctx, exec, reconcile.Options{
		DryRun: false,
		Apply:  true,
	})
	if err != nil {
		t.Fatalf("Mutating reconciliation failed: %v", err)
	}

	if mutatingReport.DryRun {
		t.Errorf("expected mutatingReport.DryRun = false")
	}
	if mutatingReport.MergedGroups != 2 {
		t.Errorf("expected 2 merged groups, got %d", mutatingReport.MergedGroups)
	}
	if mutatingReport.MergedRows != 2 {
		t.Errorf("expected 2 merged duplicate rows, got %d", mutatingReport.MergedRows)
	}
	if mutatingReport.ReassignedReleases != 2 {
		t.Errorf("expected 2 reassigned releases, got %d", mutatingReport.ReassignedReleases)
	}
	if mutatingReport.ReassignedTracks != 2 {
		t.Errorf("expected 2 reassigned tracks, got %d", mutatingReport.ReassignedTracks)
	}

	// =========================================================================
	// 4. POST-RECONCILIATION INTEGRITY & PRESERVATION VERIFICATION
	// =========================================================================

	// A. Alan Walker verification:
	// - art_alan_dup is DELETED
	// - alanCanonical is preserved with its real Deezer ID 288164
	// - relAlanDup and trk_alan_dup now belong to alanCanonical
	var countAlanDup int
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE id = $1`, alanDupID).Scan(&countAlanDup)
	if countAlanDup != 0 {
		t.Errorf("expected alan duplicate row to be deleted, found %d", countAlanDup)
	}
	var relCountAlan int
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM releases WHERE artist_id = $1`, alanCanonical.ID).Scan(&relCountAlan)
	if relCountAlan != 2 {
		t.Errorf("expected alan canonical to now own 2 releases, got %d", relCountAlan)
	}
	var trkCountAlan int
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracks WHERE artist_id = $1`, alanCanonical.ID).Scan(&trkCountAlan)
	if trkCountAlan != 3 {
		t.Errorf("expected alan canonical to now own 3 tracks, got %d", trkCountAlan)
	}

	// B. Apache 207 verification:
	var countApacheDup int
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE id = $1`, apacheDupID).Scan(&countApacheDup)
	if countApacheDup != 0 {
		t.Errorf("expected apache duplicate row to be deleted, found %d", countApacheDup)
	}
	var relCountApache int
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM releases WHERE artist_id = $1`, apacheCanonical.ID).Scan(&relCountApache)
	if relCountApache != 2 {
		t.Errorf("expected apache canonical to now own 2 releases, got %d", relCountApache)
	}

	// C. Ambiguous John Williams: BOTH rows preserved with their real IDs!
	var countJWComposer, countJWGuitarist int
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE id = $1`, jwComposer.ID).Scan(&countJWComposer)
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE id = $1`, jwGuitarist.ID).Scan(&countJWGuitarist)
	if countJWComposer != 1 || countJWGuitarist != 1 {
		t.Errorf("ambiguous John Williams rows damaged: composer=%d, guitarist=%d (both must be 1)", countJWComposer, countJWGuitarist)
	}

	// D. Ambiguous Taylor Swift: BOTH cross-provider rows preserved!
	var countTSDeezer, countTSSpotify int
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE id = $1`, tsDeezer.ID).Scan(&countTSDeezer)
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE id = $1`, tsSpotify.ID).Scan(&countTSSpotify)
	if countTSDeezer != 1 || countTSSpotify != 1 {
		t.Errorf("ambiguous Taylor Swift rows damaged: deezer=%d, spotify=%d (both must be 1)", countTSDeezer, countTSSpotify)
	}

	// E. Referential Integrity: ZERO dangling releases or tracks
	var danglingReleases, danglingTracks int
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM releases r LEFT JOIN artists a ON r.artist_id = a.id WHERE a.id IS NULL`).Scan(&danglingReleases)
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracks t LEFT JOIN artists a ON t.artist_id = a.id WHERE a.id IS NULL`).Scan(&danglingTracks)
	if danglingReleases != 0 {
		t.Errorf("found %d dangling releases with invalid artist_id", danglingReleases)
	}
	if danglingTracks != 0 {
		t.Errorf("found %d dangling tracks with invalid artist_id", danglingTracks)
	}

	// =========================================================================
	// 5. IDEMPOTENCY: SECOND RUN PRODUCES EXACTLY 0 MERGES
	// =========================================================================
	secondReport, err := reconcile.Run(ctx, exec, reconcile.Options{
		DryRun: false,
		Apply:  true,
	})
	if err != nil {
		t.Fatalf("Second reconciliation run failed: %v", err)
	}

	if secondReport.MergedGroups != 0 {
		t.Errorf("expected 0 merged groups on second run, got %d", secondReport.MergedGroups)
	}
	if secondReport.MergedRows != 0 {
		t.Errorf("expected 0 merged duplicate rows on second run, got %d", secondReport.MergedRows)
	}
	if secondReport.ProvedClusters != 0 {
		t.Errorf("expected 0 proved clusters on second run, got %d", secondReport.ProvedClusters)
	}
	// Ambiguous clusters remain detected and preserved
	if secondReport.AmbiguousClusters != 2 {
		t.Errorf("expected ambiguous clusters to remain 2, got %d", secondReport.AmbiguousClusters)
	}
}

func TestSQLExecutor_GetArtists(t *testing.T) {
	db := dbtest.Open(t)
	catalog := repository.NewCatalog(db)
	ctx := context.Background()

	a1, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Test Artist 1",
		Provider: "deezer",
		SourceID: "111",
		ImageURL: "https://img/1.jpg",
	})
	if err != nil {
		t.Fatalf("upsert a1 failed: %v", err)
	}
	a2, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Test Artist 2",
		Provider: "spotify",
		SourceID: "222",
		ImageURL: "https://img/2.jpg",
	})
	if err != nil {
		t.Fatalf("upsert a2 failed: %v", err)
	}

	exec := &reconcile.SQLExecutor{DB: db.DB}
	cands, err := exec.GetArtists(ctx, []string{a1.ID, a2.ID, "nonexistent"})
	if err != nil {
		t.Fatalf("GetArtists failed: %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(cands))
	}
	found := make(map[string]bool)
	for _, c := range cands {
		found[c.ID] = true
	}
	if !found[a1.ID] || !found[a2.ID] {
		t.Errorf("missing expected artists in %v", found)
	}
}
