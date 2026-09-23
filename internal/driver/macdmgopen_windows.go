package driver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// openDmg is macmount.go's own openDmg seam on Windows: there is no
// hdiutil/live mount available at all here, so a real macOS driver .dmg
// (HFS+/APFS UDIF image) is instead extracted, via the already-bundled
// 7z.exe (SevenZipPath - see sevenzip_windows.go's own ensureSevenZipExtracted,
// same var sfx.go/kyoceraexe.go already gate on), into a throwaway temp
// directory that stands in for a live mount point - every real caller
// (findFirstByExt/collectByExt, macmount.go) is a pure filesystem walk that
// never cares whether root is a real mount or an extracted copy, so this
// needs the exact same (root string, cleanup func() error, err error)
// signature darwin's own openDmg (macdmgopen_darwin.go) has, and no other
// code in this package needs to change at all.
//
// Two real container shapes, confirmed live (GitHub issue #3 Phase 1,
// 2026-09-18) against real vendor downloads already present on a real
// technician's own machine - not guessed:
//   - Kyocera's own "Web Build" .dmg is a multi-partition Apple Partition Map
//     image. A single `7z x` only extracts the raw partition blobs
//     (0.ddm/1.Apple_partition_map/2.Apple_UDF/3.hfs/4.Apple_UDF) - the real
//     files only appear after a SECOND `7z x` pass against the extracted
//     *.hfs blob specifically. Confirmed live that a single-pass wildcard
//     attempt (`7z x file.dmg -o<dir> "-ir!*.pkg"`) does not dig through this
//     layer on its own ("No files to process").
//   - Lexmark's, Ricoh's, and Canon's own .dmg files (both the outer,
//     zip-wrapped one and the nested one it reveals) are single-partition
//     images 7-Zip auto-flattens straight through in one pass - the real
//     .pkg/.dmg shows up directly.
//
// openDmg handles both by trying a plain extraction first, then falling back
// to a second pass against any *.hfs/*.apfs blob the first pass produced if
// nothing useful was found directly - see openDmgExtracted's own doc
// comment for the exact two-step algorithm.
//
// Known, accepted gap carried straight from GitHub issue #3's own feasibility
// research: some newer/cryptex-style APFS .dmg variants may not extract via
// 7-Zip at all. openDmg just returns the error in that case -
// indexFamilyPackage's own existing best-effort/skip-what-fails handling
// (already relied on for a real mount failure on darwin) absorbs it with no
// special-casing needed here.
func openDmg(path string) (root string, cleanup func() error, err error) {
	if SevenZipPath == "" {
		return "", nil, fmt.Errorf("7-Zip isn't available to open %s", path)
	}

	extractPath := path
	tmpCleanup := func() {}
	if strings.HasSuffix(strings.ToLower(path), ".dmg.gz") {
		decompressed, dcleanup, derr := decompressGzipToTemp(path)
		if derr != nil {
			return "", nil, fmt.Errorf("decompressing %s: %w", path, derr)
		}
		extractPath = decompressed
		tmpCleanup = dcleanup
	}

	root, cleanupDir, err := openDmgExtracted(extractPath)
	if err != nil {
		tmpCleanup()
		return "", nil, fmt.Errorf("opening %s: %w", path, err)
	}
	return root, func() error {
		err := cleanupDir()
		tmpCleanup()
		return err
	}, nil
}

// openDmgExtracted implements the real two-shape algorithm openDmg's own doc
// comment describes, against an already-decompressed .dmg on disk.
func openDmgExtracted(dmgPath string) (root string, cleanup func() error, err error) {
	tmpDir, err := os.MkdirTemp("", "pdt-mac-dmg-*")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() error { return os.RemoveAll(tmpDir) }

	if err := sevenZipExtract(dmgPath, tmpDir); err != nil {
		cleanup()
		return "", nil, err
	}

	if dmgOrPkgFoundUnder(tmpDir) {
		return tmpDir, cleanup, nil
	}

	// First pass produced no .pkg/.dmg directly. Either the multi-partition
	// shape (Kyocera - the extraction produced only raw partition blobs, no
	// real files at all) needing a second pass into its own *.hfs/*.apfs
	// blob, or a package shape with no installer whatsoever (confirmed live
	// against Canon's own "PPD" bucket: a plain folder-per-model tree of
	// *.PPD.gz files, no .pkg/.dmg/.hfs/.apfs anywhere - see
	// LocateLoosePPDs's own doc comment) where the real content is already
	// sitting in tmpDir and this function's job is already done. Without the
	// hasAnyFile fallback below, every PPD-only package hit this branch,
	// found no partition blob to dig into, and was wrongly reported as a
	// hard failure even though 7z had already extracted the real files -
	// confirmed live as the cause of 100% of Canon's PPD-bucket packages
	// landing in catalog.<mfg>.json's own FailedPackages on every Windows
	// build (darwin's hdiutil-based openDmg has no such gate at all, so the
	// identical package always succeeded there).
	hfsBlob := findFirstByExt(tmpDir, ".hfs")
	if hfsBlob == "" {
		hfsBlob = findFirstByExt(tmpDir, ".apfs")
	}
	if hfsBlob == "" {
		if hasAnyFile(tmpDir) {
			return tmpDir, cleanup, nil
		}
		cleanup()
		return "", nil, fmt.Errorf("no .pkg/.dmg found in %s, and no .hfs/.apfs partition to look inside", dmgPath)
	}

	innerDir, err := os.MkdirTemp("", "pdt-mac-dmg-hfs-*")
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if err := sevenZipExtract(hfsBlob, innerDir); err != nil {
		os.RemoveAll(innerDir)
		cleanup()
		return "", nil, err
	}
	outerCleanup := cleanup
	cleanup = func() error {
		err := os.RemoveAll(innerDir)
		outerCleanup()
		return err
	}
	if !dmgOrPkgFoundUnder(innerDir) {
		cleanup()
		return "", nil, fmt.Errorf("no .pkg/.dmg found inside %s's own %s partition", dmgPath, hfsBlob)
	}
	return innerDir, cleanup, nil
}

// hasAnyFile reports whether root contains at least one real file anywhere
// in its tree - openDmgExtracted's own fallback check for a first-pass
// extraction that found neither a .pkg/.dmg nor an .hfs/.apfs partition blob
// to dig into, but still produced real content of some other shape (a
// PPD-only image's own plain *.PPD.gz files - see LocateLoosePPDs) rather
// than nothing at all.
func hasAnyFile(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if !d.IsDir() {
			found = true
		}
		return nil
	})
	return found
}

// dmgOrPkgFoundUnder reports whether root already contains a real .pkg or
// .dmg - openDmgExtracted's own check for whether a given 7z extraction pass
// already reached real content, or needs the second, HFS-partition-specific
// pass.
func dmgOrPkgFoundUnder(root string) bool {
	return findFirstByExt(root, ".pkg") != "" || findFirstByExt(root, ".dmg") != ""
}

// sevenZipExtract runs the bundled 7z.exe's own whole-archive extraction
// (`x`, not selective - a real vendor .dmg's own top-level contents are
// small, unlike the cpio Payload case selective extraction elsewhere in this
// package is justified for) against archivePath into destDir.
func sevenZipExtract(archivePath, destDir string) error {
	cmd := exec.Command(SevenZipPath, "x", archivePath, "-o"+destDir, "-y")
	hideConsoleWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extracting %s: %w: %s", archivePath, err, strings.TrimSpace(string(out)))
	}
	return nil
}
