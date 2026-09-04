package discovery_test

import (
	"context"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/discovery"
	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/cmd/ytmdlctl/internal/runner"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{"explicit http", "http://127.0.0.1:8080", "http://127.0.0.1:8080", false},
		{"explicit https", "https://ytmdl.example.com", "https://ytmdl.example.com", false},
		{"wildcard ipv4 with port", "0.0.0.0:8080", "http://127.0.0.1:8080", false},
		{"wildcard ipv4 http with port", "http://0.0.0.0:8080", "http://127.0.0.1:8080", false},
		{"wildcard ipv6 with port", "[::]:8080", "http://127.0.0.1:8080", false},
		{"ipv6 loopback with port", "[::1]:8080", "http://[::1]:8080", false},
		{"loopback with custom port", "127.0.0.1:1234", "http://127.0.0.1:1234", false},
		{"host and port without scheme", "127.0.0.1:9090", "http://127.0.0.1:9090", false},
		{"invalid scheme", "ftp://localhost:8080", "", true},
		{"malformed url", "http://[invalid-ipv6", "", true},
		{"empty string", "", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := discovery.NormalizeURL(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("NormalizeURL(%q) err = %v, wantErr = %v", tc.input, err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.expected {
				t.Errorf("NormalizeURL(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestResolveBaseURLHierarchy(t *testing.T) {
	ctx := context.Background()

	// 1. Explicit base URL wins over everything
	u, err := discovery.ResolveBaseURL(ctx, discovery.ResolveBaseURLOptions{
		ExplicitURL:  "http://explicit:8080",
		PersistedURL: "http://persisted:8080",
	})
	if err != nil || u != "http://explicit:8080" {
		t.Errorf("explicit URL = %q, want http://explicit:8080", u)
	}

	// 2. Persisted URL wins if no explicit
	u, err = discovery.ResolveBaseURL(ctx, discovery.ResolveBaseURLOptions{
		PersistedURL: "http://persisted:8080",
	})
	if err != nil || u != "http://persisted:8080" {
		t.Errorf("persisted URL = %q, want http://persisted:8080", u)
	}

	// 3. Compose port resolution
	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "-f", "compose.yaml", "port", "frontend", "8080"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("0.0.0.0:8080\n"),
	}, nil)
	eng := engine.NewDocker(fake)

	u, err = discovery.ResolveBaseURL(ctx, discovery.ResolveBaseURLOptions{
		Engine:      eng,
		ProjectDir:  ".",
		ComposeFile: "compose.yaml",
	})
	if err != nil || u != "http://127.0.0.1:8080" {
		t.Errorf("compose port URL = %q (err: %v), want http://127.0.0.1:8080", u, err)
	}

	// 4. .env fallback if compose port fails
	u, err = discovery.ResolveBaseURL(ctx, discovery.ResolveBaseURLOptions{
		EnvVars: map[string]string{"YTMDL_HOST_PORT": "9090"},
	})
	if err != nil || u != "http://127.0.0.1:9090" {
		t.Errorf("env port fallback = %q (err: %v), want http://127.0.0.1:9090", u, err)
	}

	// 5. Default port 8080 fallback if nothing else available
	u, err = discovery.ResolveBaseURL(ctx, discovery.ResolveBaseURLOptions{})
	if err != nil || u != "http://127.0.0.1:8080" {
		t.Errorf("default fallback = %q (err: %v), want http://127.0.0.1:8080", u, err)
	}
}
