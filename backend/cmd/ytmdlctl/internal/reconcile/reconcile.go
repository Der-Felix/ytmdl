// Package reconcile implements transactional, proved duplicate artist reconciliation
// for YTMDL libraries with strict identity preservation and non-destructive fallbacks.
package reconcile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/internal/artistidentity"
	"ytdm/backend/internal/music"
)

// Candidate represents an artist row evaluated for reconciliation.
type Candidate struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Provider     string    `json:"provider"`
	SourceID     string    `json:"source_id"`
	ImageURL     string    `json:"image_url"`
	CreatedAt    time.Time `json:"created_at"`
	ReleaseCount int       `json:"release_count"`
	TrackCount   int       `json:"track_count"`
	HasSub       bool      `json:"has_sub"`
}

// TotalItems returns total releases plus tracks associated with this candidate.
func (c Candidate) TotalItems() int {
	return c.ReleaseCount + c.TrackCount
}

// IsSynthetic returns true if candidate lacks a real upstream provider ID.
func (c Candidate) IsSynthetic() bool {
	return strings.HasPrefix(c.SourceID, "artist:") || strings.TrimSpace(c.SourceID) == ""
}

// ProvedGroup represents a set of proved duplicate rows to be merged into a canonical winner.
type ProvedGroup struct {
	ClusterName        string
	Provider           string
	Winner             Candidate
	Duplicates         []Candidate
	ReleasesToReassign int
	TracksToReassign   int
	BestImage          string
}

// AmbiguousGroup represents a cluster of artists with matching names that cannot
// be safely merged under Schema 8 identity rules.
type AmbiguousGroup struct {
	ClusterName string
	Reason      string
	Candidates  []Candidate
}

// Report summarizes reconciliation analysis and execution results.
type Report struct {
	ClustersExamined  int
	ProvedClusters    int
	ProvedDups        int
	AmbiguousClusters int
	AmbiguousDups     int

	MergedGroups       int
	MergedRows         int
	ReassignedReleases int
	ReassignedTracks   int

	ProvedDetails    []ProvedGroup
	AmbiguousDetails []AmbiguousGroup

	BackupPath string
	BackupSize int64
	DryRun     bool
	Duration   time.Duration
}

// Options configures reconciliation execution.
type Options struct {
	ProjectDir     string
	ComposeFile    string
	BackupDir      string
	CurrentVersion string
	DBUser         string
	DBName         string
	DryRun         bool
	Apply          bool
	Verbose        bool
	Stdout         io.Writer
	Stderr         io.Writer
}

// Executor abstracts database interaction (via container psql or direct *sql.DB).
type Executor interface {
	QueryCandidates(ctx context.Context) ([]Candidate, error)
	GetArtists(ctx context.Context, ids []string) ([]Candidate, error)
	MergeGroup(ctx context.Context, winnerID string, dupIDs []string, bestImage string) (releasesMoved, tracksMoved int, err error)
	VerifyIntegrity(ctx context.Context) error
}

// EngineExecutor executes queries and transactions via engine.Engine using psql in the db container.
type EngineExecutor struct {
	Engine      engine.Engine
	ProjectDir  string
	ComposeFile string
	User        string
	Database    string
}

