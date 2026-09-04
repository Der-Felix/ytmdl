package discovery_test

import (
	"context"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/discovery"
	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/cmd/ytmdlctl/internal/runner"
)

func TestQueryDBSchema(t *testing.T) {
	fake := runner.NewFake()
	fake.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c", "SELECT COALESCE(MAX(version), 0) FROM schema_migrations;",
	}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("8\n"),
	}, nil)

	eng := engine.NewDocker(fake)
	schema, err := discovery.QueryDBSchema(context.Background(), eng, ".", "compose.yaml", "ytmdl", "ytmdl")
	if err != nil {
		t.Fatalf("QueryDBSchema failed: %v", err)
	}
	if schema != 8 {
		t.Errorf("schema = %d, want 8", schema)
	}
}

func TestQueryDBQueue(t *testing.T) {
	fake := runner.NewFake()
	fake.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c",
		"SELECT count(*) FROM jobs WHERE status IN ('downloading', 'tagging', 'finalizing', 'matching', 'resolving_artist', 'resolving_releases', 'resolving_tracks', 'deduplicating');",
	}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("2\n"),
	}, nil)

	fake.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c",
		"SELECT count(*) FROM jobs WHERE status NOT IN ('completed', 'failed', 'cancelled');",
	}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("5\n"),
	}, nil)

	eng := engine.NewDocker(fake)
	q, err := discovery.QueryQueueStatus(context.Background(), eng, ".", "compose.yaml", "ytmdl", "ytmdl")
	if err != nil {
		t.Fatalf("QueryQueueStatus failed: %v", err)
	}
	if q.ActiveJobs != 2 {
		t.Errorf("ActiveJobs = %d, want 2", q.ActiveJobs)
	}
	if q.TotalPending != 5 {
		t.Errorf("TotalPending = %d, want 5", q.TotalPending)
	}
}
