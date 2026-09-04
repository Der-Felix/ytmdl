package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/runner"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/music"
)

func TestCLI_MergeArtists_Usage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{"merge-artists", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, stderr.String())
	}

	code = runCLI(context.Background(), []string{"maintenance", "merge-artists", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, stderr.String())
	}

	code = runCLI(context.Background(), []string{"merge-artists"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "requires at least 2 artist IDs") {
		t.Errorf("stderr missing requires at least 2 artist IDs: %s", stderr.String())
	}

	stderr.Reset()
	code = runCLI(context.Background(), []string{"merge-artists", "id1"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "requires at least 2 artist IDs") {
		t.Errorf("stderr missing requires at least 2 artist IDs: %s", stderr.String())
	}
}

func TestCLI_MergeArtists_SelfMergeFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{"merge-artists", "art_123", "art_123"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "cannot merge canonical artist art_123 into itself") {
		t.Errorf("stderr missing self merge notice: %s", stderr.String())
	}
}

func TestCLI_MergeArtists_SafetyDistinctRealIDs(t *testing.T) {
	db, testURL := dbtest.OpenWithURL(t)
	catalog := repository.NewCatalog(db)
	ctx := context.Background()

	// Seed John Williams composer vs guitarist (distinct real Deezer IDs)
	jwComposer, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "John Williams",
		Provider: "deezer",
		SourceID: "1158",
		ImageURL: "https://img/jw1.jpg",
	})
	if err != nil {
		t.Fatalf("upsert jwComposer failed: %v", err)
	}
	jwGuitarist, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "John Williams",
		Provider: "deezer",
		SourceID: "8740",
		ImageURL: "https://img/jw2.jpg",
	})
	if err != nil {
		t.Fatalf("upsert jwGuitarist failed: %v", err)
	}

	tmpDir := t.TempDir()
	var stdout, stderr bytes.Buffer

	code := runCLIWithDeps(context.Background(), []string{
		"merge-artists",
		"--project-dir", tmpDir,
		"--db-url", testURL,
		"--apply",
		"-y",
		jwComposer.ID,
		jwGuitarist.ID,
	}, &stdout, &stderr, CLIDependencies{AllowDirectDBMutate: true})

	if code != 1 {
		t.Fatalf("code = %d, want 1; stdout: %s, stderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "distinct real IDs on the same provider represent separate catalog entities") {
		t.Errorf("stderr missing distinct real IDs rejection: %s", stderr.String())
	}

	// Verify neither artist was deleted
	var count1, count2 int
	_ = db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE id = $1`, jwComposer.ID).Scan(&count1)
	_ = db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE id = $1`, jwGuitarist.ID).Scan(&count2)
	if count1 != 1 || count2 != 1 {
		t.Fatalf("artists damaged: count1=%d, count2=%d", count1, count2)
	}
}

func TestCLI_MergeArtists_DirectDBMutateBlockedWithoutTestFlag(t *testing.T) {
	_, testURL := dbtest.OpenWithURL(t)
	tmpDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{
		"merge-artists",
		"--project-dir", tmpDir,
		"--db-url", testURL,
		"--apply",
		"-y",
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
	}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "mutating maintenance via --db-url is not permitted") {
		t.Errorf("stderr missing expected error: %s", stderr.String())
	}
}

