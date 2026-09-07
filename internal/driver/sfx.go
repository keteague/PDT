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
// value ("") skips self-extracting-archive auto-extraction entirely - what
// happens during `go test` and for any build that doesn't wire this up -
// rather than failing the catalog scan over it.
//
// Bundled specifically because self-extracting RAR archives (Lexmark's own
// driver package ships this way) have no good alternative: Go's standard
// library has no RAR reader at all, and the one pure-Go option evaluated
// (nwaples/rardecode) was found to silently corrupt exactly the .msi files
// this needs - confirmed by successfully staging the corrupted result and
// then having Windows' own msiexec reject it as an invalid package. See the
// README's "Lexmark" section for the full story. Once bundled anyway, the
// same 7z.exe also handles self-extracting 7z and Zip packages (Konica
// Minolta's own driver ships as a 7z SFX) for free - see archiveSignatures.
var SevenZipPath string

// archiveSignatures: byte sequences marking the start of real archive data
// inside a self-extracting .exe - the native PE stub that shows the
// "Extracting..." UI precedes this by anywhere up to a few hundred KB in
// packages seen so far (confirmed against real Lexmark - RAR - and Konica
// Minolta - 7z - packages), so isSelfExtractingArchive scans well past the
// first few bytes rather than checking only the file's start. 7z's own `x`
// command auto-detects the exact format from these same bytes, so nothing
// downstream needs to know which one matched.
var archiveSignatures = [][]byte{
	{'R', 'a', 'r', '!', 0x1A, 0x07, 0x01, 0x00}, // RAR5
	{'R', 'a', 'r', '!', 0x1A, 0x07, 0x00},       // RAR 1.5-4.x
	{0x37, 0x7A, 0xBC, 0xAF, 0x27, 0x1C},         // 7z
	{'P', 'K', 0x03, 0x04},                       // Zip local file header
}

// archiveSignatureScanLimit bounds how much of a candidate .exe gets read
// looking for an embedded archive signature - confirmed against real
// self-extracting packages that the signature sits a few hundred KB in, so
// this is generous headroom without reading an entire (100+MB) installer
// just to rule out an ordinary, unrelated .exe.
const archiveSignatureScanLimit = 8 << 20 // 8 MiB

// isSelfExtractingArchive reports whether path contains a known archive
// signature (see archiveSignatures) within the first archiveSignatureScanLimit
// bytes - true for a self-extracting RAR/7z/Zip archive, false for an
// ordinary executable (the common case: most .exe files encountered while
// scanning a Drivers folder are legitimate installer tools, not archives,
// and must be left alone rather than fed to 7z.exe).
func isSelfExtractingArchive(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, archiveSignatureScanLimit)
	n, _ := io.ReadFull(f, buf)
	buf = buf[:n]

	for _, sig := range archiveSignatures {
		if bytes.Contains(buf, sig) {
			return true
		}
	}
	return false
}

// ensureSfxArchivesExtracted finds every self-extracting archive .exe
// directly under root (skipping "etc"/"Archive" paths, same as
// ensureZipsExtracted) and extracts each into a sibling folder named after it
// - Foo.exe -> Foo/ - via the bundled 7z.exe, the same skip-if-already-
// extracted convention as ensureZipsExtracted (flattenRedundantWrapperDir
// then collapses that back down to just Foo/ if the archive's own content
// was already a single top-level folder, rather than leaving Foo/Foo/... -
// confirmed necessary against a real Konica Minolta package packaged exactly
// that way). This is what actually answers "any manufacturer folder with an
// archive file but no matching extracted folder should be extracted" for the
// common self-extracting formats (RAR, 7z, Zip); Kyocera's own packaging
// (kyoceraexe.go) is the one format seen so far that this can't catch, since
// its embedded archive sits inside the exe's .text PE section rather than
// simply appended after the PE stub the way these do - hence its own bespoke
// two-stage extraction. Kyocera-named exes are skipped here entirely (see
// kyoceraExeNameRe below) rather than merely left to fall through: a Kyocera
// package's raw bytes do contain a real archive signature within the scan
// window (the .text section IS the archive, just not appended cleanly the
// way this function assumes), so isSelfExtractingArchive would otherwise
// return true and this function would extract it - wrongly - into a
// same-named sibling folder containing nothing but raw PE sections
// (.text/.rsrc/.reloc/CERTIFICATE, not a single real driver file). Worse,
// that wrong folder's name then satisfies kyoceraVersionAlreadyExtracted's
// own substring check, permanently blocking the correct two-stage extraction
// from ever running for that version - confirmed live as a real regression
// the first time this generalized beyond RAR/zip. A no-op if SevenZipPath
// isn't set.
func ensureSfxArchivesExtracted(root string) {
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
		if kyoceraExeNameRe.MatchString(d.Name()) {
			return nil // kyoceraexe.go's own two-stage extraction owns this one
		}

		destDir := strings.TrimSuffix(path, filepath.Ext(path))
		if info, statErr := os.Stat(destDir); statErr == nil && info.IsDir() {
			return nil // already extracted
		}
		if !isSelfExtractingArchive(path) {
			return nil // an ordinary .exe, not an archive - leave it alone
		}

		if err := extractSfxArchive(path, destDir); err != nil {
			// Best-effort, same reasoning as ensureZipsExtracted: one bad
			// archive shouldn't stop the catalog scan from picking up
			// everything else, and cleaning up partial output means a later
			// run retries instead of mistaking it for a complete extraction.
			os.RemoveAll(destDir)
			return nil
		}
		flattenRedundantWrapperDir(destDir)
		return nil
	})
}

// extractSfxArchive shells out to the bundled 7z.exe - relied on for its own
// path-traversal protection during extraction, the same trust already placed
// in msiexec/expand/powershell elsewhere in this codebase. 7z auto-detects
// the real archive format (RAR, 7z, Zip, ...) itself; the caller only needs
// to know archivePath contains *some* known signature.
func extractSfxArchive(archivePath, destDir string) error {
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
