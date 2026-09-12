package driver

import (
	"io/fs"
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
}

// MacCatalog is the macOS analog of Catalog: Manufacturer -> installer
// packages found under Drivers/macOS/<Manufacturer>/<any version folder>/...,
// plus the flat Drivers/macOS/OpenPrinting/<Manufacturer>/*.ppd fallback
// bucket for a manufacturer/model with no vendor installer package at all.
type MacCatalog struct {
	Packages         map[string][]MacPackage
	OpenPrintingPPDs map[string][]string
}

var macPackageExts = map[string]MacPackageKind{
	".pkg": MacPackagePkg,
	".dmg": MacPackageDmg,
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
// folder, containing one or more version subfolders) for .dmg/.pkg files,
// appending each one found to catalog.Packages[mfg]. ensureMacZipsExtracted
// runs first, so a manufacturer whose macOS packages ship as .zip (confirmed
// against a real Canon download: a .zip directly wrapping one .dmg, no
// installer of its own inside the zip itself) gets extracted before this
// walk runs, exactly the same "extract first, then let the generic walk find
// whatever's inside" order the Windows side's own ensureZipsExtracted/
// BuildCatalog pairing already uses.
func scanMacPackages(catalog MacCatalog, mfg, mfgPath string) {
	ensureMacZipsExtracted(mfgPath)
	_ = filepath.WalkDir(mfgPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.EqualFold(d.Name(), "etc") || strings.EqualFold(d.Name(), "Archive") || d.Name() == "__MACOSX" {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip AppleDouble resource-fork stub files (macOS's own zip/Archive
		// Utility, or anything else that zips a folder on a Mac, litters
		// these in as "._RealFileName" - confirmed against a real Canon
		// download that one of these carries the *same* .dmg extension as
		// the real 80+ MB file it shadows, at a few hundred bytes - without
		// this, it would show up as a second, bogus catalog entry that fails
		// outright the moment something tries to mount it).
		if strings.HasPrefix(d.Name(), "._") {
			return nil
		}
		kind, ok := macPackageExts[strings.ToLower(filepath.Ext(path))]
		if !ok {
			return nil
		}
		info, err := d.Info()
		modTime := time.Time{}
		var size int64
		if err == nil {
			modTime = info.ModTime()
			size = info.Size()
		}
		catalog.Packages[mfg] = append(catalog.Packages[mfg], MacPackage{Path: path, Kind: kind, ModTime: modTime, Size: size})
		return nil
	})
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
