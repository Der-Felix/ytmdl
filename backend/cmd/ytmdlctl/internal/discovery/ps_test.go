package discovery_test

import (
	"context"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/discovery"
	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/cmd/ytmdlctl/internal/runner"
)

func TestParseDockerComposePSJSON(t *testing.T) {
	// Docker compose array JSON
	jsonArray := `[
  {"Service": "backend", "State": "running", "Health": "healthy"},
  {"Service": "frontend", "State": "running", "Health": "healthy"},
  {"Service": "db", "State": "running", "Health": "healthy"}
]`

	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "-f", "compose.yaml", "ps", "--format", "json"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(jsonArray),
	}, nil)

	eng := engine.NewDocker(fake)
	statuses, err := discovery.InspectServices(context.Background(), eng, ".", "compose.yaml")
	if err != nil {
		t.Fatalf("InspectServices failed: %v", err)
	}

	if len(statuses) != 3 {
		t.Fatalf("expected 3 services, got %d", len(statuses))
	}

	backend := statuses["backend"]
	if backend.State != "running" || backend.Health != "healthy" {
		t.Errorf("backend status = %+v, want running/healthy", backend)
	}
}

func TestParseDockerComposePSNDJSON(t *testing.T) {
	// Docker compose newline-delimited JSON
	ndjson := `{"Service": "backend", "State": "running", "Health": "healthy"}
{"Service": "frontend", "State": "running", "Health": "healthy"}
{"Service": "db", "State": "exited", "Health": "unhealthy"}
`

	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "-f", "compose.yaml", "ps", "--format", "json"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(ndjson),
	}, nil)

	eng := engine.NewDocker(fake)
	statuses, err := discovery.InspectServices(context.Background(), eng, ".", "compose.yaml")
	if err != nil {
		t.Fatalf("InspectServices failed: %v", err)
	}

	db := statuses["db"]
	if db.State != "exited" || db.Health != "unhealthy" {
		t.Errorf("db status = %+v, want exited/unhealthy", db)
	}
}

func TestParsePodmanComposePSTabular(t *testing.T) {
	// Podman compose tabular output fallback
	fake := runner.NewFake()
	fake.Register("podman", []string{"compose", "-f", "compose.yaml", "ps", "--format", "json"}, &runner.RunResult{
		ExitCode: 1, // Suppose json flag not supported or returns error
		Stderr:   []byte("flag not supported: --format json"),
	}, nil)

	fake.Register("podman", []string{"compose", "-f", "compose.yaml", "ps"}, &runner.RunResult{
		ExitCode: 0,
		Stdout: []byte(`NAME             COMMAND                  SERVICE    STATUS
ytmdl-backend    /backend/server          backend    Up 5 minutes (healthy)
ytmdl-frontend   nginx -g daemon off;    frontend   Up 5 minutes (healthy)
ytmdl-db         docker-entrypoint.sh ... db         Up 5 minutes (healthy)
`),
	}, nil)

	eng := engine.NewPodman(fake)
	statuses, err := discovery.InspectServices(context.Background(), eng, ".", "compose.yaml")
	if err != nil {
		t.Fatalf("InspectServices failed: %v", err)
	}

	if len(statuses) != 3 {
		t.Fatalf("expected 3 services, got %d", len(statuses))
	}
	if statuses["backend"].State != "running" {
		t.Errorf("backend state = %q, want running", statuses["backend"].State)
	}
}
