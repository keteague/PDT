package driver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// OpenPrintingCatalog is one manufacturer's own cached OpenPrinting PPD
// nickname index (GitHub issue #16 follow-up, 2026-09-19) - mirrors
// MacManufacturerCatalog's own provenance-based staleness pattern (see its
// doc comment) for the flat Drivers/macOS/OpenPrinting/<Manufacturer>/*.ppd
// bucket, rather than a versioned installer package tree. Persisted as
// CatalogFileName(manufacturer) inside that manufacturer's own real
// Drivers/macOS/OpenPrinting/<Manufacturer> folder - the exact same
// filename convention the vendor package catalog uses one folder over
// (Drivers/macOS/<Manufacturer>/), just with no naming collision since it's
// a different directory. "That manufacturer's own real folder" is not
// necessarily <Manufacturer> with spaces stripped the way the vendor
// package tree's own folder always is (driversfolder.go) - ppdsync.go's own
// localFolder can end up naming it either way depending on what already
// existed on a given machine (confirmed live, Ken, 2026-09-23: a real
// "Konica Minolta" install's synced PPDs sat in a folder literally spelled
// "Konica Minolta", spaces intact) - so BuildOpenPrintingNickNames derives
// this path from a real PPD's own already-known location, never by
// re-deriving the folder name from the manufacturer string itself.
//
// Exists because a real OpenPrinting PPD's own filename is routinely
// cryptic (ppdNickNameRe's own doc comment: a confirmed-live hard zero for
// Canon's real downloads against FuzzyMatchScore), while its *NickName/
// *ModelName content is usually the real, technician-recognizable model
// name - but reading and parsing every PPD's content on every combobox
// keystroke doesn't scale once a manufacturer has hundreds of them. This
// cache pays that parse cost once per PPD (until it actually changes), the
// same "expensive extraction happens once, every later lookup is a plain
// map read" pattern this codebase already established for the Windows
// Kyocera model index and the mac vendor-package catalog.
type OpenPrintingCatalog struct {
	// Entries is keyed by the PPD's own driversRoot-relative path (GitHub
	// issue #13's own portability reasoning - the same convention
	// MacPackageRef.Path/MacCatalogVariant.PackagePath already use), so a
	// catalog synced onto a different machine's differently-lettered/
	// -mounted Drivers folder still matches.
	Entries map[string]OpenPrintingCacheEntry `json:"entries"`
}

// OpenPrintingCacheEntry is one PPD's own cached provenance plus parsed
// content. ModTime/Size are the PPD file's own, checked against a fresh
// os.Stat (BuildOpenPrintingNickNames) to decide whether NickName needs
// re-parsing at all.
type OpenPrintingCacheEntry struct {
	ModTime time.Time `json:"modTime"`
	Size    int64     `json:"size"`
	// NickName is ReadPPDNickName's own result for this PPD - "" (and still
	// cached, so a genuinely nickname-less PPD isn't re-attempted on every
	// single build) when the file has no *NickName/*ModelName field at all,
	// or couldn't be read.
	NickName string `json:"nickName"`
}

// LoadOpenPrintingCatalog reads path's own persisted OpenPrintingCatalog -
// mirrors LoadMacManufacturerCatalog's own contract exactly: a missing or
// corrupt file is just an empty catalog (a cache, not user data), never an
// error, and Entries is never nil.
func LoadOpenPrintingCatalog(path string) OpenPrintingCatalog {
	empty := OpenPrintingCatalog{Entries: map[string]OpenPrintingCacheEntry{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return empty
	}
	var cat OpenPrintingCatalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return empty
	}
	if cat.Entries == nil {
		cat.Entries = map[string]OpenPrintingCacheEntry{}
	}
	return cat
}

// SaveOpenPrintingCatalog persists cat to path, creating its parent
// directory if needed - mirrors SaveMacManufacturerCatalog's own contract.
func SaveOpenPrintingCatalog(path string, cat OpenPrintingCatalog) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// OpenPrintingCatalogReport is BuildOpenPrintingNickNames' own per-
// manufacturer summary of what it actually did - GitHub issue #16 follow-up
// (Ken's own ask, 2026-09-19) for Normal/Debug-level log detail about the
// cataloging process itself: which catalog is being checked/created
// (Manufacturer alone, at Normal level), and which files changed/were
// added/removed (Added/Removed, at Debug level).
type OpenPrintingCatalogReport struct {
	Manufacturer string
	// WasNew is true when no persisted catalog.<mfg>.json existed for this
	// manufacturer's own OpenPrinting folder at all before this run
	// (nothing cached yet, or an empty file) - lets a caller log "creating"
	// rather than "checking" for a manufacturer's very first cataloging
	// pass.
	WasNew bool
	// Added is every PPD (keyed by its own driversRoot-relative path) newly
	// cached or re-parsed this run, because it was missing from the cache
	// or had changed on disk - paired with its own freshly-read NickName,
	// which may itself be "" (a real, valid outcome for a PPD with no
	// *NickName/*ModelName field at all, not a failure).
	Added map[string]string
	// Removed is every driversRoot-relative path the previous cache had
	// that no longer appears in this run's set at all (deleted, or moved
	// out of scanOpenPrintingPPDs' own scan) - sorted for deterministic
	// logging.
	Removed []string
	// Unchanged is how many PPDs matched their cached provenance exactly
	// and needed no re-parsing at all.
	Unchanged int
}

