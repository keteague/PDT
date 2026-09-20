package driver

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MacPackageKind distinguishes the two installer package shapes vendors ship
// macOS printer drivers as - see the README's "macOS is not read by this
// function at all yet" note for why both exist: most packages on disk are
// .dmg images (some wrapping a nested .dmg), most ultimately containing a
// .pkg installer, but a bare .pkg on its own is just as valid.
type MacPackageKind int

const (
	MacPackagePkg MacPackageKind = iota
	MacPackageDmg
	// MacPackageZip marks a .zip found directly under a manufacturer folder
	// (Canon: a .zip wrapping one .dmg; Konica Minolta: a .zip wrapping a
	// further-nested .zip) - GitHub issue #11's own fix: the real .dmg/.pkg
	// inside is resolved lazily, on demand, into a throwaway temp directory
	// (resolveMacZipSource, maczip.go) rather than eagerly extracted into a
	// permanent sibling folder at every catalog build. Never switched on
	// anywhere - purely informational, same as the other two Kind values
	// (see their own doc comment history) - every real dispatch decision is
	// made by resolveMacZipSource/LocatePkgWithChain from the actual file
	// extension at each step, not from this field.
	MacPackageZip
)

// MacPackage is one .dmg/.pkg file found under a manufacturer's Drivers/macOS
// folder. Deliberately carries no version - vendor filenames aren't
// consistent enough to trust (the same lesson internal/driver/fuzzy.go and
// version.go already encode for Windows .inf naming), so version resolution
// is a separate, explicit step (see PackageVersion) rather than something
// BuildMacCatalog itself does eagerly.
type MacPackage struct {
	Path    string
	Kind    MacPackageKind
	ModTime time.Time
	// Size is the file's own byte size - free from the same os.Stat this
	// scan already does for ModTime, and (together with Path/ModTime) the
	// cheap identity MacCatalogDB's own staleness check compares against,
	// with no mounting/re-inspection needed to get it.
	Size int64
	// OSVersionFolder is the name of the immediate subfolder of
	// Drivers/macOS/<Manufacturer>/ this package was found under (e.g.
	// "26-Tahoe", "10.15-Catalina") - this project's own established
	// convention (see README's "Drivers folder layout"), always a real,
	// technician-placed folder name, never invented by PDT itself (see
	// driversfolder.go's own ensureMacDriversScaffold doc comment for why
	// there's no hardcoded version list to generate one from). Used by
	// filterToCurrentOSVersionFolder to prefer a package actually meant for
	// whichever machine PDT is running on right now - confirmed live as a
	// real, previously-invisible bug: a driver placed specifically for an
	// older macOS release was showing up as a selectable "version" on a
	// current-release machine with nothing distinguishing it as OS-
	// incompatible at all.
	OSVersionFolder string
}

// MacCatalog is the macOS analog of Catalog: Manufacturer -> installer
// packages found under Drivers/macOS/<Manufacturer>/<any version folder>/...,
// plus the flat Drivers/macOS/OpenPrinting/<Manufacturer>/*.ppd fallback
// bucket for a manufacturer/model with no vendor installer package at all.
type MacCatalog struct {
	Packages         map[string][]MacPackage
	OpenPrintingPPDs map[string][]string
	// OpenPrintingNickNames caches each OpenPrinting PPD's own real
	// *NickName/*ModelName content (GitHub issue #16 follow-up, 2026-09-19),
	// keyed by manufacturer then by the PPD's own absolute path (matching
	// OpenPrintingPPDs' own path form) - see BuildOpenPrintingNickNames. Nil
	// is always a valid, safe "nothing cached yet" state -
	// OpenPrintingCandidateDetails degrades to filename-only matching, never
	// treats it as an error. BuildMacCatalog itself never populates this (a
	// fast, side-effect-free scan, no caching decisions) - only
	// BuildOpenPrintingNickNames does, as a separate step layered on top,
	// exactly the same relationship BuildMacModelIndex already has to
	// BuildMacCatalog.
	OpenPrintingNickNames map[string]map[string]string
}

