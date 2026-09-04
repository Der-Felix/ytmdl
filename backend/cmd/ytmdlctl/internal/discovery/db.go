package discovery

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"ytdm/backend/cmd/ytmdlctl/internal/engine"
)

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