// BuildOpenPrintingNickNames caches every OpenPrinting PPD's own real
// *NickName/*ModelName content for every manufacturer catalog.OpenPrintingPPDs
// actually lists, keyed by manufacturer then by the PPD's own absolute path
// (matching OpenPrintingPPDs' own path form, so OpenPrintingCandidateDetails
// can look an entry straight up with no extra conversion). A layered
// enrichment step on top of BuildMacCatalog's own fast, side-effect-free
// scan, exactly the same relationship BuildMacModelIndex already has to it -
// BuildMacCatalog itself never touches disk beyond the one directory walk,
// never makes a caching decision, and has no persist concept at all.
//
// persist mirrors BuildMacModelIndex's own parameter exactly (a
// write-protected flash drive): false still returns a fully correct,
// fully-parsed in-memory result for this run - every PPD gets read fresh
// when nothing cached is trustworthy - it just skips the disk write that
// would let a *later* run skip re-parsing PPDs that haven't changed. Safe
// to call with persist true from a read-only location too: SaveOpenPrintingCatalog's
// own os.WriteFile failure is swallowed exactly like every other
// best-effort catalog write in this package.
//
// Manufacturers are processed in alphabetical order (not map iteration
// order) so the reports returned - and any log lines a caller builds from
// them - come out in a stable, readable order every run.
func BuildOpenPrintingNickNames(catalog MacCatalog, macRoot string, persist bool) (map[string]map[string]string, []OpenPrintingCatalogReport) {
	driversRoot := filepath.Dir(macRoot)
	out := map[string]map[string]string{}
	var reports []OpenPrintingCatalogReport

	mfgs := make([]string, 0, len(catalog.OpenPrintingPPDs))
	for mfg := range catalog.OpenPrintingPPDs {
		mfgs = append(mfgs, mfg)
	}
	sort.Strings(mfgs)

	for _, mfg := range mfgs {
		paths := catalog.OpenPrintingPPDs[mfg]
		if len(paths) == 0 {
			continue
		}
		// The real per-manufacturer folder under Drivers/macOS/OpenPrinting
		// isn't reliably mfg with spaces stripped - ppdsync.go's own
		// localFolder reuses whatever folder already exists on a given
		// machine (fold-matching, ignoring spacing entirely) and otherwise
		// creates one using mfg's own literal display name, spaces and all.
		// Confirmed live (Ken, 2026-09-23): a real "Konica Minolta" install
		// synced its PPDs into a folder literally named "Konica Minolta"
		// (spaced, matching driver.Manufacturers), while this catalog cache
		// kept computing "KonicaMinolta" (concatenated) and writing an
		// orphaned catalog.konicaminolta.json there instead - a folder
		// scanOpenPrintingPPDs never even populated with any real PPDs.
		// paths are always scanOpenPrintingPPDs' own real absolute paths, so
		// deriving the folder from one directly can never drift from
		// wherever the PPDs it's actually caching really live.
		catalogPath := filepath.Join(filepath.Dir(paths[0]), CatalogFileName(mfg))
		cached := LoadOpenPrintingCatalog(catalogPath)

		fresh := map[string]OpenPrintingCacheEntry{}
		names := map[string]string{}
		report := OpenPrintingCatalogReport{Manufacturer: mfg, WasNew: len(cached.Entries) == 0, Added: map[string]string{}}
		seenRel := map[string]bool{}

		for _, path := range paths {
			relPath := relToDriversRoot(driversRoot, path)
			seenRel[relPath] = true
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			if existing, ok := cached.Entries[relPath]; ok && existing.ModTime.Equal(info.ModTime()) && existing.Size == info.Size() {
				fresh[relPath] = existing
				names[path] = existing.NickName
				report.Unchanged++
				continue
			}
			nick, _ := ReadPPDNickName(path)
			fresh[relPath] = OpenPrintingCacheEntry{ModTime: info.ModTime(), Size: info.Size(), NickName: nick}
			names[path] = nick
			report.Added[relPath] = nick
		}
		// A PPD present in the old cache but not in this run's fresh set
		// (deleted, or moved to a folder scanOpenPrintingPPDs no longer
		// scans) must not linger in the persisted file forever - same
		// "diff against the current set, not just append" concern
		// BuildMacModelIndex's own orphan-pruning already guards against
		// for the vendor package catalog.
		for relPath := range cached.Entries {
			if !seenRel[relPath] {
				report.Removed = append(report.Removed, relPath)
			}
		}
		sort.Strings(report.Removed)

		out[mfg] = names
		reports = append(reports, report)

		if (len(report.Added) > 0 || len(report.Removed) > 0) && persist {
			_ = SaveOpenPrintingCatalog(catalogPath, OpenPrintingCatalog{Entries: fresh})
		}
	}
	return out, reports
}
