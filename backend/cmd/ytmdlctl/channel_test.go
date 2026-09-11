package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/manifest"
	"ytdm/backend/cmd/ytmdlctl/internal/runner"
	"ytdm/backend/internal/update"
)

func TestResolveChannelPrecedence(t *testing.T) {
	stored := func(v string) func() (string, error) { return func() (string, error) { return v, nil } }
	unreadable := func() (string, error) { return "", errors.New("db down") }

	for _, tc := range []struct {
		name   string
		flag   string
		reader func() (string, error)
		want   update.Channel
		source string
	}{
		{"flag wins over setting", "stable", stored("development"), update.ChannelStable, "--channel flag"},
		{"setting used without flag", "", stored("development"), update.ChannelDevelopment, "installation setting"},
		{"no setting is stable", "", stored(""), update.ChannelStable, "default (no channel chosen)"},
		{"invalid setting is stable", "", stored("nightly"), update.ChannelStable, `default (stored value "nightly" is not a channel)`},
		{"unreadable setting is stable", "", unreadable, update.ChannelStable, "default (installation setting could not be read)"},
		{"no installation is stable", "", nil, update.ChannelStable, "default (installation setting could not be read)"},
		{"flag without installation", "development", nil, update.ChannelDevelopment, "--channel flag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveChannel(tc.flag, tc.reader)
			if err != nil || got.Channel != tc.want || got.Source != tc.source {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
	if _, err := resolveChannel("beta", nil); err == nil {
		t.Fatal("unknown --channel accepted")
	}

	var out bytes.Buffer
	choice, _ := resolveChannel("development", stored("stable"))
	choice.describe(&out)
	if !strings.Contains(out.String(), `overrides the installation setting "stable" for this run only`) {
		t.Fatalf("override not reported: %s", out.String())
	}
}

func TestCLIMustMatchPrereleaseTarget(t *testing.T) {
	var warn bytes.Buffer
	if err := checkCLIMatchesTarget("0.27.2-rc.1", "0.27.2-rc.1", &warn); err != nil {
		t.Fatalf("matching CLI rejected: %v", err)
	}
	if err := checkCLIMatchesTarget("0.27.1", "0.27.2-rc.1", &warn); err == nil || !strings.Contains(err.Error(), "ytmdlctl 0.27.2-rc.1 from the same release") {
		t.Fatalf("older CLI accepted for a prerelease: %v", err)
	}
	if err := checkCLIMatchesTarget("0.27.2-rc.2", "0.27.2-rc.1", &warn); err == nil {
		t.Fatal("different prerelease CLI accepted")
	}
	warn.Reset()
	if err := checkCLIMatchesTarget("0.27.1", "0.27.2", &warn); err != nil || !strings.Contains(warn.String(), "older than the target") {
		t.Fatalf("stable target: err=%v warn=%q", err, warn.String())
	}
	warn.Reset()
	if err := checkCLIMatchesTarget("dev", "0.27.2-rc.1", &warn); err != nil || !strings.Contains(warn.String(), "development build") {
		t.Fatalf("dev build: err=%v warn=%q", err, warn.String())
	}
}

// channelGitHub serves a release list with a stable release, a qualified RC,
// an incompletely published RC and a draft, plus their manifests.
func channelGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	commit := strings.Repeat("a", 40)
	manifests := map[string][]byte{}
	for _, v := range []struct {
		version string
		mv      int
		channel string
	}{{"0.27.1", 3, ""}, {"0.27.2-rc.1", 4, "development"}} {
		data, err := manifest.Generate(manifest.GeneratorOptions{
			ManifestVersion: v.mv, ReleaseVersion: v.version, TargetSchema: 12, MinUpgradeFrom: "0.15.0",
			BackendDigest:     "sha256:" + strings.Repeat("1", 64),
			BackendPlatforms:  map[string]string{"linux/amd64": "sha256:" + strings.Repeat("2", 64), "linux/arm64": "sha256:" + strings.Repeat("3", 64)},
			FrontendDigest:    "sha256:" + strings.Repeat("4", 64),
			FrontendPlatforms: map[string]string{"linux/amd64": "sha256:" + strings.Repeat("5", 64), "linux/arm64": "sha256:" + strings.Repeat("6", 64)},
			SourceCommit:      commit, Channel: v.channel,
		})
		if err != nil {
			t.Fatal(err)
		}
		manifests[v.version] = data
	}

	var srv *httptest.Server
	rel := func(tag string, pre, draft bool, full bool) string {
		assets := []string{fmt.Sprintf(`{"name":"release-manifest.json","browser_download_url":"%s/download/%s/release-manifest.json"}`, srv.URL, strings.TrimPrefix(tag, "v"))}
		if full {
			for _, a := range update.RequiredPrereleaseAssets[1:] {
				assets = append(assets, fmt.Sprintf(`{"name":%q,"browser_download_url":"%s/download/x"}`, a, srv.URL))
			}
		}
		return fmt.Sprintf(`{"tag_name":%q,"name":"YTMDL %s","draft":%t,"prerelease":%t,"html_url":"https://github.com/Der-Felix/ytmdl/releases/tag/%s","assets":[%s]}`,
			tag, tag, draft, pre, tag, strings.Join(assets, ","))
	}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/Der-Felix/ytmdl/releases":
			fmt.Fprintf(w, "[%s,%s,%s,%s]", rel("v0.27.3-rc.1", true, true, true), rel("v0.27.2-rc.2", true, false, false), rel("v0.27.2-rc.1", true, false, true), rel("v0.27.1", false, false, true))
		case "/repos/Der-Felix/ytmdl/releases/latest":
			fmt.Fprint(w, rel("v0.27.1", false, false, true))
		case "/repos/Der-Felix/ytmdl/releases/tags/v0.27.2-rc.1":
			fmt.Fprint(w, rel("v0.27.2-rc.1", true, false, true))
		case "/download/0.27.1/release-manifest.json":
			_, _ = w.Write(manifests["0.27.1"])
		case "/download/0.27.2-rc.1/release-manifest.json":
			_, _ = w.Write(manifests["0.27.2-rc.1"])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// installation writes a compose project with the local override that carries
// the writable cookie mount.
func installation(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"compose.ghcr.yaml":          "services: {}\n",
		"compose.ghcr.override.yaml": "services:\n  backend:\n    volumes:\n      - ./cookies.txt:/run/secrets/ytmdl-youtube.cookies.txt:rw\n",
		".env":                       "YTMDL_VERSION=" + version + "\nPOSTGRES_USER=ytmdl\nPOSTGRES_DB=ytmdl\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func dockerWithStoredChannel(stored string) *runner.FakeProcessRunner {
	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{Stdout: []byte("Docker Compose v2.24.0\n")}, nil)
	fake.RegisterPrefix("docker", "compose -f", &runner.RunResult{Stdout: []byte(stored + "\n")}, nil)
	return fake
}

func TestCheckFollowsInstallationChannelWithoutSideEffects(t *testing.T) {
	gh := channelGitHub(t)
	dir := installation(t, "0.27.1")
	fake := dockerWithStoredChannel("development")

	var stdout, stderr bytes.Buffer
	code := runCLIWithDeps(context.Background(), []string{"--project-dir", dir, "--file", "compose.ghcr.yaml", "--engine", "docker", "check"},
		&stdout, &stderr, CLIDependencies{Runner: fake, GitHubURL: gh.URL, HTTPClient: gh.Client()})
	out := stdout.String()
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, out, stderr.String())
	}
	for _, want := range []string{
		"Update channel:            development (prereleases) [installation setting]",
		"Latest prerelease:         0.27.2-rc.1 (prerelease)",
		"State:                     update available",
		"Managed update metadata:   available (manifest v4 verified)",
		"Source commit:             " + strings.Repeat("a", 40),
		"ytmdlctl update --channel development --target 0.27.2-rc.1 --dry-run",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	var sawRead bool
	for _, call := range fake.Calls() {
		args := strings.Join(call.Args, " ")
		for _, forbidden := range []string{" pull", " up ", " down", " stop", " restart", " rm"} {
			if strings.Contains(args, forbidden) {
				t.Fatalf("check ran a mutating command: %s", args)
			}
		}
		if strings.Contains(args, "updates.channel") {
			sawRead = true
			if !strings.Contains(args, "-f compose.ghcr.override.yaml") && !strings.Contains(args, "compose.ghcr.override.yaml") {
				t.Fatalf("channel read without the local override: %s", args)
			}
			if strings.Contains(strings.ToUpper(args), "UPDATE ") || strings.Contains(strings.ToUpper(args), "INSERT ") {
				t.Fatalf("channel read is not read-only: %s", args)
			}
		}
	}
	if !sawRead {
		t.Fatal("the installation setting was not read")
	}
}

func TestCheckOnStableNeverOffersDowngradeFromPrerelease(t *testing.T) {
	gh := channelGitHub(t)
	dir := installation(t, "0.27.2-rc.1")

	var stdout, stderr bytes.Buffer
	code := runCLIWithDeps(context.Background(), []string{"--project-dir", dir, "--file", "compose.ghcr.yaml", "--engine", "docker", "check", "--channel", "stable"},
		&stdout, &stderr, CLIDependencies{Runner: dockerWithStoredChannel("development"), GitHubURL: gh.URL, HTTPClient: gh.Client()})
	out := stdout.String()
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, out, stderr.String())
	}
	if !strings.Contains(out, "Latest public release:     0.27.1") || !strings.Contains(out, "installed version is newer than this channel offers") {
		t.Fatalf("output:\n%s", out)
	}
	if strings.Contains(out, "ytmdlctl update --") {
		t.Fatalf("a downgrade command was offered:\n%s", out)
	}
	if !strings.Contains(out, `overrides the installation setting "development"`) {
		t.Fatalf("override of the stored channel not reported:\n%s", out)
	}
}

