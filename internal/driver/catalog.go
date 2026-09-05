package driver

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Manufacturers is the fixed, known manufacturer list - each corresponds to
// one top-level folder under the Drivers root.
var Manufacturers = []string{"Canon", "HP", "Kyocera", "Ricoh", "Sharp"}

// ArchEntry is one architecture-specific build within a driver's version
// group: the INF that provides it, and the version/date it declares (which
// should match the version group's key, but is kept alongside the InfPath
// for convenience - callers read Date/Version off the ArchEntry, not the key).
type ArchEntry struct {
	InfPath string
	Date    time.Time
	Version string
}

// Catalog: Manufacturer -> DriverName -> VersionKey("version|yyyy-MM-dd") -> Arch -> ArchEntry.
//
// Arch tokens are normalized to lowercase ("32bit", "x64", "64bit", "arm64",
// "any") at insertion time. This matters more here than it did in the
// original: PowerShell hashtables compare string keys case-insensitively by
// default, so "32BIT" (Canon's folder naming) and "32bit" (Kyocera's) already
// collided harmlessly into the same bucket there. Go maps are case-sensitive,
// so without normalizing, the same logical architecture from two vendors
// would silently split into two separate map entries. Driver names and
// version keys are left as-is (case-sensitive Go map keys) since the same
// driver's name is always byte-identical across its own arch-variant INFs in
// every real package inspected - unlike the arch-token spelling, there's no
// observed cross-vendor case variance to normalize away.
type Catalog map[string]map[string]map[string]map[string]ArchEntry

var archTokenRe = regexp.MustCompile(`(?i)(?:^|[-_])(32bit|x64|64bit|arm64)(?:$|[-_.])`)

func formatDateKey(t time.Time) string {
	if t.IsZero() {
		return "0001-01-01"
	}
	return t.Format("2006-01-02")
}

// BuildCatalog ports Build-DriverCatalog: recursively scans each manufacturer
// folder under driversRoot for .inf files, skipping any path that has an
// "etc" or "Archive" segment at any depth, and groups the printer driver
// names it finds by manufacturer -> name -> version -> architecture.
func BuildCatalog(driversRoot string) (Catalog, error) {
	catalog := Catalog{}
	for _, m := range Manufacturers {
		catalog[m] = map[string]map[string]map[string]ArchEntry{}
	}

	entries, err := os.ReadDir(driversRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return catalog, nil
		}
		return nil, err
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mfg := ""
		for _, m := range Manufacturers {
			if strings.EqualFold(m, e.Name()) {
				mfg = m
				break
			}
		}
		if mfg == "" {
			continue
		}

		mfgPath := filepath.Join(driversRoot, e.Name())
		_ = filepath.WalkDir(mfgPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if strings.EqualFold(d.Name(), "etc") || strings.EqualFold(d.Name(), "Archive") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.EqualFold(filepath.Ext(path), ".inf") {
				return nil
			}

			archSeg := "any"
			rel, relErr := filepath.Rel(mfgPath, path)
			if relErr == nil {
				for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
					if m := archTokenRe.FindStringSubmatch(seg); m != nil {
						archSeg = strings.ToLower(m[1])
						break
					}
				}
			}

			info, infErr := DriverNamesFromInf(path)
			if infErr != nil {
				return nil
			}

			versionKey := info.Version + "|" + formatDateKey(info.Date)

			seenNames := map[string]bool{}
			for _, dname := range info.Names {
				if dname == "" || seenNames[dname] {
					continue
				}
				seenNames[dname] = true

				if catalog[mfg][dname] == nil {
					catalog[mfg][dname] = map[string]map[string]ArchEntry{}
				}
				if catalog[mfg][dname][versionKey] == nil {
					catalog[mfg][dname][versionKey] = map[string]ArchEntry{}
				}

				existing, has := catalog[mfg][dname][versionKey][archSeg]
				isNewer := true
				if has {
					isNewer = info.Date.After(existing.Date) ||
						(info.Date.Equal(existing.Date) && compareVersions(info.Version, existing.Version) > 0)
				}
				if isNewer {
					catalog[mfg][dname][versionKey][archSeg] = ArchEntry{InfPath: path, Date: info.Date, Version: info.Version}
				}
			}
			return nil
		})
	}
	return catalog, nil
}
