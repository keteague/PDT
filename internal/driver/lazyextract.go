package driver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnsureArchiveExtracted fully extracts archivePath (a real archive path -
// ArchEntry.ArchivePath, resolved via a real driver selection) into its own
// deterministic destination, on demand, right before Deploy needs the real
// files a .inf's own SourceDisksFiles/CopyFiles sections reference -
// StageInf (internal/printer/windows/driverinstall_windows.go) requires
// them physically present alongside the .inf, a hard Win32 constraint
// (SetupCopyOEMInfW), not a PDT design choice. GitHub issue #10's catalog
// rework stopped doing this eagerly at every catalog scan (see
// ensureZipInfsExtracted's own doc comment) specifically so it only ever
// happens for the one package actually being installed, not every package
// found locally.
//
// Reuses the exact same per-format extraction functions
// (extractZip/extractMsi+expandCompressedSiblings/extractSfxArchive/
// extractKyoceraExe) and destination-naming conventions the old eager
// ensure*Extracted helpers already used - zero change to the extraction
// logic itself, just when it runs. Same skip-if-already-extracted
// convention too: a repeat deploy of the same driver reuses the folder
// instead of re-extracting.
//
// The returned cleanup is a no-op for the normal case (the extracted
// sibling folder is meant to persist and be reused, exactly like before) -
// it only does real work for the write-protected-media fallback described
// below. Always call it once done with destDir; the caller
// (deploy_windows.go) defers it right after a successful call.
//
// Issue #10's own "Portable-mode interaction" gap, fixed 2026-09-16:
// extraction used to always write as a sibling of the archive itself, on
// whatever drive that archive happens to sit on. Confirmed as a real,
// live-blocking gap for Ken's own stated field-deployment plan (PDT run
// portably from a flash drive plugged into a client endpoint it's never
// touched before) - a write-protected flash drive would fail that write
// outright, breaking Deploy entirely for that scenario, with no fallback at
// all. extractWithFallback now retries into a throwaway local-disk scratch
// directory whenever the primary (sibling) attempt fails for any reason.
// Ken's own explicit call on the open design question the issue itself
// left unresolved (persist vs. clean up the fallback cache): clean up
// after each Deploy, not a persistent cache keyed by archive identity - a
// repeat deploy of the same driver from the same write-protected drive
// re-extracts from scratch every time, trading a little speed for never
// leaving a persistent footprint scattered across however many different
// flash drives/sessions this ever runs from.
func EnsureArchiveExtracted(archivePath string) (destDir string, cleanup func(), err error) {
	noop := func() {}
	outerCleanup := noop

	// archivePath living inside .pdt-infcache means it's a NESTED archive an
	// earlier .inf-only pass revealed from inside some outer archive - a
	// real, live example: Lexmark's own package is an outer self-extracting
	// RAR whose selective .inf-only extraction (extractInfsFromSfxArchive)
	// also pulls out several inner .msi files by design (needed to then find
	// *their* own .inf entries in turn - see ensureMsiInfsExtracted's own
	// ordering comment in catalog.go), so ArchEntry.ArchivePath for a driver
	// found that way points at one of those inner .msi files, which already
	// sits inside .pdt-infcache.
	//
	// Extracting a nested archive as a plain sibling right where it
	// currently sits (this function's own normal destDir rule, below) would
	// collide with the exact same location the earlier .inf-only pass
	// already created for it (infCacheDestDir's own "already inside
	// PdtInfCacheDirName -> extract as plain sibling" rule computes the
	// identical path) - confirmed as a real bug found live: the
	// "already extracted, skip" check below then treated that incomplete
	// .inf-only folder (containing just the cached .inf, none of the
	// companion files a real deploy needs alongside it) as if it were
	// already a full extraction, and StageInf failed with "The system cannot
	// find the file specified" trying to read a companion file that was
	// never actually there.
	//
	// Fixed by fully extracting the OUTER archive first (recursively, so
	// this handles any nesting depth - findSourceArchive/
	// EnsureArchiveExtracted are both already general enough), then
	// re-resolving archivePath to its own real position inside that
	// now-fully-extracted directory - the same relative path .inf-only
	// extraction's own selective 7z filter (-r, recursive) already preserved
	// against the cache, so it lines up with where a real, unfiltered
	// extraction puts the same file.
	//
	// outerCleanup is deliberately propagated, not called here - the nested
	// file we're about to resolve archivePath to lives inside outerDir, so
	// cleaning it up before we're done reading from it would delete what we
	// need. The nested extraction's own returned cleanup below wraps it, so
	// both eventually run together.
	if strings.Contains(archivePath, string(filepath.Separator)+PdtInfCacheDirName+string(filepath.Separator)) {
		if outerArchive, markerDir := findSourceArchive(filepath.Dir(archivePath), ""); outerArchive != "" {
			outerDir, oc, err := EnsureArchiveExtracted(outerArchive)
			if err != nil {
				return "", noop, fmt.Errorf("extracting outer archive %s for nested %s: %w", outerArchive, archivePath, err)
			}
			outerCleanup = oc
			if rel, relErr := filepath.Rel(markerDir, archivePath); relErr == nil {
				archivePath = filepath.Join(outerDir, rel)
			}
		}
	}

	name := filepath.Base(archivePath)

	if m := kyoceraExeNameRe.FindStringSubmatch(name); m != nil {
		primaryDest := filepath.Join(filepath.Dir(archivePath), "KXDriver_"+m[1])
		dest, cleanup, err := extractWithFallback(archivePath, primaryDest, extractKyoceraExe)
		if err != nil {
			return "", outerCleanup, err
		}
		return dest, combineCleanups(cleanup, outerCleanup), nil
	}

	primaryDest := strings.TrimSuffix(archivePath, filepath.Ext(archivePath))
	var extract func(archivePath, destDir string) error
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip":
		extract = func(src, dst string) error {
			if err := extractZip(src, dst); err != nil {
				return err
			}
			flattenRedundantWrapperDir(dst)
			return nil
		}
	case ".msi":
		extract = func(src, dst string) error {
			if err := extractMsi(src, dst); err != nil {
				return err
			}
			expandCompressedSiblings(dst)
			flattenRedundantWrapperDir(dst)
			return nil
		}
	case ".exe":
		extract = func(src, dst string) error {
			if err := extractSfxArchive(src, dst); err != nil {
				return err
			}
			flattenRedundantWrapperDir(dst)
			return nil
		}
	default:
		return "", outerCleanup, fmt.Errorf("don't know how to extract %s", archivePath)
	}

	dest, cleanup, err := extractWithFallback(archivePath, primaryDest, extract)
	if err != nil {
		return "", outerCleanup, err
	}
	return dest, combineCleanups(cleanup, outerCleanup), nil
}

