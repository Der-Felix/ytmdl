package repository

import (
	"context"
	"strings"
	"testing"

	"ytdm/backend/internal/music"
)

// TestUpsertArtist_RepeatedImport verifies that importing the exact same provider
// and real source ID repeatedly is strictly idempotent and produces 1 row.
func TestUpsertArtist_RepeatedImport(t *testing.T) {
	db := openTestDB(t)
	catalog := NewCatalog(db)
	ctx := context.Background()

	artist := music.Artist{
		Name:      "Alan Walker",
		Provider:  "deezer",
		SourceID:  "288164",
		SourceURL: "https://www.deezer.com/artist/288164",
		ImageURL:  "https://e-cdns-images.dzcdn.net/images/artist/alan.jpg",
	}

	var firstID string
	for i := 0; i < 10; i++ {
		saved, err := catalog.UpsertArtist(ctx, artist)
		if err != nil {
			t.Fatalf("upsert iteration %d failed: %v", i, err)
		}
		if i == 0 {
			firstID = saved.ID
		} else if saved.ID != firstID {
			t.Fatalf("iteration %d produced new id %q, want %q", i, saved.ID, firstID)
		}
	}

	// Verify exactly 1 row in database
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE provider = 'deezer' AND source_id = '288164'`).Scan(&count)
	if err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 artist row, got %d", count)
	}
}

// TestUpsertArtist_RepeatedSynthetic verifies that repeated ingestion with the same
// provider and same synthetic key resolves idempotently to 1 row.
func TestUpsertArtist_RepeatedSynthetic(t *testing.T) {
	db := openTestDB(t)
	catalog := NewCatalog(db)
	ctx := context.Background()

	synthetic := music.Artist{
		Name:     "Alan Walker",
		Provider: "deezer",
		SourceID: "artist:alan-walker",
		ImageURL: "https://img.test/alan.jpg",
	}

	var firstID string
	for i := 0; i < 10; i++ {
		saved, err := catalog.UpsertArtist(ctx, synthetic)
		if err != nil {
			t.Fatalf("upsert iteration %d failed: %v", i, err)
		}
		if i == 0 {
			firstID = saved.ID
		} else if saved.ID != firstID {
			t.Fatalf("iteration %d produced new id %q, want %q", i, saved.ID, firstID)
		}
	}

	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE provider = 'deezer' AND source_id = 'artist:alan-walker'`).Scan(&count)
	if err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 artist row, got %d", count)
	}
}

// TestUpsertArtist_ArtworkPreservation verifies that an existing valid image_url
// is never overwritten by an empty string or whitespace.
func TestUpsertArtist_ArtworkPreservation(t *testing.T) {
	db := openTestDB(t)
	catalog := NewCatalog(db)
	ctx := context.Background()

	initial := music.Artist{
		Name:     "Alan Walker",
		Provider: "deezer",
		SourceID: "288164",
		ImageURL: "https://img.test/alan_highres.jpg",
	}

	saved, err := catalog.UpsertArtist(ctx, initial)
	if err != nil {
		t.Fatalf("initial upsert failed: %v", err)
	}

	// Re-upsert with empty image_url
	updateEmpty := music.Artist{
		Name:     "Alan Walker",
		Provider: "deezer",
		SourceID: "288164",
		ImageURL: "",
	}
	afterEmpty, err := catalog.UpsertArtist(ctx, updateEmpty)
	if err != nil {
		t.Fatalf("upsert with empty image failed: %v", err)
	}
	if afterEmpty.ImageURL != "https://img.test/alan_highres.jpg" {
		t.Fatalf("image_url was overwritten with empty string, got %q", afterEmpty.ImageURL)
	}

	// Re-upsert with whitespace-only image_url
	updateWhitespace := music.Artist{
		Name:     "Alan Walker",
		Provider: "deezer",
		SourceID: "288164",
		ImageURL: "   \t\n   ",
	}
	afterWhitespace, err := catalog.UpsertArtist(ctx, updateWhitespace)
	if err != nil {
		t.Fatalf("upsert with whitespace image failed: %v", err)
	}
	if afterWhitespace.ImageURL != "https://img.test/alan_highres.jpg" {
		t.Fatalf("image_url was overwritten with whitespace string, got %q", afterWhitespace.ImageURL)
	}

	// Query DB directly to verify persistence
	var dbImage string
	err = db.QueryRowContext(ctx, `SELECT image_url FROM artists WHERE id = $1`, saved.ID).Scan(&dbImage)
	if err != nil {
		t.Fatalf("failed to query image_url: %v", err)
	}
	if dbImage != "https://img.test/alan_highres.jpg" {
		t.Fatalf("database image_url was overwritten, got %q", dbImage)
	}
}