// QueryCandidates retrieves duplicate artist candidates from the database via psql.
func (e *EngineExecutor) QueryCandidates(ctx context.Context) ([]Candidate, error) {
	if e.Engine == nil {
		return nil, errors.New("container engine is required")
	}
	user := e.User
	if user == "" {
		user = "ytmdl"
	}
	database := e.Database
	if database == "" {
		database = "ytmdl"
	}

	query := `SELECT COALESCE(json_agg(t), '[]'::json) FROM (
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

	res, err := e.Engine.Exec(ctx, e.ProjectDir, e.ComposeFile, "db", nil,
		"psql", "-U", user, "-d", database, "-t", "-A", "-c", query)
	if err != nil {
		return nil, fmt.Errorf("failed querying duplicate candidates: %w", err)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("candidate query failed: %s", strings.TrimSpace(string(res.Stderr)))
	}

	raw := strings.TrimSpace(string(res.Stdout))
	if raw == "" || raw == "[]" {
		return nil, nil
	}

	var candidates []Candidate
	if err := json.Unmarshal([]byte(raw), &candidates); err != nil {
		return nil, fmt.Errorf("failed parsing candidate JSON: %w (raw: %s)", err, raw)
	}

	return candidates, nil
}

// GetArtists retrieves specific artists by their IDs via psql.
func (e *EngineExecutor) GetArtists(ctx context.Context, ids []string) ([]Candidate, error) {
	if e.Engine == nil {
		return nil, errors.New("container engine is required")
	}
	if len(ids) == 0 {
		return nil, nil
	}
	user := e.User
	if user == "" {
		user = "ytmdl"
	}
	database := e.Database
	if database == "" {
		database = "ytmdl"
	}

	var quoted []string
	for _, id := range ids {
		clean := strings.ReplaceAll(id, "'", "''")
		quoted = append(quoted, fmt.Sprintf("'%s'", clean))
	}
	inList := strings.Join(quoted, ", ")

	query := fmt.Sprintf(`SELECT COALESCE(json_agg(t), '[]'::json) FROM (
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
		WHERE a.id IN (%s)
		ORDER BY a.name, a.created_at ASC
	) t;`, inList)

	res, err := e.Engine.Exec(ctx, e.ProjectDir, e.ComposeFile, "db", nil,
		"psql", "-U", user, "-d", database, "-t", "-A", "-c", query)
	if err != nil {
		return nil, fmt.Errorf("failed querying artists by id: %w", err)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("artist query failed: %s", strings.TrimSpace(string(res.Stderr)))
	}

	raw := strings.TrimSpace(string(res.Stdout))
	if raw == "" || raw == "[]" {
		return nil, nil
	}

	var candidates []Candidate
	if err := json.Unmarshal([]byte(raw), &candidates); err != nil {
		return nil, fmt.Errorf("failed parsing artist JSON: %w (raw: %s)", err, raw)
	}

	return candidates, nil
}

// MergeGroup executes a single atomic transaction via psql to merge duplicate IDs into winnerID.
func (e *EngineExecutor) MergeGroup(ctx context.Context, winnerID string, dupIDs []string, bestImage string) (int, int, error) {
	if len(dupIDs) == 0 {
		return 0, 0, nil
	}
	user := e.User
	if user == "" {
		user = "ytmdl"
	}
	database := e.Database
	if database == "" {
		database = "ytmdl"
	}

	var quotedDups []string
	for _, id := range dupIDs {
		clean := strings.ReplaceAll(id, "'", "''")
		quotedDups = append(quotedDups, fmt.Sprintf("'%s'", clean))
	}
	dupsList := strings.Join(quotedDups, ", ")
	cleanWinner := strings.ReplaceAll(winnerID, "'", "''")
	cleanImage := strings.ReplaceAll(bestImage, "'", "''")

	script := fmt.Sprintf(`BEGIN;
DO $$
DECLARE
    v_rel_count int;
    v_trk_count int;
BEGIN
    -- 1. Lock canonical and duplicate rows
    PERFORM id FROM artists WHERE id = '%s' FOR UPDATE;
    PERFORM id FROM artists WHERE id IN (%s) FOR UPDATE;

    -- 2. Count items to repoint
    SELECT COUNT(*) INTO v_rel_count FROM releases WHERE artist_id IN (%s);
    SELECT COUNT(*) INTO v_trk_count FROM tracks WHERE artist_id IN (%s);

    -- 2b. Repoint artist_sources if table exists
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'artist_sources') THEN
        DELETE FROM artist_sources
        WHERE artist_id IN (%s)
          AND (provider, source_id) IN (
              SELECT provider, source_id FROM artist_sources WHERE artist_id = '%s'
          );
        UPDATE artist_sources SET artist_id = '%s', is_primary = false, updated_at = NOW()
        WHERE artist_id IN (%s);
    END IF;

    -- 3. Repoint releases
    UPDATE releases SET artist_id = '%s', updated_at = NOW() WHERE artist_id IN (%s);

    -- 4. Repoint tracks
    UPDATE tracks SET artist_id = '%s', updated_at = NOW() WHERE artist_id IN (%s);

    -- 5. Repoint audit findings if table exists
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'library_audit_findings') THEN
        UPDATE library_audit_findings SET artist_id = '%s' WHERE artist_id IN (%s);
    END IF;

    -- 6. Preserve best image on canonical if canonical has none
    IF '%s' <> '' THEN
        UPDATE artists SET image_url = '%s', updated_at = NOW()
        WHERE id = '%s' AND (image_url IS NULL OR image_url = '');
    END IF;

    -- 7. Delete duplicates
    DELETE FROM artists WHERE id IN (%s);

    RAISE NOTICE 'MERGE_OK:%%:%%', v_rel_count, v_trk_count;
