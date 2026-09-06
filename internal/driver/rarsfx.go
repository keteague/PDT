package driver

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SevenZipPath is the path to a working 7z.exe, set by main at startup (see
// the repo root's sevenzip.go) from its own embedded copy. Left at its zero
// value ("") skips self-extracting-RAR auto-extraction entirely - what
// happens during `go test` and for any build that doesn't wire this up -
// rather than failing the catalog scan over it.
//
// Bundled specifically because self-extracting RAR archives (Lexmark's own
// driver package ships this way) have no good alternative: Go's standard
// library has no RAR reader at all, and the one pure-Go option evaluated
// (nwaples/rardecode) was found to silently corrupt exactly the .msi files
// this needs - confirmed by successfully staging the corrupted result and
// then having Windows' own msiexec reject it as an invalid package. See the
// README's "Lexmark" section for the full story.
var SevenZipPath string

// rarSignatures: the byte sequence marking the start of RAR archive data -
// RAR5's is one byte longer than the older 1.5-4.x one. A self-extracting
// archive's native PE stub (the code that shows the "Extracting..." UI)
// precedes this by anywhere up to a few hundred KB in packages seen so far,
// so isSelfExtractingRar scans well past that rather than checking only the
// first few bytes.
var rarSignatures = [][]byte{
	{'R', 'a', 'r', '!', 0x1A, 0x07, 0x01, 0x00}, // RAR5
	{'R', 'a', 'r', '!', 0x1A, 0x07, 0x00},       // RAR 1.5-4.x
}

// rarSignatureScanLimit bounds how much of a candidate .exe gets read
// looking for an embedded RAR signature - confirmed against a real
// self-extracting package that the signature sits a few hundred KB in, so
// this is generous headroom without reading an entire (100+MB) installer
// just to rule out an ordinary, unrelated .exe.
const rarSignatureScanLimit = 8 << 20 // 8 MiB

// isSelfExtractingRar reports whether path contains a RAR signature within
// the first rarSignatureScanLimit bytes - true for a self-extracting RAR
// archive, false for an ordinary executable (the common case: most .exe
// files encountered while scanning a Drivers folder are legitimate installer
// tools, not archives, and must be left alone rather than fed to 7z.exe).
func isSelfExtractingRar(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, rarSignatureScanLimit)
	n, _ := io.ReadFull(f, buf)
	buf = buf[:n]

	for _, sig := range rarSignatures {
		if bytes.Contains(buf, sig) {
			return true
		}
	}
	return false
}

// ensureRarSfxExtracted finds every self-extracting RAR .exe directly under
// root (skipping "etc"/"Archive" paths, same as ensureZipsExtracted) and
// extracts each into a sibling folder named after it - Foo.exe -> Foo/ - via
// the bundled 7z.exe, the same skip-if-already-extracted convention as
// ensureZipsExtracted. A no-op if SevenZipPath isn't set.
func ensureRarSfxExtracted(root string) {
	if SevenZipPath == "" {
		return
	}
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
		if !strings.EqualFold(filepath.Ext(path), ".exe") {
			return nil
		}

		destDir := strings.TrimSuffix(path, filepath.Ext(path))
		if info, statErr := os.Stat(destDir); statErr == nil && info.IsDir() {
			return nil // already extracted
		}
		if !isSelfExtractingRar(path) {
			return nil // an ordinary .exe, not an archive - leave it alone
		}

		if err := extractRarSfx(path, destDir); err != nil {
			// Best-effort, same reasoning as ensureZipsExtracted: one bad
			// archive shouldn't stop the catalog scan from picking up
			// everything else, and cleaning up partial output means a later
			// run retries instead of mistaking it for a complete extraction.
			os.RemoveAll(destDir)
		}
		return nil
	})
}

// extractRarSfx shells out to the bundled 7z.exe - relied on for its own
// path-traversal protection during extraction, the same trust already placed
// in msiexec/expand/powershell elsewhere in this codebase.
func extractRarSfx(archivePath, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	cmd := exec.Command(SevenZipPath, "x", archivePath, "-o"+destDir, "-y")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("7z extraction of %s failed: %w: %s", archivePath, err, out)
	}
	return nil
}
