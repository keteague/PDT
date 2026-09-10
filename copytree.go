package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// CopyProgress is copyTreeMerge's onProgress payload - both a file count and
// a byte count, since a file count alone is a poor progress/ETA signal when
// (as a real Drivers folder always is) file sizes vary wildly: a run of
// thousands of tiny .cat/.dll files can fly by and then stall for minutes on
// one large driver installer, which a file-count-based percentage would
// completely misrepresent. Bytes are the honest signal; the file counts are
// kept alongside purely for the "(done/total files)" label text.
type CopyProgress struct {
	DoneFiles  int
	TotalFiles int
	DoneBytes  int64
	TotalBytes int64
}

// copyTreeWorkers bounds how many files copyTreeMerge copies concurrently.
// USB flash media is latency-bound as much as bandwidth-bound - per-file
// open/write/close overhead dominates for the many-small-files case a real
// Drivers folder always is - so a modest amount of concurrency overlaps that
// latency instead of paying it serially. Deliberately conservative rather
// than scaling with CPU count: this is an I/O-bound workload against a
// single external device, not a compute-bound one, and too much concurrency
// risks thrashing a cheap USB controller's own command queue rather than
// helping - 4 is a reasonable, safely-conservative starting point pending
// real measurement against actual USB hardware.
const copyTreeWorkers = 4

// copyBufferSize is copyFile's read/write buffer, well above io.Copy's own
// default (32KB) to cut the number of read/write syscalls per file - most
// impactful for the larger driver installer files a real Drivers folder
// also contains alongside its many small ones.
const copyBufferSize = 1024 * 1024

// copyJob is one file copyTreeMerge's worker pool needs to (maybe) copy.
type copyJob struct {
	rel  string // slash-separated, relative to destDir/srcDir
	path string // the real file to read - a resolved symlink target, or srcDir+rel directly
	size int64
}

