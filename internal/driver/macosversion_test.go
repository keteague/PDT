package driver

import "testing"

// disableOSVersionFiltering overrides currentMacOSVersionPrefixFunc so
// filterToCurrentOSVersionFolder becomes a no-op for the rest of the test -
// most of this package's own tests build a MacCatalog to exercise something
// entirely unrelated to OS-version filtering, and without this they'd
// depend on whichever real macOS version happens to be running the test (a
// real risk, not hypothetical: this package's own fixtures conveniently
// span version folders "15" and "26" - filtering could silently exclude
// one, changing which package a test observes as "newest" for reasons that
// have nothing to do with what it's actually testing). Restored via
// t.Cleanup so it never leaks into a later test.
func disableOSVersionFiltering(t *testing.T) {
	t.Helper()
	original := currentMacOSVersionPrefixFunc
	currentMacOSVersionPrefixFunc = func() string { return "" }
	t.Cleanup(func() { currentMacOSVersionPrefixFunc = original })
}

func TestOsVersionFolderPrefix(t *testing.T) {
	tests := []struct {
		folder     string
		wantPrefix string
		wantOK     bool
	}{
		{"26-Tahoe", "26", true},
		{"10.15-Catalina", "10.15", true},
		{"27-GoldenGate", "27", true},
		{"15", "15", true},
		{"26", "26", true},
		{"", "", false},
		{"Archive", "", false},
		{"CustomFolderName", "", false},
	}
	for _, tt := range tests {
		prefix, ok := osVersionFolderPrefix(tt.folder)
		if prefix != tt.wantPrefix || ok != tt.wantOK {
			t.Errorf("osVersionFolderPrefix(%q) = (%q, %v), want (%q, %v)", tt.folder, prefix, ok, tt.wantPrefix, tt.wantOK)
		}
	}
}

// TestOSVersionFolderAtLeast guards issue #12's own safety guardrail (Ken's
// own explicit, conservative call, 2026-09-16): the installer-version-gate
// fallback should only ever be attempted for a package sitting in a macOS
// 14+ folder, never a legacy "10.x" one (predates the modern integer-major-
// version era this fallback is scoped to) and never an unparsable/missing
// folder name (no confident basis to take the risky path).
func TestOSVersionFolderAtLeast(t *testing.T) {
	tests := []struct {
		folder   string
		minMajor int
		want     bool
	}{
		{"14-Sonoma", 14, true},
		{"15-Sequoia", 14, true},
		{"26-Tahoe", 14, true},
		{"13-Ventura", 14, false},
		{"12-Monterey", 14, false},
		{"10.15-Catalina", 14, false},
		{"10.15-Catalina", 11, false}, // legacy "10.x" is always false, even against a lower threshold
		{"", 14, false},
		{"Archive", 14, false},
		{"CustomFolderName", 14, false},
		{"14", 14, true}, // bare numeric, no codename
	}
	for _, tt := range tests {
		got := OSVersionFolderAtLeast(tt.folder, tt.minMajor)
		if got != tt.want {
			t.Errorf("OSVersionFolderAtLeast(%q, %d) = %v, want %v", tt.folder, tt.minMajor, got, tt.want)
		}
	}
}

func TestFilterToCurrentOSVersionFolder_ExcludesMismatchedRelease(t *testing.T) {
	t.Cleanup(func() { currentMacOSVersionPrefixFunc = detectCurrentMacOSVersionPrefix })
	currentMacOSVersionPrefixFunc = func() string { return "26" }

	packages := []MacPackage{
		{Path: "UFRII_v10.19.21_mac.dmg", OSVersionFolder: "10.15-Catalina"},
		{Path: "UFRII_v10.19.25_mac.dmg", OSVersionFolder: "26-Tahoe"},
	}
	got := filterToCurrentOSVersionFolder(packages)
	if len(got) != 1 || got[0].Path != "UFRII_v10.19.25_mac.dmg" {
		t.Errorf("expected only the 26-Tahoe package to survive filtering, got %+v", got)
	}
}

func TestFilterToCurrentOSVersionFolder_ReturnsEmptyWhenNothingMatches(t *testing.T) {
	t.Cleanup(func() { currentMacOSVersionPrefixFunc = detectCurrentMacOSVersionPrefix })
	currentMacOSVersionPrefixFunc = func() string { return "27" }

	// A technician who never placed anything in the "27" folder gets
	// nothing back - deliberately, not a fallback to an OS-mismatched
	// package (reversed from this function's own original v0.9.3 design,
	// Ken's own follow-up 2026-09-13) - this empty result is exactly what
	// lets the resolution chain fall through to Apple's own bundled
	// Generic PostScript/PCL drivers instead (macgeneric.go).
	packages := []MacPackage{
		{Path: "UFRII_v10.19.21_mac.dmg", OSVersionFolder: "10.15-Catalina"},
		{Path: "UFRII_v10.19.25_mac.dmg", OSVersionFolder: "26-Tahoe"},
	}
	got := filterToCurrentOSVersionFolder(packages)
	if len(got) != 0 {
		t.Errorf("expected an empty result when nothing matches the current OS, got %+v", got)
	}
}

func TestFilterToCurrentOSVersionFolder_UndeterminedOSKeepsEverything(t *testing.T) {
	t.Cleanup(func() { currentMacOSVersionPrefixFunc = detectCurrentMacOSVersionPrefix })
	currentMacOSVersionPrefixFunc = func() string { return "" }

	packages := []MacPackage{
		{Path: "UFRII_v10.19.21_mac.dmg", OSVersionFolder: "10.15-Catalina"},
		{Path: "UFRII_v10.19.25_mac.dmg", OSVersionFolder: "26-Tahoe"},
	}
	got := filterToCurrentOSVersionFolder(packages)
	if len(got) != 2 {
		t.Errorf("expected no filtering at all when the current OS can't be determined, got %+v", got)
	}
}

func TestFilterToCurrentOSVersionFolder_UnrecognizedFolderNameNeverExcluded(t *testing.T) {
	t.Cleanup(func() { currentMacOSVersionPrefixFunc = detectCurrentMacOSVersionPrefix })
	currentMacOSVersionPrefixFunc = func() string { return "26" }

	// A package sitting directly in the manufacturer folder (no real
	// version-folder nesting at all, or a custom/non-standard name) can't
	// be confidently excluded - keep it rather than risk silently hiding a
	// real, intentionally-placed driver.
	packages := []MacPackage{
		{Path: "SomeDriver.dmg", OSVersionFolder: "CustomFolder"},
		{Path: "UFRII_v10.19.25_mac.dmg", OSVersionFolder: "26-Tahoe"},
	}
	got := filterToCurrentOSVersionFolder(packages)
	if len(got) != 2 {
		t.Errorf("expected the unrecognized-folder package to survive filtering too, got %+v", got)
	}
}
