package driver

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// PdtInfCacheDirName is the folder name the .inf-only metadata cache
// GitHub issue #10's own catalog rework writes into (as
// <Manufacturer>/<version>/PdtInfCacheDirName/<ArchiveBaseName>/...) - a
// single, obviously-PDT-internal name that both the cache-writing side and
// anything skipping it (Sync, "Remove INF") can share without duplicating
// the literal string. Dot-prefixed so it also reads, to a technician
// browsing the folder directly, as "PDT's own, not a real driver".
const PdtInfCacheDirName = ".pdt-infcache"

// infCacheDestDir returns the .inf-only cache destination for archivePath
// (found somewhere under root, possibly nested) -
// root/PdtInfCacheDirName/<archivePath's own path relative to root, with its
// extension stripped>. Nested rather than flat (keyed just by archive
// basename) so two same-named archives at different depths under root never
// collide.
//
// archivePath already living inside root/PdtInfCacheDirName itself - a
// nested archive an earlier ensure*InfsExtracted pass revealed there (real
// example: Lexmark's own package, an outer self-extracting RAR revealing an
// inner .msi into the cache) - is handled specially: it extracts as a plain
// sibling right where it already sits (Foo.msi -> Foo/), not into a second,
// wrongly-doubled PdtInfCacheDirName/PdtInfCacheDirName/... nested inside
// the first.
// pdtSourceMarkerName is a small marker file writeSourceMarker drops
// directly inside a .inf-only cache destination, recording exactly which
// archive produced it - findSourceArchive later walks back up from a
// discovered .inf to the nearest one of these to resolve
// ArchEntry.ArchivePath (what Deploy needs to know which archive to fully
// extract on demand - see EnsureArchiveExtracted). A marker per cache
// destination, not a single index, so this stays correct at any cascade
// depth (a .msi an earlier pass revealed from inside another archive gets
// its own marker pointing at *that* .msi, not the outermost one).
const pdtSourceMarkerName = ".pdt-source"

// writeSourceMarker is best-effort - a missing marker just means
// findSourceArchive won't resolve an ArchivePath for whatever .inf ends up
// under destDir (ArchEntry.ArchivePath stays ""), which only disables lazy
// re-extraction at Deploy time for that one entry; it was never required
// for BuildCatalog's own .inf parsing to work.
func writeSourceMarker(destDir, archivePath string) {
	_ = os.WriteFile(filepath.Join(destDir, pdtSourceMarkerName), []byte(archivePath), 0o644)
}

// findSourceArchive walks up from dir (typically filepath.Dir of a
// discovered .inf) looking for the nearest pdtSourceMarkerName written by
// writeSourceMarker, stopping once it reaches stopAt (the manufacturer
// folder) without finding one. Returns ("", "") if none found - e.g. an
// .inf sitting somewhere already fully extracted from before this rework
// existed at all, which never got a marker written for it. markerDir (the
// directory the marker was actually found in - the .inf-only cache's own
// destDir) is what ArchEntry.InfRelPath gets computed relative to, so
// EnsureArchiveExtracted's own real, fully-extracted destination can be
// joined with that same relative path to find the real .inf once it exists.
func findSourceArchive(dir, stopAt string) (archivePath, markerDir string) {
	for {
		if data, err := os.ReadFile(filepath.Join(dir, pdtSourceMarkerName)); err == nil {
			return string(data), dir
		}
		if dir == stopAt {
			return "", ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
	}
}

// pruneOrphanedInfCache removes each top-level entry under
// root/PdtInfCacheDirName whose own recorded source archive (via the
// .pdt-source marker writeSourceMarker leaves at the top of every .inf-only
// cache entry) no longer exists on disk - GitHub issue #10's own "no stale
// files once a driver is removed or archived" requirement, confirmed live
// as a real correctness gap, not just disk hygiene: scanManufacturerFolders'
// own .inf-discovery walk finds whatever .inf files are actually sitting on
// disk regardless of whether their source archive still exists, so without
// this, a removed/archived driver's stale cached .inf would keep that
// driver visible in the catalog indefinitely. Only ever inspects TOP-LEVEL
// cache entries (direct children of PdtInfCacheDirName) - removing one
// removes everything nested inside it too (a cascade's own inner .msi
// cache, if any), so there's no need to separately validate nested markers.
// A cache entry with no marker at all (shouldn't normally happen, but could
// from an older PDT version or a manual folder) is left alone rather than
// guessed at.
//
// Best-effort and silently skipped on any failure - correctness never
// depends on the removal itself succeeding (only on running this before the
// .inf-discovery walk, so a stale entry that IS successfully removed can
// never be found by it), so a write-protected flash drive (Ken's own real
// field-deployment plan) just keeps whatever stale entries it already has
// rather than erroring - no worse than before this function existed, and
// still correct once run again from a writable location.
func pruneOrphanedInfCache(root string) {
	cacheRoot := filepath.Join(root, PdtInfCacheDirName)
	entries, err := os.ReadDir(cacheRoot)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		entryDir := filepath.Join(cacheRoot, e.Name())
		data, err := os.ReadFile(filepath.Join(entryDir, pdtSourceMarkerName))
		if err != nil {
			continue // no marker - leave it alone rather than guess
		}
		if _, statErr := os.Stat(string(data)); os.IsNotExist(statErr) {
			os.RemoveAll(entryDir)
		}
	}
}