// TestUpsertArtist_SameNameDifferentArtist_Preserved is the MANDATORY same-provider negative test.
// Two distinct artists with the same display name ("John Williams") but different
// real provider IDs on the same provider must remain two separate canonical rows.
func TestUpsertArtist_SameNameDifferentArtist_Preserved(t *testing.T) {
	db := openTestDB(t)
	catalog := NewCatalog(db)
	ctx := context.Background()

	composer := music.Artist{
		Name:     "John Williams",
		Provider: "deezer",
		SourceID: "1158", // Film composer
		ImageURL: "https://img.test/composer.jpg",
	}
	guitarist := music.Artist{
		Name:     "John Williams",
		Provider: "deezer",
		SourceID: "8740", // Classical guitarist
		ImageURL: "https://img.test/guitarist.jpg",
	}

	savedA, err := catalog.UpsertArtist(ctx, composer)
	if err != nil {
		t.Fatalf("failed to upsert composer: %v", err)
	}
	savedB, err := catalog.UpsertArtist(ctx, guitarist)
	if err != nil {
		t.Fatalf("failed to upsert guitarist: %v", err)
	}

	if savedA.ID == savedB.ID {
		t.Fatalf("FATAL: Distinct artists with same name were incorrectly merged! Both got ID %q", savedA.ID)
	}

	// Verify 2 distinct rows in database
	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE name = 'John Williams'`).Scan(&count)
	if err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 distinct rows for John Williams, got %d", count)
	}

	// Verify ListArtistsFiltered returns 2 distinct cards
	artists, total, err := catalog.ListArtistsFiltered(ctx, ArtistListFilter{Query: "John Williams"})
	if err != nil {
		t.Fatalf("ListArtistsFiltered failed: %v", err)
	}
	if total != 2 || len(artists) != 2 {
		t.Fatalf("expected 2 artists in list, got total=%d len=%d", total, len(artists))
	}
	if artists[0].ID == artists[1].ID {
		t.Fatalf("ListArtistsFiltered returned duplicate IDs")
	}
}

// TestUpsertArtist_CrossProviderSameName_Preserved is the MANDATORY cross-provider negative test.
// Section 8:
// Deezer John Williams (real ID deezer-real-A) vs Spotify John Williams (real ID spotify-real-B):
// -> TWO artists.
// Deezer John Williams (artist:john-williams) vs Spotify John Williams (artist:john-williams):
// -> Without stronger proof, MUST NOT be automatically merged cross-provider!
func TestUpsertArtist_CrossProviderSameName_Preserved(t *testing.T) {
	db := openTestDB(t)
	catalog := NewCatalog(db)
	ctx := context.Background()

	// 1. Cross-provider with real provider IDs
	artDeezerReal, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "John Williams",
		Provider: "deezer",
		SourceID: "deezer-real-A",
	})
	if err != nil {
		t.Fatalf("failed to upsert deezer real: %v", err)
	}

	artSpotifyReal, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "John Williams",
		Provider: "spotify",
		SourceID: "spotify-real-B",
	})
	if err != nil {
		t.Fatalf("failed to upsert spotify real: %v", err)
	}

	if artDeezerReal.ID == artSpotifyReal.ID {
		t.Fatalf("FATAL: Cross-provider real IDs were incorrectly merged! %q == %q", artDeezerReal.ID, artSpotifyReal.ID)
	}

	// 2. Cross-provider with synthetic keys and same display name
	artDeezerSynth, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Synthesized Artist",
		Provider: "deezer",
		SourceID: "artist:synthesized-artist",
	})
	if err != nil {
		t.Fatalf("failed to upsert deezer synthetic: %v", err)
	}

	artSpotifySynth, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Synthesized Artist",
		Provider: "spotify",
		SourceID: "artist:synthesized-artist",
	})
	if err != nil {
		t.Fatalf("failed to upsert spotify synthetic: %v", err)
	}

	if artDeezerSynth.ID == artSpotifySynth.ID {
		t.Fatalf("FATAL: Cross-provider synthetic keys were merged without proof! %q == %q", artDeezerSynth.ID, artSpotifySynth.ID)
	}

	// Verify both exist as separate rows in the database
	var synthCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE name = 'Synthesized Artist'`).Scan(&synthCount)
	if err != nil {
		t.Fatalf("query synthCount failed: %v", err)
	}
	if synthCount != 2 {
		t.Fatalf("expected 2 distinct rows for Synthesized Artist, got %d", synthCount)
	}

	// Run ReconcileDuplicateArtists: must NOT merge cross-provider synthetic rows
	report, err := catalog.ReconcileDuplicateArtists(ctx)
	if err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}
	if report.MergedCount != 0 {
		t.Fatalf("expected 0 merges for cross-provider synthetic rows, got %d", report.MergedCount)
	}

	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE name = 'Synthesized Artist'`).Scan(&synthCount)
	if err != nil {
		t.Fatalf("query synthCount after reconcile failed: %v", err)
	}
	if synthCount != 2 {
		t.Fatalf("expected 2 distinct rows for Synthesized Artist after reconcile, got %d", synthCount)
	}
}

// TestMergeArtists_DefensiveValidation verifies that:
// 1. Attempting to merge a duplicate with a distinct real provider ID is refused.
// 2. Merging a synthetic duplicate row into canonical succeeds atomically.
func TestMergeArtists_DefensiveValidation(t *testing.T) {
	db := openTestDB(t)
	catalog := NewCatalog(db)
	ctx := context.Background()

	// Canonical artist with real ID
	canonical, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "John Williams",
		Provider: "deezer",
		SourceID: "1158",
		ImageURL: "https://img.test/canonical.jpg",
	})
	if err != nil {
		t.Fatalf("failed to create canonical: %v", err)
	}

	// Duplicate with distinct real ID on same provider
	realDup, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "John Williams",
		Provider: "deezer",
		SourceID: "8740",
	})
	if err != nil {
		t.Fatalf("failed to create realDup: %v", err)
	}

	// Defensive check: MergeArtists MUST refuse to merge realDup into canonical!
	err = catalog.MergeArtists(ctx, canonical.ID, []string{realDup.ID})
	if err == nil {
		t.Fatalf("FATAL: MergeArtists allowed merging a duplicate with a distinct real provider ID!")
	}
	if !strings.Contains(err.Error(), "cannot merge artist") {
		t.Fatalf("unexpected error message: %v", err)
	}

	// Both artists must still exist in DB
	var realCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE id IN ($1, $2)`, canonical.ID, realDup.ID).Scan(&realCount)
	if err != nil || realCount != 2 {
		t.Fatalf("real provider rows were deleted despite error! count=%d, err=%v", realCount, err)
	}

	// Duplicate with synthetic ID on same provider
	synthDup, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "John Williams",
		Provider: "deezer",
		SourceID: "artist:john-williams",
		ImageURL: "https://img.test/better_artwork.jpg",
	})
	if err != nil {
		t.Fatalf("failed to create synthDup: %v", err)
	}

	// Merging synthetic duplicate into canonical MUST succeed
	err = catalog.MergeArtists(ctx, canonical.ID, []string{synthDup.ID})
	if err != nil {
		t.Fatalf("MergeArtists failed for synthetic duplicate: %v", err)
	}

	// Verify synthetic duplicate is deleted
	var remaining int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE id = $1`, synthDup.ID).Scan(&remaining)
	if err != nil || remaining != 0 {
		t.Fatalf("expected synthetic duplicate to be deleted, got %d", remaining)
	}
}

// TestReconcileDuplicateArtists_ProvedSameProviderDuplicate verifies that ReconcileDuplicateArtists
// merges proved duplicates on the same provider (real ID + synthetic key), while keeping
// ambiguous records untouched, and is completely idempotent on subsequent runs.
func TestReconcileDuplicateArtists_ProvedSameProviderDuplicate(t *testing.T) {
	db := openTestDB(t)
	catalog := NewCatalog(db)
	ctx := context.Background()

	// 1. Canonical Deezer Alan Walker (real ID)
	realAlan, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Alan Walker",
		Provider: "deezer",
		SourceID: "288164",
		ImageURL: "https://img.test/alan_sub.jpg",
	})
	if err != nil {
		t.Fatalf("failed to upsert realAlan: %v", err)
	}

	// 2. Synthetic Deezer Alan Walker (pre-existing worker duplicate on same provider)
	synthAlanDeezer := music.Artist{
		ID:       music.NewID(),
		Name:     "Alan Walker",
		Provider: "deezer",
		SourceID: "artist:alan-walker",
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO artists (id, name, sort_key, provider, source_id, image_url, created_at, updated_at)
		VALUES ($1, $2, 'alan walker', $3, $4, '', NOW(), NOW())`,
		synthAlanDeezer.ID, synthAlanDeezer.Name, synthAlanDeezer.Provider, synthAlanDeezer.SourceID)
	if err != nil {
		t.Fatalf("failed to insert synthAlanDeezer: %v", err)
	}
	_ = catalog.AddArtistSource(ctx, music.ArtistSource{
		ID:         music.NewID(),
		ArtistID:   synthAlanDeezer.ID,
		Provider:   synthAlanDeezer.Provider,
		SourceKind: music.SourceKindLegacySynthetic,
		SourceID:   synthAlanDeezer.SourceID,
	})

	// Seed subscription for Alan Walker on Deezer
	_, err = db.ExecContext(ctx, `
		INSERT INTO artist_subscriptions (id, provider, artist_source_id, artist_name, artist_image_url, next_sync_at, created_at, updated_at)
		VALUES ('sub_aw', 'deezer', '288164', 'Alan Walker', 'https://img.test/alan_sub.jpg', NOW(), NOW(), NOW())
		ON CONFLICT (provider, artist_source_id) DO NOTHING`)
	if err != nil {
		t.Fatalf("failed to insert subscription: %v", err)
	}

	// 3. Spotify Alan Walker (cross-provider, must NOT be merged into Deezer)
	spotifyAlan, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Alan Walker",
		Provider: "spotify",
		SourceID: "artist:alan-walker",
	})
	if err != nil {
		t.Fatalf("failed to upsert spotifyAlan: %v", err)
	}

	// Also seed the ambiguous John Williams rows (distinct real IDs on same provider)
	_, err = catalog.UpsertArtist(ctx, music.Artist{
		Name:     "John Williams",
		Provider: "deezer",
		SourceID: "1158",
	})
	if err != nil {
		t.Fatalf("failed to upsert john williams 1: %v", err)
	}
	_, err = catalog.UpsertArtist(ctx, music.Artist{
		Name:     "John Williams",
		Provider: "deezer",
		SourceID: "8740",
	})
	if err != nil {
		t.Fatalf("failed to upsert john williams 2: %v", err)
	}

	// Run 1: Reconcile duplicate artists
	report1, err := catalog.ReconcileDuplicateArtists(ctx)
	if err != nil {
		t.Fatalf("ReconcileDuplicateArtists run 1 failed: %v", err)
	}

	// synthAlanDeezer should have been merged into realAlan (MergedCount == 1)
	if report1.MergedCount != 1 {
		t.Fatalf("expected MergedCount = 1 on run 1, got %d", report1.MergedCount)
	}

	// Verify realAlan remains
	var countReal int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE id = $1`, realAlan.ID).Scan(&countReal)
	if err != nil || countReal != 1 {
		t.Fatalf("realAlan missing: count=%d, err=%v", countReal, err)
	}

	// Verify synthAlanDeezer is deleted
	var countSynth int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE id = $1`, synthAlanDeezer.ID).Scan(&countSynth)
	if err != nil || countSynth != 0 {
		t.Fatalf("synthAlanDeezer was not deleted: count=%d", countSynth)
	}

	// Verify spotifyAlan is UNTOUCHED (cross-provider identity preserved!)
	var countSpotify int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE id = $1`, spotifyAlan.ID).Scan(&countSpotify)
	if err != nil || countSpotify != 1 {
		t.Fatalf("spotifyAlan was incorrectly deleted! count=%d", countSpotify)
	}

	// Verify John Williams is UNTOUCHED (2 distinct real rows on Deezer)
	var jwCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE name = 'John Williams'`).Scan(&jwCount)
	if err != nil || jwCount != 2 {
		t.Fatalf("expected 2 John Williams rows untouched, got %d", jwCount)
	}

	// Run 2: Idempotency check — must produce 0 merges
	report2, err := catalog.ReconcileDuplicateArtists(ctx)
	if err != nil {
		t.Fatalf("ReconcileDuplicateArtists run 2 failed: %v", err)
	}
	if report2.MergedCount != 0 {
		t.Fatalf("expected 0 merges on run 2 (idempotency violated), got %d", report2.MergedCount)
	}
}

