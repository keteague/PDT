package driver

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// flattenRedundantWrapperDir collapses destDir/onlyEntry -> destDir whenever
// an extraction's own content turns out to be a single folder, *named after
// destDir itself*, wrapping everything else - rather than leaving that
// redundant nesting in place next to the sibling folder
// ensureZipsExtracted/ensureSfxArchivesExtracted already created. Confirmed
// necessary against a real Konica Minolta package (KM_UPD_pcl6_win64_...
// inst.exe) whose own internal archive already nests everything under a
// folder named after the package itself - without this, extracting it into
// the usual Foo.exe -> Foo/ sibling folder produced Foo/Foo/... instead of
// the expected Foo/....
//
// Matching by name (not just "destDir's only entry happens to be a folder")
// is deliberate: a driver package's real, intentional content is very often
// itself a single top-level folder (a "Driver" subfolder is common), and
// blindly hoisting that up too would just move the problem rather than fix
// it - confirmed by a real regression caught in this package's own test
// suite before landing on the name-match condition. Requiring the nested
// folder's name to match destDir's own name is exactly the signal that
// distinguishes "this zip/exe was itself created from a folder that already
// had the archive's own name" (the redundant case) from "this package's
// real layout happens to start with one folder" (not redundant at all).
//
// Ignores a stray ".DS_Store" sitting alongside the real single folder,
// rather than requiring literally the only entry - confirmed necessary
// against a real Konica Minolta zip whose own top level has both the real
// wrapper folder *and* a macOS Finder-authored ".DS_Store" file, which
// otherwise defeated the "exactly one entry" check entirely, leaving a
// redundant Foo/Foo/... nesting in place unflattened.
func flattenRedundantWrapperDir(destDir string) {
	entries, err := os.ReadDir(destDir)
	if err != nil {
		return
	}
	var onlyDir fs.DirEntry
	for _, e := range entries {
		if e.Name() == ".DS_Store" {
			continue
		}
		if onlyDir != nil || !e.IsDir() {
			return // more than one real entry, or a non-directory - not the redundant-wrapper shape
		}
		onlyDir = e
	}
	if onlyDir == nil {
		return
	}
	if !strings.EqualFold(onlyDir.Name(), filepath.Base(destDir)) {
		return
	}

	nested := filepath.Join(destDir, onlyDir.Name())
	tmp := destDir + ".pdt-flatten-tmp"
	if err := os.Rename(nested, tmp); err != nil {
		return
	}
	// RemoveAll, not Remove: destDir may still hold a stray ".DS_Store" at
	// this point (only the real wrapper folder was just moved out above) -
	// a plain Remove requires an empty directory and would fail here,
	// silently leaving the redundant nesting in place (confirmed live as a
	// real bug against a real Konica Minolta zip's own stray ".DS_Store").
	if err := os.RemoveAll(destDir); err != nil {
		_ = os.Rename(tmp, nested) // best-effort undo, leave destDir as it was
		return
	}
	_ = os.Rename(tmp, destDir)
}
