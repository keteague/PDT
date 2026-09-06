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
// one top-level folder under the Drivers root. This is every manufacturer PDT
// knows about, regardless of whether its drivers are actually present
// locally - see ManufacturersWithDrivers for the subset that's actually
// deployable.
var Manufacturers = []string{"Canon", "HP", "Kyocera", "Ricoh", "Sharp", "Toshiba", "Xerox", "Konica Minolta", "Lexmark"}

// foldMatchIgnoringSpaces compares two folder/manufacturer names
// case-insensitively and ignoring spaces - confirmed necessary against a
// real Drivers folder, where "Konica Minolta" (this app's own display name,
// spaced out for readability in the UI) sat on disk as "KonicaMinolta" (no
// space), which a plain strings.EqualFold never matches.
func foldMatchIgnoringSpaces(a, b string) bool {
	return strings.EqualFold(strings.ReplaceAll(a, " ", ""), strings.ReplaceAll(b, " ", ""))
}

// ManufacturersWithDrivers is the subset of Manufacturers that actually have
// at least one usable driver in catalog. The Defaults panel's Manufacturer
// dropdown (and each grid row's) should only ever offer a manufacturer as a
// deployment option once its drivers are actually present locally - unlike
// Settings > External Sites, which lists every manufacturer in Manufacturers
// regardless, so a URL can be configured before its drivers are ever added.
func ManufacturersWithDrivers(catalog Catalog) []string {
	var out []string
	for _, m := range Manufacturers {
		if len(catalog[m]) > 0 {
			out = append(out, m)
		}
	}
	return out
}

// vagueNameFilterBrand: manufacturer -> a brand token that must appear
// (case-insensitively) in a driver name for it to be considered specific
// enough to deploy under. Several vendors' INFs declare the exact same
// driver under both a branded, versioned name and a generic, manufacturer-
// less alias - confirmed against the real Ricoh Universal Driver package
// ("RICOH PCL6 UniversalDriver V4.45" alongside "PCL6 Driver for Universal
// Print", the latter unusable since nothing about it identifies which
// vendor's driver it actually is) - and the same pattern is expected from
// Xerox's and Konica Minolta's own multi-name INFs.
var vagueNameFilterBrand = map[string]string{
	"Ricoh":          "RICOH",
	"Xerox":          "XEROX",
	"Konica Minolta": "KONICA",
}

func isUsableDriverName(manufacturer, name string) bool {
	brand, ok := vagueNameFilterBrand[manufacturer]
	if !ok {
		return true
	}
	return strings.Contains(strings.ToUpper(name), brand)
}

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
// folder for .inf files, skipping any path that has an "etc" or "Archive"
// segment at any depth, and groups the printer driver names it finds by
// manufacturer -> name -> version -> architecture.
//
// Layout: driversRoot/Windows/<any version folder, e.g. "11">/<Manufacturer>/...
// - every version folder found under Windows/ is scanned and merged into one
// catalog (a driver is rarely genuinely Windows-version-specific the way it
// can be for macOS, where the on-disk layout instead nests version under
// manufacturer - Drivers/macOS/<Manufacturer>/<version>/... - a difference
// that matters once a macOS Deployer exists to read it; this function only
// ever reads the Windows side).
//
// Back-compat: if driversRoot has no "Windows" subfolder at all, it's treated
// as the older flat layout (driversRoot/<Manufacturer>/... directly, no
// platform/version nesting) - keeps this working unmodified against existing
// testdata fixtures and any pre-reorg Drivers folder.
func BuildCatalog(driversRoot string) (Catalog, error) {
	catalog := Catalog{}
	for _, m := range Manufacturers {
		catalog[m] = map[string]map[string]map[string]ArchEntry{}
	}

	windowsRoot := ""
	rootEntries, err := os.ReadDir(driversRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return catalog, nil
		}
		return nil, err
	}
	for _, e := range rootEntries {
		if e.IsDir() && strings.EqualFold(e.Name(), "Windows") {
			windowsRoot = filepath.Join(driversRoot, e.Name())
			break
		}
	}

	if windowsRoot == "" {
		scanManufacturerFolders(catalog, driversRoot)
		return catalog, nil
	}

	versionEntries, err := os.ReadDir(windowsRoot)
	if err != nil {
		return catalog, nil
	}
	for _, ve := range versionEntries {
		if !ve.IsDir() {
			continue
		}
		scanManufacturerFolders(catalog, filepath.Join(windowsRoot, ve.Name()))
	}
	return catalog, nil
}

// scanManufacturerFolders scans root's immediate children for manufacturer-
// named folders and merges whatever driver .infs they contain into catalog.
// Safe to call multiple times against the same catalog (e.g. once per
// Windows version folder) - ArchEntry merging already keeps the newer file
// whenever the same manufacturer/name/version/arch is seen more than once.
func scanManufacturerFolders(catalog Catalog, root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mfg := ""
		for _, m := range Manufacturers {
			if foldMatchIgnoringSpaces(m, e.Name()) {
				mfg = m
				break
			}
		}
		if mfg == "" {
			continue
		}

		mfgPath := filepath.Join(root, e.Name())
		ensureZipsExtracted(mfgPath)
		ensureRarSfxExtracted(mfgPath)
		ensureMsiExtracted(mfgPath)
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
				if !isUsableDriverName(mfg, dname) {
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
}
