package driver

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ensureMacZipsExtracted finds every .zip under root (a manufacturer's
// Drivers/macOS/<Manufacturer> folder, skipping "etc"/"Archive"/"__MACOSX"
// paths, same as scanMacPackages' own walk) and extracts each one into a
// sibling folder named after it - Foo.zip -> Foo/ - reusing zip.go's own
// extractZip/flattenRedundantWrapperDir rather than a second implementation
// (that code has no Windows-only dependency at all, just archive/zip from
// the standard library). Confirmed necessary against a real package:
// Canon's own macOS driver downloads ship as a .zip directly wrapping one
// .dmg (no installer of its own inside the zip) - BuildMacCatalog only ever
// looks for .dmg/.pkg files already sitting on disk, so a driver that's
// never been unzipped is otherwise completely invisible to it, exactly the
// same class of gap ensureZipsExtracted closes on the Windows side.
// Extraction is skipped (not retried) once the destination folder exists,
// however it got there, so this is cheap to call on every catalog build.
//
// Repeats the whole walk until a full pass extracts nothing new (capped at
// 5 passes, generous headroom over any real nesting depth seen so far) -
// confirmed necessary against a real Konica Minolta download: its own real
// zip wraps two region subfolders (WW_A4/WW_Letter, worldwide A4 vs. US
// Letter paper-size defaults), each holding its own *inner* zip
// ("<models>.pkg.zip") wrapping the real .pkg - two levels of zip nesting,
// not the one level Canon's own shape needed. A single WalkDir pass doesn't
// reliably discover a zip that's only created *by* that same pass's own
// extraction (Go's ReadDir is called once per directory as the walk
// reaches it, before any later sibling extraction could have populated a
// not-yet-visited one) - silently leaving the real .pkg permanently
// unextracted, and therefore invisible to BuildMacCatalog, with no error at
// all.
func ensureMacZipsExtracted(root string) {
	for pass := 0; pass < 5; pass++ {
		extractedAny := false
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if strings.EqualFold(d.Name(), "etc") || strings.EqualFold(d.Name(), "Archive") || d.Name() == "__MACOSX" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.EqualFold(filepath.Ext(path), ".zip") {
				return nil
			}

			destDir := strings.TrimSuffix(path, filepath.Ext(path))
			if info, statErr := os.Stat(destDir); statErr == nil && info.IsDir() {
				return nil // already extracted
			}

			if err := extractZip(path, destDir); err != nil {
				// Best-effort, same reasoning as ensureZipsExtracted: one bad
				// .zip shouldn't stop the rest of the catalog scan, and cleaning
				// up partial output means a later run retries instead of
				// mistaking a partial extraction for a complete one.
				os.RemoveAll(destDir)
				return nil
			}
			flattenRedundantWrapperDir(destDir)
			extractedAny = true
			return nil
		})
		if !extractedAny {
			return
		}
	}
}