// collectCopyJobs recursively walks srcAbs (destDir-relative path rel so
// far), creating each subdirectory at the destination and appending one
// copyJob per regular file found under it - including, in place of any
// symlink or Windows junction it encounters, the file(s) under that link's
// *resolved* target. A plain filepath.WalkDir does not follow such a link
// (Go reports it as a non-directory entry regardless of what it points to),
// so without this, a directory symlink under srcDir - confirmed live on a
// real Drivers folder aliasing several macOS version folders to one real
// shared driver folder - was treated as a single small file, and copying
// "it" opened the link's own path and tried to read file bytes from what is
// actually a directory, which succeeds in creating the destination file
// (truncating it to empty first) before failing on the actual read -
// leaving a broken, silently-empty 0-byte file at the destination in place
// of the real content the link pointed to. exFAT (what a flash drive is
// always formatted as) cannot represent a symlink/junction at all, so
// there's no way to preserve the link itself across that copy - copying the
// resolved content in its place, even though that duplicates real bytes
// across every alias of the same target, is the only way to work around
// that and have the actual driver content survive the trip at all.
//
// ancestors is NOT a "seen this target already, skip it" set - the same
// real target reached through two unrelated links legitimately needs its
// own duplicated copy at each place it's linked from. It only ever holds
// the chain of resolved directory paths between srcAbs's own root and the
// current recursion depth, guarding against a link that resolves to one of
// its own ancestors (a genuine cycle, which would otherwise recurse
// forever) - entries are added before recursing into a resolved link
// target and removed again once that recursion returns.
func collectCopyJobs(destDir, srcAbs, rel string, ancestors map[string]bool, jobs *[]copyJob, totalBytes *int64, errs *[]error) {
	entries, err := os.ReadDir(srcAbs)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s: %w", srcAbs, err))
		return
	}
	for _, entry := range entries {
		childAbs := filepath.Join(srcAbs, entry.Name())
		childRel := entry.Name()
		if rel != "" {
			childRel = rel + "/" + entry.Name()
		}

		if entry.Type()&fs.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(childAbs)
			if err != nil {
				*errs = append(*errs, fmt.Errorf("%s: %w", childAbs, err))
				continue
			}
			info, err := os.Stat(resolved)
			if err != nil {
				*errs = append(*errs, fmt.Errorf("%s: %w", childAbs, err))
				continue
			}
			if !info.IsDir() {
				*jobs = append(*jobs, copyJob{rel: childRel, path: resolved, size: info.Size()})
				*totalBytes += info.Size()
				continue
			}
			if ancestors[resolved] {
				*errs = append(*errs, fmt.Errorf("%s: symlink cycle (resolves to its own ancestor %s), skipping", childAbs, resolved))
				continue
			}
			if err := os.MkdirAll(filepath.Join(destDir, filepath.FromSlash(childRel)), 0o755); err != nil {
				*errs = append(*errs, fmt.Errorf("%s: %w", childAbs, err))
				continue
			}
			ancestors[resolved] = true
			collectCopyJobs(destDir, resolved, childRel, ancestors, jobs, totalBytes, errs)
			delete(ancestors, resolved)
			continue
		}

		if entry.IsDir() {
			if err := os.MkdirAll(filepath.Join(destDir, filepath.FromSlash(childRel)), 0o755); err != nil {
				*errs = append(*errs, fmt.Errorf("%s: %w", childAbs, err))
				continue
			}
			collectCopyJobs(destDir, childAbs, childRel, ancestors, jobs, totalBytes, errs)
			continue
		}

		info, err := entry.Info()
		if err != nil {
			*errs = append(*errs, fmt.Errorf("%s: %w", childAbs, err))
			continue
		}
		*jobs = append(*jobs, copyJob{rel: childRel, path: childAbs, size: info.Size()})
		*totalBytes += info.Size()
	}
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
// Actual file copies run on copyTreeWorkers concurrent goroutines - measured
// live as a real speedup for a fresh write to an empty (newly formatted)
// flash drive, where every file must be copied and the destination-size
// skip check below never has anything to skip. Directory creation stays a
// single sequential pass first (cheap - far fewer directories than files -
// and avoids any concurrent-MkdirAll subtlety entirely) before the file
// copies are handed to the worker pool.
//
// onProgress, if non-nil, is called after every file (copied or skipped as
// unchanged) with how many files/bytes of the total under srcDir have been
// processed so far - confirmed live this is worth having at all: a real
// Drivers folder in the tens of thousands of files took several minutes
// over a real USB port with zero indication it hadn't just hung. Called
// serially (never concurrently) regardless of copyTreeWorkers, so callers
// don't need their own synchronization.
//
// A symlink or Windows junction under srcDir (confirmed live: a real
// Drivers folder can have one, e.g. aliasing several macOS version folders
// to one real shared driver folder to avoid keeping duplicate copies
// locally) is followed and its resolved target copied as real content,
// rather than preserved as a link - exFAT (what a flash drive is formatted
// as) has no symlink/junction support at all, so writing one across used to
// silently produce a truncated, empty 0-byte file at the destination
// instead of any of the real content it pointed to. See collectCopyJobs.
func copyTreeMerge(destDir, srcDir string, onProgress func(CopyProgress)) error {
	existing := listFileSizes(destDir)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	var jobs []copyJob
	var totalBytes int64
	var errs []error
	ancestors := map[string]bool{}
	if resolvedRoot, err := filepath.EvalSymlinks(srcDir); err == nil {
		ancestors[resolvedRoot] = true
	}
	collectCopyJobs(destDir, srcDir, "", ancestors, &jobs, &totalBytes, &errs)
	totalFiles := len(jobs)

	var (
		mu        sync.Mutex
		doneFiles int
		doneBytes int64
	)
	jobCh := make(chan copyJob)
	var wg sync.WaitGroup
	for i := 0; i < copyTreeWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, copyBufferSize)
			for j := range jobCh {
				target := filepath.Join(destDir, filepath.FromSlash(j.rel))
				var copyErr error
				if sz, ok := existing[j.rel]; !ok || sz != j.size {
					copyErr = copyFile(target, j.path, buf)
				}
				mu.Lock()
				if copyErr != nil {
					errs = append(errs, fmt.Errorf("%s: %w", j.path, copyErr))
				}
				doneFiles++
				doneBytes += j.size
				if onProgress != nil {
					onProgress(CopyProgress{DoneFiles: doneFiles, TotalFiles: totalFiles, DoneBytes: doneBytes, TotalBytes: totalBytes})
				}
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)
	wg.Wait()

	return errors.Join(errs...)
}

// copyFile copies srcPath to destPath using buf as the read/write buffer
// (see copyBufferSize), creating destPath's parent directory if needed and
// overwriting whatever's already at destPath.
func copyFile(destPath, srcPath string, buf []byte) error {
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
	_, err = io.CopyBuffer(dst, src, buf)
	return err
}
