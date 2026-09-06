package driver

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// compressedSiblingRe matches Microsoft's legacy single-file-compressed
// naming convention: the last character of the real extension replaced with
// "_", optionally suffixed "64" for a 64-bit-specific variant - e.g. ".dl_"
// (real: ".dll"), ".dl_64", ".gd_", ".in_". Confirmed against a real package
// this mapping is *not* the simple same-first-two-letters guess it looks
// like (".gd_" decompresses to ".gdl", not ".gpd"; ".in_" to ".ini", not
// ".inf") - which is exactly why expandCompressedSiblings below uses
// expand.exe's own -R flag (restore original name, read out of the
// compressed file's own header) instead of guessing.
var compressedSiblingRe = regexp.MustCompile(`(?i)\.[a-z0-9]{2}_(64)?$`)

// ensureMsiExtracted finds every .msi directly under root (skipping
// "etc"/"Archive" paths, same as ensureZipsExtracted) and unpacks each into
// a sibling folder - Foo.msi -> Foo/ - via an MSI *administrative install*
// (msiexec /a ... TARGETDIR=..., which only extracts files to their real
// names/paths rather than installing anything), then decompresses every
// Microsoft legacy-compressed sibling file the install produces. Confirmed
// necessary against a real package (Lexmark's driver ships its .inf and
// companion files this way, reachable only after ensureRarSfxExtracted has
// already unpacked its outer self-extracting RAR wrapper) - BuildCatalog
// only ever looks for .inf files already sitting on disk in their real,
// uncompressed form. Uses msiexec.exe/expand.exe directly (both are part of
// Windows itself), unlike RAR extraction, which needs the bundled 7z.exe.
func ensureMsiExtracted(root string) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.EqualFold(d.Name(), "etc") || strings.EqualFold(d.Name(), "Archive") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".msi") {
			return nil
		}

		destDir := strings.TrimSuffix(path, filepath.Ext(path))
		if info, statErr := os.Stat(destDir); statErr == nil && info.IsDir() {
			return nil // already extracted
		}

		if err := extractMsi(path, destDir); err != nil {
			// Best-effort, same reasoning as ensureZipsExtracted: one bad
			// .msi shouldn't stop the catalog scan from picking up
			// everything else, and cleaning up partial output means a later
			// run retries instead of mistaking it for a complete extraction.
			os.RemoveAll(destDir)
			return nil
		}
		expandCompressedSiblings(destDir)
		return nil
	})
}

// extractMsi runs an MSI administrative install: unpacks msiPath's files to
// their real names/paths under destDir without installing anything.
func extractMsi(msiPath, destDir string) error {
	cmd := exec.Command("msiexec.exe", "/a", msiPath, "/qn", "TARGETDIR="+destDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("msiexec /a on %s failed: %w: %s", msiPath, err, out)
	}
	return nil
}

// expandCompressedSiblings decompresses every Microsoft legacy-compressed
// file (see compressedSiblingRe) found under root, in place, via expand.exe
// -R. Best-effort per file - one file that fails to expand doesn't stop the
// others, and a file that isn't actually needed by any .inf just sits there
// unused rather than breaking anything.
func expandCompressedSiblings(root string) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !compressedSiblingRe.MatchString(path) {
			return nil
		}
		cmd := exec.Command("expand.exe", "-R", path)
		_ = cmd.Run()
		return nil
	})
}