// extractWithFallback tries extract(archivePath, primaryDest) first (the
// normal, persistent sibling-of-the-archive location, reused as-is on a
// repeat call); if that fails for any reason, retries once into a
// throwaway local-disk scratch directory instead - see
// EnsureArchiveExtracted's own doc comment for why (a write-protected flash
// drive, Ken's own real field-deployment scenario, confirmed live as a gap
// with no fallback at all before this).
//
// Deliberately doesn't try to distinguish *why* the first attempt failed
// (a genuinely corrupt archive fails identically in both locations, at the
// cost of one harmless extra attempt) - simpler and more robust than
// pattern-matching per-tool (7z/msiexec/archive/zip) error text the way
// issue #12's own installer-version-gate fallback needed to on macOS; here,
// any failure writing to the archive's own location is equally deserving
// of the same retry, since the only reason to prefer that location at all
// is keeping the extracted copy traveling with the archive when that's
// actually possible.
//
// On a fallback success, the returned cleanup removes the scratch
// directory - the caller is expected to call it once done, per Ken's own
// explicit call (2026-09-16) that this cache should not persist across
// deploys.
func extractWithFallback(archivePath, primaryDest string, extract func(archivePath, destDir string) error) (destDir string, cleanup func(), err error) {
	noop := func() {}
	if info, statErr := os.Stat(primaryDest); statErr == nil && info.IsDir() {
		return primaryDest, noop, nil
	}
	primaryErr := extract(archivePath, primaryDest)
	if primaryErr == nil {
		return primaryDest, noop, nil
	}
	os.RemoveAll(primaryDest)

	scratchDir, mkErr := os.MkdirTemp("", "pdt-extract-fallback-*")
	if mkErr != nil {
		return "", noop, primaryErr
	}
	if fbErr := extract(archivePath, scratchDir); fbErr != nil {
		os.RemoveAll(scratchDir)
		return "", noop, primaryErr // surface the original, honest failure - not the fallback's own
	}
	return scratchDir, func() { os.RemoveAll(scratchDir) }, nil
}

// combineCleanups returns a func that calls both, inner first - the order
// matters whenever inner's own destDir lives inside whatever outer
// extracted (the nested-archive case above): deleting outer first would
// pull the rug out from under a caller that hasn't finished with inner yet
// if the two cleanups somehow ran concurrently, though in practice both
// only ever run sequentially, once, after Deploy is fully done with both.
func combineCleanups(inner, outer func()) func() {
	return func() {
		inner()
		outer()
	}
}