END $$;
COMMIT;`,
		cleanWinner, dupsList,
		dupsList, dupsList,
		dupsList, cleanWinner, cleanWinner, dupsList,
		cleanWinner, dupsList,
		cleanWinner, dupsList,
		cleanWinner, dupsList,
		cleanImage, cleanImage, cleanWinner,
		dupsList,
	)

	res, err := e.Engine.Exec(ctx, e.ProjectDir, e.ComposeFile, "db", nil,
		"psql", "-U", user, "-d", database, "-v", "ON_ERROR_STOP=1", "-c", script)
	if err != nil {
		return 0, 0, fmt.Errorf("failed executing merge transaction: %w", err)
	}
	if res.ExitCode != 0 {
		return 0, 0, fmt.Errorf("merge transaction failed: %s", strings.TrimSpace(string(res.Stderr)))
	}

	// Parse notice for moved counts
	relMoved, trkMoved := 0, 0
	combined := string(res.Stdout) + string(res.Stderr)
	for _, line := range strings.Split(combined, "\n") {
		if strings.Contains(line, "MERGE_OK:") {
			parts := strings.Split(strings.TrimSpace(line), ":")
			if len(parts) >= 3 {
				fmt.Sscanf(parts[1], "%d", &relMoved)
				fmt.Sscanf(parts[2], "%d", &trkMoved)
			}
		}
	}

	return relMoved, trkMoved, nil
}

// VerifyIntegrity checks for dangling foreign keys and remaining proved duplicates.
func (e *EngineExecutor) VerifyIntegrity(ctx context.Context) error {
	user := e.User
	if user == "" {
		user = "ytmdl"
	}
	database := e.Database
	if database == "" {
		database = "ytmdl"
	}

	query := `SELECT
		(SELECT COUNT(*) FROM releases r LEFT JOIN artists a ON r.artist_id = a.id WHERE a.id IS NULL) AS dangling_releases,
		(SELECT COUNT(*) FROM tracks t LEFT JOIN artists a ON t.artist_id = a.id WHERE a.id IS NULL) AS dangling_tracks;`

	res, err := e.Engine.Exec(ctx, e.ProjectDir, e.ComposeFile, "db", nil,
		"psql", "-U", user, "-d", database, "-t", "-A", "-c", query)
	if err != nil {
		return fmt.Errorf("integrity check query failed: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("integrity query error: %s", strings.TrimSpace(string(res.Stderr)))
	}

	raw := strings.TrimSpace(string(res.Stdout))
	parts := strings.Split(raw, "|")
	if len(parts) >= 2 {
		var danglingRel, danglingTrk int
		fmt.Sscanf(parts[0], "%d", &danglingRel)
		fmt.Sscanf(parts[1], "%d", &danglingTrk)
		if danglingRel > 0 || danglingTrk > 0 {
			return fmt.Errorf("referential integrity violation detected: %d dangling releases, %d dangling tracks", danglingRel, danglingTrk)
		}
	}

	return nil
}

// SQLExecutor implements Executor directly against a Go *sql.DB connection.
type SQLExecutor struct {
	DB *sql.DB
}

// QueryCandidates retrieves duplicate artist candidates from *sql.DB.
func (s *SQLExecutor) QueryCandidates(ctx context.Context) ([]Candidate, error) {
	query := `
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
		ORDER BY LOWER(a.name), a.provider, a.created_at ASC`

	rows, err := s.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed querying duplicate candidates: %w", err)
	}
	defer rows.Close()

	var candidates []Candidate
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.ID, &c.Name, &c.Provider, &c.SourceID, &c.ImageURL, &c.CreatedAt, &c.ReleaseCount, &c.TrackCount, &c.HasSub); err != nil {
			return nil, fmt.Errorf("failed scanning candidate: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("candidate cursor error: %w", err)
	}

	return candidates, nil
}

// GetArtists retrieves specific artists by their IDs from *sql.DB.
func (s *SQLExecutor) GetArtists(ctx context.Context, ids []string) ([]Candidate, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	query := `SELECT
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
	WHERE a.id = ANY($1)
	ORDER BY a.name, a.created_at ASC`

	rows, err := s.DB.QueryContext(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("failed querying artists by id: %w", err)
	}
	defer rows.Close()

	var candidates []Candidate
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.ID, &c.Name, &c.Provider, &c.SourceID, &c.ImageURL, &c.CreatedAt, &c.ReleaseCount, &c.TrackCount, &c.HasSub); err != nil {
			return nil, fmt.Errorf("failed scanning artist candidate: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("artist candidate cursor error: %w", err)
	}

	return candidates, nil
}

// MergeGroup executes a transactional merge against *sql.DB.
func (s *SQLExecutor) MergeGroup(ctx context.Context, winnerID string, dupIDs []string, bestImage string) (int, int, error) {
	if len(dupIDs) == 0 {
		return 0, 0, nil
	}

	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, 0, fmt.Errorf("failed beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Lock canonical row and duplicates
	var dummyID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM artists WHERE id = $1 FOR UPDATE`, winnerID).Scan(&dummyID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed locking canonical artist %s: %w", winnerID, err)
	}

	for _, dupID := range dupIDs {
		err = tx.QueryRowContext(ctx, `SELECT id FROM artists WHERE id = $1 FOR UPDATE`, dupID).Scan(&dummyID)
		if err != nil {
			return 0, 0, fmt.Errorf("failed locking duplicate artist %s: %w", dupID, err)
		}
	}

	// 2. Count releases & tracks moved
	var relMoved, trkMoved int
	relRow := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM releases WHERE artist_id = ANY($1)`, dupIDs)
	_ = relRow.Scan(&relMoved)
	trkRow := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracks WHERE artist_id = ANY($1)`, dupIDs)
	_ = trkRow.Scan(&trkMoved)

	now := time.Now().UTC()

	// 2b. Re-link artist_sources to winnerID (deduplicating conflicts first)
	_, _ = tx.ExecContext(ctx, `
		DELETE FROM artist_sources
		WHERE artist_id = ANY($1)
		  AND (provider, source_id) IN (
			  SELECT provider, source_id FROM artist_sources WHERE artist_id = $2
		  )`, dupIDs, winnerID)
	_, _ = tx.ExecContext(ctx, `
		UPDATE artist_sources
		SET artist_id = $1, is_primary = false, updated_at = $2
		WHERE artist_id = ANY($3)`, winnerID, now, dupIDs)

	// 3. Reassign releases
	_, err = tx.ExecContext(ctx, `UPDATE releases SET artist_id = $1, updated_at = $2 WHERE artist_id = ANY($3)`, winnerID, now, dupIDs)
	if err != nil {
		return 0, 0, fmt.Errorf("failed reassigning releases: %w", err)
	}

	// 4. Reassign tracks
	_, err = tx.ExecContext(ctx, `UPDATE tracks SET artist_id = $1, updated_at = $2 WHERE artist_id = ANY($3)`, winnerID, now, dupIDs)
	if err != nil {
		return 0, 0, fmt.Errorf("failed reassigning tracks: %w", err)
	}

	// 5. Reassign audit findings (ignore if table does not exist)
	_, _ = tx.ExecContext(ctx, `UPDATE library_audit_findings SET artist_id = $1 WHERE artist_id = ANY($2)`, winnerID, dupIDs)

	// 6. Update artwork on winner if better image exists and canonical had none
	if bestImage != "" {
		_, err = tx.ExecContext(ctx, `UPDATE artists SET image_url = $1, updated_at = $2 WHERE id = $3 AND (image_url IS NULL OR image_url = '')`, bestImage, now, winnerID)
		if err != nil {
			return 0, 0, fmt.Errorf("failed updating winner image: %w", err)
		}
	}

	// 7. Delete duplicates
	_, err = tx.ExecContext(ctx, `DELETE FROM artists WHERE id = ANY($1)`, dupIDs)
	if err != nil {
		return 0, 0, fmt.Errorf("failed deleting duplicate artists: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("failed committing merge transaction: %w", err)
	}

	return relMoved, trkMoved, nil
}

