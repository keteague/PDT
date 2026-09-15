package driver

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// RescanPackage is one archive-shaped driver package sitting somewhere under
// a manufacturer's own Drivers folder - the Rescan dialog's own per-package
// checkbox. RelPath is relative to the manufacturer folder (forward-slash
// separated, so it round-trips through the frontend and back unchanged) -
// what RemoveInfCacheForSelection uses to find that exact package's own
// .pdt-infcache entry again via infCacheDestDir, without re-walking the
// manufacturer folder to rediscover it.
type RescanPackage struct {
	Name    string `json:"name"`
	RelPath string `json:"relPath"`
}

// RescanManufacturer is one manufacturer row in the Rescan dialog's own
// tree - only manufacturers that actually have at least one recognized
// archive present locally are included (see ListRescanTargets).
type RescanManufacturer struct {
	Name     string          `json:"name"`
	Packages []RescanPackage `json:"packages"`
}

// ListRescanTargets lists every manufacturer actually present under
// driversRoot (Windows/<any version folder>/<Manufacturer>, or the older
// flat driversRoot/<Manufacturer> layout - same back-compat rule
// buildCatalog itself uses) along with the archive files sitting in its own
// folder - exactly the same packages
// ensureZipInfsExtracted/ensureSfxArchiveInfsExtracted/
// ensureMsiInfsExtracted/ensureKyoceraExeInfsExtracted would themselves find
// and process. Feeds the Rescan dialog's manufacturer/package tree; a
// manufacturer present under more than one Windows version folder is merged
// into a single row, matching buildCatalog's own multi-version-merge
// behavior.
func ListRescanTargets(driversRoot string) []RescanManufacturer {
	byName := map[string]*RescanManufacturer{}
	var order []string

	addPackages := func(mfg, mfgPath string) {
		packages := listRescanPackages(mfgPath)
		if len(packages) == 0 {
			return
		}
		rm, ok := byName[mfg]
		if !ok {
			rm = &RescanManufacturer{Name: mfg}
			byName[mfg] = rm
			order = append(order, mfg)
		}
		rm.Packages = append(rm.Packages, packages...)
	}

	for _, mfgPath := range manufacturerRoots(driversRoot) {
		mfg, ok := matchManufacturer(filepath.Base(mfgPath))
		if !ok {
			continue
		}
		addPackages(mfg, mfgPath)
	}

	out := make([]RescanManufacturer, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name])
	}
	return out
}

// matchManufacturer resolves a folder name (e.g. "KonicaMinolta", no space,
// as it actually sits on disk) to its canonical display name in
// Manufacturers (e.g. "Konica Minolta") - see foldMatchIgnoringSpaces.
func matchManufacturer(folderName string) (string, bool) {
	for _, m := range Manufacturers {
		if foldMatchIgnoringSpaces(m, folderName) {
			return m, true
		}
	}
	return "", false
}

// manufacturerRoots lists every manufacturer-shaped folder actually present
// directly under driversRoot's own Windows/<version> layout, or driversRoot
// itself for the older flat layout - the same root-resolution buildCatalog
// applies, factored out here so ListRescanTargets/manufacturerFolderPaths
// don't each re-implement it.
func manufacturerRoots(driversRoot string) []string {
	windowsRoot := ""
	rootEntries, err := os.ReadDir(driversRoot)
	if err != nil {
		return nil
	}
	for _, e := range rootEntries {
		if e.IsDir() && strings.EqualFold(e.Name(), "Windows") {
			windowsRoot = filepath.Join(driversRoot, e.Name())
			break
		}
	}

	var scanRoots []string
	if windowsRoot == "" {
		scanRoots = []string{driversRoot}
	} else {
		versionEntries, err := os.ReadDir(windowsRoot)
		if err != nil {
			return nil
		}
		for _, ve := range versionEntries {
			if ve.IsDir() {
				scanRoots = append(scanRoots, filepath.Join(windowsRoot, ve.Name()))
			}
		}
	}

	var out []string
	for _, root := range scanRoots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				out = append(out, filepath.Join(root, e.Name()))
			}
		}
	}
	return out
}

// listRescanPackages walks mfgPath (a single manufacturer folder) for
// recognized archive files, applying the exact same directory-skip rule
// (etc/Archive/PdtInfCacheDirName) and archive-type recognition
// (zip/msi/self-extracting exe/Kyocera's own two-stage exe) every
// ensure*InfsExtracted helper already uses.
func listRescanPackages(mfgPath string) []RescanPackage {
	var out []RescanPackage
	_ = filepath.WalkDir(mfgPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.EqualFold(d.Name(), "etc") || strings.EqualFold(d.Name(), "Archive") || d.Name() == PdtInfCacheDirName {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		isArchive := false
		switch strings.ToLower(filepath.Ext(name)) {
		case ".zip", ".msi":
			isArchive = true
		case ".exe":
			isArchive = kyoceraExeNameRe.MatchString(name) || isSelfExtractingArchive(path)
		}
		if !isArchive {
			return nil
		}
		rel, relErr := filepath.Rel(mfgPath, path)
		if relErr != nil {
			rel = name
		}
		out = append(out, RescanPackage{Name: name, RelPath: filepath.ToSlash(rel)})
		return nil
	})
	return out
}

// RemoveInfCacheForSelection best-effort removes the .pdt-infcache entry for
// each package named in selected - the Rescan dialog's own "Remove INF"
// checkbox, forcing a fresh .inf extraction for exactly that scope on the
// next BuildCatalog call. Each entry is "Manufacturer/RelPath"
// (RescanManufacturer.Name + "/" + RescanPackage.RelPath, exactly as the
// dialog's own per-package checkboxes report it).
//
// Best-effort and silent on failure, matching pruneOrphanedInfCache's own
// write-protected-media contract (Ken's own real field-deployment plan:
// flash drives run write-protected) - the RefreshDriverCatalog rebuild that
// always follows this still produces a correct result even when a removal
// here doesn't actually take; it just means that one entry gets reused
// rather than refreshed until run again from a writable location.
func RemoveInfCacheForSelection(driversRoot string, selected []string) {
	rootsByMfg := map[string][]string{}
	for _, mfgPath := range manufacturerRoots(driversRoot) {
		mfg, ok := matchManufacturer(filepath.Base(mfgPath))
		if !ok {
			continue
		}
		rootsByMfg[mfg] = append(rootsByMfg[mfg], mfgPath)
	}

	for _, sel := range selected {
		idx := strings.Index(sel, "/")
		if idx < 0 {
			continue
		}
		mfgName, relPath := sel[:idx], sel[idx+1:]
		for _, mfgPath := range rootsByMfg[mfgName] {
			archivePath := filepath.Join(mfgPath, filepath.FromSlash(relPath))
			os.RemoveAll(infCacheDestDir(mfgPath, archivePath))
		}
	}
}
