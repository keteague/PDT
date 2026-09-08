package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// samePath reports whether a and b name the same location on disk - a
// case-insensitive comparison of their cleaned, absolute forms (Windows
// paths aren't case-sensitive), used to detect a copy that would write a
// directory tree onto itself before ever attempting it. Neither path needs
// to actually exist.
func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(absA), filepath.Clean(absB))
}

// countFiles counts every regular file (not directory) under root - a
// cheap directory-listing pass compared to the actual copy that follows,
// used only to give copyTreeMerge's onProgress callback a known total up
// front. 0 if root doesn't exist or can't be read.
func countFiles(root string) int {
	n := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

// listFileSizes bulk-lists every regular file under root into a single
// relative-path -> size map, in one directory walk - the whole reason
// copyTreeMerge does this instead of an os.Stat call per source file: even
// though a stat is individually cheap, tens of thousands of them each
// costing their own round-trip to a real USB-attached filesystem adds up to
// the dominant cost of a repeat sync, confirmed live once file-count itself
// stopped being the limiting factor. One sequential directory walk here,
// against however many random-access stat calls copyTreeMerge would
// otherwise make, is the actual fix - not a stronger content check (CRC32/
// MD5 hashing was considered and rejected: computing a hash means reading
// every byte of every file on both sides, which costs far more I/O than the
// stat calls it would replace, for a correctness guarantee this specific
// case doesn't need - driver packages are downloaded once and never
// silently modified in place afterward, so a size match is already about as
// good as a hash match here). Empty map (not nil, not an error) if root
// doesn't exist or can't be read.
func listFileSizes(root string) map[string]int64 {
	sizes := map[string]int64{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		sizes[filepath.ToSlash(rel)] = info.Size()
		return nil
	})
	return sizes
}

// copyTreeMerge recursively copies srcDir's content into destDir, creating
// destDir if it doesn't exist. Unlike os.CopyFS - which refuses to overwrite
// any file already present in the destination (its own documentation:
// "CopyFS will not overwrite existing files ... errors.Is(err, fs.ErrExist)
// will be true", "Copying stops at and returns the first error encountered")
// - this merges into an already-populated destination without error,
// confirmed live as a real bug: a flash drive that already had anything
// under Drivers\ (a prior Write to Flash Drive, or manual copying) made
// every subsequent write fail on the very first pre-existing path it
// encountered, silently copying none of the real driver files after it -
// exactly what both a repeat Write to Flash Drive and the toolbar's Sync
// button need to not do. A file already at the destination with the same
// size is left alone rather than re-copied - the common case for a
// re-sync, where most driver packages never change once downloaded - via a
// single bulk listing of the destination up front (see listFileSizes), not
// an os.Stat call per file - so this stays fast even against a Drivers
// folder with gigabytes in it and tens of thousands of files.
//
// Best-effort per file, unlike filepath.WalkDir's own default behavior:
// one file that can't be copied doesn't stop the rest of the tree from
// copying. Confirmed live as a second real bug on top of the one above -
// Lexmark's own driver package (thousands of files, deeply nested) hit an
// error partway through on a real flash drive, and because the original
// version of this function let that error abort the entire WalkDir, every
// manufacturer that sorts after Lexmark (Ricoh, Sharp, Toshiba, Xerox) never
// got copied at all, with nothing in the result to explain why. The
// returned error (nil on full success) joins every per-file failure
// together - non-nil means "at least one file didn't make it", not
// "nothing did".
//
// onProgress, if non-nil, is called after every file (copied or skipped as
// unchanged) with how many of the total files under srcDir have been
// processed so far - confirmed live this is worth having at all: a real
// Drivers folder in the tens of thousands of files took several minutes
// over a real USB port with zero indication it hadn't just hung.
func copyTreeMerge(destDir, srcDir string, onProgress func(done, total int)) error {
	existing := listFileSizes(destDir)

	total := 0
	if onProgress != nil {
		total = countFiles(srcDir)
	}
	done := 0
	var errs []error
	_ = filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			return nil
		}
		target := filepath.Join(destDir, rel)
		if d.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			return nil
		}
		if sz, ok := existing[filepath.ToSlash(rel)]; !ok || sz != info.Size() {
			if err := copyFile(target, path); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
			}
		}
		done++
		if onProgress != nil {
			onProgress(done, total)
		}
		return nil
	})
	return errors.Join(errs...)
}

// copyFile copies srcPath to destPath, creating destPath's parent directory
// if needed and overwriting whatever's already at destPath.
func copyFile(destPath, srcPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
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
