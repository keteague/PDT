package driver

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// resolveMacZipSource resolves path to a real, on-disk .dmg/.pkg ready for
// hdiutil/pkgutil - a pure pass-through (path itself, no-op cleanup) for
// anything that isn't a .zip (every manufacturer whose real download ships
// as a .dmg/.pkg/.dmg.gz directly, unaffected by any of this). For a .zip
// (Canon: a .zip wrapping one .dmg; Konica Minolta: a .zip wrapping a
// further-nested .zip), extracts into a throwaway temp directory instead of
// a permanent sibling folder (GitHub issue #11 - the previous version of
// this file, ensureMacZipsExtracted, extracted eagerly into
// Foo.zip's own sibling Foo/ at every catalog build and never cleaned it
// up, confirmed live as real, repeatedly-regenerated disk bloat: ~2.6G/36
// folders on a real machine). Always call cleanup once done with the
// resolved path, even on a later error - it removes the whole temp
// directory this created, if any.
//
// Repeats extraction passes against the same temp directory until a full
// pass finds nothing new (capped at 5, generous headroom over any real
// nesting depth seen so far) - the exact same multi-pass loop
// ensureMacZipsExtracted's own doc comment explained was necessary against
// a real Konica Minolta download (WW_A4/WW_Letter region subfolders, each
// holding its own inner "<models>.pkg.zip" wrapping the real .pkg - two
// levels of zip nesting, not the one level Canon's own shape needs). A
// single pass doesn't reliably discover a zip only created *by* that same
// pass's own extraction (Go's ReadDir is called once per directory as the
// walk reaches it), leaving the real .pkg unextracted with no error at all.
func resolveMacZipSource(path string) (realPath string, cleanup func(), err error) {
	noop := func() {}
	if !strings.EqualFold(filepath.Ext(path), ".zip") {
		return path, noop, nil
	}

	tmpDir, err := os.MkdirTemp("", "pdt-maczip-*")
	if err != nil {
		return "", noop, err
	}
	cleanup = func() { os.RemoveAll(tmpDir) }

	if err := extractZip(path, tmpDir); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("extracting %s: %w", path, err)
	}
	flattenRedundantWrapperDir(tmpDir)

	for pass := 0; pass < 5; pass++ {
		extractedAny := false
		_ = filepath.WalkDir(tmpDir, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				if strings.EqualFold(d.Name(), "etc") || strings.EqualFold(d.Name(), "Archive") || d.Name() == "__MACOSX" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.EqualFold(filepath.Ext(p), ".zip") {
				return nil
			}
			destDir := strings.TrimSuffix(p, filepath.Ext(p))
			if info, statErr := os.Stat(destDir); statErr == nil && info.IsDir() {
				return nil // already extracted this pass
			}
			if err := extractZip(p, destDir); err != nil {
				// Best-effort, same reasoning as the eager version this
				// replaced: one bad nested .zip shouldn't abort the whole
				// resolution, and cleaning up partial output means a retry
				// (there won't be one within this same call, but the
				// temp dir itself is thrown away by cleanup either way)
				// never mistakes a partial extraction for a complete one.
				os.RemoveAll(destDir)
				return nil
			}
			flattenRedundantWrapperDir(destDir)
			extractedAny = true
			return nil
		})
		if !extractedAny {
			break
		}
	}

	if real := findFirstByExt(tmpDir, ".dmg"); real != "" {
		return real, cleanup, nil
	}
	if real := findFirstByExt(tmpDir, ".pkg"); real != "" {
		return real, cleanup, nil
	}
	cleanup()
	return "", noop, fmt.Errorf("no .dmg or .pkg found inside %s", path)
}
