package driver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// kyoceraExeNameRe matches Kyocera's current driver package naming and
// captures its version token - e.g. "KXDRIVER 8.6A.1412.exe" -> "8.6A.1412"
// (also matches the underscore-separated "KXDriver_8.6.1022.exe" form).
// Kyocera stopped shipping .zip-packaged drivers roughly 8 months before
// this was written (see the README's own "Kyocera" section) in favor of
// this format: an ordinary PE executable with the real driver archive
// embedded as a resource rather than a true self-extracting archive the way
// Lexmark's RAR-SFX packages are.
var kyoceraExeNameRe = regexp.MustCompile(`(?i)^kxdriver[ _](.+)\.exe$`)

// ensureKyoceraExesExtracted finds every Kyocera driver package .exe
// directly under root and - unless a sibling folder's name already contains
// that package's own version token - extracts it via the two-stage 7z
// process documented in the README's "Kyocera" section: 7z can pull the
// embedded archive straight out of the .exe's own ".text" PE section
// without ever running the installer, and that extracted ".text" file is
// itself a normal archive, extracted the same way a second time. A no-op if
// SevenZipPath isn't set (see its own doc comment) - same convention as
// ensureRarSfxExtracted.
func ensureKyoceraExesExtracted(root string) {
	if SevenZipPath == "" {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}

	var existingDirs []string
	for _, e := range entries {
		if e.IsDir() {
			existingDirs = append(existingDirs, e.Name())
		}
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := kyoceraExeNameRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		version := m[1]
		if kyoceraVersionAlreadyExtracted(existingDirs, version) {
			continue
		}

		destDir := filepath.Join(root, "KXDriver_"+version)
		if err := extractKyoceraExe(filepath.Join(root, e.Name()), destDir); err != nil {
			// Best-effort, same reasoning as every other ensure*Extracted
			// helper in this package: one bad package shouldn't stop the
			// catalog scan from picking up everything else, and cleaning up
			// partial output means a later run retries instead of mistaking
			// it for a complete extraction.
			os.RemoveAll(destDir)
		}
	}
}

// kyoceraVersionAlreadyExtracted reports whether any existing subfolder's
// name already contains version - a substring match rather than an exact
// one, since the manual process this automates has never enforced one fixed
// destination-folder naming convention (folders seen so far: "KXDriver_X",
// "KXDRIVER_X", one with a "_2" suffix from a duplicate download).
func kyoceraVersionAlreadyExtracted(existingDirs []string, version string) bool {
	version = strings.ToLower(version)
	for _, name := range existingDirs {
		if strings.Contains(strings.ToLower(name), version) {
			return true
		}
	}
	return false
}

// extractKyoceraExe runs the two-stage extraction: first pulls exePath's own
// embedded ".text" PE section out to a scratch folder via 7z (an ordinary
// executable's .text code section is at most a few MB; a Kyocera driver
// package's is the entire driver archive packed in as data instead of
// compiled code - confirmed against a real ~250MB package), then extracts
// that ".text" file itself - the real driver data (a 32bit/64bit/arm64
// split, Setup.exe, KmInstall.exe, etc.) - into destDir.
func extractKyoceraExe(exePath, destDir string) error {
	scratch, err := os.MkdirTemp("", "pdt-kyocera-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)

	stage1 := filepath.Join(scratch, "stage1")
	if err := os.MkdirAll(stage1, 0o755); err != nil {
		return err
	}
	if out, err := exec.Command(SevenZipPath, "x", exePath, "-o"+stage1, "-y").CombinedOutput(); err != nil {
		return fmt.Errorf("extracting %s (stage 1): %w: %s", exePath, err, out)
	}

	textPath := filepath.Join(stage1, ".text")
	if info, err := os.Stat(textPath); err != nil || info.IsDir() {
		return fmt.Errorf("expected embedded archive %q not found after extracting %s", ".text", exePath)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	if out, err := exec.Command(SevenZipPath, "x", textPath, "-o"+destDir, "-y").CombinedOutput(); err != nil {
		return fmt.Errorf("extracting %s (stage 2): %w: %s", exePath, err, out)
	}
	return nil
}
