package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"PDT/internal/cloudsync"

	"github.com/minio/minio-go/v7"
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
	// LocalOnly: a removed row this technician chose to keep local only
	// (cloudSyncState.LocalOnly).
	LocalOnly bool `json:"localOnly"`
}

// CloudSyncPlanResult is GetCloudSyncPlan's own outcome - Error non-empty
// (Items empty) covers both "not configured yet" and a real connection
// failure, since the tree view shows either the same way: a message instead
// of a tree.
type CloudSyncPlanResult struct {
	Items []CloudSyncPlanItem `json:"items"`
	Error string              `json:"error"`
	// ReadOnly: the configured key can't write to the bucket (see
	// cloudCanWrite) - Items then holds no upload rows at all.
	ReadOnly bool `json:"readOnly"`
}

// CloudSyncResult is SyncCloud's own outcome - the same
// succeeded/failed-by-key shape as BatchDriveResult (flashdrive.go), so one
// bad file doesn't stop the rest of the batch from completing.
// SkippedUploads counts uploads not attempted (or refused mid-run) because
// the key is read-only - reported once, not as a failure per file.
// Deleted lists local files removed because they'd been removed from the
// cloud (cloudSyncActionRemoved) - kept apart from Succeeded, which is
// transfers.
type CloudSyncResult struct {
	Succeeded      []string          `json:"succeeded"`
	Deleted        []string          `json:"deleted"`
	Failed         map[string]string `json:"failed"`
	ReadOnly       bool              `json:"readOnly"`
	SkippedUploads int               `json:"skippedUploads"`
}

// cloudSyncActionRemoved is the plan action for a local-only file that this
// machine has seen in the cloud before (cloudSyncState.InCloud) - removed
// from the bucket since, so this computer holds the only copy. Shown to
// every technician with a per-file choice instead of a checkbox (Ken,
// 2026-11-03): sync it back to the cloud (write access only), delete the
// local copy, or keep it local only. No choice means nothing happens.
const cloudSyncActionRemoved = "removed"

// The per-file choices SyncCloud accepts for a removed row. "Keep local
// only" isn't one - it's saved the moment it's picked
// (SetCloudSyncLocalOnly) and needs no sync run.
const (
	removedChoiceSync   = "sync"
	removedChoiceDelete = "delete"
)

// recordInCloud adds every path present in the bucket (anything but a
// local-only upload) to state.InCloud and drops entries gone from both
// sides, so the set only ever covers paths still on at least one side.
func recordInCloud(state *cloudSyncState, plan []cloudsync.PlanItem) {
	present := make(map[string]bool, len(plan))
	for _, p := range plan {
		present[p.RelPath] = true
		if p.Action != cloudsync.ActionUpload {
			state.InCloud[p.RelPath] = true
		}
	}
	for rel := range state.InCloud {
		if !present[rel] {
			delete(state.InCloud, rel)
		}
	}
	// A Local Only choice only means something while the file is still a
	// removed one - back in the cloud, or deleted locally, it's dropped.
	removed := map[string]bool{}
	for _, p := range plan {
		if isRemovedFromCloud(state, p) {
			removed[p.RelPath] = true
		}
	}
	for rel := range state.LocalOnly {
		if !removed[rel] {
			delete(state.LocalOnly, rel)
		}
	}
}

// What SyncCloud does with one requested removed-from-cloud file.
const (
	removedOutcomeNone = iota
	removedOutcomeDelete
	removedOutcomeUpload
	removedOutcomeSkippedUpload
)

// decideRemoved maps a removed file's choice to what SyncCloud does. Only an
// explicit choice acts; a file kept local only is never touched, whatever
// was sent; and a read-only key can't upload, so "sync" is skipped.
func decideRemoved(choice string, localOnly, readOnly bool) int {
	switch {
	case localOnly:
		return removedOutcomeNone
	case choice == removedChoiceDelete:
		return removedOutcomeDelete
	case choice == removedChoiceSync && readOnly:
		return removedOutcomeSkippedUpload
	case choice == removedChoiceSync:
		return removedOutcomeUpload
	}
	return removedOutcomeNone
}

// isRemovedFromCloud: a local-only plan item this machine has seen in the
// cloud before.
func isRemovedFromCloud(state *cloudSyncState, p cloudsync.PlanItem) bool {
	return p.Action == cloudsync.ActionUpload && state.InCloud[p.RelPath]
}

