package driver

import (
	"regexp"
	"time"
)

// ResolvedDriver is the final answer for one row's driver selection - which
// exact local .inf provides it, and whether that was an explicit version pin
// (a decorated Candidates() label) as opposed to a plain name that resolved
// to "whatever's newest and compatible". Ports Resolve-DriverSelection's
// return shape; deliberately has no Manufacturer field, matching the
// original - callers already have that as a separate value from the row.
type ResolvedDriver struct {
	Name              string
	InfPath           string
	Date              time.Time
	Version           string
	IsExplicitVersion bool
}

var decoratedLabelRe = regexp.MustCompile(`^(.+) \(v([0-9.]+) - (\d{4}-\d{2}-\d{2})(?:, .+)?\)$`)

// Resolve ports Resolve-DriverSelection. selection is either a plain driver
// name or one of Candidates()' decorated labels. Returns (nil, nil) - not an
// error - when the manufacturer/name/version can't be resolved to anything
// usable on this machine, matching the original's $null-return-means-not-found
// convention.
func Resolve(catalog Catalog, manufacturer, selection string) (*ResolvedDriver, error) {
	if manufacturer == "" || selection == "" {
		return nil, nil
	}
	mfgCatalog, ok := catalog[manufacturer]
	if !ok {
		return nil, nil
	}

	cleanName := selection
	requestedVersion := ""
	requestedDate := ""
	if m := decoratedLabelRe.FindStringSubmatch(selection); m != nil {
		cleanName, requestedVersion, requestedDate = m[1], m[2], m[3]
	}

	versionGroups, ok := mfgCatalog[cleanName]
	if !ok {
		return nil, nil
	}

	// Only ever resolve to a version-group this machine can actually use - if
	// the exact requested version is present but built for a different
	// architecture (e.g. a saved row named an ARM64-only build and this run
	// is on amd64), treat it the same as "not found" rather than silently
	// installing a driver of the wrong architecture.
	var archMap map[string]ArchEntry
	isExplicitVersion := false
	if requestedVersion != "" {
		for _, vg := range versionGroups {
			var sample ArchEntry
			for _, v := range vg {
				sample = v
				break
			}
			if sample.Version == requestedVersion && formatDateKey(sample.Date) == requestedDate && archMapCompatible(vg) {
				archMap = vg
				isExplicitVersion = true
				break
			}
		}
	}
	if archMap == nil {
		// No version encoded in the selection, the encoded one isn't present
		// locally anymore, or it's incompatible with this machine's
		// architecture - fall back to whichever compatible version is newest.
		var newestKey string
		var newestDate time.Time
		found := false
		for vkey, vg := range versionGroups {
			if !archMapCompatible(vg) {
				continue
			}
			var sample ArchEntry
			for _, v := range vg {
				sample = v
				break
			}
			if !found || sample.Date.After(newestDate) {
				newestKey, newestDate, found = vkey, sample.Date, true
			}
		}
		if !found {
			return nil, nil
		}
		archMap = versionGroups[newestKey]
	}

	var entry ArchEntry
	picked := false
	for _, tok := range PreferredArchTokens() {
		if e, ok := archMap[tok]; ok {
			entry, picked = e, true
			break
		}
	}
	if !picked {
		if e, ok := archMap["any"]; ok {
			entry, picked = e, true
		}
	}
	if !picked {
		for _, e := range archMap {
			entry, picked = e, true
			break
		}
	}
	if !picked {
		return nil, nil
	}

	return &ResolvedDriver{Name: cleanName, InfPath: entry.InfPath, Date: entry.Date, Version: entry.Version, IsExplicitVersion: isExplicitVersion}, nil
}
