package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"PDT/internal/cloudsync"
)

// Events the Write/Sync-to-flash-drive progress dialog listens for so it can
// list the files being transferred like the Cloud Sync dialog does (Ken,
// 2026-09-20), for both a local copy (see newFlashCopyProgressFunc) and a
// download from the cloud: one plan event (everything about to be
// transferred, in order) at the start of each step, followed by per-file
// progress events. The aggregate bar and speed meter come from the ordinary
// flashCopyProgressEvent.
// flashCloudMinConcurrency is the floor for parallel downloads when Drivers
// come from the cloud (see syncCloudDriversToDrive).
const flashCloudMinConcurrency = 12

const (
	flashFilePlanEvent = "flashcopy-plan"
	flashFileEvent     = "flashcopy-files"
)

// FlashFilePlan is flashFilePlanEvent's payload.
type FlashFilePlan struct {
	Letter string   `json:"letter"`
	Step   string   `json:"step"`
	Files  []string `json:"files"`
}

// FlashFileProgress is one entry of flashFileEvent's payload (an array,
// batched - see flashEmitter): one file's own transfer.
type FlashFileProgress struct {
	Letter     string `json:"letter"`
	RelPath    string `json:"relPath"`
	Done       int64  `json:"done"`
	Total      int64  `json:"total"`
	EtaSeconds int    `json:"etaSeconds"`
}

// Driver source values for the sync dialog's Drivers combo box (Ken,
// 2026-09-20): "local" copies this laptop's own Drivers folder onto the
// drive (what Sync always did); "cloud" downloads the shared cloud
// repository straight onto the drive instead, never touching this laptop's
// own Drivers folder - useful when the laptop's copy is stale or partial but
// the team's bucket is current.
const (
	driversSourceLocal = "local"
	driversSourceCloud = "cloud"
)

// cloudDownloadJobs picks out, from a BuildPlan result computed against a
// flash drive's own Drivers folder, everything that needs downloading -
// files the bucket has and the drive doesn't - plus how many total bytes
// that is and how many paths were left alone as conflicts (present on both
// sides with different sizes). Conflicts are never overwritten, matching the
// regular Cloud Sync's own rule (see cloudsync.ActionConflict): silently
// preferring either side risks destroying real content, so they're reported
// for a human instead. Uploads (files only the drive has) are ignored
// outright - this direction only ever pulls from the cloud.
func cloudDownloadJobs(plan []cloudsync.PlanItem) (jobs []cloudsync.PlanItem, totalBytes int64, conflicts int) {
	for _, item := range plan {
		switch item.Action {
		case cloudsync.ActionDownload:
			jobs = append(jobs, item)
			totalBytes += item.RemoteSize
		case cloudsync.ActionConflict:
			conflicts++
		}
	}
	return jobs, totalBytes, conflicts
}

// syncCloudDriversToDrive downloads whatever the shared bucket has that
// letter's Drivers folder lacks, straight onto the drive, then runs the same
// postSyncDriversHook a local sync ends with (scaffold + extract anything
// newly arrived) so the drive is immediately usable. progress is fed
// aggregate files/bytes across the whole download - the flash copy dialog's
// own per-drive bar - and ctx is the flash-sync cancel context, so the
// dialog's Cancel button stops it.
func (a *App) syncCloudDriversToDrive(ctx context.Context, letter string, progress func(CopyProgress)) (conflicts int, err error) {
	cfg, err := a.cloudSyncConfig()
	if err != nil {
		return 0, err
	}
	core, err := cloudsync.NewCore(cfg)
	if err != nil {
		return 0, err
	}
	prefix := a.settings.CloudSync.Prefix
	dest := filepath.Join(letter, "Drivers")

	plan, err := cloudsync.BuildPlan(ctx, core, cfg.Bucket, prefix, dest)
	if err != nil {
		return 0, fmt.Errorf("comparing the cloud repository with %s: %w", dest, err)
	}
	jobs, totalBytes, conflicts := cloudDownloadJobs(plan)
	planFiles := make([]string, len(jobs))
	for i, item := range jobs {
		planFiles[i] = item.RelPath
	}

	// A Drivers repo is dominated by thousands of tiny files (.inf caches,
	// catalogs), where each object costs a full request round trip - with only
	// Cloud Sync's default 3 workers the link sits mostly idle (Ken saw ~1 hour
	// on gigabit fiber). This flow writes to a local drive rather than sharing
	// the bucket connection with uploads, so it uses at least
	// flashCloudMinConcurrency workers whatever the Settings value is.
	concurrency := a.settings.CloudSync.ConcurrentTransfers
	if concurrency < flashCloudMinConcurrency {
		concurrency = flashCloudMinConcurrency
	}

	var (
		mu         sync.Mutex
		perFile    = map[string]int64{} // bytes so far of each in-flight file
		doneBytes  int64
		doneFiles  int
		errs       []error
		gate       = &cloudsync.PauseGate{}
		sem        = make(chan struct{}, concurrency)
		wg         sync.WaitGroup
		totalFiles = len(jobs)
	)
	// emit reports progress (the caller holds mu, so updates reach progress
	// in order; progress itself only records state - see flashEmitter - and
	// never waits on the UI).
	emit := func(file string, fileDone, fileTotal int64) {
		if progress == nil {
			return
		}
		progress(CopyProgress{DoneFiles: doneFiles, TotalFiles: totalFiles, DoneBytes: doneBytes, TotalBytes: totalBytes,
			File: file, FileDone: fileDone, FileTotal: fileTotal})
	}
	if progress != nil {
		progress(CopyProgress{TotalFiles: totalFiles, TotalBytes: totalBytes, Plan: planFiles})
	}

feed:
	for _, item := range jobs {
		select {
		case <-ctx.Done():
			break feed
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(item cloudsync.PlanItem) {
			defer wg.Done()
			defer func() { <-sem }()

			localPath := filepath.Join(dest, filepath.FromSlash(item.RelPath))
			onProgress := func(done, total int64) {
				mu.Lock()
				doneBytes += done - perFile[item.RelPath]
				perFile[item.RelPath] = done
				emit(item.RelPath, done, item.RemoteSize)
				mu.Unlock()
			}
			dlErr := cloudsync.Download(ctx, gate, core, cfg.Bucket, prefix, item.RelPath, localPath, item.RemoteSize, onProgress)

			mu.Lock()
			if dlErr != nil {
				errs = append(errs, fmt.Errorf("%s: %w", item.RelPath, dlErr))
			} else {
				doneBytes += item.RemoteSize - perFile[item.RelPath]
				doneFiles++
			}
			delete(perFile, item.RelPath)
			// Always tells the dialog this file is finished (it also leaves
			// the in-transfer list on failure), even when the download never
			// reported a final byte count (empty files).
			emit(item.RelPath, item.RemoteSize, item.RemoteSize)
			mu.Unlock()
		}(item)
	}
	wg.Wait()

	if ctx.Err() != nil {
		return conflicts, ctx.Err()
	}
	if hookErr := postSyncDriversHook(dest); hookErr != nil {
		errs = append(errs, hookErr)
	}
	// One bad object shouldn't bury the rest in the message: the first few
	// are enough to act on, and the count says how many more there were.
	const maxShown = 5
	if len(errs) > maxShown {
		errs = append(errs[:maxShown], fmt.Errorf("...and %d more failures", len(errs)-maxShown))
	}
	return conflicts, errors.Join(errs...)
}
