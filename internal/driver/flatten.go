package driver

import (
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
func flattenRedundantWrapperDir(destDir string) {
	entries, err := os.ReadDir(destDir)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		return
	}
	if !strings.EqualFold(entries[0].Name(), filepath.Base(destDir)) {
		return
	}

	nested := filepath.Join(destDir, entries[0].Name())
	tmp := destDir + ".pdt-flatten-tmp"
	if err := os.Rename(nested, tmp); err != nil {
		return
	}
	if err := os.Remove(destDir); err != nil {
		_ = os.Rename(tmp, nested) // best-effort undo, leave destDir as it was
		return
	}
	_ = os.Rename(tmp, destDir)
}
