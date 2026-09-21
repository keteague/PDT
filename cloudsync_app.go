package main

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"PDT/internal/cloudsync"
)

// cloudSyncEarlyReleaseFraction is how far into a file's own transfer
// SyncCloud starts the next queued file - Ken's own explicit request ("when
// a file is at 95% transfer completed, begin transfer of the next file"),
// so a lane never sits idle waiting out a file's last few percent (often
// mostly transfer-finalization overhead - a multipart CompleteMultipartUpload
// round trip, a final disk flush/rename - rather than real data movement)
// before starting genuinely new work.
const cloudSyncEarlyReleaseFraction = 0.95

// cloudSyncCancelGracePeriod bounds how long SyncCloud waits, once Cancel
// has been hit, for every in-flight transfer to actually unwind before
// giving up and returning anyway - a real bug reported live: a stuck
// upload left the Cancel button frozen on "Canceling..." forever, and
// Upload/Download's own context-cancellation handling (confirmed correct
// in isolation via internal/cloudsync's own cancel_hang_test.go/
// cancel_hang_multipart_test.go) couldn't be made to reproduce it against a
// synthetic slow server - so rather than trust every future network
// condition to always unblock as promptly as ctx cancellation is supposed
// to guarantee, Cancel gets a hard upper bound here: whatever's still
// running past this keeps running in the background (not reflected in the
// result), but the UI is guaranteed to recover either way.
const cloudSyncCancelGracePeriod = 10 * time.Second

const cloudSyncProgressEvent = "cloudsync-progress"
const cloudSyncTotalProgressEvent = "cloudsync-total-progress"

// CloudSyncProgress is cloudSyncProgressEvent's payload - one event stream
// shared by every in-flight upload/download in a SyncCloud run, told apart
// by RelPath. EtaSeconds is this ONE file's own estimate (0 until known -
// see etaEstimator), not the whole batch's - see CloudSyncTotalProgress for
// that.
type CloudSyncProgress struct {
	RelPath    string `json:"relPath"`
	Direction  string `json:"direction"` // "upload" or "download"
	Done       int64  `json:"done"`
	Total      int64  `json:"total"`
	EtaSeconds int    `json:"etaSeconds"`
}

// CloudSyncTotalProgress is cloudSyncTotalProgressEvent's payload - the
// whole SyncCloud run's own aggregate progress (every selected file summed
// together), for the progress dialog's bottom-of-dialog total bar/ETA/
// transfer-rate meter.
type CloudSyncTotalProgress struct {
	DoneBytes       int64   `json:"doneBytes"`
	TotalBytes      int64   `json:"totalBytes"`
	RateBytesPerSec float64 `json:"rateBytesPerSec"`
	EtaSeconds      int     `json:"etaSeconds"`
}

// CloudSyncPlanItem is one cloudsync.PlanItem plus this technician's own
// persisted selection/new-since-last-view state (see cloudsyncstate.go) -
// what the tree view actually renders.
type CloudSyncPlanItem struct {
	RelPath    string `json:"relPath"`
	Action     string `json:"action"`
	LocalSize  int64  `json:"localSize"`
	RemoteSize int64  `json:"remoteSize"`
	Selected   bool   `json:"selected"`
	IsNew      bool   `json:"isNew"`
}

// CloudSyncPlanResult is GetCloudSyncPlan's own outcome - Error non-empty
// (Items empty) covers both "not configured yet" and a real connection
// failure, since the tree view shows either the same way: a message instead
// of a tree.
type CloudSyncPlanResult struct {
	Items []CloudSyncPlanItem `json:"items"`
	Error string              `json:"error"`
}

// CloudSyncResult is SyncCloud's own outcome - the same
// succeeded/failed-by-key shape as BatchDriveResult (flashdrive.go), so one
// bad file doesn't stop the rest of the batch from completing.
type CloudSyncResult struct {
	Succeeded []string          `json:"succeeded"`
	Failed    map[string]string `json:"failed"`
}

