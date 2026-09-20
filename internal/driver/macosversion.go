package driver

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// currentMacOSVersionPrefixFunc detects the macOS-version-folder prefix that
// should match whichever machine this process is actually running on right
// now - a plain var, not a bare function call, so tests can override it
// deterministically instead of depending on whichever real macOS version
// happens to be running the test. detectCurrentMacOSVersionPrefix is the
// real implementation; production code never needs to touch this var
// directly.
//
// This matters more than it might look: PDT is designed to travel on a
// synced flash drive from a technician's own laptop to whichever client
// endpoint it gets plugged into next (see README's "Write to Flash Drive"/
// Sync) - Ken's own explicit scenario (2026-09-13): a technician configures
// drivers on a laptop running the latest macOS, then runs PDT from the same
// flash drive on a client endpoint that's still on an older release. The
// right driver to use is always whichever matches the machine PDT is
// *actually executing on at that moment*, queried live via `sw_vers` every
// time - never cached across runs, never assumed to be the machine that
// built the catalog.
var currentMacOSVersionPrefixFunc = detectCurrentMacOSVersionPrefix

var (
	currentMacOSVersionOnce   sync.Once
	currentMacOSVersionCached string
)

// detectCurrentMacOSVersionPrefix shells out to the real `sw_vers` once per
// process (cheap, but not free - BuildMacModelIndex calls into this
// indirectly once per family, across every manufacturer) and caches the
// result. Returns "" when it can't be determined at all (not running on
// macOS, sw_vers missing/failed) - every caller treats that as "can't apply
// the filter, don't exclude anything" rather than a hard failure, the same
// "best-effort, degrade gracefully" discipline this codebase applies
// throughout.
//
// A legacy "10.x" release (Sierra through Catalina) is reported as
// "10.<minor>" (e.g. "10.15"), matching this project's own established
// Drivers/macOS/<Manufacturer>/10.15-Catalina/ folder-naming convention for
// that era; 11+ ("Big Sur" onward) is reported as just the integer major
// version ("26"), matching e.g. .../26-Tahoe/ - confirmed against Ken's own
// real Drivers folder, which has both styles side by side.
func detectCurrentMacOSVersionPrefix() string {
	currentMacOSVersionOnce.Do(func() {
		out, err := exec.Command("sw_vers", "-productVersion").Output()
		if err != nil {
			return
		}
		version := strings.TrimSpace(string(out))
		parts := strings.Split(version, ".")
		if len(parts) == 0 || parts[0] == "" {
			return
		}
		if parts[0] == "10" && len(parts) >= 2 {
			currentMacOSVersionCached = "10." + parts[1]
			return
		}
		currentMacOSVersionCached = parts[0]
	})
	return currentMacOSVersionCached
}

// osVersionFolderPrefixRe matches this project's own established
// <number>-<Codename> folder-naming convention ("26-Tahoe",
// "10.15-Catalina") - also accepting a bare number with no codename at all
// ("26"), the shape a couple of this package's own older test fixtures use.
var osVersionFolderPrefixRe = regexp.MustCompile(`^(\d+(?:\.\d+)?)(?:-|$)`)

