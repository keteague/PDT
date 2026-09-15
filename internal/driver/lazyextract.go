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
