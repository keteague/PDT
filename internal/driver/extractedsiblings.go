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

// PartialDownloadSuffix marks a Cloud Sync download still in progress (see
// cloudsync.Download): kept on disk, including after Cancel, so a later run can
// resume it with a Range request instead of starting over.
const PartialDownloadSuffix = ".pdt-partial"

// DSStoreFileName is Finder's own per-folder metadata file (icon
// positions/view settings) - macOS creates one in nearly every folder it
// browses, including a Drivers folder mounted from a Windows share or synced
// from a Windows machine. Pure Finder chrome, never anything a deploy or a
// catalog scan reads - Sync (both flash-drive and Cloud Sync) skips it
// outright, on both the local and remote side, so it's never transferred at
// all rather than round-tripped as if it were real driver content.
const DSStoreFileName = ".DS_Store"

// IsIgnoredDotEntry reports whether name - a single path segment, a file or
// directory's own bare name rather than a full path - is a dotfile/
// dotfolder that Sync (flash-drive Write/Sync and Cloud Sync alike) should
// skip entirely: .DS_Store, a stray .git a technician's own tooling left in
// a Drivers folder, or anything else dot-prefixed - none of it is real
// driver content, and a Drivers folder is exactly the kind of place stray
// tooling clutter accumulates over time on a long-lived shared laptop.
//
// PdtInfCacheDirName is the sole exception, despite being dot-prefixed
// itself (deliberately, so it reads to a technician browsing the folder
// directly as "PDT's own, not a real driver" - see its own doc comment):
// its contents are real, useful catalog metadata (the .inf-only extraction
// cache GitHub issue #10's own catalog rework builds), not disposable
// clutter, so it and everything under it should travel with the rest of the
// folder rather than being silently dropped.
//
// An in-progress Cloud Sync download (PartialDownloadSuffix) is skipped too:
// it is a resumable half-file, never something to upload or copy onward.
func IsIgnoredDotEntry(name string) bool {
	if strings.HasSuffix(name, PartialDownloadSuffix) {
		return true
	}
	return name != PdtInfCacheDirName && strings.HasPrefix(name, ".")
}

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

// prepareInfCacheDest decides what to do about an .inf-only cache
// destination that already exists: skip=true when it's a finished entry
// (carries the source marker writeSourceMarker leaves as the very last step
// of a successful pass), so the caller leaves it alone; otherwise it's an
// interrupted or pre-rework leftover and gets removed so the caller
// re-extracts into a clean folder. Confirmed live (Ken, 2026-09-20) as the
// real cause of two separate "driver never shows up / fails at Deploy"
// reports from the same drivers folder: Toshiba's universal zip (no .inf
// files in its cache at all, so zero Windows drivers) and Lexmark's
// self-extracting package (inner .msi markers pointing at a full-extraction
// folder that no longer exists, so Deploy ran msiexec on a missing file and
// failed with 1619) - both left by an older PDT version, both previously
// trusted purely because the folder existed. A folder that can't be removed
// (write-protected media) is left alone (skip=true) rather than retried into.
func prepareInfCacheDest(destDir string) (skip bool) {
	info, err := os.Stat(destDir)
	if err != nil || !info.IsDir() {
		return false
	}
	if _, markerErr := os.Stat(filepath.Join(destDir, pdtSourceMarkerName)); markerErr == nil {
		return true
	}
	return os.RemoveAll(destDir) != nil
}

