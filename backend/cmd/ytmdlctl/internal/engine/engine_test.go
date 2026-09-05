package engine_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/cmd/ytmdlctl/internal/runner"
)

func TestDockerEngineCommands(t *testing.T) {
	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("Docker Compose version v2.29.0\n"),
	}, nil)
	fake.Register("docker", []string{"compose", "-f", "compose.ghcr.yaml", "ps", "--format", "{{.Service}}"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("backend\nfrontend\ndb\n"),
	}, nil)
	fake.Register("docker", []string{"compose", "-f", "compose.ghcr.yaml", "port", "frontend", "8080"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("0.0.0.0:8080\n"),
	}, nil)

	eng := engine.NewDocker(fake)
	if eng.Name() != "docker" {
		t.Errorf("Name() = %q, want docker", eng.Name())
	}

	ctx := context.Background()
	ver, err := eng.ComposeVersion(ctx)
	if err != nil {
		t.Fatalf("ComposeVersion failed: %v", err)
	}
	if !strings.Contains(ver, "v2.29.0") {
		t.Errorf("got %q, want v2.29.0", ver)
	}

	running, err := eng.IsServiceRunning(ctx, ".", "compose.ghcr.yaml", "backend")
	if err != nil {
		t.Fatalf("IsServiceRunning failed: %v", err)
	}
	if !running {
		t.Error("expected backend to be running")
	}

	port, err := eng.Port(ctx, ".", "compose.ghcr.yaml", "frontend", 8080)
	if err != nil {
		t.Fatalf("Port failed: %v", err)
	}
	if port != "0.0.0.0:8080" {
		t.Errorf("Port = %q, want 0.0.0.0:8080", port)
	}
}

func TestPodmanEngineCommands(t *testing.T) {
	fake := runner.NewFake()
	fake.Register("podman", []string{"compose", "version"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("podman compose version 1.3.0\n"),
	}, nil)

	eng := engine.NewPodman(fake)
	if eng.Name() != "podman" {
		t.Errorf("Name() = %q, want podman", eng.Name())
	}

	ctx := context.Background()
	ver, err := eng.ComposeVersion(ctx)
	if err != nil {
		t.Fatalf("ComposeVersion failed: %v", err)
	}
	if !strings.Contains(ver, "1.3.0") {
		t.Errorf("got %q, want 1.3.0", ver)
	}
}

func TestResolveEngineExplicitDocker(t *testing.T) {
	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("v2.29.0\n"),
	}, nil)

	ctx := context.Background()
	eng, err := engine.Resolve(ctx, fake, engine.ResolveOptions{
		ProjectDir:     ".",
		ComposeFile:    "compose.ghcr.yaml",
		ExplicitEngine: "docker",
		IsMutating:     true,
	})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if eng.Name() != "docker" {
		t.Errorf("got engine %q, want docker", eng.Name())
	}
}

func TestResolveEngineAutoOnlyDockerAvailable(t *testing.T) {
	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("v2.29.0\n"),
	}, nil)
	fake.Register("podman", []string{"compose", "version"}, nil, errors.New("not found"))

	ctx := context.Background()
	eng, err := engine.Resolve(ctx, fake, engine.ResolveOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		IsMutating:  true,
	})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if eng.Name() != "docker" {
		t.Errorf("got engine %q, want docker", eng.Name())
	}
}

func TestResolveEngineAutoOnlyPodmanAvailable(t *testing.T) {
	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, nil, errors.New("not found"))
	fake.Register("podman", []string{"compose", "version"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("1.3.0\n"),
	}, nil)

	ctx := context.Background()
	eng, err := engine.Resolve(ctx, fake, engine.ResolveOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		IsMutating:  true,
	})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if eng.Name() != "podman" {
		t.Errorf("got engine %q, want podman", eng.Name())
	}
}