func TestCLI_MergeArtists_DryRun_NoWrites(t *testing.T) {
	db, testURL := dbtest.OpenWithURL(t)
	catalog := repository.NewCatalog(db)
	ctx := context.Background()

	alanCanonical, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Alan Walker",
		Provider: "deezer",
		SourceID: "288164",
		ImageURL: "https://img/walker.jpg",
	})
	if err != nil {
		t.Fatalf("upsert alanCanonical failed: %v", err)
	}

	alanDup, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Alan Walker",
		Provider: "deezer",
		SourceID: "artist:alan-walker",
		ImageURL: "",
	})
	if err != nil {
		t.Fatalf("upsert alanDup failed: %v", err)
	}

	rel, err := catalog.UpsertRelease(ctx, music.Release{
		Title:    "Faded",
		Provider: "deezer",
		SourceID: "rel_1",
	}, alanDup.ID)
	if err != nil {
		t.Fatalf("upsert rel failed: %v", err)
	}
	_, err = catalog.UpsertTrack(ctx, music.Track{
		Title:          "Faded",
		SourceProvider: "deezer",
		SourceID:       "trk_1",
	}, rel.ID, alanDup.ID, 0)
	if err != nil {
		t.Fatalf("upsert track failed: %v", err)
	}

	tmpDir := t.TempDir()
	var stdout, stderr bytes.Buffer

	code := runCLI(context.Background(), []string{
		"merge-artists",
		"--project-dir", tmpDir,
		"--db-url", testURL,
		alanCanonical.ID,
		alanDup.ID,
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "DRY RUN (read-only preview, 0 writes)") {
		t.Errorf("missing DRY RUN banner: %s", out)
	}
	if !strings.Contains(out, "Releases:           1 to be repointed") {
		t.Errorf("missing planned releases: %s", out)
	}
	if !strings.Contains(out, "PREVIEW COMPLETE") {
		t.Errorf("missing PREVIEW COMPLETE: %s", out)
	}

	// Verify 0 writes made
	var dupCount int
	_ = db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE id = $1`, alanDup.ID).Scan(&dupCount)
	if dupCount != 1 {
		t.Fatalf("duplicate artist was deleted in dry-run!")
	}
}

func TestCLI_MergeArtists_RealPostgres_Mutating_E2E(t *testing.T) {
	db, testURL := dbtest.OpenWithURL(t)
	catalog := repository.NewCatalog(db)
	ctx := context.Background()

	// 1. Canonical artist with 1 release & track
	canonical, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Apache 207",
		Provider: "deezer",
		SourceID: "14878271",
		ImageURL: "https://img/apache.jpg",
	})
	if err != nil {
		t.Fatalf("upsert canonical failed: %v", err)
	}
	rel1, _ := catalog.UpsertRelease(ctx, music.Release{
		Title:    "Platte",
		Provider: "deezer",
		SourceID: "rel_1",
	}, canonical.ID)
	_, _ = catalog.UpsertTrack(ctx, music.Track{
		Title:          "Roller",
		SourceProvider: "deezer",
		SourceID:       "trk_1",
	}, rel1.ID, canonical.ID, 0)

	// 2. Duplicate synthetic artist with 1 release & track
	dup, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Apache 207",
		Provider: "deezer",
		SourceID: "artist:apache-207",
		ImageURL: "https://img/apache-alt.jpg",
	})
	if err != nil {
		t.Fatalf("upsert duplicate failed: %v", err)
	}
	rel2, _ := catalog.UpsertRelease(ctx, music.Release{
		Title:    "Treppenkind",
		Provider: "deezer",
		SourceID: "rel_2",
	}, dup.ID)
	_, _ = catalog.UpsertTrack(ctx, music.Track{
		Title:          "Fame",
		SourceProvider: "deezer",
		SourceID:       "trk_2",
	}, rel2.ID, dup.ID, 0)

	tmpDir := t.TempDir()
	var stdout, stderr bytes.Buffer

	code := runCLIWithDeps(context.Background(), []string{
		"merge-artists",
		"--project-dir", tmpDir,
		"--db-url", testURL,
		"--apply",
		"-y",
		canonical.ID,
		dup.ID,
	}, &stdout, &stderr, CLIDependencies{AllowDirectDBMutate: true})

	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "SUCCESS") {
		t.Errorf("missing SUCCESS in stdout: %s", out)
	}
	if !strings.Contains(out, "Merged Duplicates:    1") {
		t.Errorf("missing Merged Duplicates count: %s", out)
	}
	if !strings.Contains(out, "Reassigned Releases:  1") {
		t.Errorf("missing Reassigned Releases count: %s", out)
	}
	if !strings.Contains(out, "Reassigned Tracks:    1") {
		t.Errorf("missing Reassigned Tracks count: %s", out)
	}

	// Verify duplicate artist row deleted
	var dupCount int
	_ = db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE id = $1`, dup.ID).Scan(&dupCount)
	if dupCount != 0 {
		t.Fatalf("expected duplicate artist row to be deleted, got %d", dupCount)
	}

	// Verify canonical artist now owns both releases and both tracks
	var relCount, trkCount int
	_ = db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM releases WHERE artist_id = $1`, canonical.ID).Scan(&relCount)
	_ = db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracks WHERE artist_id = $1`, canonical.ID).Scan(&trkCount)
	if relCount != 2 {
		t.Errorf("expected canonical artist to own 2 releases, got %d", relCount)
	}
	if trkCount != 2 {
		t.Errorf("expected canonical artist to own 2 tracks, got %d", trkCount)
	}

	// Verify Schema 9 artist_sources re-pointing
	var sourceCount int
	_ = db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM artist_sources WHERE artist_id = $1`, canonical.ID).Scan(&sourceCount)
	if sourceCount < 2 {
		t.Errorf("expected canonical artist to have at least 2 sources in artist_sources, got %d", sourceCount)
	}
}

func TestCLI_ReconcileArtists_QuiescentLifecycle_WithEngine(t *testing.T) {
	tmpDir := t.TempDir()

	composeContent := `services:
  db:
    image: postgres:18-alpine
  backend:
    image: ytmdl-backend:0.16.0