// cloudSyncConfig assembles cloudsync.Config from Settings' own non-secret
// CloudSync fields plus the OS-keychain-stored secret - a clear, specific
// error (surfaced straight to the tree view/log) rather than a confusing
// authentication failure deeper in when any of the four required pieces
// hasn't been filled in yet.
func (a *App) cloudSyncConfig() (cloudsync.Config, error) {
	secret, err := cloudsync.LoadSecretKey()
	if err != nil {
		return cloudsync.Config{}, err
	}
	cs := a.settings.CloudSync
	if cs.Endpoint == "" || cs.Bucket == "" || cs.AccessKeyID == "" || secret == "" {
		return cloudsync.Config{}, errors.New("Cloud Sync isn't set up yet - fill in the R2 Endpoint, Bucket, Access Key ID, and Secret Access Key in Settings first")
	}
	return cloudsync.Config{
		Endpoint:        cs.Endpoint,
		Bucket:          cs.Bucket,
		AccessKeyID:     cs.AccessKeyID,
		SecretAccessKey: secret,
	}, nil
}

// GetCloudSyncPlan lists this technician's own Drivers folder against the
// shared bucket and returns one CloudSyncPlanItem per relative path found on
// either side - the Cloud Sync modal's own tree view is built entirely from
// this, client-side, by splitting each RelPath on "/".
//
// Every item's IsNew reflects whatever this technician has already been
// shown before (see cloudsyncstate.go's own Seen set) - and, since this
// function is what shows them, every RelPath it returns is folded into that
// same Seen set before returning, so the highlight shows exactly once: this
// call, not the next one.
func (a *App) GetCloudSyncPlan() CloudSyncPlanResult {
	<-a.ready
	cfg, err := a.cloudSyncConfig()
	if err != nil {
		return CloudSyncPlanResult{Error: err.Error()}
	}
	core, err := cloudsync.NewCore(cfg)
	if err != nil {
		return CloudSyncPlanResult{Error: err.Error()}
	}

	ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
	defer cancel()
	plan, err := cloudsync.BuildPlan(ctx, core, cfg.Bucket, a.settings.CloudSync.Prefix, driversRoot())
	if err != nil {
		return CloudSyncPlanResult{Error: err.Error()}
	}

	state := loadCloudSyncState()
	items := make([]CloudSyncPlanItem, len(plan))
	for i, p := range plan {
		items[i] = CloudSyncPlanItem{
			RelPath:    p.RelPath,
			Action:     string(p.Action),
			LocalSize:  p.LocalSize,
			RemoteSize: p.RemoteSize,
			Selected:   !state.Deselected[p.RelPath],
			IsNew:      !state.Seen[p.RelPath],
		}
		state.Seen[p.RelPath] = true
	}
	_ = saveCloudSyncState(state)
	return CloudSyncPlanResult{Items: items}
}

// SaveCloudSyncSelection persists exactly which relative paths are
// currently unchecked in the tree view - called whenever the user toggles a
// checkbox, so the selection survives closing and reopening the modal (and
// PDT restarting) rather than resetting to "everything checked" every time.
func (a *App) SaveCloudSyncSelection(deselected []string) error {
	<-a.ready
	state := loadCloudSyncState()
	newDeselected := make(map[string]bool, len(deselected))
	for _, p := range deselected {
		newDeselected[p] = true
	}
	state.Deselected = newDeselected
	return saveCloudSyncState(state)
}

// newCloudSyncProgressFunc returns a progress callback for one file's own
// transfer, throttled to at most once every 150ms - except the transfer's
// own final update (done >= total), always sent so the progress dialog
// never sits on a stale percentage once a file actually finishes. Carries
// its own etaEstimator (reset on the first call), the same per-step ETA
// machinery newFlashCopyProgressFunc already uses for flash-drive Sync -
// see its own doc comment for why a plain since-the-start average doesn't
// work.
func (a *App) newCloudSyncProgressFunc(relPath, direction string) func(done, total int64) {
	var lastEmit time.Time
	var est etaEstimator
	started := false
	return func(done, total int64) {
		now := time.Now()
		if !started {
			started = true
			est.reset(now, done)
		}
		etaSeconds := est.sample(now, done, total)
		final := done >= total
		if !final && now.Sub(lastEmit) < 150*time.Millisecond {
			return
		}
		lastEmit = now
		if final {
			etaSeconds = 0
		}
		runtime.EventsEmit(a.ctx, cloudSyncProgressEvent, CloudSyncProgress{
			RelPath: relPath, Direction: direction, Done: done, Total: total, EtaSeconds: etaSeconds,
		})
	}
}