func TestResolveEngineAutoBothAvailableDockerOwnsProject(t *testing.T) {
	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{ExitCode: 0, Stdout: []byte("v2\n")}, nil)
	fake.Register("podman", []string{"compose", "version"}, &runner.RunResult{ExitCode: 0, Stdout: []byte("v1\n")}, nil)

	fake.Register("docker", []string{"compose", "-f", "compose.ghcr.yaml", "ps", "-q"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("container1\ncontainer2\n"),
	}, nil)
	fake.Register("podman", []string{"compose", "-f", "compose.ghcr.yaml", "ps", "-q"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(""),
	}, nil)

	ctx := context.Background()
	eng, err := engine.Resolve(ctx, fake, engine.ResolveOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		IsMutating:  true,
	})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if eng.Name() != "docker" {
		t.Errorf("got engine %q, want docker", eng.Name())
	}
}

func TestResolveEngineAutoBothAvailableMutatingAmbiguityFails(t *testing.T) {
	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{ExitCode: 0, Stdout: []byte("v2\n")}, nil)
	fake.Register("podman", []string{"compose", "version"}, &runner.RunResult{ExitCode: 0, Stdout: []byte("v1\n")}, nil)

	// Neither engine currently has running containers
	fake.Register("docker", []string{"compose", "-f", "compose.ghcr.yaml", "ps", "-q"}, &runner.RunResult{ExitCode: 0, Stdout: []byte("")}, nil)
	fake.Register("podman", []string{"compose", "-f", "compose.ghcr.yaml", "ps", "-q"}, &runner.RunResult{ExitCode: 0, Stdout: []byte("")}, nil)

	ctx := context.Background()
	_, err := engine.Resolve(ctx, fake, engine.ResolveOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		IsMutating:  true,
	})
	if !errors.Is(err, engine.ErrAmbiguousEngine) {
		t.Fatalf("got %v, want ErrAmbiguousEngine", err)
	}
}

func TestResolveEngineNeitherAvailableFails(t *testing.T) {
	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, nil, errors.New("not found"))
	fake.Register("podman", []string{"compose", "version"}, nil, errors.New("not found"))

	ctx := context.Background()
	_, err := engine.Resolve(ctx, fake, engine.ResolveOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		IsMutating:  true,
	})
	if !errors.Is(err, engine.ErrNoEngineFound) {
		t.Fatalf("got %v, want ErrNoEngineFound", err)
	}
}

func TestParseDigestFromInspectDocker(t *testing.T) {
	dockerJSON := `[
		{
			"Id": "sha256:abcd",
			"RepoDigests": [
				"ghcr.io/der-felix/ytmdl-backend@sha256:1111111111111111111111111111111111111111111111111111111111111111"
			]
		}
	]`

	digest, err := engine.ParseDigestFromInspect([]byte(dockerJSON), "ghcr.io/der-felix/ytmdl-backend:0.16.0")
	if err != nil {
		t.Fatalf("ParseDigestFromInspect failed: %v", err)
	}
	expected := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	if digest != expected {
		t.Errorf("digest = %q, want %q", digest, expected)
	}
}

func TestParseDigestFromInspectPodman(t *testing.T) {
	podmanJSON := `[
		{
			"Digest": "sha256:2222222222222222222222222222222222222222222222222222222222222222",
			"RepoDigests": [
				"ghcr.io/der-felix/ytmdl-frontend@sha256:2222222222222222222222222222222222222222222222222222222222222222"
			]
		}
	]`

	digest, err := engine.ParseDigestFromInspect([]byte(podmanJSON), "ghcr.io/der-felix/ytmdl-frontend:0.16.0")
	if err != nil {
		t.Fatalf("ParseDigestFromInspect failed: %v", err)
	}
	expected := "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	if digest != expected {
		t.Errorf("digest = %q, want %q", digest, expected)
	}
}

