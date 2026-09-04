package discovery

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ytdm/backend/cmd/ytmdlctl/internal/engine"
)

var identifierRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,62}$`)

func isValidIdentifier(name string) bool {
	return identifierRegex.MatchString(name)
}

func quoteLiteral(val string) string {
	return `'` + strings.ReplaceAll(val, `'`, `''`) + `'`
}

// QueueStatus represents active and pending queue metrics.
type QueueStatus struct {
	ActiveJobs   int
	TotalPending int
}

// QueryDBSchema retrieves the current schema version from PostgreSQL schema_migrations.
func QueryDBSchema(ctx context.Context, eng engine.Engine, projectDir, composeFile, user, database string) (int, error) {
	if eng == nil || composeFile == "" {
		return 0, errors.New("engine or compose file missing")
	}
	if user == "" {
		user = "ytmdl"
	}
	if database == "" {
		database = "ytmdl"
	}
	if !isValidIdentifier(user) {
		return 0, fmt.Errorf("invalid database user %q", user)
	}
	if !isValidIdentifier(database) {
		return 0, fmt.Errorf("invalid database name %q", database)
	}

	query := "SELECT COALESCE(MAX(version), 0) FROM schema_migrations;"
	res, err := eng.Exec(ctx, projectDir, composeFile, "db", nil,
		"psql", "-U", user, "-d", database, "-t", "-A", "-c", query)
	if err != nil {
		return 0, fmt.Errorf("failed executing schema query: %w", err)
	}

	raw := strings.TrimSpace(string(res.Stdout))
	version, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid schema version %q returned by database", raw)
	}

	return version, nil
}

// QueryQueueStatus retrieves active and pending job counts from the database.
func QueryQueueStatus(ctx context.Context, eng engine.Engine, projectDir, composeFile, user, database string) (*QueueStatus, error) {
	if eng == nil || composeFile == "" {
		return nil, errors.New("engine or compose file missing")
	}
	if user == "" {
		user = "ytmdl"
	}
	if database == "" {
		database = "ytmdl"
	}
	if !isValidIdentifier(user) {
		return nil, fmt.Errorf("invalid database user %q", user)
	}
	if !isValidIdentifier(database) {
		return nil, fmt.Errorf("invalid database name %q", database)
	}

	activeQuery := "SELECT count(*) FROM jobs WHERE status IN ('downloading', 'tagging', 'finalizing', 'matching', 'resolving_artist', 'resolving_releases', 'resolving_tracks', 'deduplicating');"
	resActive, err := eng.Exec(ctx, projectDir, composeFile, "db", nil,
		"psql", "-U", user, "-d", database, "-t", "-A", "-c", activeQuery)
	if err != nil {
		return nil, fmt.Errorf("failed querying active jobs: %w", err)
	}

	activeCount, err := strconv.Atoi(strings.TrimSpace(string(resActive.Stdout)))
	if err != nil {
		return nil, fmt.Errorf("invalid active jobs count from database: %w", err)
	}

	pendingQuery := "SELECT count(*) FROM jobs WHERE status NOT IN ('completed', 'failed', 'cancelled');"
	resPending, err := eng.Exec(ctx, projectDir, composeFile, "db", nil,
		"psql", "-U", user, "-d", database, "-t", "-A", "-c", pendingQuery)
	if err != nil {
		return nil, fmt.Errorf("failed querying pending jobs: %w", err)
	}

	pendingCount, err := strconv.Atoi(strings.TrimSpace(string(resPending.Stdout)))
	if err != nil {
		return nil, fmt.Errorf("invalid pending jobs count from database: %w", err)
	}

	return &QueueStatus{
		ActiveJobs:   activeCount,
		TotalPending: pendingCount,
	}, nil
}

// VerifyDBQuiescence verifies that no application writers or active client transactions
// are currently executing against the target database (excluding administrative tools like psql, pg_dump, or the query itself).
func VerifyDBQuiescence(ctx context.Context, eng engine.Engine, projectDir, composeFile, user, database string) error {
	if eng == nil || composeFile == "" {
		return errors.New("engine or compose file missing")
	}
	if user == "" {
		user = "ytmdl"
	}
	if database == "" {
		database = "ytmdl"
	}
	if !isValidIdentifier(user) {
		return fmt.Errorf("invalid database user %q", user)
	}
	if !isValidIdentifier(database) {
		return fmt.Errorf("invalid database name %q", database)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		query := fmt.Sprintf("SELECT count(*) FROM pg_stat_activity WHERE datname = %s AND pid <> pg_backend_pid() AND state = 'active' AND application_name NOT IN ('ytmdlctl', 'pg_dump', 'psql');", quoteLiteral(database))
		res, err := eng.Exec(ctx, projectDir, composeFile, "db", nil,
			"psql", "-U", user, "-d", database, "-t", "-A", "-c", query)
		if err != nil {
			return fmt.Errorf("failed executing database quiescence query: %w", err)
		}
		raw := strings.TrimSpace(string(res.Stdout))
		count, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("invalid active writer count %q from database: %w", raw, err)
		}
		if count == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("database %q has %d active writer transaction(s) still in progress", database, count)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