// SyncCloud uploads/downloads every relPath in relPaths that still actually
// needs it by the time this runs - the plan is rebuilt fresh here rather
// than trusting whatever direction the frontend last saw for each path, so
// a file that became synced (someone else already uploaded it) or a
// conflict (its size changed since the tree view was last shown) in the
// meantime is safely skipped instead of blindly overwritten either way.
//
// Concurrency is a semaphore of Settings' own CloudSync.ConcurrentTransfers
// size, but a slot is released the moment its file reaches
// cloudSyncEarlyReleaseFraction (95%) done, not strictly at completion -
// Ken's own explicit request, so the next queued file starts while the
// outgoing one is still finishing its own last few percent (typically
// mostly finalization overhead: a multipart CompleteMultipartUpload round
// trip, a final rename/Chtimes) rather than a lane sitting idle waiting for
// that to wrap up first. Actual simultaneous transfers can therefore
// transiently run a little over ConcurrentTransfers right at a handoff -
// bounded in practice since only one file can be "in its own last 5%" at a
// time per lane.
func (a *App) SyncCloud(relPaths []string) CloudSyncResult {
	<-a.ready
	result := CloudSyncResult{Succeeded: []string{}, Failed: map[string]string{}}

	cfg, err := a.cloudSyncConfig()
	if err != nil {
		result.Failed["*"] = err.Error()
		return result
	}
	core, err := cloudsync.NewCore(cfg)
	if err != nil {
		result.Failed["*"] = err.Error()
		return result
	}

	ctx, cancel := context.WithCancel(a.ctx)
	gate := &cloudsync.PauseGate{}
	a.cloudSyncMu.Lock()
	a.cloudSyncCancel = cancel
	a.cloudSyncGate = gate
	a.cloudSyncMu.Unlock()
	defer func() {
		a.cloudSyncMu.Lock()
		a.cloudSyncCancel = nil
		a.cloudSyncGate = nil
		a.cloudSyncMu.Unlock()
		cancel()
	}()

	// Run BuildPlan's own remote listing in a goroutine rather than awaiting
	// it directly, so Cancel is bounded here the same way it already is
	// below for the per-file wg.Wait() join: listRemote checks ctx between
	// pages (see its own doc comment), but can't abort a single in-flight
	// page request - minio-go's ListObjectsV2 takes no context at all - so
	// without this, Cancel landing mid-request during the plan rebuild could
	// still freeze the button until that one request happens to return.
	// Whatever's still running past cloudSyncCancelGracePeriod keeps running
	// in the background and its result is simply discarded, matching
	// cloudSyncCancelGracePeriod's own doc comment.
	type planResult struct {
		plan []cloudsync.PlanItem
		err  error
	}
	planCh := make(chan planResult, 1)
	go func() {
		p, err := cloudsync.BuildPlan(ctx, core, cfg.Bucket, a.settings.CloudSync.Prefix, driversRoot())
		planCh <- planResult{p, err}
	}()

	var plan []cloudsync.PlanItem
	select {
	case r := <-planCh:
		plan, err = r.plan, r.err
	case <-ctx.Done():
		select {
		case r := <-planCh:
			plan, err = r.plan, r.err
		case <-time.After(cloudSyncCancelGracePeriod):
			result.Failed["*"] = ctx.Err().Error()
			return result
		}
	}
	if err != nil {
		result.Failed["*"] = err.Error()
		return result
	}
	requested := make(map[string]bool, len(relPaths))
	for _, p := range relPaths {
		requested[p] = true
	}
	var jobs []cloudsync.PlanItem
	var totalBytes int64
	for _, item := range plan {
		if !requested[item.RelPath] {
			continue
		}
		if item.Action != cloudsync.ActionUpload && item.Action != cloudsync.ActionDownload {
			continue
		}
		jobs = append(jobs, item)
		if item.Action == cloudsync.ActionUpload {
			totalBytes += item.LocalSize
		} else {
			totalBytes += item.RemoteSize
		}
	}

	concurrency := a.settings.CloudSync.ConcurrentTransfers
	if concurrency < 1 {
		concurrency = defaultConcurrentTransfers
	}

	var (
		dataMu      sync.Mutex // guards perFileDone and result.Succeeded/Failed
		perFileDone = map[string]int64{}

		totalMu       sync.Mutex // guards totalEst/lastTotalEmit - serializes total-progress emission
		totalEst      etaEstimator
		lastTotalEmit time.Time
	)
	totalEst.reset(time.Now(), 0)

	emitTotalProgress := func() {
		dataMu.Lock()
		var doneBytes int64
		for _, d := range perFileDone {
			doneBytes += d
		}
		dataMu.Unlock()

		totalMu.Lock()
		defer totalMu.Unlock()
		now := time.Now()
		final := totalBytes > 0 && doneBytes >= totalBytes
		if !final && now.Sub(lastTotalEmit) < 150*time.Millisecond {
			return
		}
		lastTotalEmit = now
		etaSeconds := totalEst.sample(now, doneBytes, totalBytes)
		rate := totalEst.rate()
		if final {
			etaSeconds = 0
			rate = 0
		}
		runtime.EventsEmit(a.ctx, cloudSyncTotalProgressEvent, CloudSyncTotalProgress{
			DoneBytes: doneBytes, TotalBytes: totalBytes, RateBytesPerSec: rate, EtaSeconds: etaSeconds,
		})
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
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

			var released int32
			release := func() {
				if atomic.CompareAndSwapInt32(&released, 0, 1) {
					<-sem
				}
			}
			defer release()

			localPath := filepath.Join(driversRoot(), filepath.FromSlash(item.RelPath))
			direction := "upload"
			if item.Action == cloudsync.ActionDownload {
				direction = "download"
			}
			fileProgress := a.newCloudSyncProgressFunc(item.RelPath, direction)
			progress := func(done, total int64) {
				dataMu.Lock()
				perFileDone[item.RelPath] = done
				dataMu.Unlock()
				fileProgress(done, total)
				emitTotalProgress()
				if total > 0 && float64(done)/float64(total) >= cloudSyncEarlyReleaseFraction {
					release()
				}
			}

			var transferErr error
			if item.Action == cloudsync.ActionUpload {
				transferErr = cloudsync.Upload(ctx, gate, core, cfg.Bucket, a.settings.CloudSync.Prefix, item.RelPath, localPath, progress)
			} else {
				transferErr = cloudsync.Download(ctx, gate, core, cfg.Bucket, a.settings.CloudSync.Prefix, item.RelPath, localPath, item.RemoteSize, progress)
			}

			dataMu.Lock()
			if transferErr != nil {
				result.Failed[item.RelPath] = transferErr.Error()
			} else {
				result.Succeeded = append(result.Succeeded, item.RelPath)
			}
			dataMu.Unlock()
		}(item)
	}

	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-ctx.Done():
		select {
		case <-waitDone:
		case <-time.After(cloudSyncCancelGracePeriod):
			// See cloudSyncCancelGracePeriod's own doc comment - whatever's
			// still running past this point keeps running in the
			// background rather than being reflected below, but Cancel
			// must never be able to freeze the whole dialog forever.
		}
	}

	return result
}

// SetCloudSyncPaused pauses or resumes the currently-running SyncCloud call,
// if any (a no-op otherwise) - see cloudsync.PauseGate's own doc comment for
// how this differs from CancelCloudSync.
func (a *App) SetCloudSyncPaused(paused bool) {
	a.cloudSyncMu.Lock()
	gate := a.cloudSyncGate
	a.cloudSyncMu.Unlock()
	if gate != nil {
		gate.SetPaused(paused)
	}
}

// CancelCloudSync cancels the currently-running SyncCloud call, if any (a
// no-op otherwise) - immediately, not "let the current file finish" like
// StopDeploy: the one file each worker is mid-transfer on when Cancel lands
// is aborted. An upload's incomplete multipart upload is aborted on the bucket,
// but a download's partial file is KEPT (Ken, 2026-09-20), exactly like Pause or
// a genuine network failure: the next Sync resumes it from that point with a
// Range request instead of re-downloading (see cloudsync.Download).
func (a *App) CancelCloudSync() {
	a.cloudSyncMu.Lock()
	cancel := a.cloudSyncCancel
	a.cloudSyncMu.Unlock()
	if cancel != nil {
		cancel()
	}
}