// deleteRemovedLocal deletes relPath's local copy under root, refusing any
// path that would resolve outside it.
func deleteRemovedLocal(root, relPath string) error {
	full := filepath.Join(root, filepath.FromSlash(relPath))
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to delete %q: outside the Drivers folder", relPath)
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// cloudWriteAccess caches each key's probed write access for this session,
// keyed by endpoint+bucket+access key, so changing any of them in Settings
// probes again. Only conclusive answers are cached.
var (
	cloudWriteAccessMu sync.Mutex
	cloudWriteAccess   = map[string]bool{}
)

func cloudWriteAccessKey(cfg cloudsync.Config) string {
	return cfg.Endpoint + "|" + cfg.Bucket + "|" + cfg.AccessKeyID
}

// cloudCanWrite reports whether cfg's key may upload, probing the bucket
// once per session (cloudsync.ProbeWriteAccess). Read-only keys - the
// public guest key in the README - would otherwise have every local-only
// file planned as an upload and fail each one with AccessDenied on every
// sync. An inconclusive probe answers true and isn't cached: never hide
// uploads from a technician who can really write.
func (a *App) cloudCanWrite(ctx context.Context, core *minio.Core, cfg cloudsync.Config) bool {
	key := cloudWriteAccessKey(cfg)
	cloudWriteAccessMu.Lock()
	v, ok := cloudWriteAccess[key]
	cloudWriteAccessMu.Unlock()
	if ok {
		return v
	}
	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	canWrite, err := cloudsync.ProbeWriteAccess(pctx, core, cfg.Bucket, a.settings.CloudSync.Prefix)
	if err != nil {
		return true
	}
	setCloudWriteAccess(cfg, canWrite)
	return canWrite
}

func setCloudWriteAccess(cfg cloudsync.Config, canWrite bool) {
	cloudWriteAccessMu.Lock()
	cloudWriteAccess[cloudWriteAccessKey(cfg)] = canWrite
	cloudWriteAccessMu.Unlock()
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
	if !a.IsTechLaptopCopy() {
		return CloudSyncPlanResult{Error: errNotTechLaptop.Error()}
	}
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
	// Probe write access alongside the listing rather than after it, so a
	// first open doesn't pay for the two round trips in sequence.
	canWriteCh := make(chan bool, 1)
	go func() { canWriteCh <- a.cloudCanWrite(ctx, core, cfg) }()
	plan, err := cloudsync.BuildPlan(ctx, core, cfg.Bucket, a.settings.CloudSync.Prefix, driversRoot())
	if err != nil {
		return CloudSyncPlanResult{Error: err.Error()}
	}
	readOnly := !<-canWriteCh

	state := loadCloudSyncState()
	items := planTreeItems(plan, &state, readOnly)
	_ = saveCloudSyncState(state)
	return CloudSyncPlanResult{Items: items, ReadOnly: readOnly}
}

// planTreeItems turns a fresh plan into the tree's rows, updating state's
// InCloud and Seen sets along the way (the caller saves it). Split out of
// GetCloudSyncPlan so the read-only/removed-from-cloud rules are testable
// without a bucket.
func planTreeItems(plan []cloudsync.PlanItem, state *cloudSyncState, readOnly bool) []CloudSyncPlanItem {
	recordInCloud(state, plan)
	items := make([]CloudSyncPlanItem, 0, len(plan))
	for _, p := range plan {
		action := string(p.Action)
		selected := !state.Deselected[p.RelPath]
		// A local-only file this machine has seen in the cloud before is a
		// removed row for everyone, with a per-file choice rather than a
		// checkbox (see cloudSyncActionRemoved).
		//
		// Any other local-only file is an upload - except for a read-only
		// key, which never sees upload options at all: left out entirely,
		// and not marked Seen, so if this technician later switches to a
		// write key they still get the "new" highlight the first time those
		// uploads appear.
		if isRemovedFromCloud(state, p) {
			action, selected = cloudSyncActionRemoved, false
		} else if readOnly && p.Action == cloudsync.ActionUpload {
			continue
		}
		items = append(items, CloudSyncPlanItem{
			RelPath:    p.RelPath,
			Action:     action,
			LocalSize:  p.LocalSize,
			RemoteSize: p.RemoteSize,
			Selected:   selected,
			IsNew:      !state.Seen[p.RelPath],
			LocalOnly:  action == cloudSyncActionRemoved && state.LocalOnly[p.RelPath],
		})
		state.Seen[p.RelPath] = true
	}
	return items
}

// CloudFileUploaderResult is GetCloudFileUploader's own outcome - Uploader
// empty (Error also empty) means the object predates upload-attribution
// tagging (see uploaderMetaKey/cloudsync.Upload) and genuinely has no
// recorded uploader, not a failed lookup.
// IsYou: Uploader is the Access Key ID this PDT is configured with, so the
// row can say "you" instead of a 32-character key ID.
type CloudFileUploaderResult struct {
	Uploader string `json:"uploader"`
	IsYou    bool   `json:"isYou"`
	Error    string `json:"error"`
}

// GetCloudFileUploader looks up which technician's R2 Access Key ID
// uploaded relPath's current object in the shared bucket - a single
// on-demand HEAD request (cloudsync.StatUploader), deliberately never
// called for every row in the Cloud Sync tree view at once (Ken's own
// explicit "on demand only" - see StatUploader's own doc comment for why a
// bulk equivalent isn't even possible against R2/S3's own ListObjectsV2).
func (a *App) GetCloudFileUploader(relPath string) CloudFileUploaderResult {
	<-a.ready
	cfg, err := a.cloudSyncConfig()
	if err != nil {
		return CloudFileUploaderResult{Error: err.Error()}
	}
	core, err := cloudsync.NewCore(cfg)
	if err != nil {
		return CloudFileUploaderResult{Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	uploader, err := cloudsync.StatUploader(ctx, core, cfg.Bucket, a.settings.CloudSync.Prefix, relPath)
	if err != nil {
		return CloudFileUploaderResult{Error: err.Error()}
	}
	return CloudFileUploaderResult{Uploader: uploader, IsYou: uploader != "" && uploader == cfg.AccessKeyID}
}

// SaveCloudSyncSelection persists exactly which relative paths are
// currently unchecked in the tree view - called whenever the user toggles a
// checkbox, so the selection survives closing and reopening the modal (and
// PDT restarting) rather than resetting to "everything checked" every time.
// SetCloudSyncLocalOnly records (or clears) "Keep local only" for one
// removed-from-cloud file - saved immediately, like the checkbox selection,
// since it needs no sync run.
func (a *App) SetCloudSyncLocalOnly(relPath string, localOnly bool) error {
	<-a.ready
	state := loadCloudSyncState()
	if localOnly {
		state.LocalOnly[relPath] = true
	} else {
		delete(state.LocalOnly, relPath)
	}
	return saveCloudSyncState(state)
}

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
		a.emit(cloudSyncProgressEvent, CloudSyncProgress{
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
//
// removedChoices carries the choice for each requested removed-from-cloud
// file (removedChoiceSync / removedChoiceDelete); a removed file with no
// choice, or one marked Keep local only, is left alone.
func (a *App) SyncCloud(relPaths []string, removedChoices map[string]string) CloudSyncResult {
	<-a.ready
	result := CloudSyncResult{Succeeded: []string{}, Deleted: []string{}, Failed: map[string]string{}}
	if !a.IsTechLaptopCopy() {
		result.Failed["*"] = errNotTechLaptop.Error()
		return result
	}

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
	// Normally already cached by GetCloudSyncPlan; uploads are dropped here
	// regardless of what the tree sent, so a stale selection can't slip one
	// through.
	readOnly := !a.cloudCanWrite(ctx, core, cfg)
	result.ReadOnly = readOnly
	// Set when an upload is refused mid-run (the key's permissions changed
	// after the probe, or the probe was inconclusive): every upload not yet
	// started is skipped instead of failing one by one.
	var refusedMidRun atomic.Bool
	state := loadCloudSyncState()
	recordInCloud(&state, plan)
	var jobs []cloudsync.PlanItem
	var totalBytes int64
	for _, item := range plan {
		if !requested[item.RelPath] {
			continue
		}
		if item.Action != cloudsync.ActionUpload && item.Action != cloudsync.ActionDownload {
			continue
		}
		// A removed-from-cloud file acts only on its explicit choice -
		// re-verified against this fresh plan, so a file that's back in the
		// bucket (or was never there) is never deleted.
		if isRemovedFromCloud(&state, item) {
			switch decideRemoved(removedChoices[item.RelPath], state.LocalOnly[item.RelPath], readOnly) {
			case removedOutcomeDelete:
				if err := deleteRemovedLocal(driversRoot(), item.RelPath); err != nil {
					result.Failed[item.RelPath] = err.Error()
				} else {
					result.Deleted = append(result.Deleted, item.RelPath)
					delete(state.InCloud, item.RelPath)
				}
				continue
			case removedOutcomeSkippedUpload:
				result.SkippedUploads++
				continue
			case removedOutcomeNone:
				continue
			}
			// removedOutcomeUpload: an ordinary upload below.
		} else if readOnly && item.Action == cloudsync.ActionUpload {
			result.SkippedUploads++
			continue
		}
		jobs = append(jobs, item)
		if item.Action == cloudsync.ActionUpload {
			totalBytes += item.LocalSize
		} else {
			totalBytes += item.RemoteSize
		}
	}

	_ = saveCloudSyncState(state)

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
		a.emit(cloudSyncTotalProgressEvent, CloudSyncTotalProgress{
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
				if refusedMidRun.Load() {
					transferErr = cloudsync.ErrReadOnly
				} else {
					transferErr = cloudsync.Upload(ctx, gate, core, cfg.Bucket, a.settings.CloudSync.Prefix, item.RelPath, localPath, cfg.AccessKeyID, progress)
					if cloudsync.IsAccessDenied(transferErr) {
						refusedMidRun.Store(true)
						setCloudWriteAccess(cfg, false)
					}
				}
			} else {
				transferErr = cloudsync.Download(ctx, gate, core, cfg.Bucket, a.settings.CloudSync.Prefix, item.RelPath, localPath, item.RemoteSize, progress)
			}

			dataMu.Lock()
			if item.Action == cloudsync.ActionUpload && cloudsync.IsAccessDenied(transferErr) {
				result.SkippedUploads++
				result.ReadOnly = true
			} else if transferErr != nil {
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
