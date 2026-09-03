package update

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var semverRegex = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+([0-9A-Za-z.-]+))?$`)

// SemVer represents a parsed Semantic Version 2.0.0.
type SemVer struct {
	Major      int
	Minor      int
	Patch      int
	PreRelease string
}

// ParseSemVer parses a version string (with or without leading 'v') into SemVer.
func ParseSemVer(v string) (SemVer, error) {
	v = strings.TrimSpace(v)
	matches := semverRegex.FindStringSubmatch(v)
	if matches == nil {
		return SemVer{}, fmt.Errorf("invalid semver string: %q", v)
	}

	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])
	pre := ""
	if len(matches) > 4 {
		pre = matches[4]
	}

	return SemVer{
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		PreRelease: pre,
	}, nil
}

// Compare compares two SemVer versions according to SemVer 2.0.0 rules:
// - returns -1 if a < b
// - returns 0 if a == b
// - returns 1 if a > b
func (a SemVer) Compare(b SemVer) int {
	if a.Major != b.Major {
		if a.Major < b.Major {
			return -1
		}
		return 1
	}
	if a.Minor != b.Minor {
		if a.Minor < b.Minor {
			return -1
		}
		return 1
	}
	if a.Patch != b.Patch {
		if a.Patch < b.Patch {
			return -1
		}
		return 1
	}

	// According to SemVer 2.0.0:
	// A normal version has higher precedence than a pre-release version with the same major.minor.patch.
	if a.PreRelease == "" && b.PreRelease != "" {
		return 1
	}
	if a.PreRelease != "" && b.PreRelease == "" {
		return -1
	}
	if a.PreRelease == b.PreRelease {
		return 0
	}

	// Lexicographical comparison of dot-separated identifiers for pre-release
	aParts := strings.Split(a.PreRelease, ".")
	bParts := strings.Split(b.PreRelease, ".")
	minLen := len(aParts)
	if len(bParts) < minLen {
		minLen = len(bParts)
	}

	for i := 0; i < minLen; i++ {
		aPart := aParts[i]
		bPart := bParts[i]
		if aPart == bPart {
			continue
		}
		aNum, aErr := strconv.Atoi(aPart)
		bNum, bErr := strconv.Atoi(bPart)
		if aErr == nil && bErr == nil {
			if aNum < bNum {
				return -1
			}
			return 1
		}
		if aErr == nil && bErr != nil {
			return -1 // numeric identifiers always have lower precedence than non-numeric
		}
		if aErr != nil && bErr == nil {
			return 1
		}
		if aPart < bPart {
			return -1
		}
		return 1
	}

	if len(aParts) < len(bParts) {
		return -1
	}
	if len(aParts) > len(bParts) {
		return 1
	}
	return 0
}

// String returns the normalized string representation (without leading 'v').
func (a SemVer) String() string {
	res := fmt.Sprintf("%d.%d.%d", a.Major, a.Minor, a.Patch)
	if a.PreRelease != "" {
		res += "-" + a.PreRelease
	}
	return res
}
