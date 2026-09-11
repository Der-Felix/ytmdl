package update

import (
	"fmt"
	"strings"
)

// Channel names the stream of releases an installation follows. The backend,
// the UI and ytmdlctl apply exactly the same rules through this file, so they
// can never disagree about which release a channel offers.
type Channel string

const (
	// ChannelStable offers regular, published releases only - never drafts,
	// never prereleases. It is the default for every installation.
	ChannelStable Channel = "stable"
	// ChannelDevelopment offers explicitly published, qualified prereleases.
	// An administrator has to choose it deliberately.
	ChannelDevelopment Channel = "development"
)

// DefaultChannel is the channel of an installation that never chose one.
const DefaultChannel = ChannelStable

// SettingsKeyChannel is the settings key the chosen channel is stored under.
// It is the single source of truth: the UI writes it, the backend and
// ytmdlctl read it.
const SettingsKeyChannel = "updates.channel"

// ParseChannel accepts a channel name. An empty value selects the default.
func ParseChannel(raw string) (Channel, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return DefaultChannel, nil
	case string(ChannelStable):
		return ChannelStable, nil
	case string(ChannelDevelopment):
		return ChannelDevelopment, nil
	default:
		return "", fmt.Errorf("unknown update channel %q (expected %q or %q)", raw, ChannelStable, ChannelDevelopment)
	}
}

// RequiredPrereleaseAssets are the assets the release pipeline publishes for
// every qualified release. A prerelease lacking any of them was not produced
// by the qualified pipeline (or its publication is incomplete) and is never
// offered.
var RequiredPrereleaseAssets = []string{
	"release-manifest.json",
	"SHA256SUMS",
	"ytmdlctl-linux-amd64",
	"ytmdlctl-linux-arm64",
	"ytmdlctl-darwin-amd64",
	"ytmdlctl-darwin-arm64",
}

// ReleaseCandidate is the channel-relevant part of a GitHub release.
type ReleaseCandidate struct {
	Tag        string
	Draft      bool
	Prerelease bool
	AssetNames []string
}

// Eligible reports whether a release belongs to channel. The GitHub flag and
// the version must agree: a release flagged as prerelease must carry a
// prerelease version and vice versa, so a mislabelled release is never
// offered on either channel.
func (c ReleaseCandidate) Eligible(channel Channel) (SemVer, bool) {
	if c.Draft {
		return SemVer{}, false
	}
	v, err := ParseSemVer(c.Tag)
	if err != nil {
		return SemVer{}, false
	}
	isPre := v.PreRelease != ""
	if isPre != c.Prerelease {
		return SemVer{}, false
	}
	switch channel {
	case ChannelStable:
		return v, !isPre
	case ChannelDevelopment:
		return v, isPre && c.hasAssets(RequiredPrereleaseAssets)
	default:
		return SemVer{}, false
	}
}

func (c ReleaseCandidate) hasAssets(required []string) bool {
	have := make(map[string]bool, len(c.AssetNames))
	for _, name := range c.AssetNames {
		have[name] = true
	}
	for _, name := range required {
		if !have[name] {
			return false
		}
	}
	return true
}

// SelectLatest returns the index of the highest SemVer release channel
// offers, or -1 when it offers none.
func SelectLatest(candidates []ReleaseCandidate, channel Channel) int {
	best := -1
	var bestVersion SemVer
	for i, c := range candidates {
		v, ok := c.Eligible(channel)
		if !ok {
			continue
		}
		if best < 0 || v.Compare(bestVersion) > 0 {
			best, bestVersion = i, v
		}
	}
	return best
}