func TestParseDigestFromInspectErrors(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		imageRef    string
		expectedErr string
	}{
		{
			name:        "empty inspect",
			json:        `[]`,
			imageRef:    "ghcr.io/der-felix/ytmdl-backend:0.16.0",
			expectedErr: "empty data",
		},
		{
			name:        "no repo digests or digest",
			json:        `[{"RepoDigests": []}]`,
			imageRef:    "ghcr.io/der-felix/ytmdl-backend:0.16.0",
			expectedErr: "no valid repository digest found",
		},
		{
			name:        "malformed digest syntax",
			json:        `[{"RepoDigests": ["ghcr.io/der-felix/ytmdl-backend@sha256:short"]}]`,
			imageRef:    "ghcr.io/der-felix/ytmdl-backend:0.16.0",
			expectedErr: "no valid repository digest found",
		},
		{
			name: "multiple ambiguous digests",
			json: `[{
				"RepoDigests": [
					"other-repo-1@sha256:1111111111111111111111111111111111111111111111111111111111111111",
					"other-repo-2@sha256:2222222222222222222222222222222222222222222222222222222222222222"
				]
			}]`,
			imageRef:    "ghcr.io/der-felix/ytmdl-backend:0.16.0",
			expectedErr: "no valid repository digest found",
		},
		{
			name: "digest field fallback refused when repo digests empty",
			json: `[{
				"Digest": "sha256:3333333333333333333333333333333333333333333333333333333333333333",
				"RepoDigests": []
			}]`,
			imageRef:    "ghcr.io/der-felix/ytmdl-backend:0.16.0",
			expectedErr: "no valid repository digest found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := engine.ParseDigestFromInspect([]byte(tc.json), tc.imageRef)
			if err == nil || !strings.Contains(err.Error(), tc.expectedErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.expectedErr, err)
			}
		})
	}
}

func TestParseDigestFromInspectMultipleIrrelevantRepoDigests(t *testing.T) {
	json := `[{
		"RepoDigests": [
			"example.invalid/foo@sha256:3333333333333333333333333333333333333333333333333333333333333333",
			"ghcr.io/der-felix/ytmdl-backend@sha256:1111111111111111111111111111111111111111111111111111111111111111",
			"example.invalid/bar@sha256:4444444444444444444444444444444444444444444444444444444444444444"
		]
	}]`
	digest, err := engine.ParseDigestFromInspect([]byte(json), "ghcr.io/der-felix/ytmdl-backend:0.16.0")
	if err != nil {
		t.Fatalf("ParseDigestFromInspect failed: %v", err)
	}
	expected := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	if digest != expected {
		t.Errorf("digest = %q, want %q", digest, expected)
	}
}

func TestVerifyExpectedDigestScenarios(t *testing.T) {
	expectedRepo := "ghcr.io/der-felix/ytmdl-backend"
	digestA := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestB := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	digestC := "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	unrelatedDigest := "sha256:9999999999999999999999999999999999999999999999999999999999999999"

	tests := []struct {
		name           string
		repoDigests    []string
		expectedDigest string
		expectPass     bool
	}{
		{
			name: "expected digest first in RepoDigests",
			repoDigests: []string{
				expectedRepo + "@" + digestA,
				expectedRepo + "@" + digestB,
			},
			expectedDigest: digestA,
			expectPass:     true,
		},
		{
			name: "expected digest second in RepoDigests",
			repoDigests: []string{
				expectedRepo + "@" + digestA,
				expectedRepo + "@" + digestB,
			},
			expectedDigest: digestB,
			expectPass:     true,
		},
		{
			name: "expected digest last in RepoDigests",
			repoDigests: []string{
				expectedRepo + "@" + digestA,
				expectedRepo + "@" + digestB,
				expectedRepo + "@" + digestC,
			},
			expectedDigest: digestC,
			expectPass:     true,
		},
		{
			name: "expected digest absent",
			repoDigests: []string{
				expectedRepo + "@" + digestA,
				expectedRepo + "@" + digestB,
			},
			expectedDigest: digestC,
			expectPass:     false,
		},
		{
			name: "expected digest exists only under wrong repository",
			repoDigests: []string{
				"example.invalid/foo@" + digestA,
				"other.registry/bar@" + digestB,
			},
			expectedDigest: digestA,
			expectPass:     false,
		},
		{
			name: "duplicate identical expected digest",
			repoDigests: []string{
				expectedRepo + "@" + digestA,
				expectedRepo + "@" + digestA,
				expectedRepo + "@" + digestB,
			},
			expectedDigest: digestA,
			expectPass:     true,
		},
		{
			name: "malformed unrelated digest is skipped and valid digest passes",
			repoDigests: []string{
				"not-a-repo-digest",
				expectedRepo + "@invalid_not_sha256",
				expectedRepo + "@" + digestA,
			},
			expectedDigest: digestA,
			expectPass:     true,
		},
		{
			name: "multiple valid repository digests",
			repoDigests: []string{
				expectedRepo + "@" + digestA,
				expectedRepo + "@" + digestB,
				"unrelated/repo@" + unrelatedDigest,
			},
			expectedDigest: digestB,
			expectPass:     true,
		},
		{
			name: "reversed Podman output ordering yields identical success",
			repoDigests: []string{
				expectedRepo + "@" + digestB,
				expectedRepo + "@" + digestA,
			},
			expectedDigest: digestA,
			expectPass:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			jsonBytes, err := json.Marshal([]map[string]any{
				{
					"Id":          "sha256:dummyimageid12345",
					"RepoDigests": tc.repoDigests,
				},
			})
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}

			err = engine.VerifyExpectedDigest(jsonBytes, expectedRepo, tc.expectedDigest)
			if tc.expectPass && err != nil {
				t.Fatalf("expected PASS but got error: %v", err)
			}
			if !tc.expectPass && err == nil {
				t.Fatalf("expected FAIL but got nil error")
			}
		})
	}
}

