package driver

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CleanupPackage is one driver archive in the Driver Cleanup dialog (Ken,
// 2026-11-04): every package under a manufacturer's Windows folder,
// including those moved into its Archive folder, which Rescan leaves out.
type CleanupPackage struct {
	Name string `json:"name"`
	// RelPath identifies the exact file, relative to the Drivers folder
	// (forward slashes) - what DeleteDriverPackages takes. Not relative to
	// the manufacturer folder like RescanPackage.RelPath: a manufacturer
	// can exist under more than one Windows\<version> folder, and deletion
	// must never be ambiguous about which file. It's also exactly Cloud
	// Sync's name for the same file.
	RelPath string `json:"relPath"`
	// TreePath is where the dialog shows it: "<Manufacturer>/<path inside
	// the manufacturer folder>", with "Windows <version>/" after the
	// manufacturer when it exists under more than one version folder.
	TreePath string `json:"treePath"`
	Size     int64  `json:"size"`
	Archived bool   `json:"archived"` // inside an Archive folder
}

// ListCleanupPackages lists every driver archive under driversRoot's
// manufacturer folders (manufacturerRoots), Archive folders included.
// Skipped, as derived data rather than packages: .pdt-infcache, the etc
// folder, any other dot-folder, and an archive's own extracted folder
// (ExtractedSiblingDirs) - which DeleteDriverPackages removes along with
// its archive.
func ListCleanupPackages(driversRoot string) []CleanupPackage {
	roots := manufacturerRoots(driversRoot)
	rootsPerMfg := map[string]int{}
	for _, r := range roots {
		if mfg, ok := matchManufacturer(filepath.Base(r)); ok {
			rootsPerMfg[mfg]++
		}
	}
	var out []CleanupPackage
	for _, mfgPath := range roots {
		mfg, ok := matchManufacturer(filepath.Base(mfgPath))
		if !ok {
			continue
		}
		prefix := mfg
		if rootsPerMfg[mfg] > 1 {
			prefix += "/Windows " + filepath.Base(filepath.Dir(mfgPath))
		}
		_ = filepath.WalkDir(mfgPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path == mfgPath {
					return nil
				}
				if strings.EqualFold(d.Name(), "etc") || strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				if isExtractedSibling(path) {
					return filepath.SkipDir
				}
				return nil
			}
			if !isDriverArchive(path, d.Name()) {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			relRoot, err1 := filepath.Rel(driversRoot, path)
			relMfg, err2 := filepath.Rel(mfgPath, path)
			if err1 != nil || err2 != nil {
				return nil
			}
			relMfg = filepath.ToSlash(relMfg)
			out = append(out, CleanupPackage{
				Name:     d.Name(),
				RelPath:  filepath.ToSlash(relRoot),
				TreePath: prefix + "/" + relMfg,
				Size:     info.Size(),
				Archived: inArchiveFolder(relMfg),
			})
			return nil
		})
	}
	return out
}

func inArchiveFolder(relSlash string) bool {
	for _, seg := range strings.Split(relSlash, "/") {
		if strings.EqualFold(seg, "Archive") {
			return true
		}
	}
	return false
}

// isExtractedSibling reports whether dir is an archive's extracted folder
// (Foo.zip beside Foo/) per ExtractedSiblingDirs.
func isExtractedSibling(dir string) bool {
	parent := filepath.Dir(dir)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return false
	}
	return ExtractedSiblingDirs(parent, entries)[filepath.Base(dir)]
}

// DeleteDriverPackages deletes each archive named in relPaths (relative to
// driversRoot, as ListCleanupPackages reports them) together with all of
// PDT's cached data for it: its .pdt-infcache entry (and anything nested
// inside, e.g. an inner .msi a cascade revealed) and its extracted sibling
// folder. Refuses any path outside driversRoot's manufacturer folders or
// that isn't a recognized driver archive, so it can't be pointed at
// anything else. Deploy-time full extractions live in the temp folder and
// are cleaned up after each run, so there's nothing of those to remove.
func DeleteDriverPackages(driversRoot string, relPaths []string) (deleted []string, failed map[string]string) {
	failed = map[string]string{}
	roots := manufacturerRoots(driversRoot)
	for _, rel := range relPaths {
		if err := deleteDriverPackage(driversRoot, roots, rel); err != nil {
			failed[rel] = err.Error()
			continue
		}
		deleted = append(deleted, rel)
	}
	return deleted, failed
}

func deleteDriverPackage(driversRoot string, roots []string, rel string) error {
	full := filepath.Join(driversRoot, filepath.FromSlash(rel))
	mfgRoot := ""
	for _, r := range roots {
		if within(r, full) {
			mfgRoot = r
			break
		}
	}
	if mfgRoot == "" {
		return fmt.Errorf("not inside a manufacturer folder under the Drivers folder")
	}
	info, err := os.Stat(full)
	if err != nil {
		return err
	}
	if info.IsDir() || !isDriverArchive(full, filepath.Base(full)) {
		return fmt.Errorf("not a driver package")
	}

	// Cached data first, while the archive still exists to identify it.
	if sibling := siblingFolderOf(full); sibling != "" {
		if err := os.RemoveAll(sibling); err != nil {
			return fmt.Errorf("removing its extracted folder: %w", err)
		}
	}
	if err := os.RemoveAll(infCacheDestDir(mfgRoot, full)); err != nil {
		return fmt.Errorf("removing its cached .inf files: %w", err)
	}
	return os.Remove(full)
}

// siblingFolderOf returns archive's extracted folder beside it, if
// ExtractedSiblingDirs recognizes one that belongs to this archive.
func siblingFolderOf(archive string) string {
	parent := filepath.Dir(archive)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return ""
	}
	siblings := ExtractedSiblingDirs(parent, entries)
	base := strings.TrimSuffix(filepath.Base(archive), filepath.Ext(archive))
	for name := range siblings {
		if strings.EqualFold(name, base) { // the same case-insensitive match ExtractedSiblingDirs used
			return filepath.Join(parent, name)
		}
	}
	if m := kyoceraExeNameRe.FindStringSubmatch(filepath.Base(archive)); m != nil {
		// Kyocera's two-stage .exe extracts to a folder named after its
		// version rather than the file.
		for name := range siblings {
			if strings.Contains(strings.ToLower(name), strings.ToLower(m[1])) {
				return filepath.Join(parent, name)
			}
		}
	}
	return ""
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
