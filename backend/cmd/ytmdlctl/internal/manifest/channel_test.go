package manifest_test

import (
	"strings"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/manifest"
	"ytdm/backend/internal/update"
)

func channelOpts(version string, mv int, commit, channel string) manifest.GeneratorOptions {
	return manifest.GeneratorOptions{
		ManifestVersion: mv, ReleaseVersion: version, TargetSchema: 12, MinUpgradeFrom: "0.15.0",
		BackendDigest:     "sha256:" + strings.Repeat("1", 64),
		BackendPlatforms:  map[string]string{"linux/amd64": "sha256:" + strings.Repeat("2", 64), "linux/arm64": "sha256:" + strings.Repeat("3", 64)},
		FrontendDigest:    "sha256:" + strings.Repeat("4", 64),
		FrontendPlatforms: map[string]string{"linux/amd64": "sha256:" + strings.Repeat("5", 64), "linux/arm64": "sha256:" + strings.Repeat("6", 64)},
		SourceCommit:      commit, Channel: channel,
	}
}

func TestManifestV4TiesVersionChannelAndCommit(t *testing.T) {
	commit := strings.Repeat("0123456789abcdef", 3)[:40]
	for _, tc := range []struct {
		name    string
		opts    manifest.GeneratorOptions
		wantErr string
	}{
		{"prerelease v4 development", channelOpts("0.27.2-rc.1", 4, commit, "development"), ""},
		{"stable v4", channelOpts("0.27.2", 4, commit, "stable"), ""},
		{"stable v3 without channel", channelOpts("0.27.2", 3, "", ""), ""},
		{"prerelease v3", channelOpts("0.27.2-rc.1", 3, "", ""), "requires manifest version 4"},
		{"prerelease on stable channel", channelOpts("0.27.2-rc.1", 4, commit, "stable"), "must be published on the development channel"},
		{"stable on development channel", channelOpts("0.27.2", 4, commit, "development"), "must be published on the stable channel"},
		{"short commit", channelOpts("0.27.2-rc.1", 4, "abc1234", "development"), "40 character"},
		{"missing channel", channelOpts("0.27.2-rc.1", 4, commit, ""), "requires channel"},
		{"unknown channel", channelOpts("0.27.2-rc.1", 4, commit, "nightly"), "requires channel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := manifest.Generate(tc.opts)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				m, derr := manifest.Decode(data)
				if derr != nil {
					t.Fatal(derr)
				}
				if verr := m.Validate("v" + tc.opts.ReleaseVersion); verr != nil {
					t.Fatalf("round trip failed: %v", verr)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestOlderManifestsStayStableAndCompatible(t *testing.T) {
	data, err := manifest.Generate(channelOpts("0.27.1", 3, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "source_commit") || strings.Contains(string(data), "channel") {
		t.Fatalf("a v3 manifest carries v4 fields, which older ytmdlctl binaries would reject:\n%s", data)
	}
	m, err := manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if m.ReleaseChannel() != update.ChannelStable {
		t.Fatalf("v3 manifest channel = %q", m.ReleaseChannel())
	}
	if p, err := m.FindUpgradePath(12); err != nil || p.RollbackClassification != manifest.RollbackSchemaNeutral {
		t.Fatalf("upgrade path %+v %v", p, err)
	}
	// A v3 manifest that smuggles v4 fields is rejected.
	injected := strings.Replace(string(data), `"required_env"`, `"channel": "development",`+"\n  "+`"required_env"`, 1)
	if m, err := manifest.Decode([]byte(injected)); err == nil {
		if verr := m.Validate("v0.27.1"); verr == nil {
			t.Fatal("v3 manifest with a channel field validated")
		}
	}
}

func TestManifestV4UpgradePathsLikeV3(t *testing.T) {
	data, err := manifest.Generate(channelOpts("0.27.2-rc.1", 4, strings.Repeat("b", 40), "development"))
	if err != nil {
		t.Fatal(err)
	}
	m, _ := manifest.Decode(data)
	for _, src := range []int{8, 11, 12} {
		if _, err := m.FindUpgradePath(src); err != nil {
			t.Errorf("schema %d: %v", src, err)
		}
	}
	if _, err := m.FindUpgradePath(13); err == nil {
		t.Error("an unknown source schema had an upgrade path")
	}
	if m.ReleaseChannel() != update.ChannelDevelopment || m.SourceCommit != strings.Repeat("b", 40) {
		t.Fatalf("manifest %+v", m)
	}
}
