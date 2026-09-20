package driver

import (
	"fmt"
	"io"
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
// companion files this way, reachable only after ensureSfxArchivesExtracted has
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
			// A directory whose name plus ".msi" exists as its own sibling
			// file is itself a prior extraction's destination (Foo.msi ->
			// Foo/) - never hunt for more .msi files to extract inside one,
			// even across separate runs. Confirmed against a real package
			// (Canon's DiasSetup.msi administratively installs a verbatim
			// copy of itself one level into its own output, apparently for
			// its own uninstaller's use) that without this guard, every
			// fresh run treated that leftover copy as new, unextracted work
			// and extracted it again - nesting one level deeper on every
			// single run, forever, with no bound. Confirmed live: 12+ levels
			// and 100+MB of pure duplication from exactly this, containing
			// zero .inf files the catalog scan could ever have wanted.
			if info, statErr := os.Stat(path + ".msi"); statErr == nil && !info.IsDir() {
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

// compressedInfRe narrows compressedSiblingRe to just the one compressed
// extension that can ever restore to a real .inf - "*.in_" (optionally
// "64"-suffixed). Confirmed by compressedSiblingRe's own doc comment that
// this mapping isn't predictable from the name alone (".in_" can restore to
// either ".ini" or ".inf" depending on the file's own embedded header,
// which only expand.exe -R actually reads) - so this only narrows *which*
// files are worth the expand.exe call at all, not what each one turns into.
var compressedInfRe = regexp.MustCompile(`(?i)\.in_(64)?$`)

// ensureMsiInfsExtracted is ensureMsiExtracted's .inf-only sibling - GitHub
// issue #10's catalog rework. There's no selective-extract mode via
// msiexec /a the way 7z offers for zip/RAR/7z archives, so getting at the
// .inf at all still means running the same full administrative install -
// but only ever into a throwaway scratch directory (see extractInfsFromMsi):
// the full payload (hundreds of driver files) is never kept, only ever
// transiently unpacked before the resulting .inf file(s) are copied into the
// permanent cache and the scratch dir is discarded. Same
// skip-if-already-cached and Canon-DiasSetup-style self-nesting guard as
// ensureMsiExtracted. Deliberately does NOT skip PdtInfCacheDirName while
// searching for source .msi files - see ensureZipInfsExtracted's own doc
// comment for why: confirmed live as a real necessity for Lexmark's own
// package, an outer self-extracting RAR wrapping this exact .msi, which
// ensureSfxArchiveInfsExtracted reveals *into* PdtInfCacheDirName first.
func ensureMsiInfsExtracted(root string) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.EqualFold(d.Name(), "etc") || strings.EqualFold(d.Name(), "Archive") {
				return filepath.SkipDir
			}
			if info, statErr := os.Stat(path + ".msi"); statErr == nil && !info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".msi") {
			return nil
		}

		destDir := infCacheDestDir(root, path)
		if prepareInfCacheDest(destDir) {
			return nil // already cached (see prepareInfCacheDest)
		}

		if err := extractInfsFromMsi(path, destDir); err != nil {
			os.RemoveAll(destDir)
			return nil
		}
		writeSourceMarker(destDir, path)
		return nil
	})
}

// extractInfsFromMsi runs the same full administrative install extractMsi
// already does, but into a throwaway os.MkdirTemp scratch dir - msiexec /a
// has no selective-extract mode, so getting at the .inf at all means
// unpacking everything first. Every "*.in_"-pattern compressed sibling that
// might turn out to be a real .inf gets expanded (see compressedInfRe's own
// doc comment for why this can't be narrowed further than that by name
// alone) - narrower than expandCompressedSiblings' own full sweep of every
// compressed sibling of every kind, since nothing else in the package is
// being kept here anyway. Whatever ends up named "*.inf" in the scratch dir
// afterward (already real, or just restored by expand.exe) is copied into
// destDir with its own path (relative to the scratch root) preserved -
// scanManufacturerFolders' own arch-token detection needs that same
// structure a full extraction would have produced. The scratch dir itself
// is always removed before returning, regardless of outcome.
func extractInfsFromMsi(msiPath, destDir string) error {
	scratch, err := os.MkdirTemp("", "pdt-msi-inf-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)

	if err := extractMsi(msiPath, scratch); err != nil {
		return err
	}
	return copyInfsFromExtractedTree(scratch, destDir)
}

// copyInfsFromExtractedTree is extractInfsFromMsi's own post-extraction
// half, split out separately so it's testable directly against a synthetic
// "already extracted" directory - without needing a real .msi/msiexec at
// all - the same way extractInfsFromZip/extractInfsFromSfxArchive can be
// tested against a real archive without a real driver package inside it.
// Expands every "*.in_"-pattern compressed sibling that might turn out to
// be a real .inf (see compressedInfRe's own doc comment for why this can't
// be narrowed further than that by name alone) - narrower than
// expandCompressedSiblings' own full sweep of every compressed sibling of
// every kind, since nothing else in the package is being kept here anyway.
// Whatever ends up named "*.inf" in srcDir afterward (already real, or just
// restored by expand.exe) is copied into destDir with its own path
// (relative to srcDir) preserved - scanManufacturerFolders' own arch-token
// detection needs that same structure a full extraction would have
// produced. Returns an error if nothing ending up named ".inf" was found at
// all.
func copyInfsFromExtractedTree(srcDir, destDir string) error {
	_ = filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !compressedInfRe.MatchString(path) {
			return nil
		}
		cmd := exec.Command("expand.exe", "-R", path)
		_ = cmd.Run()
		return nil
	})

	foundAny := false
	_ = filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".inf") {
			return nil
		}
		rel, relErr := filepath.Rel(srcDir, path)
		if relErr != nil {
			return nil
		}
		target := filepath.Join(destDir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil
		}
		if err := copyPlainFile(target, path); err == nil {
			foundAny = true
		}
		return nil
	})
	if !foundAny {
		return fmt.Errorf("no .inf found under %s", srcDir)
	}
	return nil
}

// copyPlainFile copies srcPath to destPath, overwriting whatever's already
// at destPath - no buffering optimization needed, every caller only ever
// copies a small .inf file.
func copyPlainFile(destPath, srcPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}