`
	if err := os.WriteFile(filepath.Join(tmpDir, "compose.yaml"), []byte(composeContent), 0644); err != nil {
		t.Fatalf("failed writing compose: %v", err)
	}

	// Mock health server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"status":"ok","version":"0.16.0","uptime_seconds":42,"checks":{"database":{"ok":true}}}}`)
	}))
	defer ts.Close()

	fakeRunner := runner.NewFake()

	// 1. compose version check
	fakeRunner.Register("docker", []string{"compose", "version"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("Docker Compose version v2.24.0\n"),
	}, nil)

	// 2. compose ps check
	fakeRunner.Register("docker", []string{"compose", "-f", "compose.yaml", "ps", "--format", "{{.Service}}"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("db\nbackend\n"),
	}, nil)

	// 3. Queue check
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c",
		"SELECT count(*) FROM jobs WHERE status IN ('downloading', 'tagging', 'finalizing', 'matching', 'resolving_artist', 'resolving_releases', 'resolving_tracks', 'deduplicating');",
	}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("0\n"),
	}, nil)

	// 4. Stop backend service (Quiescent Model A)
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "stop", "backend",
	}, &runner.RunResult{
		ExitCode: 0,
	}, nil)

	// 5. pg_dump backup
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"pg_dump", "-U", "ytmdl", "-d", "ytmdl", "-Fc",
	}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("PGDUMP_MAGIC_HEADER_DATA"),
	}, nil)

	// pg_restore validation check
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"pg_restore", "--list",
	}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("; Archive created by pg_dump\n"),
	}, nil)

	// 6. candidate query via psql
	candidateJSON := `[
		{
			"id": "art_1",
			"name": "Alan Walker",
			"provider": "deezer",
			"source_id": "12345",
			"image_url": "https://img/walker.jpg",
			"created_at": "2026-09-04T08:00:00Z",
			"release_count": 3,
			"track_count": 8,
			"has_sub": true
		},
		{
			"id": "art_2",
			"name": "Alan Walker",
			"provider": "deezer",
			"source_id": "artist:alan-walker",
			"image_url": "",
			"created_at": "2026-09-04T09:00:00Z",
			"release_count": 1,
			"track_count": 2,
			"has_sub": false
		}
	]`
	querySQL := `SELECT COALESCE(json_agg(t), '[]'::json) FROM (
		SELECT
			a.id,
			a.name,
			a.provider,
			a.source_id,
			COALESCE(a.image_url, '') AS image_url,
			a.created_at,
			(SELECT COUNT(*) FROM releases r WHERE r.artist_id = a.id) AS release_count,
			(SELECT COUNT(*) FROM tracks t WHERE t.artist_id = a.id) AS track_count,
			EXISTS(
				SELECT 1 FROM artist_subscriptions s
				WHERE s.provider = a.provider AND s.artist_source_id = a.source_id
			) AS has_sub
		FROM artists a
		WHERE LOWER(a.name) IN (
			SELECT LOWER(name)
			FROM artists
			GROUP BY LOWER(name)
			HAVING COUNT(*) > 1
		)
		ORDER BY LOWER(a.name), a.provider, a.created_at ASC
	) t;`

	// 6. candidate query via psql (first call returns candidate fixture, second returns empty array)
	fakeRunner.RegisterSequence("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c", querySQL,
	}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(candidateJSON + "\n"),
	}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("[]\n"),
	})

	// 7. Merge transaction via psql
	fakeRunner.RegisterPrefix("docker", "compose -f compose.yaml exec -T db psql -U ytmdl -d ytmdl -v ON_ERROR_STOP=1 -c BEGIN;", &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("NOTICE: MERGE_OK:1:2\nCOMMIT\n"),
	}, nil)

	// 8. Integrity check query
	integrityQuery := `SELECT
		(SELECT COUNT(*) FROM releases r LEFT JOIN artists a ON r.artist_id = a.id WHERE a.id IS NULL) AS dangling_releases,
		(SELECT COUNT(*) FROM tracks t LEFT JOIN artists a ON t.artist_id = a.id WHERE a.id IS NULL) AS dangling_tracks;`
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c", integrityQuery,
	}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("0|0\n"),
	}, nil)

	// 9. Restart backend service (Quiescent Model A)
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "up", "-d", "--no-deps", "backend",
	}, &runner.RunResult{
		ExitCode: 0,
	}, nil)

	var stdout, stderr bytes.Buffer
	code := runCLIWithDeps(context.Background(), []string{
		"reconcile-artists",
		"--project-dir", tmpDir,
		"--file", "compose.yaml",
		"--engine", "docker",
		"--base-url", ts.URL,
		"--apply",
		"-y",
	}, &stdout, &stderr, CLIDependencies{
		Runner: fakeRunner,
	})

	if code != 0 {
		t.Fatalf("code = %d, want 0; stdout: %s, stderr: %s", code, stdout.String(), stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "Stopping backend service to ensure database quiescence...") {
		t.Errorf("stdout missing backend stop notice: %s", out)
	}
	if !strings.Contains(out, "Restarting backend service...") {
		t.Errorf("stdout missing backend restart notice: %s", out)
	}
	if !strings.Contains(out, "Backend service restarted successfully.") {
		t.Errorf("stdout missing backend restart success: %s", out)
	}
	if !strings.Contains(out, "Backend health verification: PASS") {
		t.Errorf("stdout missing backend health verification PASS: %s", out)
	}
	if !strings.Contains(out, "SUCCESS") {
		t.Errorf("stdout missing SUCCESS: %s", out)
	}
}