// TestSubscriptionLinking_ExactFirstAndSameNameNegative is the MANDATORY subscription negative test.
// Section 15:
// Artist A: name = John Williams, source = real identity A (1158)
// Artist B: name = John Williams, source = real identity B (8740)
// Subscription belongs only to Artist A.
// Expected:
// Artist A: subscribed = true, gets artwork.
// Artist B: subscribed = false, must NOT inherit Artist A artwork!
func TestSubscriptionLinking_ExactFirstAndSameNameNegative(t *testing.T) {
	db := openTestDB(t)
	catalog := NewCatalog(db)
	ctx := context.Background()

	// Seed subscription ONLY for John Williams film composer (1158)
	_, err := db.ExecContext(ctx, `
		INSERT INTO artist_subscriptions (id, provider, artist_source_id, artist_name, artist_image_url, next_sync_at, created_at, updated_at)
		VALUES ('sub_jw_composer', 'deezer', '1158', 'John Williams', 'https://img.test/composer_art.jpg', NOW(), NOW(), NOW())`)
	if err != nil {
		t.Fatalf("failed to insert subscription: %v", err)
	}

	// Artist A: Film composer
	artA, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "John Williams",
		Provider: "deezer",
		SourceID: "1158",
	})
	if err != nil {
		t.Fatalf("failed to upsert artist A: %v", err)
	}

	// Artist B: Classical guitarist (different real ID, no subscription)
	artB, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "John Williams",
		Provider: "deezer",
		SourceID: "8740",
	})
	if err != nil {
		t.Fatalf("failed to upsert artist B: %v", err)
	}

	// Verify Artist A
	detailA, err := catalog.GetLibraryArtistDetail(ctx, artA.ID)
	if err != nil {
		t.Fatalf("GetLibraryArtistDetail artA failed: %v", err)
	}
	if !detailA.Subscribed {
		t.Fatalf("expected Artist A to be subscribed=true, got false")
	}
	if detailA.SubscriptionID != "sub_jw_composer" {
		t.Fatalf("Artist A subscription ID = %q, want 'sub_jw_composer'", detailA.SubscriptionID)
	}
	if detailA.Artist.ImageURL != "https://img.test/composer_art.jpg" {
		t.Fatalf("Artist A did not receive subscription artwork, got %q", detailA.Artist.ImageURL)
	}

	// Verify Artist B (MUST NOT BE SUBSCRIBED AND MUST NOT INHERIT ARTWORK)
	detailB, err := catalog.GetLibraryArtistDetail(ctx, artB.ID)
	if err != nil {
		t.Fatalf("GetLibraryArtistDetail artB failed: %v", err)
	}
	if detailB.Subscribed {
		t.Fatalf("FATAL: Artist B (guitarist) was incorrectly marked subscribed due to same name!")
	}
	if detailB.SubscriptionID != "" {
		t.Fatalf("FATAL: Artist B got subscription ID %q!", detailB.SubscriptionID)
	}
	if detailB.Artist.ImageURL != "" {
		t.Fatalf("FATAL: Artist B inherited Artist A artwork %q!", detailB.Artist.ImageURL)
	}
}

