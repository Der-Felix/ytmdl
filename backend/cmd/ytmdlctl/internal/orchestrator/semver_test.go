package orchestrator

import "testing"

func TestIsSemverGreaterHonoursPrereleases(t *testing.T) {
	for _, tc := range []struct {
		target, current string
		want            bool
	}{
		{"0.27.2", "0.27.2-rc.1", true}, // final after its candidates
		{"0.27.2-rc.1", "0.27.1", true},
		{"0.27.2-rc.2", "0.27.2-rc.1", true},
		{"0.27.2-rc.10", "0.27.2-rc.2", true},
		{"0.27.2-rc.1", "0.27.2", false},
		{"0.27.1", "0.27.2-rc.1", false}, // no silent downgrade
		{"0.27.2-rc.1", "0.27.2-rc.1", false},
		{"v0.28.0", "0.27.9", true},
		{"garbage", "0.27.1", false},
	} {
		if got := isSemverGreater(tc.target, tc.current); got != tc.want {
			t.Errorf("isSemverGreater(%q, %q) = %v, want %v", tc.target, tc.current, got, tc.want)
		}
	}
}