func TestCLI_MergeArtists_QuiescentLifecycle_WithEngine(t *testing.T) {
	tmpDir := t.TempDir()

	composeContent := `services:
  db:
    image: postgres:18-alpine
  backend:
    image: ytmdl-backend:0.16.0
`
	if err := os.WriteFile(filepath.Join(tmpDir, "compose.yaml"), []byte(composeContent), 0644); err != nil {
		t.Fatalf("failed writing compose: %v", err)
	}

	// Mock health server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"status":"ok","version":"0.16.0","uptime_seconds":42,"checks":{"database":{"ok":true}}}}`)
	}))
	defer ts.Close()

	fakeRunner := runner.NewFake()

	// 1. compose version check
	fakeRunner.Register("docker", []string{"compose", "version"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("Docker Compose version v2.24.0\n"),
	}, nil)

	// 2. compose ps check
	fakeRunner.Register("docker", []string{"compose", "-f", "compose.yaml", "ps", "--format", "{{.Service}}"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("db\nbackend\n"),
	}, nil)

	// 3. Queue check
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c",
		"SELECT count(*) FROM jobs WHERE status IN ('downloading', 'tagging', 'finalizing', 'matching', 'resolving_artist', 'resolving_releases', 'resolving_tracks', 'deduplicating');",
	}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("0\n"),
	}, nil)

	// 4. Artists lookup
	artistsJSON := `[
		{
			"id": "art_1",
			"name": "Alan Walker",
			"provider": "deezer",
			"source_id": "288164",
			"image_url": "https://img/walker.jpg",
			"created_at": "2026-09-04T08:00:00Z",
			"release_count": 2,
			"track_count": 4,
			"has_sub": true
		},
		{
			"id": "art_2",
			"name": "Alan Walker",
			"provider": "deezer",
			"source_id": "artist:alan-walker",
			"image_url": "",
			"created_at": "2026-09-04T09:00:00Z",
			"release_count": 1,
			"track_count": 2,
			"has_sub": false
		}
	]`
	fakeRunner.RegisterPrefix("docker", "compose -f compose.yaml exec -T db psql -U ytmdl -d ytmdl -t -A -c SELECT COALESCE(json_agg(t), '[]'::json) FROM (", &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(artistsJSON + "\n"),
	}, nil)

	// 5. Stop backend service (Quiescent Model A)
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "stop", "backend",
	}, &runner.RunResult{
		ExitCode: 0,
	}, nil)

	// 6. pg_dump backup
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"pg_dump", "-U", "ytmdl", "-d", "ytmdl", "-Fc",
	}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("PGDUMP_MAGIC_HEADER_DATA"),
	}, nil)

	// pg_restore validation check
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"pg_restore", "--list",
	}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("; Archive created by pg_dump\n"),
	}, nil)

	// 7. Merge transaction via psql
	fakeRunner.RegisterPrefix("docker", "compose -f compose.yaml exec -T db psql -U ytmdl -d ytmdl -v ON_ERROR_STOP=1 -c BEGIN;", &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("NOTICE: MERGE_OK:1:2\nCOMMIT\n"),
	}, nil)

	// 8. Integrity check query
	integrityQuery := `SELECT
		(SELECT COUNT(*) FROM releases r LEFT JOIN artists a ON r.artist_id = a.id WHERE a.id IS NULL) AS dangling_releases,
		(SELECT COUNT(*) FROM tracks t LEFT JOIN artists a ON t.artist_id = a.id WHERE a.id IS NULL) AS dangling_tracks;`
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c", integrityQuery,
	}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("0|0\n"),
	}, nil)

	// 9. Restart backend service (Quiescent Model A)
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "up", "-d", "--no-deps", "backend",
	}, &runner.RunResult{
		ExitCode: 0,
	}, nil)

	var stdout, stderr bytes.Buffer
	code := runCLIWithDeps(context.Background(), []string{
		"merge-artists",
		"--project-dir", tmpDir,
		"--file", "compose.yaml",
		"--engine", "docker",
		"--base-url", ts.URL,
		"--apply",
		"-y",
		"art_1",
		"art_2",
	}, &stdout, &stderr, CLIDependencies{
		Runner: fakeRunner,
	})

	if code != 0 {
		t.Fatalf("code = %d, want 0; stdout: %s, stderr: %s", code, stdout.String(), stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "Stopping backend service to ensure database quiescence...") {
		t.Errorf("stdout missing backend stop notice: %s", out)
	}
	if !strings.Contains(out, "Restarting backend service...") {
		t.Errorf("stdout missing backend restart notice: %s", out)
	}
	if !strings.Contains(out, "Backend service restarted successfully.") {
		t.Errorf("stdout missing backend restart success: %s", out)
	}
	if !strings.Contains(out, "Backend health verification: PASS") {
		t.Errorf("stdout missing backend health verification PASS: %s", out)
	}
	if !strings.Contains(out, "SUCCESS") {
		t.Errorf("stdout missing SUCCESS: %s", out)
	}
	if !strings.Contains(out, "Merged Duplicates:    1") {
		t.Errorf("stdout missing Merged Duplicates count: %s", out)
	}
}

func TestCLI_MergeArtists_DirectDBMutateBlockedEvenWithEnvVar(t *testing.T) {
	_, testURL := dbtest.OpenWithURL(t)
	tmpDir := t.TempDir()

	// Verify that even if someone sets the legacy environment variable, runCLI blocks it.
	t.Setenv("YTMDL_TEST_ALLOW_DIRECT_DB_MUTATE", "1")

	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{
		"merge-artists",
		"--project-dir", tmpDir,
		"--db-url", testURL,
		"--apply",
		"-y",
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
	}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "mutating maintenance via --db-url is not permitted") {
		t.Errorf("stderr missing expected error: %s", stderr.String())
	}
}