func TestVerifyExpectedDigestOrderIndependence(t *testing.T) {
	repo := "ghcr.io/der-felix/ytmdl-backend"
	d1 := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	d2 := "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	d3 := "sha256:3333333333333333333333333333333333333333333333333333333333333333"

	order1 := []string{repo + "@" + d1, repo + "@" + d2, repo + "@" + d3}
	order2 := []string{repo + "@" + d3, repo + "@" + d1, repo + "@" + d2}
	order3 := []string{repo + "@" + d2, repo + "@" + d3, repo + "@" + d1}

	json1, _ := json.Marshal([]map[string]any{{"RepoDigests": order1}})
	json2, _ := json.Marshal([]map[string]any{{"RepoDigests": order2}})
	json3, _ := json.Marshal([]map[string]any{{"RepoDigests": order3}})

	for _, target := range []string{d1, d2, d3} {
		if err := engine.VerifyExpectedDigest(json1, repo, target); err != nil {
			t.Errorf("order1 failed for %s: %v", target, err)
		}
		if err := engine.VerifyExpectedDigest(json2, repo, target); err != nil {
			t.Errorf("order2 failed for %s: %v", target, err)
		}
		if err := engine.VerifyExpectedDigest(json3, repo, target); err != nil {
			t.Errorf("order3 failed for %s: %v", target, err)
		}
	}
}

func TestVerifyExpectedDigestRealWorldPodmanOutput(t *testing.T) {
	// Real-world output from podman inspect ghcr.io/der-felix/ytmdl-backend:0.15.0 on Debian 13
	realInspectJSON := `[{
		"Id": "5a0980359952c48792cbb68d1f23513eabab28e8b4b14b8425966a76c7c505e5",
		"RepoDigests": [
			"ghcr.io/der-felix/ytmdl-backend@sha256:7067831c6bb931429e91a353650c5e64ee6471c1577da7ca4a65f921c9a3454d",
			"ghcr.io/der-felix/ytmdl-backend@sha256:c1364e3d92ab63b4f39c5dd726f108d289b3265b646a919b19ea4f4cd2c91ea1"
		]
	}]`
	repo := "ghcr.io/der-felix/ytmdl-backend"
	manifestListDigest := "sha256:7067831c6bb931429e91a353650c5e64ee6471c1577da7ca4a65f921c9a3454d"
	instanceDigest := "sha256:c1364e3d92ab63b4f39c5dd726f108d289b3265b646a919b19ea4f4cd2c91ea1"

	// 1. Verify manifest list digest matches
	if err := engine.VerifyExpectedDigest([]byte(realInspectJSON), repo, manifestListDigest); err != nil {
		t.Fatalf("expected manifest list digest to match, got: %v", err)
	}

	// 2. Verify platform instance digest matches
	if err := engine.VerifyExpectedDigest([]byte(realInspectJSON), repo, instanceDigest); err != nil {
		t.Fatalf("expected instance digest to match, got: %v", err)
	}

	// 3. Reversed array order matches identically
	reversedJSON := `[{
		"Id": "5a0980359952c48792cbb68d1f23513eabab28e8b4b14b8425966a76c7c505e5",
		"RepoDigests": [
			"ghcr.io/der-felix/ytmdl-backend@sha256:c1364e3d92ab63b4f39c5dd726f108d289b3265b646a919b19ea4f4cd2c91ea1",
			"ghcr.io/der-felix/ytmdl-backend@sha256:7067831c6bb931429e91a353650c5e64ee6471c1577da7ca4a65f921c9a3454d"
		]
	}]`
	if err := engine.VerifyExpectedDigest([]byte(reversedJSON), repo, manifestListDigest); err != nil {
		t.Fatalf("reversed: expected manifest list digest to match, got: %v", err)
	}
	if err := engine.VerifyExpectedDigest([]byte(reversedJSON), repo, instanceDigest); err != nil {
		t.Fatalf("reversed: expected instance digest to match, got: %v", err)
	}

	// 4. Incorrect digest fails
	wrongDigest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if err := engine.VerifyExpectedDigest([]byte(realInspectJSON), repo, wrongDigest); err == nil {
		t.Fatalf("expected wrong digest to fail, got nil")
	}
}

