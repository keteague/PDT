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
func EnsureArchiveExtracted(archivePath string) (string, error) {
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
	if strings.Contains(archivePath, string(filepath.Separator)+PdtInfCacheDirName+string(filepath.Separator)) {
		if outerArchive, markerDir := findSourceArchive(filepath.Dir(archivePath), ""); outerArchive != "" {
			outerDir, err := EnsureArchiveExtracted(outerArchive)
			if err != nil {
				return "", fmt.Errorf("extracting outer archive %s for nested %s: %w", outerArchive, archivePath, err)
			}
			if rel, relErr := filepath.Rel(markerDir, archivePath); relErr == nil {
				archivePath = filepath.Join(outerDir, rel)
			}
		}
	}

	name := filepath.Base(archivePath)

	if m := kyoceraExeNameRe.FindStringSubmatch(name); m != nil {
		destDir := filepath.Join(filepath.Dir(archivePath), "KXDriver_"+m[1])
		if info, statErr := os.Stat(destDir); statErr == nil && info.IsDir() {
			return destDir, nil
		}
		if err := extractKyoceraExe(archivePath, destDir); err != nil {
			os.RemoveAll(destDir)
			return "", err
		}
		return destDir, nil
	}

	destDir := strings.TrimSuffix(archivePath, filepath.Ext(archivePath))
	if info, statErr := os.Stat(destDir); statErr == nil && info.IsDir() {
		return destDir, nil
	}

	var extractErr error
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip":
		extractErr = extractZip(archivePath, destDir)
	case ".msi":
		if extractErr = extractMsi(archivePath, destDir); extractErr == nil {
			expandCompressedSiblings(destDir)
		}
	case ".exe":
		extractErr = extractSfxArchive(archivePath, destDir)
	default:
		return "", fmt.Errorf("don't know how to extract %s", archivePath)
	}
	if extractErr != nil {
		os.RemoveAll(destDir)
		return "", extractErr
	}
	flattenRedundantWrapperDir(destDir)
	return destDir, nil
}
