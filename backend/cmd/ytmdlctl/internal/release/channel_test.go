package release

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/manifest"
	"ytdm/backend/internal/update"
)

type fakeRelease struct {
	tag         string
	pre, draft  bool
	full        bool
	manifestRaw []byte
}

func serveReleases(t *testing.T, releases ...fakeRelease) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	body := func(r fakeRelease) string {
		assets := []string{}
		if r.manifestRaw != nil {
			assets = append(assets, fmt.Sprintf(`{"name":"release-manifest.json","browser_download_url":"%s/download/%s"}`, srv.URL, r.tag))
		}
		if r.full {
			for _, a := range update.RequiredPrereleaseAssets[1:] {
				assets = append(assets, fmt.Sprintf(`{"name":%q,"browser_download_url":"%s/x"}`, a, srv.URL))
			}
		}
		return fmt.Sprintf(`{"tag_name":%q,"draft":%t,"prerelease":%t,"assets":[%s]}`, r.tag, r.draft, r.pre, strings.Join(assets, ","))
	}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/repos/Der-Felix/ytmdl/releases" {
			parts := make([]string, 0, len(releases))
			for _, r := range releases {
				parts = append(parts, body(r))
			}
			fmt.Fprintf(w, "[%s]", strings.Join(parts, ","))
			return
		}
		for _, r := range releases {
			if req.URL.Path == "/repos/Der-Felix/ytmdl/releases/tags/"+r.tag {
				fmt.Fprint(w, body(r))
				return
			}
			if req.URL.Path == "/download/"+r.tag {
				_, _ = w.Write(r.manifestRaw)
				return
			}
		}
		http.NotFound(w, req)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func genManifest(t *testing.T, version string, mv int, channel string) []byte {
	t.Helper()
	data, err := manifest.Generate(manifest.GeneratorOptions{
		ManifestVersion: mv, ReleaseVersion: version, TargetSchema: 12, MinUpgradeFrom: "0.15.0",
		BackendDigest:     "sha256:" + strings.Repeat("1", 64),
		BackendPlatforms:  map[string]string{"linux/amd64": "sha256:" + strings.Repeat("2", 64), "linux/arm64": "sha256:" + strings.Repeat("3", 64)},
		FrontendDigest:    "sha256:" + strings.Repeat("4", 64),
		FrontendPlatforms: map[string]string{"linux/amd64": "sha256:" + strings.Repeat("5", 64), "linux/arm64": "sha256:" + strings.Repeat("6", 64)},
		SourceCommit:      strings.Repeat("c", 40), Channel: channel,
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestExplicitTargetsFollowChannelRules(t *testing.T) {
	rc := genManifest(t, "0.27.2-rc.1", 4, "development")
	srv := serveReleases(t,
		fakeRelease{tag: "v0.27.2-rc.1", pre: true, full: true, manifestRaw: rc},
		fakeRelease{tag: "v0.27.2-rc.2", pre: true, manifestRaw: rc},         // incomplete publication
		fakeRelease{tag: "v0.27.3-rc.1", pre: true, draft: true, full: true}, // draft
		fakeRelease{tag: "v0.27.4", pre: true, full: true},                   // flag and version disagree
		fakeRelease{tag: "v0.27.1", full: true, manifestRaw: genManifest(t, "0.27.1", 3, "")},
	)
	c := NewClient(srv.URL, "", "test", srv.Client())
	ctx := context.Background()

	if _, err := c.FetchTagForChannel(ctx, "0.27.2-rc.1", update.ChannelStable); err == nil || !strings.Contains(err.Error(), "--channel development") {
		t.Fatalf("prerelease on stable: %v", err)
	}
	rel, err := c.FetchTagForChannel(ctx, "0.27.2-rc.1", update.ChannelDevelopment)
	if err != nil || rel.Version != "0.27.2-rc.1" {
		t.Fatalf("qualified rc: %+v %v", rel, err)
	}
	if _, err := c.DownloadManifest(ctx, rel); err != nil {
		t.Fatalf("rc manifest: %v", err)
	}
	for tag, want := range map[string]string{"0.27.2-rc.2": "not a qualified", "0.27.3-rc.1": "draft", "0.27.4": "not a qualified"} {
		if _, err := c.FetchTagForChannel(ctx, tag, update.ChannelDevelopment); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", tag, err)
		}
	}
	// A regular release can be targeted explicitly from either channel.
	for _, ch := range []update.Channel{update.ChannelStable, update.ChannelDevelopment} {
		if rel, err := c.FetchTagForChannel(ctx, "v0.27.1", ch); err != nil || rel.Version != "0.27.1" {
			t.Errorf("%s: %+v %v", ch, rel, err)
		}
	}
	if _, err := c.FetchTagForChannel(ctx, "latest", update.ChannelDevelopment); err == nil {
		t.Error("a non-semver tag was requested")
	}
}

func TestDevelopmentChannelSelectsHighestQualifiedPrerelease(t *testing.T) {
	srv := serveReleases(t,
		fakeRelease{tag: "v0.27.2-rc.2", pre: true, full: true, manifestRaw: []byte("{}")},
		fakeRelease{tag: "v0.27.2-rc.10", pre: true, full: true, manifestRaw: []byte("{}")},
		fakeRelease{tag: "v0.27.2-rc.11", pre: true},
		fakeRelease{tag: "v0.28.0-rc.1", pre: true, draft: true, full: true, manifestRaw: []byte("{}")},
		fakeRelease{tag: "v0.27.1", full: true},
	)
	rel, err := NewClient(srv.URL, "", "test", srv.Client()).FetchForChannel(context.Background(), update.ChannelDevelopment)
	if err != nil || rel.TagName != "v0.27.2-rc.10" {
		t.Fatalf("%+v %v", rel, err)
	}
	empty := serveReleases(t, fakeRelease{tag: "v0.27.1", full: true})
	if _, err := NewClient(empty.URL, "", "test", empty.Client()).FetchForChannel(context.Background(), update.ChannelDevelopment); err == nil {
		t.Fatal("development channel invented a release")
	}
}

func TestManifestMustMatchGitHubPrereleaseFlag(t *testing.T) {
	srv := serveReleases(t,
		// A development manifest on a release GitHub shows as stable.
		fakeRelease{tag: "v0.27.2-rc.1", full: true, manifestRaw: genManifest(t, "0.27.2-rc.1", 4, "development")},
	)
	c := NewClient(srv.URL, "", "test", srv.Client())
	rel := &ReleaseInfo{TagName: "v0.27.2-rc.1", Prerelease: false, Assets: []Asset{{Name: "release-manifest.json", BrowserDownloadURL: srv.URL + "/download/v0.27.2-rc.1"}}}
	if _, err := c.DownloadManifest(context.Background(), rel); err == nil || !strings.Contains(err.Error(), "does not match the GitHub release") {
		t.Fatalf("mismatch accepted: %v", err)
	}
	// A prerelease whose manifest is an older, channel-less version is rejected.
	old := serveReleases(t, fakeRelease{tag: "v0.27.2-rc.1", pre: true, full: true, manifestRaw: []byte(strings.Replace(string(genManifest(t, "0.27.1", 3, "")), "0.27.1", "0.27.2-rc.1", -1))})
	rel2 := &ReleaseInfo{TagName: "v0.27.2-rc.1", Prerelease: true, Assets: []Asset{{Name: "release-manifest.json", BrowserDownloadURL: old.URL + "/download/v0.27.2-rc.1"}}}
	if _, err := NewClient(old.URL, "", "test", old.Client()).DownloadManifest(context.Background(), rel2); err == nil || !strings.Contains(err.Error(), "requires manifest version 4") {
		t.Fatalf("v3 prerelease manifest accepted: %v", err)
	}
}