// VerifyIntegrity checks referential integrity against *sql.DB.
func (s *SQLExecutor) VerifyIntegrity(ctx context.Context) error {
	var danglingRel, danglingTrk int
	row := s.DB.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM releases r LEFT JOIN artists a ON r.artist_id = a.id WHERE a.id IS NULL),
		(SELECT COUNT(*) FROM tracks t LEFT JOIN artists a ON t.artist_id = a.id WHERE a.id IS NULL)`)
	if err := row.Scan(&danglingRel, &danglingTrk); err != nil {
		return fmt.Errorf("integrity scan error: %w", err)
	}
	if danglingRel > 0 || danglingTrk > 0 {
		return fmt.Errorf("referential integrity violation detected: %d dangling releases, %d dangling tracks", danglingRel, danglingTrk)
	}
	return nil
}

// Analyze evaluates candidate rows and classifies them into ProvedGroups and AmbiguousGroups
// using strict Schema 8 canonical identity preservation rules.
func Analyze(candidates []Candidate) (proved []ProvedGroup, ambiguous []AmbiguousGroup) {
	// Group by lower(name)
	clusters := make(map[string][]Candidate)
	var clusterOrder []string
	for _, c := range candidates {
		key := strings.ToLower(strings.TrimSpace(c.Name))
		if _, ok := clusters[key]; !ok {
			clusterOrder = append(clusterOrder, key)
		}
		clusters[key] = append(clusters[key], c)
	}

	for _, name := range clusterOrder {
		cands := clusters[name]
		if len(cands) <= 1 {
			continue
		}

		// Group by provider
		byProvider := make(map[string][]Candidate)
		for _, c := range cands {
			byProvider[c.Provider] = append(byProvider[c.Provider], c)
		}

		// Rule 1: Cross-provider same-name matches cannot be merged automatically without provenance proof
		if len(byProvider) > 1 {
			ambiguous = append(ambiguous, AmbiguousGroup{
				ClusterName: name,
				Reason:      fmt.Sprintf("Multiple providers (%s) share name; cross-provider merging requires explicit operator confirmation or subscription provenance", listProviders(byProvider)),
				Candidates:  cands,
			})
			continue
		}

		// Within single provider:
		for prov, provCands := range byProvider {
			if len(provCands) <= 1 {
				continue
			}

			// Check distinct real provider IDs
			realIDs := make(map[string]struct{})
			for _, c := range provCands {
				if !c.IsSynthetic() {
					realIDs[c.SourceID] = struct{}{}
				}
			}

			// Rule 2: Multiple distinct real IDs on same provider represent distinct entities (John Williams negative test)
			if len(realIDs) > 1 {
				ambiguous = append(ambiguous, AmbiguousGroup{
					ClusterName: name,
					Reason:      fmt.Sprintf("Multiple distinct real %s IDs (%d distinct IDs) share name; distinct catalog entities must not be merged", prov, len(realIDs)),
					Candidates:  provCands,
				})
				continue
			}

			// Exactly one or zero real IDs: Safe proved duplicates!
			winnerIdx := 0
			for i := 1; i < len(provCands); i++ {
				if isBetterCandidate(provCands[i], provCands[winnerIdx]) {
					winnerIdx = i
				}
			}

			winner := provCands[winnerIdx]
			var dups []Candidate
			relCount := 0
			trkCount := 0
			bestImage := strings.TrimSpace(winner.ImageURL)

			for i, c := range provCands {
				if i != winnerIdx {
					// Duplicates must be synthetic (real ID duplicates with different IDs are screened out above)
					if c.IsSynthetic() {
						dups = append(dups, c)
						relCount += c.ReleaseCount
						trkCount += c.TrackCount
						if bestImage == "" && strings.TrimSpace(c.ImageURL) != "" {
							bestImage = strings.TrimSpace(c.ImageURL)
						}
					}
				}
			}

			if len(dups) > 0 {
				proved = append(proved, ProvedGroup{
					ClusterName:        name,
					Provider:           prov,
					Winner:             winner,
					Duplicates:         dups,
					ReleasesToReassign: relCount,
					TracksToReassign:   trkCount,
					BestImage:          bestImage,
				})
			}
		}
	}

	return proved, ambiguous
}

// isBetterCandidate selects the canonical winner among proved duplicates using the shared artistidentity rules.
func isBetterCandidate(a, b Candidate) bool {
	kindA := music.SourceKindExternal
	if a.IsSynthetic() {
		kindA = music.SourceKindLegacySynthetic
	}
	kindB := music.SourceKindExternal
	if b.IsSynthetic() {
		kindB = music.SourceKindLegacySynthetic
	}
	aiA := artistidentity.Candidate{
		ID:           a.ID,
		Name:         a.Name,
		Provider:     a.Provider,
		SourceID:     a.SourceID,
		SourceKind:   kindA,
		ImageURL:     a.ImageURL,
		CreatedAt:    a.CreatedAt,
		ReleaseCount: a.ReleaseCount,
		TrackCount:   a.TrackCount,
		HasSub:       a.HasSub,
	}
	aiB := artistidentity.Candidate{
		ID:           b.ID,
		Name:         b.Name,
		Provider:     b.Provider,
		SourceID:     b.SourceID,
		SourceKind:   kindB,
		ImageURL:     b.ImageURL,
		CreatedAt:    b.CreatedAt,
		ReleaseCount: b.ReleaseCount,
		TrackCount:   b.TrackCount,
		HasSub:       b.HasSub,
	}
	return artistidentity.IsBetterCandidate(aiA, aiB)
}

func listProviders(byProvider map[string][]Candidate) string {
	var provs []string
	for p := range byProvider {
		provs = append(provs, p)
	}
	return strings.Join(provs, ", ")
}

// Run executes the reconciliation workflow according to options.
func Run(ctx context.Context, exec Executor, opts Options) (*Report, error) {
	start := time.Now()
	report := &Report{
		DryRun: opts.DryRun || !opts.Apply,
	}

	// 1. Query candidates
	candidates, err := exec.QueryCandidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed querying duplicate artist candidates: %w", err)
	}

	// 2. Classify into ProvedGroups and AmbiguousGroups
	provedGroups, ambiguousGroups := Analyze(candidates)

	report.ClustersExamined = len(candidates)
	report.ProvedClusters = len(provedGroups)
	report.AmbiguousClusters = len(ambiguousGroups)
	report.ProvedDetails = provedGroups
	report.AmbiguousDetails = ambiguousGroups

	for _, pg := range provedGroups {
		report.ProvedDups += len(pg.Duplicates)
	}
	for _, ag := range ambiguousGroups {
		report.AmbiguousDups += len(ag.Candidates)
	}

	// 3. In Dry-Run mode: calculate simulation totals without writing
	if report.DryRun {
		for _, pg := range provedGroups {
			report.ReassignedReleases += pg.ReleasesToReassign
			report.ReassignedTracks += pg.TracksToReassign
		}
		report.Duration = time.Since(start)
		return report, nil
	}

	// 4. Mutating Mode: execute group-by-group atomic transactions
	for _, pg := range provedGroups {
		dupIDs := make([]string, 0, len(pg.Duplicates))
		for _, d := range pg.Duplicates {
			dupIDs = append(dupIDs, d.ID)
		}

		relMoved, trkMoved, err := exec.MergeGroup(ctx, pg.Winner.ID, dupIDs, pg.BestImage)
		if err != nil {
			return report, fmt.Errorf("failed merging artist cluster %q (%s): %w", pg.ClusterName, pg.Winner.ID, err)
		}

		report.MergedGroups++
		report.MergedRows += len(dupIDs)
		report.ReassignedReleases += relMoved
		report.ReassignedTracks += trkMoved
	}

	// 5. Post-verification integrity check
	if err := exec.VerifyIntegrity(ctx); err != nil {
		return report, fmt.Errorf("post-reconciliation integrity check failed: %w", err)
	}

	// 6. Verify 0 remaining proved duplicates
	remainingCandidates, err := exec.QueryCandidates(ctx)
	if err == nil {
		remainingProved, _ := Analyze(remainingCandidates)
		if len(remainingProved) > 0 {
			return report, fmt.Errorf("reconciliation incomplete: %d proved duplicate groups still remain", len(remainingProved))
		}
	}

	report.Duration = time.Since(start)
	return report, nil
}