var macPackageExts = map[string]MacPackageKind{
	".pkg": MacPackagePkg,
	".dmg": MacPackageDmg,
	".zip": MacPackageZip,
}

// BuildMacCatalog scans driversRoot/macOS the same way BuildCatalog scans
// driversRoot/Windows: driversRoot/macOS/<Manufacturer>/... recursively for
// .dmg/.pkg files (Archive/etc segments skipped, same convention as
// scanManufacturerFolders), plus driversRoot/macOS/OpenPrinting/<Manufacturer>/
// for a flat bucket of *.ppd/*.ppd.gz fallback PPDs. A missing driversRoot or
// missing macOS subfolder is treated as an empty catalog, not an error - same
// not-exist-is-empty convention as BuildCatalog.
func BuildMacCatalog(driversRoot string) (MacCatalog, error) {
	catalog := MacCatalog{
		Packages:         map[string][]MacPackage{},
		OpenPrintingPPDs: map[string][]string{},
	}
	for _, m := range Manufacturers {
		catalog.Packages[m] = nil
		catalog.OpenPrintingPPDs[m] = nil
	}

	macRoot := ""
	rootEntries, err := os.ReadDir(driversRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return catalog, nil
		}
		return catalog, err
	}
	for _, e := range rootEntries {
		if e.IsDir() && strings.EqualFold(e.Name(), "macOS") {
			macRoot = filepath.Join(driversRoot, e.Name())
			break
		}
	}
	if macRoot == "" {
		return catalog, nil
	}

	mfgEntries, err := os.ReadDir(macRoot)
	if err != nil {
		return catalog, nil
	}
	for _, e := range mfgEntries {
		if !e.IsDir() {
			continue
		}
		if strings.EqualFold(e.Name(), "OpenPrinting") {
			scanOpenPrintingPPDs(catalog, filepath.Join(macRoot, e.Name()))
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
		scanMacPackages(catalog, mfg, filepath.Join(macRoot, e.Name()))
	}
	return catalog, nil
}

// scanMacPackages walks mfgPath (a manufacturer's Drivers/macOS/<Manufacturer>
// folder, containing one or more version subfolders) for .dmg/.pkg/.zip
// files, appending each one found to catalog.Packages[mfg]. A .zip is
// recorded as its own MacPackageZip entry, not extracted here at all - see
// resolveMacZipSource (maczip.go) and GitHub issue #11: the real .dmg/.pkg a
// .zip wraps (confirmed against a real Canon download: a .zip directly
// wrapping one .dmg, no installer of its own inside the zip itself) is only
// ever resolved on demand, into a throwaway temp directory, by whatever
// later needs real bytes (indexFamilyPackage) - never eagerly, and never
// left behind as a permanent sibling folder the way this function's own
// previous version (ensureMacZipsExtracted, called unconditionally here)
// did.
//
// A manual recursive walk (os.ReadDir per directory), not filepath.WalkDir -
// needed so ExtractedSiblingDirs can see a whole directory's sibling list at
// once, the same reason cloudsync's own listLocal/copyTreeMerge's
// collectCopyJobs are shaped this way. Any directory ExtractedSiblingDirs
// recognizes as a .zip's own already-extracted sibling (a real leftover
// from before this fix existed - confirmed live, twice, as real disk bloat:
// ~2.6G/36 folders on a real machine, always regenerated by the very next
// catalog build) is both skipped AND removed outright here - once this scan
// has recorded the .zip itself as its own MacPackage entry, that leftover
// folder is pure waste with nothing left depending on it.
func scanMacPackages(catalog MacCatalog, mfg, mfgPath string) {
	walkMacPackages(catalog, mfg, mfgPath, mfgPath, "")
}