// osVersionFolderPrefix extracts folderName's own leading version-number
// prefix ("26-Tahoe" -> "26", "10.15-Catalina" -> "10.15", bare "26" ->
// "26") - ok is false for a folder name that doesn't start with a
// recognizable version number at all (an unrecognized/custom folder name),
// which filterToCurrentOSVersionFolder treats the same as "can't tell,"
// never as a confident exclusion.
func osVersionFolderPrefix(folderName string) (string, bool) {
	m := osVersionFolderPrefixRe.FindStringSubmatch(folderName)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// filterToCurrentOSVersionFolder narrows packages down to just the ones
// sitting in the OS-version folder matching the machine this process is
// actually running on right now (see MacPackage.OSVersionFolder's own doc
// comment for the real, live-confirmed bug this fixes). Falls back to the
// full, unfiltered slice in exactly one case - the current OS version
// couldn't be determined at all (no confident basis to exclude anything).
// A package whose own OSVersionFolder doesn't match the recognized
// <number>-<Codename> shape at all is always kept (never confidently
// excluded), for the same "no confident basis to exclude it" reasoning.
//
// When the current OS *is* known but genuinely nothing matches, this
// returns an empty slice rather than falling back to the unfiltered set -
// a deliberate reversal of this function's own original v0.9.3 design
// (Ken's own follow-up, 2026-09-13): installing a driver built for a
// different macOS release carries real risk (an installer that refuses
// outright on an OS-version check, or worse, one that "succeeds" but the
// driver doesn't actually work correctly) that PDT has no way to detect
// after the fact - "an OS-mismatched driver beats none" was the wrong
// tradeoff. A confirmed-empty result here is exactly what lets the whole
// resolution chain (ResolveMac, packagesInFamily/newestInFamily,
// MacModelCandidates) correctly fall through to Apple's own bundled
// Generic PostScript/PCL drivers (macgeneric.go) instead - a real, safe,
// OS-version-proof option - rather than silently installing something
// unverified.
func filterToCurrentOSVersionFolder(packages []MacPackage) []MacPackage {
	current := currentMacOSVersionPrefixFunc()
	if current == "" {
		return packages
	}
	var matched []MacPackage
	for _, p := range packages {
		prefix, ok := osVersionFolderPrefix(p.OSVersionFolder)
		if !ok || prefix == current {
			matched = append(matched, p)
		}
	}
	return matched
}

// osVersionFolderRank returns a comparable "bigger is newer" rank for a
// <number>-<Codename> driver folder name - GitHub issue #16 follow-up (Ken's
// own ask, 2026-09-19): the macOS Driver modal should favor whichever real
// macOS release is actually newest (today, v27 "Golden Gate" over v26
// "Tahoe") when more than one OS-version folder's packages coexist
// unfiltered in the same MacModelIndex, falling back to the next-newest
// automatically whenever the newest folder simply has no driver for this
// particular family/model - which falls out for free here, since ranking is
// only ever compared among variants that already exist for that exact
// (family, model) pair. Deliberately generalized to "parse the leading
// number, bigger wins" rather than a hardcoded "27, else 26" - the same
// folder-naming convention this project already commits to keeps this
// correct every future macOS release with no code change needed.
//
// A legacy "10.x" folder always ranks below every modern integer-major
// folder (same era boundary OSVersionFolderAtLeast already draws - Big
// Sur/11 onward is the modern integer-major era). An unrecognized or
// missing folder name ranks lowest of all - nothing to prefer it for. Used
// only as a tiebreaker (MacModelCandidateDetails), so ties for machines
// where every candidate already comes from the same single folder (any
// native mac run - see filterToCurrentOSVersionFolder's own doc comment)
// are harmless no-ops.
func osVersionFolderRank(folder string) int {
	prefix, ok := osVersionFolderPrefix(folder)
	if !ok {
		return -1
	}
	if strings.HasPrefix(prefix, "10.") {
		return 0
	}
	major, err := strconv.Atoi(prefix)
	if err != nil {
		return -1
	}
	return major + 1
}

// OSVersionFolderAtLeast reports whether folderName's own leading version
// number (osVersionFolderPrefix) is macOS major version minMajor or newer -
// the issue #12 installer-version-gate fallback's own safety guardrail
// (Ken's own explicit, conservative call, 2026-09-16): a driver package old
// enough to still be filed under a legacy "10.x-Codename" folder is always
// false here regardless of minMajor, since every such folder predates the
// modern integer-major-version era (Big Sur/11 onward) this fallback is
// scoped to. A folder name that doesn't match the recognized
// <number>-<Codename> shape at all - or is missing entirely - is also
// false, the same "no confident basis, don't apply the risky path" reasoning
// filterToCurrentOSVersionFolder already uses for exclusion decisions.
func OSVersionFolderAtLeast(folderName string, minMajor int) bool {
	prefix, ok := osVersionFolderPrefix(folderName)
	if !ok {
		return false
	}
	if strings.HasPrefix(prefix, "10.") {
		return false
	}
	major, err := strconv.Atoi(prefix)
	if err != nil {
		return false
	}
	return major >= minMajor
}