// TestSubscriptionLinking_LegacySyntheticFallback verifies Section 16:
// Safe unique candidate links; ambiguous same-name candidates do not link.
func TestSubscriptionLinking_LegacySyntheticFallback(t *testing.T) {
	db := openTestDB(t)
	catalog := NewCatalog(db)
	ctx := context.Background()

	// 1. Safe unique legacy candidate:
	// Subscription for "Unique Artist" on Deezer
	_, err := db.ExecContext(ctx, `
		INSERT INTO artist_subscriptions (id, provider, artist_source_id, artist_name, artist_image_url, next_sync_at, created_at, updated_at)
		VALUES ('sub_unique', 'deezer', 'unique_real_id', 'Unique Artist', 'https://img.test/unique.jpg', NOW(), NOW(), NOW())`)
	if err != nil {
		t.Fatalf("failed to insert subscription: %v", err)
	}

	// Exactly 1 synthetic artist in DB for "Unique Artist" on Deezer
	uniqueArtist, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Unique Artist",
		Provider: "deezer",
		SourceID: "artist:unique-artist",
	})
	if err != nil {
		t.Fatalf("failed to upsert uniqueArtist: %v", err)
	}

	detailUnique, err := catalog.GetLibraryArtistDetail(ctx, uniqueArtist.ID)
	if err != nil {
		t.Fatalf("GetLibraryArtistDetail failed: %v", err)
	}
	if !detailUnique.Subscribed {
		t.Fatalf("expected unique synthetic candidate to link to subscription, got false")
	}
	if detailUnique.SubscriptionID != "sub_unique" {
		t.Fatalf("expected subscription ID 'sub_unique', got %q", detailUnique.SubscriptionID)
	}
	if detailUnique.Artist.ImageURL != "https://img.test/unique.jpg" {
		t.Fatalf("expected inherited artwork, got %q", detailUnique.Artist.ImageURL)
	}

	// 2. Ambiguous same-name legacy candidates:
	// Subscription for "Ambiguous Artist"
	_, err = db.ExecContext(ctx, `
		INSERT INTO artist_subscriptions (id, provider, artist_source_id, artist_name, artist_image_url, next_sync_at, created_at, updated_at)
		VALUES ('sub_ambig', 'deezer', 'ambig_real_id', 'Ambiguous Artist', 'https://img.test/ambig.jpg', NOW(), NOW(), NOW())`)
	if err != nil {
		t.Fatalf("failed to insert subscription: %v", err)
	}

	// Two synthetic artists with the same name on Deezer (multiple candidates)
	ambig1, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Ambiguous Artist",
		Provider: "deezer",
		SourceID: "artist:ambiguous-artist-1",
	})
	if err != nil {
		t.Fatalf("failed to upsert ambig1: %v", err)
	}
	ambig2, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Ambiguous Artist",
		Provider: "deezer",
		SourceID: "artist:ambiguous-artist-2",
	})
	if err != nil {
		t.Fatalf("failed to upsert ambig2: %v", err)
	}

	// Neither ambig1 nor ambig2 should link because the pairing is ambiguous!
	detailAmbig1, err := catalog.GetLibraryArtistDetail(ctx, ambig1.ID)
	if err != nil {
		t.Fatalf("detail ambig1 failed: %v", err)
	}
	if detailAmbig1.Subscribed {
		t.Fatalf("FATAL: Ambiguous candidate 1 incorrectly linked to subscription!")
	}

	detailAmbig2, err := catalog.GetLibraryArtistDetail(ctx, ambig2.ID)
	if err != nil {
		t.Fatalf("detail ambig2 failed: %v", err)
	}
	if detailAmbig2.Subscribed {
		t.Fatalf("FATAL: Ambiguous candidate 2 incorrectly linked to subscription!")
	}
}

