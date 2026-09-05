package driver

import (
	"strconv"
	"strings"
)

func parseVersionParts(v string) []int {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, _ := strconv.Atoi(strings.TrimSpace(p))
		out[i] = n
	}
	return out
}

// compareVersions compares two dot-separated numeric version strings
// component-wise, treating a missing trailing component as 0, and returns
// <0, 0, >0 like strings.Compare. This is a simplified stand-in for
// PowerShell's [version] comparison (real .NET semantics treat a missing
// component as -1 rather than 0, which only differs from this when comparing
// versions with different component counts - not expected in practice since
// every DriverVer value seen in the field has 4 dotted components).
// CompareVersions is the exported form of compareVersions, for callers
// outside this package that need to compare an installed driver's version
// (read from the registry, not this package's own catalog) against a
// ResolvedDriver's Version.
func CompareVersions(a, b string) int {
	return compareVersions(a, b)
}

func compareVersions(a, b string) int {
	pa, pb := parseVersionParts(a), parseVersionParts(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
