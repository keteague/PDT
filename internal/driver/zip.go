package driver

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ensureZipsExtracted finds every .zip directly under root (skipping "etc"/
// "Archive" paths, same as the main driver scan) and extracts each one into a
// sibling folder named after it - Foo.zip -> Foo/ - if that folder doesn't
// already exist (flattenRedundantWrapperDir then collapses that back down to
// just Foo/ if the zip's own content was already a single top-level folder,
// rather than leaving Foo/Foo/...). Confirmed necessary against a real
// package (Sharp's UD3 driver ships as a .zip): BuildCatalog only ever looks
// for .inf files already sitting on disk, so a driver that's never been
// extracted is otherwise completely invisible to it. Extraction is skipped
// (not retried) once the destination folder exists, however it got there -
// manually by the user or by an earlier run of this - so this is cheap to
// call on every catalog build.
func ensureZipsExtracted(root string) {
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
		if !strings.EqualFold(filepath.Ext(path), ".zip") {
			return nil
		}

		destDir := strings.TrimSuffix(path, filepath.Ext(path))
		if info, statErr := os.Stat(destDir); statErr == nil && info.IsDir() {
			return nil // already extracted
		}

		if err := extractZip(path, destDir); err != nil {
			// Best-effort: one corrupt/unreadable zip shouldn't stop the
			// catalog scan from picking up everything else. Clean up
			// whatever partial output there might be so a later run - once
			// whatever's wrong with this zip is fixed - retries instead of
			// mistaking a partial extraction for a complete one.
			os.RemoveAll(destDir)
			return nil
		}
		flattenRedundantWrapperDir(destDir)
		return nil
	})
}

// extractZip extracts every entry in zipPath into destDir (created if
// needed), rejecting any entry whose name would resolve outside destDir
// ("zip slip") rather than silently following it.
func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", zipPath, err)
	}
	defer r.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	for _, f := range r.File {
		target := filepath.Join(destDir, f.Name)
		if !isWithinDir(destDir, target) {
			return fmt.Errorf("%s: entry %q would extract outside the destination folder", zipPath, f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extractZipFile(f, target); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFile(f *zip.File, target string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// isWithinDir reports whether target is dir itself or a descendant of it,
// after resolving both to clean absolute-comparable form - guards against a
// zip entry using ".." or an absolute path to write outside destDir.
func isWithinDir(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}