// TestPersistDownload_CrossProviderAlanWalker verifies Section 7:
// Downloading Alan Walker from Deezer and Spotify without explicit cross-provider proof
// MUST NOT auto-merge them merely because normalized names match.
func TestPersistDownload_CrossProviderAlanWalker(t *testing.T) {
	db := openTestDB(t)
	catalog := NewCatalog(db)
	ctx := context.Background()

	// 1. Download from Deezer
	artDeezer, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Alan Walker",
		Provider: "deezer",
		SourceID: "artist:alan-walker",
		ImageURL: "https://img.test/alan_deezer.jpg",
	})
	if err != nil {
		t.Fatalf("failed to upsert Deezer artist: %v", err)
	}

	relDeezer, err := catalog.PersistDownload(ctx, music.LibraryEntry{
		Artist: &artDeezer,
		Release: &music.Release{
			Title:       "Faded",
			AlbumArtist: "Alan Walker",
			Provider:    "deezer",
			SourceID:    "rel_dz_faded",
		},
		Track: music.Track{
			Title:          "Faded",
			AlbumArtist:    "Alan Walker",
			SourceProvider: "deezer",
			SourceID:       "tr_dz_faded",
			DurationMS:     212000,
		},
		File: music.File{Path: "Alan Walker/Faded/01.opus", Codec: "opus", SizeBytes: 4096},
	}, 4000)
	if err != nil {
		t.Fatalf("failed to persist Deezer download: %v", err)
	}

	// 2. Download from Spotify (synthetic key artist:alan-walker on Spotify)
	artSpotify, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Alan Walker",
		Provider: "spotify",
		SourceID: "artist:alan-walker",
	})
	if err != nil {
		t.Fatalf("failed to upsert Spotify artist: %v", err)
	}

	// Crucial assertion: without explicit cross-provider mapping, Deezer and Spotify
	// are two separate provider entities!
	if artSpotify.ID == artDeezer.ID {
		t.Fatalf("FATAL: Cross-provider downloads automatically merged without explicit proof!")
	}

	relSpotify, err := catalog.PersistDownload(ctx, music.LibraryEntry{
		Artist: &artSpotify,
		Release: &music.Release{
			Title:       "Alone",
			AlbumArtist: "Alan Walker",
			Provider:    "spotify",
			SourceID:    "rel_sp_alone",
		},
		Track: music.Track{
			Title:          "Alone",
			AlbumArtist:    "Alan Walker",
			SourceProvider: "spotify",
			SourceID:       "tr_sp_alone",
			DurationMS:     161000,
		},
		File: music.File{Path: "Alan Walker/Alone/01.opus", Codec: "opus", SizeBytes: 4096},
	}, 4000)
	if err != nil {
		t.Fatalf("failed to persist Spotify download: %v", err)
	}

	// Releases reference their respective provider's artist entity
	if relDeezer.ArtistID != artDeezer.ID {
		t.Fatalf("relDeezer artistID = %q, want %q", relDeezer.ArtistID, artDeezer.ID)
	}
	if relSpotify.ArtistID != artSpotify.ID {
		t.Fatalf("relSpotify artistID = %q, want %q", relSpotify.ArtistID, artSpotify.ID)
	}

	// Both artist rows exist in DB
	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE name = 'Alan Walker'`).Scan(&count)
	if err != nil || count != 2 {
		t.Fatalf("expected 2 separate artist rows in DB, got %d", count)
	}

	// ListArtistsFiltered returns 2 cards (one Deezer, one Spotify)
	artists, total, err := catalog.ListArtistsFiltered(ctx, ArtistListFilter{Query: "Alan Walker"})
	if err != nil {
		t.Fatalf("ListArtistsFiltered failed: %v", err)
	}
	if total != 2 || len(artists) != 2 {
		t.Fatalf("expected 2 distinct provider artists in library list, got total=%d len=%d", total, len(artists))
	}
}

// TestUpsertArtist_Apache207Collaboration verifies Section 18 & 19:
// Solo releases and collaboration releases with structured credits resolve to
// the single canonical Apache 207 entity on that provider, without creating
// compound pseudo-artists, while preserving full credits in album_artist.
func TestUpsertArtist_Apache207Collaboration(t *testing.T) {
	db := openTestDB(t)
	catalog := NewCatalog(db)
	ctx := context.Background()

	// Solo release on Deezer
	art1, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Apache 207",
		Provider: "deezer",
		SourceID: "artist:apache-207",
		ImageURL: "https://img.test/apache207.jpg",
	})
	if err != nil {
		t.Fatalf("failed to upsert Apache 207: %v", err)
	}

	rel1, err := catalog.PersistDownload(ctx, music.LibraryEntry{
		Artist: &art1,
		Release: &music.Release{
			Title:       "Roller",
			AlbumArtist: "Apache 207",
			Artists:     []string{"Apache 207"},
			Provider:    "deezer",
			SourceID:    "rel_roller",
		},
		Track: music.Track{
			Title:          "Roller",
			AlbumArtist:    "Apache 207",
			Artists:        []string{"Apache 207"},
			SourceProvider: "deezer",
			SourceID:       "tr_roller",
			DurationMS:     159000,
		},
		File: music.File{Path: "Apache 207/Roller/01.opus", Codec: "opus", SizeBytes: 4096},
	}, 4000)
	if err != nil {
		t.Fatalf("failed to persist Roller: %v", err)
	}

	// Collaboration release with structured credits (Primary artist: Apache 207)
	// Worker resolves primary artist as Apache 207 on Deezer
	artCollab, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Apache 207",
		Provider: "deezer",
		SourceID: "artist:apache-207",
	})
	if err != nil {
		t.Fatalf("failed to resolve artist for Madonna: %v", err)
	}
	if artCollab.ID != art1.ID {
		t.Fatalf("collaboration resolved to ID %q, want canonical %q", artCollab.ID, art1.ID)
	}

	rel2, err := catalog.PersistDownload(ctx, music.LibraryEntry{
		Artist: &artCollab,
		Release: &music.Release{
			Title:       "Madonna",
			AlbumArtist: "Apache 207 & Bausa",
			Artists:     []string{"Apache 207", "Bausa"},
			Provider:    "deezer",
			SourceID:    "rel_madonna",
		},
		Track: music.Track{
			Title:          "Madonna",
			AlbumArtist:    "Apache 207 & Bausa",
			Artists:        []string{"Apache 207", "Bausa"},
			SourceProvider: "deezer",
			SourceID:       "tr_madonna",
			DurationMS:     190000,
		},
		File: music.File{Path: "Apache 207/Madonna/01.opus", Codec: "opus", SizeBytes: 4096},
	}, 4000)
	if err != nil {
		t.Fatalf("failed to persist Madonna: %v", err)
	}

	// Verify both releases are linked to the canonical Apache 207 ID
	if rel1.ArtistID != art1.ID || rel2.ArtistID != art1.ID {
		t.Fatalf("releases not linked to canonical ID %q (rel1=%q, rel2=%q)", art1.ID, rel1.ArtistID, rel2.ArtistID)
	}

	// Verify exactly 1 artist in library for Apache 207
	artists, total, err := catalog.ListArtistsFiltered(ctx, ArtistListFilter{Query: "Apache 207"})
	if err != nil {
		t.Fatalf("ListArtistsFiltered failed: %v", err)
	}
	if total != 1 || len(artists) != 1 {
		t.Fatalf("expected 1 artist in library, got total=%d len=%d", total, len(artists))
	}
	if artists[0].ReleaseCount != 2 {
		t.Fatalf("expected ReleaseCount=2, got %d", artists[0].ReleaseCount)
	}

	// Verify NO compound pseudo-artist "Apache 207 & Bausa" exists in artists table
	var compoundCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE name ILIKE '%Bausa%'`).Scan(&compoundCount)
	if err != nil {
		t.Fatalf("failed to query compound artist: %v", err)
	}
	if compoundCount != 0 {
		t.Fatalf("compound pseudo-artist row was created in artists table! count=%d", compoundCount)
	}
}