// writeSourceMarker is best-effort - a missing marker just means
// findSourceArchive won't resolve an ArchivePath for whatever .inf ends up
// under destDir (ArchEntry.ArchivePath stays ""), which only disables lazy
// re-extraction at Deploy time for that one entry; it was never required
// for BuildCatalog's own .inf parsing to work.
//
// root is the manufacturer folder archivePath was found under (every
// ensure*InfsExtracted caller already has this as its own "root" parameter)
// - archivePath is stored relative to root (relToDriversRoot, reused
// verbatim from the identical GitHub issue #13 fix on the macOS catalog
// side), not as the absolute string itself. Ken's own explicit design
// (2026-10-02, GitHub issue: stale .pdt-source markers): an absolute path
// bakes in whatever drive letter/machine happened to be true the one moment
// this was written, and .pdt-infcache is explicitly meant to travel with
// the rest of the Drivers folder via Sync/Cloud Sync (IsIgnoredDotEntry's
// own doc comment) - to a different drive letter, a different computer
// entirely, even a different OS. A root-relative value means the same real
// file everywhere it's ever read, resolved fresh against whatever THIS
// machine's own driversRoot()/manufacturer folder currently is (see
// resolveMarkerArchivePath) - the identical fix this package's own
// relToDriversRoot/absFromDriversRoot pair already shipped for
// catalog.<mfg>.json.
func writeSourceMarker(root, destDir, archivePath string) {
	stored := relToDriversRoot(root, archivePath)
	_ = os.WriteFile(filepath.Join(destDir, pdtSourceMarkerName), []byte(stored), 0o644)
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
// The returned archivePath is always resolved to a real, directly-openable
// path on THIS machine - see resolveMarkerArchivePath for how a marker's
// own stored content (relative, going forward - or absolute, from an older
// PDT version) gets there.
func findSourceArchive(dir, stopAt string) (archivePath, markerDir string) {
	for {
		if data, err := os.ReadFile(filepath.Join(dir, pdtSourceMarkerName)); err == nil {
			return resolveMarkerArchivePath(dir, string(data)), dir
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

// resolveMarkerArchivePath turns a .pdt-source marker's stored content
// (found in markerDir) into a real, directly-openable path on THIS machine.
//
// The common, going-forward case: stored is root-relative (see
// writeSourceMarker's own doc comment) - resolved by finding the
// manufacturer folder markerDir is nested under (mfgPathFromCacheDir) and
// joining it back on, via absFromDriversRoot (reused verbatim, same as
// writeSourceMarker reuses relToDriversRoot - its own inverse is exactly
// what's needed here too). Portable by construction: never encodes a drive
// letter or machine-specific prefix at all, so this always resolves
// correctly regardless of what drive letter this flash drive happens to
// mount as on whatever machine is reading it today.
//
// Two backward-compatible cases for a marker an older PDT version wrote,
// before this fix, always as an absolute path:
//   - still resolves correctly (the common case for a technician's local,
//     installed copy, where the absolute path never actually moves) - used
//     as-is.
//   - stale (GitHub issue: stale .pdt-source markers, confirmed live
//     2026-10-02 against R.K. Black Inc's own Drivers folder: a marker
//     baked in whatever absolute path - even a malformed one, from the
//     degenerate-os.Executable() bug resolveAgainstExe/app.go fixes - was
//     true wherever/whenever it was first written, carried forward
//     verbatim by every later copy/Sync of the Drivers folder since,
//     regardless of this machine's own current drive letter) - recovered
//     from the marker's own current on-disk location instead (see
//     recoverArchivePathFromMarkerLocation), since .pdt-infcache's own
//     folder structure already mirrors the real archive's position
//     relative to its manufacturer folder.
//
// "Old-format" is detected via filepath.VolumeName(stored), not
// filepath.IsAbs - deliberately broader: the real, confirmed-live stale
// marker this was built against ("E:Drivers\Windows\11\Sharp\...", missing
// its separator) is itself a Windows drive-relative path, which
// filepath.IsAbs correctly reports as NOT absolute (same reasoning as
// resolveAgainstExe's own guard, app.go) - treating it as "new-format
// relative" instead would join it onto the live root as a literal path
// segment (producing a nonsense doubled-up path, confirmed by this
// function's own test), rather than recognizing it as exactly the
// corrupted-absolute case recoverArchivePathFromMarkerLocation exists for.
// A genuine new-format value (always written via relToDriversRoot, always
// forward-slash, never carrying a drive letter) never has a volume name, so
// this never misroutes the common case.
func resolveMarkerArchivePath(markerDir, stored string) string {
	if filepath.VolumeName(stored) == "" {
		if root := mfgPathFromCacheDir(markerDir); root != "" {
			return absFromDriversRoot(root, stored)
		}
		return stored
	}
	if fileExists(stored) {
		return stored
	}
	if root := mfgPathFromCacheDir(markerDir); root != "" {
		return recoverArchivePathFromMarkerLocation(root, markerDir, stored)
	}
	return stored
}

// mfgPathFromCacheDir walks up from a .pdt-infcache entry's own directory -
// markerDir, wherever writeSourceMarker's own destDir ended up, possibly
// several levels deep for a nested/cascaded archive (ensureMsiInfsExtracted's
// own cascade comment) - to the manufacturer folder it's nested under: the
// parent of the nearest ancestor literally named PdtInfCacheDirName. Returns
// "" if markerDir somehow isn't under a PdtInfCacheDirName at all (shouldn't
// happen - every real marker is written by one of the ensure*InfsExtracted
// helpers, always somewhere under <manufacturer folder>/PdtInfCacheDirName).
func mfgPathFromCacheDir(markerDir string) string {
	dir := markerDir
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		if filepath.Base(parent) == PdtInfCacheDirName {
			return filepath.Dir(parent)
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
		// Resolved the same way findSourceArchive would (root-relative,
		// going forward - see writeSourceMarker/resolveMarkerArchivePath's
		// own doc comments) - stat'ing the marker's raw stored content
		// directly would wrongly treat every root-relative entry as
		// orphaned (a bare "Foo.zip" never exists relative to this
		// process's own working directory).
		if _, statErr := os.Stat(resolveMarkerArchivePath(entryDir, string(data))); os.IsNotExist(statErr) {
			os.RemoveAll(entryDir)
		}
	}
}

// fileExists reports whether path is a real, regular (non-directory) file -
// recoverArchivePathFromMarkerLocation's own "does this path actually exist
// on this machine" check, package-local since the main package's own
// identically-named helper (exportconfigs.go) isn't importable from here.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// recoverArchivePathFromMarkerLocation reconstructs an archive's real path
// from where its own .pdt-source marker currently sits on disk, used when
// storedArchivePath (the marker's literal, stored contents) doesn't exist
// on this machine.
//
// Confirmed live (2026-10-02, GitHub issue: stale .pdt-source markers): a
// marker is written once, by writeSourceMarker, at whatever moment its
// .inf was first extracted - and is never revisited after that. A portable/
// removable-drive launch always uses BuildCatalogNoExtract (see
// scanManufacturerFolders' own extract parameter / loadCatalog's doc
// comment), which skips ensure*InfsExtracted entirely, so a flash drive's
// own .pdt-infcache is pure, never-locally-regenerated carried-over state -
// exactly matching IsIgnoredDotEntry's own doc comment that it's meant to
// "travel with the rest of the folder" via Sync. If the machine/drive-letter
// that was true at write time was ever wrong even once (confirmed: an
// actual historical instance of resolveAgainstExe's own degenerate-
// os.Executable() bug - app.go - baked "E:Drivers\..." into a marker,
// missing separator and all), that mistake now ships, verbatim, in every
// copy of the Drivers folder made from that point on, on every machine that
// ever reads it, forever - nothing about a normal Rescan/Refresh ever
// revisits an already-cached marker to confirm it still resolves on
// whatever machine is reading it today.
//
// The fix doesn't require parsing or guessing where the stale prefix in
// storedArchivePath ends: markerDir's own position under
// root/PdtInfCacheDirName mirrors the real archive's position under root,
// extension stripped (see infCacheDestDir's non-nested case, which this
// mirrors in reverse) - so root joined with that same relative position,
// plus storedArchivePath's own file extension (the one part of the stored
// string that's never machine- or drive-letter-specific), reconstructs
// exactly where the real archive sits on THIS machine, regardless of what
// absolute prefix the marker happened to be written with.
func recoverArchivePathFromMarkerLocation(root, markerDir, storedArchivePath string) string {
	cacheRoot := filepath.Join(root, PdtInfCacheDirName)
	rel, err := filepath.Rel(cacheRoot, markerDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return storedArchivePath
	}
	return filepath.Join(root, rel) + filepath.Ext(storedArchivePath)
}

// inInfCache reports whether path already lives inside root/PdtInfCacheDirName
// - i.e. it's a nested archive an earlier ensure*InfsExtracted pass revealed
// there, rather than a real driver archive a technician put in the Drivers
// folder.
func inInfCache(root, path string) bool {
	rel, err := filepath.Rel(filepath.Join(root, PdtInfCacheDirName), path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// removeProcessedCacheArchive deletes archivePath - a nested archive
// (Lexmark's inner .msi files) that an earlier pass pulled into the cache
// only so its own .inf files could then be extracted - once destDir, that
// extraction's result, carries its finished-entry marker. The cache is meant
// to hold .inf files only (and travels with Sync), so keeping the archive
// itself just wastes disk: 152MB for Lexmark's Universal package alone. Safe
// because nothing reads the file again: the marker's recorded path is only
// ever used as a string, and lazyextract.ensureLocked re-resolves it to the
// real file inside a fresh full extraction of the outer archive. Also what
// clears the .msi files an older PDT version left behind. Best-effort.
func removeProcessedCacheArchive(destDir, archivePath string) {
	if _, err := os.Stat(filepath.Join(destDir, pdtSourceMarkerName)); err == nil {
		_ = os.Remove(archivePath)
	}
}

func infCacheDestDir(root, archivePath string) string {
	cacheRoot := filepath.Join(root, PdtInfCacheDirName)
	if inInfCache(root, archivePath) {
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