func infCacheDestDir(root, archivePath string) string {
	cacheRoot := filepath.Join(root, PdtInfCacheDirName)
	if rel, err := filepath.Rel(cacheRoot, archivePath); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return strings.TrimSuffix(archivePath, filepath.Ext(archivePath))
	}

	rel, err := filepath.Rel(root, archivePath)
	if err != nil {
		rel = filepath.Base(archivePath)
	}
	rel = strings.TrimSuffix(rel, filepath.Ext(rel))
	return filepath.Join(cacheRoot, rel)
}

// ExtractedSiblingDirs, given dirPath's own immediate children (entries,
// e.g. from os.ReadDir), returns the subset of directory names among them
// that are the deterministic extraction output of some archive file also in
// entries - the exact same archive-to-folder naming convention
// ensureZipsExtracted/ensureMsiExtracted/ensureSfxArchivesExtracted/
// ensureKyoceraExesExtracted each already use to decide where to extract an
// archive TO (Foo.zip/.msi/a real self-extracting .exe -> Foo/; Kyocera's
// own KXDriver_<version>.exe -> whichever sibling folder name *contains*
// that version token - see kyoceraVersionAlreadyExtracted's own doc comment
// for why that one isn't a simple basename match), computed here without
// extracting anything.
//
// Sync (both flash-drive Sync - copytree.go - and Cloud Sync -
// internal/cloudsync) uses this to skip transferring the extracted sprawl
// entirely: once a technician has the archive itself, the extracted folder
// is a derived, re-creatable artifact BuildCatalog regenerates locally on
// its own - confirmed live a real Drivers folder was 5.4GB/22,570 files with
// both kept forever, vs. ~1.5GB/~25 files for just the archives (GitHub
// issue #10). The same naming convention is reused for the Deploy-time
// on-demand full-extraction cache once that lands (issue #10's own "deeper
// option") - this function doesn't need to change for that.
//
// Only recognizes a direct parent/child relationship (an archive and its
// extracted folder sitting side by side in the same directory) - this
// matches every extraction helper's own behavior, none of which ever
// extracts anywhere but a same-level sibling of the archive itself.
// isSelfExtractingArchive needs to read file bytes, so dirPath is required
// to resolve full paths for that check - entries themselves only carry bare
// names.
func ExtractedSiblingDirs(dirPath string, entries []fs.DirEntry) map[string]bool {
	dirNames := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			dirNames[e.Name()] = true
		}
	}

	result := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()

		if m := kyoceraExeNameRe.FindStringSubmatch(name); m != nil {
			version := strings.ToLower(m[1])
			for dirName := range dirNames {
				if strings.Contains(strings.ToLower(dirName), version) {
					result[dirName] = true
				}
			}
			continue
		}

		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".zip" && ext != ".msi" && ext != ".exe" {
			continue
		}
		if ext == ".exe" && !isSelfExtractingArchive(filepath.Join(dirPath, name)) {
			continue
		}
		base := strings.TrimSuffix(name, filepath.Ext(name))
		for dirName := range dirNames {
			if strings.EqualFold(dirName, base) {
				result[dirName] = true
				break
			}
		}
	}
	return result
}