func TestCheckRejectsPrereleaseTargetOnStable(t *testing.T) {
	gh := channelGitHub(t)
	var stdout, stderr bytes.Buffer
	code := runCLIWithDeps(context.Background(), []string{"--project-dir", t.TempDir(), "check", "--target", "0.27.2-rc.1"},
		&stdout, &stderr, CLIDependencies{GitHubURL: gh.URL, HTTPClient: gh.Client()})
	if code != 1 || !strings.Contains(stderr.String(), "--channel development") {
		t.Fatalf("code %d stderr %s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = runCLIWithDeps(context.Background(), []string{"--project-dir", t.TempDir(), "check", "--channel", "development", "--target", "0.27.2-rc.1"},
		&stdout, &stderr, CLIDependencies{GitHubURL: gh.URL, HTTPClient: gh.Client()})
	if code != 0 || !strings.Contains(stdout.String(), "Requested release:         0.27.2-rc.1 (prerelease)") {
		t.Fatalf("code %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
}

func TestCheckNetworkFailureIsNotReportedAsUpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer srv.Close()
	for _, channel := range []string{"stable", "development"} {
		var stdout, stderr bytes.Buffer
		code := runCLIWithDeps(context.Background(), []string{"--project-dir", t.TempDir(), "check", "--channel", channel},
			&stdout, &stderr, CLIDependencies{GitHubURL: srv.URL, HTTPClient: srv.Client()})
		if code != 1 || strings.Contains(stdout.String(), "up to date") || !strings.Contains(stderr.String(), "error checking releases on the "+channel+" channel") {
			t.Fatalf("%s: code %d\n%s\n%s", channel, code, stdout.String(), stderr.String())
		}
	}
}