// TestSchema9_LosslessMultiProviderArtistSources tests that Schema 9:
// 1. Decouples provider sources from artists table into artist_sources.
// 2. Allows multiple real and synthetic provider identities to attach to 1 canonical artist.
// 3. Preserves all provider sources losslessly during merges.
// 4. Maintains separation of distinct same-name artists on the same provider.
func TestSchema9_LosslessMultiProviderArtistSources(t *testing.T) {
	db := openTestDB(t)
	catalog := NewCatalog(db)
	ctx := context.Background()

	// 1. Create canonical Alan Walker with Deezer provider source
	alan, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:      "Alan Walker",
		Provider:  "deezer",
		SourceID:  "288164",
		SourceURL: "https://www.deezer.com/artist/288164",
		ImageURL:  "https://img.test/alan.jpg",
	})
	if err != nil {
		t.Fatalf("upsert canonical Alan Walker failed: %v", err)
	}

	// 2. Attach Spotify external source to the same canonical artist
	spotifySource := music.ArtistSource{
		ID:         music.NewID(),
		ArtistID:   alan.ID,
		Provider:   "spotify",
		SourceKind: music.SourceKindExternal,
		SourceID:   "7vk5e3vY1uw9plTHJAMQUN",
		SourceURL:  "https://open.spotify.com/artist/7vk5e3vY1uw9plTHJAMQUN",
	}
	if err := catalog.AddArtistSource(ctx, spotifySource); err != nil {
		t.Fatalf("add spotify source failed: %v", err)
	}

	// 3. Attach YouTube Music fallback source to the same canonical artist via UpsertArtist with explicit ID
	_, err = catalog.UpsertArtist(ctx, music.Artist{
		ID:        alan.ID,
		Name:      "Alan Walker",
		Provider:  "ytmusic",
		SourceID:  "artist:alan-walker",
		SourceURL: "https://music.youtube.com/channel/UCr0zYt7mF_y_X7Z7J0b2h1g",
	})
	if err != nil {
		t.Fatalf("attach ytmusic fallback source failed: %v", err)
	}

	// 4. Verify FindArtistBySource resolves each provider source to the exact same canonical ID
	byDeezer, err := catalog.FindArtistBySource(ctx, "deezer", "288164")
	if err != nil || byDeezer == nil || byDeezer.ID != alan.ID {
		t.Fatalf("FindArtistBySource(deezer) = %+v, want ID %s", byDeezer, alan.ID)
	}

	bySpotify, err := catalog.FindArtistBySource(ctx, "spotify", "7vk5e3vY1uw9plTHJAMQUN")
	if err != nil || bySpotify == nil || bySpotify.ID != alan.ID {
		t.Fatalf("FindArtistBySource(spotify) = %+v, want ID %s", bySpotify, alan.ID)
	}

	byYTMusic, err := catalog.FindArtistBySource(ctx, "ytmusic", "artist:alan-walker")
	if err != nil || byYTMusic == nil || byYTMusic.ID != alan.ID {
		t.Fatalf("FindArtistBySource(ytmusic) = %+v, want ID %s", byYTMusic, alan.ID)
	}

	// 5. Verify GetArtist retrieves all attached sources
	loaded, err := catalog.GetArtist(ctx, alan.ID)
	if err != nil {
		t.Fatalf("GetArtist failed: %v", err)
	}
	if len(loaded.Sources) != 3 {
		t.Fatalf("expected 3 attached sources on Alan Walker, got %d", len(loaded.Sources))
	}

	// 6. Test MergeArtists preserves external provider source from duplicate row
	tidalArtist, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:      "Alan Walker",
		Provider:  "tidal",
		SourceID:  "999888",
		SourceURL: "https://tidal.com/artist/999888",
	})
	if err != nil {
		t.Fatalf("upsert tidalArtist failed: %v", err)
	}

	// Merge tidalArtist into alan
	if err := catalog.MergeArtists(ctx, alan.ID, []string{tidalArtist.ID}); err != nil {
		t.Fatalf("MergeArtists failed: %v", err)
	}

	// Verify tidalArtist row in artists is deleted
	var tidalArtistCount int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE id = $1`, tidalArtist.ID).Scan(&tidalArtistCount)
	if tidalArtistCount != 0 {
		t.Fatalf("expected tidalArtist row to be deleted from artists, got %d", tidalArtistCount)
	}

	// Verify tidal source was transferred to alan and preserved in artist_sources!
	byTidal, err := catalog.FindArtistBySource(ctx, "tidal", "999888")
	if err != nil || byTidal == nil || byTidal.ID != alan.ID {
		t.Fatalf("FindArtistBySource(tidal) failed after merge: %+v, want alan.ID %s", byTidal, alan.ID)
	}

	// 7. Negative Test: John Williams film composer vs guitarist remain distinct entities
	jwFilm, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "John Williams",
		Provider: "deezer",
		SourceID: "1158",
	})
	if err != nil {
		t.Fatalf("upsert jwFilm failed: %v", err)
	}

	jwGuitar, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "John Williams",
		Provider: "deezer",
		SourceID: "8740",
	})
	if err != nil {
		t.Fatalf("upsert jwGuitar failed: %v", err)
	}

	if jwFilm.ID == jwGuitar.ID {
		t.Fatalf("John Williams film and guitar should have different IDs, got %s == %s", jwFilm.ID, jwGuitar.ID)
	}

	// MergeArtists must refuse to merge two distinct real provider IDs on the same provider
	if err := catalog.MergeArtists(ctx, jwFilm.ID, []string{jwGuitar.ID}); err == nil {
		t.Fatalf("expected MergeArtists to reject merging distinct real provider IDs on the same provider")
	}
}