func TestCheckPodmanProviderCompatibility(t *testing.T) {
	ctx := context.Background()

	// 1. Docker engine -> always passes
	fakeDocker := runner.NewFake()
	engDocker := engine.NewDocker(fakeDocker)
	if err := engine.CheckPodmanProviderCompatibility(ctx, engDocker); err != nil {
		t.Fatalf("expected nil for docker engine, got: %v", err)
	}

	// 2. Podman with Docker Compose V2 provider -> passes
	fakePodmanV2 := runner.NewFake()
	fakePodmanV2.Register("podman", []string{"compose", "version"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("Executing external compose provider \"/usr/libexec/docker/cli-plugins/docker-compose\"\nDocker Compose version 2.26.1"),
	}, nil)
	engPodmanV2 := engine.NewPodman(fakePodmanV2)
	if err := engine.CheckPodmanProviderCompatibility(ctx, engPodmanV2); err != nil {
		t.Fatalf("expected nil for podman with compose v2, got: %v", err)
	}

	// 3. Podman with python podman-compose -> fails with actionable error
	fakePodmanPy := runner.NewFake()
	fakePodmanPy.Register("podman", []string{"compose", "version"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("Executing external compose provider \"/usr/bin/podman-compose\"\npodman version 5.4.2\npodman-compose version 1.3.0"),
	}, nil)
	engPodmanPy := engine.NewPodman(fakePodmanPy)
	err := engine.CheckPodmanProviderCompatibility(ctx, engPodmanPy)
	if err == nil {
		t.Fatalf("expected error for python podman-compose, got nil")
	}
	if !strings.Contains(err.Error(), "not compatible with YTMDL's user namespace configuration") {
		t.Errorf("expected actionable error message, got: %v", err)
	}
}

func TestEngineExecStreamAndConfigAndPull(t *testing.T) {
	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "-f", "compose.yaml", "exec", "-T", "db", "pg_dump", "-U", "ytmdl", "-d", "ytmdl", "-Fc"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("DUMP_CONTENT"),
	}, nil)
	fake.Register("docker", []string{"compose", "-f", "compose.yaml", "config"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("services:\n  backend:\n    image: ghcr.io/der-felix/ytmdl-backend:0.16.0\n"),
	}, nil)
	fake.Register("docker", []string{"compose", "-f", "compose.yaml", "pull", "backend", "frontend"}, &runner.RunResult{
		ExitCode: 0,
	}, nil)

	eng := engine.NewDocker(fake)
	ctx := context.Background()

	// 1. ExecStream
	var out bytes.Buffer
	res, err := eng.ExecStream(ctx, ".", "compose.yaml", "db", nil, &out, "pg_dump", "-U", "ytmdl", "-d", "ytmdl", "-Fc")
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("ExecStream failed: %v", err)
	}
	if out.String() != "DUMP_CONTENT" {
		t.Errorf("streamed out = %q, want DUMP_CONTENT", out.String())
	}

	// 2. Config
	res, err = eng.Config(ctx, ".", "compose.yaml", map[string]string{"YTMDL_VERSION": "0.16.0"})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("Config failed: %v", err)
	}

	// 3. Pull
	res, err = eng.Pull(ctx, ".", "compose.yaml", map[string]string{"YTMDL_VERSION": "0.16.0"}, "backend", "frontend")
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("Pull failed: %v", err)
	}
}