func walkMacPackages(catalog MacCatalog, mfg, mfgPath, absDir, osVersionFolder string) {
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return
	}
	extractedSiblings := ExtractedSiblingDirs(absDir, entries)
	for _, e := range entries {
		path := filepath.Join(absDir, e.Name())
		if e.IsDir() {
			if strings.EqualFold(e.Name(), "etc") || strings.EqualFold(e.Name(), "Archive") || e.Name() == "__MACOSX" {
				continue
			}
			if mfg == "Konica Minolta" && isKonicaMinoltaA4RegionDir(e.Name()) {
				continue
			}
			if extractedSiblings[e.Name()] {
				os.RemoveAll(path)
				continue
			}
			childOSVersionFolder := osVersionFolder
			if absDir == mfgPath {
				childOSVersionFolder = e.Name()
			}
			walkMacPackages(catalog, mfg, mfgPath, path, childOSVersionFolder)
			continue
		}
		// Skip AppleDouble resource-fork stub files (macOS's own zip/Archive
		// Utility, or anything else that zips a folder on a Mac, litters
		// these in as "._RealFileName" - confirmed against a real Canon
		// download that one of these carries the *same* .dmg extension as
		// the real 80+ MB file it shadows, at a few hundred bytes - without
		// this, it would show up as a second, bogus catalog entry that fails
		// outright the moment something tries to mount it).
		if strings.HasPrefix(e.Name(), "._") {
			continue
		}
		kind, ok := macPackageExts[strings.ToLower(filepath.Ext(path))]
		if !ok {
			// filepath.Ext only ever sees the trailing ".gz" on a compound
			// ".dmg.gz" name (Toshiba's own real download shape, confirmed
			// live 2026-09-13 - a plain gzip-wrapped UDIF image, not
			// recognized by hdiutil's own format autodetection without
			// decompressing first - see isDmgLikePath/mountDmg) - checked by
			// suffix here too, not added to macPackageExts itself, which is
			// keyed by a single bare extension.
			if !isDmgLikePath(path) {
				continue
			}
			kind = MacPackageDmg
		}
		info, err := e.Info()
		modTime := time.Time{}
		var size int64
		if err == nil {
			modTime = info.ModTime()
			size = info.Size()
		}
		catalog.Packages[mfg] = append(catalog.Packages[mfg], MacPackage{Path: path, Kind: kind, ModTime: modTime, Size: size, OSVersionFolder: osVersionFolder})
	}
}

// scanOpenPrintingPPDs scans openPrintingRoot (Drivers/macOS/OpenPrinting)'s
// immediate manufacturer subfolders for *.ppd/*.ppd.gz files - a flat bucket,
// no version nesting, unlike scanMacPackages above.
func scanOpenPrintingPPDs(catalog MacCatalog, openPrintingRoot string) {
	entries, err := os.ReadDir(openPrintingRoot)
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
		mfgPath := filepath.Join(openPrintingRoot, e.Name())
		ppdEntries, err := os.ReadDir(mfgPath)
		if err != nil {
			continue
		}
		for _, pe := range ppdEntries {
			// "._Something.ppd" is an AppleDouble resource-fork stub, not a
			// real PPD - see scanMacPackages' own comment for why this needs
			// skipping explicitly rather than trusting the extension alone.
			if pe.IsDir() || strings.HasPrefix(pe.Name(), "._") {
				continue
			}
			lower := strings.ToLower(pe.Name())
			if strings.HasSuffix(lower, ".ppd") || strings.HasSuffix(lower, ".ppd.gz") {
				catalog.OpenPrintingPPDs[mfg] = append(catalog.OpenPrintingPPDs[mfg], filepath.Join(mfgPath, pe.Name()))
			}
		}
	}
}

// MacManufacturersWithPackages is the macOS analog of
// ManufacturersWithDrivers - the subset of Manufacturers that have at least
// one installer package OR at least one OpenPrinting fallback PPD present.
func MacManufacturersWithPackages(catalog MacCatalog) []string {
	out := []string{}
	for _, m := range Manufacturers {
		if len(catalog.Packages[m]) > 0 || len(catalog.OpenPrintingPPDs[m]) > 0 {
			out = append(out, m)
		}
	}
	return out
}
