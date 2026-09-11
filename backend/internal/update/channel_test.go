package update

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSemVerPrereleaseOrdering(t *testing.T) {
	ordered := []string{
		"0.27.1",
		"0.27.2-rc.1",
		"0.27.2-rc.2",
		"0.27.2-rc.10",
		"0.27.2",
		"0.27.3-rc.1",
		"0.28.0",
	}
	shuffled := []string{"0.27.2-rc.10", "0.28.0", "0.27.2", "0.27.1", "0.27.2-rc.2", "0.27.3-rc.1", "0.27.2-rc.1"}
	sort.Slice(shuffled, func(i, j int) bool {
		a, _ := ParseSemVer(shuffled[i])
		b, _ := ParseSemVer(shuffled[j])
		return a.Compare(b) < 0
	})
	if strings.Join(shuffled, " ") != strings.Join(ordered, " ") {
		t.Fatalf("order = %v, want %v", shuffled, ordered)
	}
}

func TestParseChannel(t *testing.T) {
	for raw, want := range map[string]Channel{"": ChannelStable, "stable": ChannelStable, " Development ": ChannelDevelopment} {
		got, err := ParseChannel(raw)
		if err != nil || got != want {
			t.Errorf("ParseChannel(%q) = %q, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"beta", "nightly", "dev"} {
		if _, err := ParseChannel(raw); err == nil {
			t.Errorf("ParseChannel(%q) accepted an unknown channel", raw)
		}
	}
}

var allAssets = append([]string(nil), RequiredPrereleaseAssets...)

func TestChannelEligibility(t *testing.T) {
	for _, tc := range []struct {
		name        string
		c           ReleaseCandidate
		stable, dev bool
	}{
		{"stable release", ReleaseCandidate{Tag: "v0.27.1"}, true, false},
		{"qualified prerelease", ReleaseCandidate{Tag: "v0.27.2-rc.1", Prerelease: true, AssetNames: allAssets}, false, true},
		{"prerelease without manifest", ReleaseCandidate{Tag: "v0.27.2-rc.1", Prerelease: true, AssetNames: []string{"SHA256SUMS"}}, false, false},
		{"draft stable", ReleaseCandidate{Tag: "v0.27.2", Draft: true}, false, false},
		{"draft prerelease", ReleaseCandidate{Tag: "v0.27.2-rc.1", Draft: true, Prerelease: true, AssetNames: allAssets}, false, false},
		{"prerelease flag on stable version", ReleaseCandidate{Tag: "v0.27.2", Prerelease: true, AssetNames: allAssets}, false, false},
		{"rc version not flagged", ReleaseCandidate{Tag: "v0.27.2-rc.1", AssetNames: allAssets}, false, false},
		{"not semver", ReleaseCandidate{Tag: "nightly"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := tc.c.Eligible(ChannelStable); ok != tc.stable {
				t.Errorf("stable eligible = %v, want %v", ok, tc.stable)
			}
			if _, ok := tc.c.Eligible(ChannelDevelopment); ok != tc.dev {
				t.Errorf("development eligible = %v, want %v", ok, tc.dev)
			}
		})
	}
}

func TestSelectLatestAcrossManyVersions(t *testing.T) {
	cands := []ReleaseCandidate{
		{Tag: "v0.27.2-rc.2", Prerelease: true, AssetNames: allAssets},
		{Tag: "v0.27.1"},
		{Tag: "v0.27.2-rc.10", Prerelease: true, AssetNames: allAssets},
		{Tag: "v0.27.3-rc.1", Prerelease: true, Draft: true, AssetNames: allAssets},
		{Tag: "v0.27.0"},
		{Tag: "v0.28.0-rc.1", Prerelease: true},
	}
	if i := SelectLatest(cands, ChannelDevelopment); i < 0 || cands[i].Tag != "v0.27.2-rc.10" {
		t.Fatalf("development selected %d", i)
	}
	if i := SelectLatest(cands, ChannelStable); i < 0 || cands[i].Tag != "v0.27.1" {
		t.Fatalf("stable selected %d", i)
	}
	if i := SelectLatest(nil, ChannelStable); i != -1 {
		t.Fatalf("empty list selected %d", i)
	}
}

// fakeGitHub serves a release list and the latest stable release.
type fakeGitHub struct {
	calls    atomic.Int32
	releases []map[string]any
	fail     bool
}

func release(tag string, prerelease, draft bool, assets ...string) map[string]any {
	list := make([]map[string]any, 0, len(assets))
	for _, a := range assets {
		list = append(list, map[string]any{"name": a})
	}
	return map[string]any{
		"tag_name": tag, "name": "YTMDL " + tag, "draft": draft, "prerelease": prerelease,
		"published_at": "2026-09-11T12:00:00Z", "html_url": "https://github.com/Der-Felix/ytmdl/releases/tag/" + tag,
		"body": "notes for " + tag, "assets": list,
	}
}

func (g *fakeGitHub) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.calls.Add(1)
		if g.fail {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		switch r.URL.Path {
		case "/repos/Der-Felix/ytmdl/releases":
			_ = json.NewEncoder(w).Encode(g.releases)
		case "/repos/Der-Felix/ytmdl/releases/latest":
			// GitHub's "latest" is the newest regular, published release.
			var best map[string]any
			var bestV SemVer
			for _, rel := range g.releases {
				if rel["draft"].(bool) || rel["prerelease"].(bool) {
					continue
				}
				v, _ := ParseSemVer(rel["tag_name"].(string))
				if best == nil || v.Compare(bestV) > 0 {
					best, bestV = rel, v
				}
			}
			if best == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(best)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func standardReleases() []map[string]any {
	return []map[string]any{
		release("v0.27.3-rc.1", true, true, allAssets...),  // draft: never offered
		release("v0.27.2-rc.2", true, false, "SHA256SUMS"), // incomplete publication
		release("v0.27.2-rc.1", true, false, allAssets...),
		release("v0.27.1", false, false, allAssets...),
		release("v0.27.0", false, false, allAssets...),
	}
}

type memoryStore struct {
	mu     sync.Mutex
	values map[string]string
	writes int
	err    error
}

func (m *memoryStore) All(context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for k, v := range m.values {
		out[k] = v
	}
	return out, nil
}

func (m *memoryStore) SetMany(_ context.Context, values map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.writes++
	for k, v := range values {
		m.values[k] = v
	}
	return nil
}

func newChannelService(t *testing.T, current string, gh *fakeGitHub, store *memoryStore) *Service {
	t.Helper()
	srv := gh.server(t)
	s := NewService(Config{Enabled: true, BaseURL: srv.URL}, current, srv.Client(), nil)
	if store != nil {
		if err := s.UseSettings(context.Background(), store); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestStableChannelIgnoresPrereleasesAndDrafts(t *testing.T) {
	gh := &fakeGitHub{releases: standardReleases()}
	s := newChannelService(t, "0.27.0", gh, nil)
	st, _ := s.GetStatus(context.Background(), true)
	if st.Channel != ChannelStable || st.State != StateUpdateAvailable || st.LatestVersion != "0.27.1" || st.LatestPrerelease {
		t.Fatalf("status %+v", st)
	}
	want := []string{"ytmdlctl update --channel stable --target 0.27.1 --dry-run", "ytmdlctl update --channel stable --target 0.27.1"}
	if strings.Join(st.UpdateCommands, "|") != strings.Join(want, "|") {
		t.Fatalf("commands %v", st.UpdateCommands)
	}
}

func TestDevelopmentChannelFindsPublishedRC(t *testing.T) {
	gh := &fakeGitHub{releases: standardReleases()}
	store := &memoryStore{values: map[string]string{SettingsKeyChannel: "development"}}
	s := newChannelService(t, "0.27.1", gh, store)
	st, _ := s.GetStatus(context.Background(), true)
	if st.Channel != ChannelDevelopment || st.State != StateUpdateAvailable {
		t.Fatalf("status %+v", st)
	}
	// rc.2 lacks its manifest and rc.3 is a draft: rc.1 is the qualified one.
	if st.LatestVersion != "0.27.2-rc.1" || !st.LatestPrerelease || st.ReleaseNotes != "notes for v0.27.2-rc.1" {
		t.Fatalf("target %+v", st)
	}
	if st.UpdateCommands[1] != "ytmdlctl update --channel development --target 0.27.2-rc.1" {
		t.Fatalf("commands %v", st.UpdateCommands)
	}
}

func TestDevelopmentChannelPointsToNewerStableRelease(t *testing.T) {
	gh := &fakeGitHub{releases: append(standardReleases(), release("v0.27.2", false, false, allAssets...))}
	store := &memoryStore{values: map[string]string{SettingsKeyChannel: "development"}}
	s := newChannelService(t, "0.27.2-rc.1", gh, store)
	st, _ := s.GetStatus(context.Background(), true)
	if st.State != StateUpToDate || st.NewerStableVersion != "0.27.2" || len(st.UpdateCommands) != 0 {
		t.Fatalf("status %+v", st)
	}
}

func TestSwitchingBackToStableNeverOffersADowngrade(t *testing.T) {
	gh := &fakeGitHub{releases: standardReleases()}
	store := &memoryStore{values: map[string]string{SettingsKeyChannel: "development"}}
	s := newChannelService(t, "0.27.2-rc.1", gh, store)

	if err := s.SetChannel(context.Background(), ChannelStable); err != nil {
		t.Fatal(err)
	}
	st, _ := s.GetStatus(context.Background(), false)
	if st.Channel != ChannelStable || st.State != StateAheadOfChannel || st.LatestVersion != "0.27.1" {
		t.Fatalf("status %+v", st)
	}
	if len(st.UpdateCommands) != 0 {
		t.Fatalf("a downgrade was offered: %v", st.UpdateCommands)
	}
	if !st.CurrentPrerelease {
		t.Fatal("installed prerelease not marked")
	}
}

func TestChannelChangeOnlyRecordsTheChoice(t *testing.T) {
	gh := &fakeGitHub{releases: standardReleases()}
	store := &memoryStore{values: map[string]string{}}
	s := newChannelService(t, "0.27.1", gh, store)
	if _, err := s.GetStatus(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	before := gh.calls.Load()

	if err := s.SetChannel(context.Background(), ChannelDevelopment); err != nil {
		t.Fatal(err)
	}
	if gh.calls.Load() != before {
		t.Fatal("changing the channel contacted GitHub")
	}
	if store.values[SettingsKeyChannel] != "development" || store.writes != 1 {
		t.Fatalf("channel not persisted: %+v", store.values)
	}
	// The cached stable answer is not reused for the new channel.
	st, _ := s.GetStatus(context.Background(), false)
	if st.Channel != ChannelDevelopment || st.Cached {
		t.Fatalf("stale answer after the channel change: %+v", st)
	}

	if err := s.SetChannel(context.Background(), "nightly"); err == nil {
		t.Fatal("an unknown channel was accepted")
	}
	store.err = errors.New("disk full")
	if err := s.SetChannel(context.Background(), ChannelStable); err == nil || s.Channel() != ChannelDevelopment {
		t.Fatal("a failed write changed the channel in memory")
	}
}

func TestStoredChannelSurvivesRestartAndInvalidFallsBack(t *testing.T) {
	gh := &fakeGitHub{releases: standardReleases()}
	if s := newChannelService(t, "0.27.1", gh, &memoryStore{values: map[string]string{SettingsKeyChannel: "development"}}); s.Channel() != ChannelDevelopment {
		t.Fatal("stored channel not loaded")
	}
	if s := newChannelService(t, "0.27.1", gh, &memoryStore{values: map[string]string{SettingsKeyChannel: "nightly"}}); s.Channel() != ChannelStable {
		t.Fatal("invalid stored channel did not fall back to stable")
	}
	if s := newChannelService(t, "0.27.1", gh, &memoryStore{values: map[string]string{}}); s.Channel() != ChannelStable {
		t.Fatal("existing installation without a stored channel is not stable")
	}
}

func TestNetworkFailureIsNotNoUpdate(t *testing.T) {
	for _, channel := range []Channel{ChannelStable, ChannelDevelopment} {
		gh := &fakeGitHub{releases: standardReleases(), fail: true}
		store := &memoryStore{values: map[string]string{SettingsKeyChannel: string(channel)}}
		s := newChannelService(t, "0.27.1", gh, store)
		st, _ := s.GetStatus(context.Background(), true)
		if st.State != StateUnavailable || st.Failure != FailureUnexpectedStatus {
			t.Fatalf("%s: status %+v", channel, st)
		}
	}
	// A refused connection is a network failure.
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	s := NewService(Config{Enabled: true, BaseURL: url}, "0.27.1", &http.Client{Timeout: time.Second}, nil)
	if st, _ := s.GetStatus(context.Background(), true); st.State != StateUnavailable || st.Failure != FailureNetwork {
		t.Fatalf("status %+v", st)
	}
}

func TestDevelopmentChannelWithoutQualifiedPrerelease(t *testing.T) {
	gh := &fakeGitHub{releases: []map[string]any{release("v0.27.1", false, false, allAssets...), release("v0.27.2-rc.1", true, false)}}
	store := &memoryStore{values: map[string]string{SettingsKeyChannel: "development"}}
	s := newChannelService(t, "0.27.1", gh, store)
	st, _ := s.GetStatus(context.Background(), true)
	if st.State != StateNoChannelRelease || st.LatestVersion != "" {
		t.Fatalf("status %+v", st)
	}
}
