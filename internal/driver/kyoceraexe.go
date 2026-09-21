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
// ensureSfxArchivesExtracted.
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
		if !e.IsDir() {
			continue
		}
		full := filepath.Join(root, e.Name())
		if looksLikeRawPEDump(full) {
			// A leftover from the exact bug ensureSfxArchivesExtracted's own
			// Kyocera exclusion now prevents going forward (see its doc
			// comment): a plain `7z x` run directly against a Kyocera
			// package's raw exe, rather than this file's own two-stage
			// process, produces just its PE sections at the top level -
			// still fooled kyoceraVersionAlreadyExtracted's substring match
			// below, since its folder name still contained the version
			// token, permanently blocking a real re-extraction. Removing it
			// here - rather than merely excluding new occurrences - is what
			// actually repairs an install that already has one sitting
			// around from before this fix existed.
			os.RemoveAll(full)
			continue
		}
		existingDirs = append(existingDirs, e.Name())
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

// looksLikeRawPEDump reports whether dir is a leftover of an .exe having
// been fed to a plain, non-Kyocera-aware extractor instead of this file's
// own two-stage process: a Kyocera package's raw PE sections at its top
// level - ".text" (the actual embedded driver archive, itself never
// unpacked), ".rsrc"/".rsrc_1", ".reloc", "CERTIFICATE" - not a single real
// driver file. A top-level ".text" *file* (not a folder - a real extracted
// driver package has no reason to ever produce a bare file by that name) is
// the unambiguous tell.
func looksLikeRawPEDump(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".text"))
	return err == nil && !info.IsDir()
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

// ensureKyoceraExeInfsExtracted is ensureKyoceraExesExtracted's .inf-only
// sibling - GitHub issue #10's catalog rework. Stage 1 (pulling the
// embedded ".text" PE section out via 7z) is unavoidable and already
// small; stage 2 uses 7z's own selective-extraction filter instead of a
// full unpack of the real driver payload (see extractInfsFromKyoceraExe).
// Destination folders live under PdtInfCacheDirName instead of directly in
// root, but kyoceraVersionAlreadyExtracted's own version-substring check
// (not a simple basename match - see its own doc comment) is applied the
// same way, just against that cache folder's own existing entries.
func ensureKyoceraExeInfsExtracted(root string) {
	if SevenZipPath == "" {
		return
	}
	cacheRoot := filepath.Join(root, PdtInfCacheDirName)
	var existingDirs []string
	if cacheEntries, err := os.ReadDir(cacheRoot); err == nil {
		for _, e := range cacheEntries {
			if e.IsDir() {
				existingDirs = append(existingDirs, e.Name())
			}
		}
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return
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

		destDir := filepath.Join(cacheRoot, "KXDriver_"+version)
		exePath := filepath.Join(root, e.Name())
		if err := extractInfsFromKyoceraExe(exePath, destDir); err != nil {
			os.RemoveAll(destDir)
			warnExtraction("%s: could not extract .inf from %s: %v", filepath.Base(root), e.Name(), err)
			continue
		}
		writeSourceMarker(destDir, exePath)
	}
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

// extractInfsFromKyoceraExe is extractKyoceraExe's .inf-only sibling - stage
// 1 (pulling the embedded ".text" PE section out) is unavoidable and
// already small regardless of what's kept afterward; stage 2 uses 7z's own
// selective-extraction filter (see extractInfsFromSfxArchive's own doc
// comment for the same technique) instead of a full unpack of the real
// driver payload.
func extractInfsFromKyoceraExe(exePath, destDir string) error {
	scratch, err := os.MkdirTemp("", "pdt-kyocera-inf-*")
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
	if out, err := exec.Command(SevenZipPath, "x", textPath, "-o"+destDir, "*.inf", "-r", "-y").CombinedOutput(); err != nil {
		return fmt.Errorf("7z .inf-only extraction of %s (stage 2) failed: %w: %s", exePath, err, out)
	}
	return nil
}