func TestEngineUpServicesAndInspectContainer(t *testing.T) {
	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "-f", "compose.yaml", "up", "-d", "--no-deps", "backend"}, &runner.RunResult{
		ExitCode: 0,
	}, nil)
	fake.Register("docker", []string{"compose", "-f", "compose.yaml", "ps", "-q", "backend"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("c1234567890a\n"),
	}, nil)
	fake.Register("docker", []string{"inspect", "c1234567890a"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(`[{"Image": "sha256:abcd", "Config": {"Image": "ghcr.io/der-felix/ytmdl-backend:0.16.0"}}]`),
	}, nil)

	eng := engine.NewDocker(fake)
	ctx := context.Background()

	res, err := eng.UpServices(ctx, ".", "compose.yaml", map[string]string{"YTMDL_VERSION": "0.16.0"}, "backend")
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("UpServices failed: %v", err)
	}

	cid, err := eng.GetServiceContainerID(ctx, ".", "compose.yaml", "backend")
	if err != nil {
		t.Fatalf("GetServiceContainerID failed: %v", err)
	}
	if cid != "c1234567890a" {
		t.Errorf("cid = %q, want c1234567890a", cid)
	}

	imgRef, imgID, err := eng.InspectContainerImage(ctx, cid)
	if err != nil {
		t.Fatalf("InspectContainerImage failed: %v", err)
	}
	if imgRef != "ghcr.io/der-felix/ytmdl-backend:0.16.0" {
		t.Errorf("imgRef = %q, want ghcr.io/der-felix/ytmdl-backend:0.16.0", imgRef)
	}
	if imgID != "sha256:abcd" {
		t.Errorf("imgID = %q, want sha256:abcd", imgID)
	}
}

func TestVerifyBothExpectedDigests(t *testing.T) {
	expectedRepo := "ghcr.io/der-felix/ytmdl-backend"
	indexDigest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	platformDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	unrelatedDigest := "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

	tests := []struct {
		name        string
		repoDigests []string
		expectPass  bool
	}{
		{
			name: "both index and platform present",
			repoDigests: []string{
				expectedRepo + "@" + indexDigest,
				expectedRepo + "@" + platformDigest,
			},
			expectPass: true,
		},
		{
			name: "both index and platform present in reversed order with extra digests",
			repoDigests: []string{
				"other/repo@" + unrelatedDigest,
				expectedRepo + "@" + platformDigest,
				expectedRepo + "@" + indexDigest,
			},
			expectPass: true,
		},
		{
			name: "missing index digest",
			repoDigests: []string{
				expectedRepo + "@" + platformDigest,
			},
			expectPass: false,
		},
		{
			name: "missing platform digest",
			repoDigests: []string{
				expectedRepo + "@" + indexDigest,
			},
			expectPass: false,
		},
		{
			name: "wrong repository for index digest",
			repoDigests: []string{
				"other/repo@" + indexDigest,
				expectedRepo + "@" + platformDigest,
			},
			expectPass: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			jsonBytes, err := json.Marshal([]map[string]any{
				{
					"Id":          "sha256:dummy",
					"RepoDigests": tc.repoDigests,
				},
			})
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}
			err = engine.VerifyBothExpectedDigests(jsonBytes, expectedRepo, indexDigest, platformDigest)
			if tc.expectPass && err != nil {
				t.Fatalf("expected PASS but got: %v", err)
			}
			if !tc.expectPass && err == nil {
				t.Fatalf("expected FAIL but got nil")
			}
		})
	}
}
