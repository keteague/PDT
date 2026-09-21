package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"PDT/internal/flashdrive"
	"PDT/internal/update"
)

// flashCopyProgressEvent is emitted throughout WritePortablePDT/
// SyncToFlashDrives - the toolbar's copy-progress dialog listens for
// it, since a real Drivers folder can easily be tens of thousands of files
// and take several minutes over a real USB port with otherwise zero
// indication it hadn't just hung (confirmed live).
const flashCopyProgressEvent = "flashcopy-progress"

// FlashCopyProgress is flashCopyProgressEvent's payload. EtaSeconds is 0
// until it's actually known (see newFlashCopyProgressFunc) - the frontend
// treats 0 as "no estimate yet" rather than "0 seconds remaining".
type FlashCopyProgress struct {
	Letter     string `json:"letter"`
	Step       string `json:"step"`
	Done       int    `json:"done"`
	Total      int    `json:"total"`
	DoneBytes  int64  `json:"doneBytes"`
	TotalBytes int64  `json:"totalBytes"`
	EtaSeconds int    `json:"etaSeconds"`
	// RateBytesPerSec is the step's current time-decayed transfer rate (see
	// etaEstimator.rate) for the dialog's speed meter; 0 until known.
	RateBytesPerSec float64 `json:"rateBytesPerSec"`
}

// etaMinElapsed is how long a step has to run before newFlashCopyProgressFunc
// will report an ETA at all - a rate measured over the first fraction of a
// second (or the first handful of tiny files) is unreliable and would flash
// a wrong, confidence-destroying estimate at the very start of a copy;
// waiting this long trades a few seconds of "no estimate yet" for one that's
// actually stable.
const etaMinElapsed = 3 * time.Second

// etaWindow is the span of recent history the transfer rate is averaged over
// (Ken, 2026-09-20): the time remaining is the bytes still to transfer divided
// by the average rate of the last 30 seconds. Long enough to smooth the
// bursts a real Drivers folder produces (runs of tiny files, then one huge
// installer), short enough to follow a genuine change in throughput.
const etaWindow = 30 * time.Second

// etaSampleSpacing coalesces samples arriving faster than this into one, so
// the window holds a few hundred points at most however often a copy loop
// reports progress.
const etaSampleSpacing = 100 * time.Millisecond

type etaSample struct {
	at    time.Time
	bytes int64
}

// etaEstimator turns a stream of (time, bytes transferred so far)
// observations into a transfer rate and a time-remaining estimate:
//
//	rate      = bytes transferred over the last etaWindow / that window's span
//	remaining = (totalBytes - doneBytes) / rate
//
// - the whole point being that it works from BYTES still to go and a recent
// AVERAGE rate, not from per-file counts or an instantaneous reading. (An
// earlier version used a since-the-start average, which a long run of tiny,
// overhead-bound files dragged down for ages, and then an exponentially
// decayed window, which was still too twitchy against a real Drivers
// folder's bimodal file sizes.) Until a full window of history exists the
// average simply covers everything since the step began. Zero value is not
// ready to use - call reset first.
type etaEstimator struct {
	stepStart time.Time
	samples   []etaSample // oldest first; samples[0] anchors the window
}

// reset starts a new step's estimate from scratch at now, with doneBytes as
// that step's own starting point (normally 0, but need not be).
func (e *etaEstimator) reset(now time.Time, doneBytes int64) {
	e.stepStart = now
	e.samples = append(e.samples[:0], etaSample{at: now, bytes: doneBytes})
}

// observe records one (now, doneBytes) observation and trims history older
// than etaWindow, always keeping the newest sample at or before the window's
// edge as its anchor.
func (e *etaEstimator) observe(now time.Time, doneBytes int64) {
	if n := len(e.samples); n > 1 && now.Sub(e.samples[n-1].at) < etaSampleSpacing {
		e.samples[n-1] = etaSample{at: now, bytes: doneBytes}
	} else {
		e.samples = append(e.samples, etaSample{at: now, bytes: doneBytes})
	}
	cutoff := now.Add(-etaWindow)
	drop := 0
	for drop+1 < len(e.samples) && !e.samples[drop+1].at.After(cutoff) {
		drop++
	}
	if drop > 0 {
		e.samples = append(e.samples[:0], e.samples[drop:]...)
	}
}

// sample feeds one new (now, doneBytes) observation into the window and
// returns the current estimated seconds remaining until doneBytes reaches
// totalBytes - 0 ("not known yet") until at least etaMinElapsed has passed
// since reset, or once totalBytes is reached.
func (e *etaEstimator) sample(now time.Time, doneBytes, totalBytes int64) int {
	e.observe(now, doneBytes)
	if doneBytes >= totalBytes || now.Sub(e.stepStart) < etaMinElapsed {
		return 0
	}
	rate := e.rate()
	if rate <= 0 {
		return 0
	}
	return int(float64(totalBytes-doneBytes) / rate)
}

// rate returns the average bytes-per-second over the last etaWindow (0 before
// two observations exist) - the same number sample derives its own return
// value from, exposed separately for the live transfer-rate meter.
func (e *etaEstimator) rate() float64 {
	n := len(e.samples)
	if n < 2 {
		return 0
	}
	first, last := e.samples[0], e.samples[n-1]
	dt := last.at.Sub(first.at).Seconds()
	if dt <= 0 || last.bytes <= first.bytes {
		return 0
	}
	return float64(last.bytes-first.bytes) / dt
}

// flashEmitInterval is how often a copy's accumulated progress reaches the UI.
const flashEmitInterval = 200 * time.Millisecond

// flashEmitter turns a copy's progress callbacks into a handful of UI events
// per second, WITHOUT ever making the copy wait on the UI.
//
// That last part matters: runtime.EventsEmit hands its script to the webview
// on the UI thread and waits for it, and the copy's progress callback runs
// under copyTreeMerge's own lock with every worker queued behind it. Emitting
// an event per file from there (thousands a second for a Drivers repo full of
// tiny files) let a busy UI throttle the whole copy - a write that used to
// take ~20 minutes took an hour. Now the callback only records the latest
// state under a mutex; a timer sends it every flashEmitInterval, from its own
// goroutine, as one aggregate event plus one batched per-file event.
//
// Safe to call from several goroutines (the cloud download does).
type flashEmitter struct {
	a      *App
	letter string

	mu      sync.Mutex // guards the fields below, the estimator included
	est     etaEstimator
	curStep string
	files   map[string]FlashFileProgress // latest update per file since the last flush
	agg     *FlashCopyProgress
	timer   *time.Timer

	emitMu sync.Mutex // serializes actual emission so events stay in order
}

// flashBatch is what one flush sends, in this order.
type flashBatch struct {
	plan  *FlashFilePlan
	files []FlashFileProgress
	agg   *FlashCopyProgress
}

// newFlashCopyProgressFunc returns a stepProgressFunc that reports progress
// for letter to the dialog: the aggregate bar/speed/ETA (flashCopyProgressEvent,
// see etaEstimator for how the ETA is computed), plus - when the copy supplies
// them - the step's file plan and per-file progress for the file list. A
// step's final update (every file processed) is sent immediately so the
// dialog never sits on a stale percentage; everything else is coalesced.
func (a *App) newFlashCopyProgressFunc(letter string) stepProgressFunc {
	em := &flashEmitter{a: a, letter: letter, files: map[string]FlashFileProgress{}}
	return em.update
}

// takeLocked drains everything pending into a batch (caller holds e.mu).
func (e *flashEmitter) takeLocked() flashBatch {
	var b flashBatch
	if len(e.files) > 0 {
		b.files = make([]FlashFileProgress, 0, len(e.files))
		for _, f := range e.files {
			b.files = append(b.files, f)
		}
		e.files = map[string]FlashFileProgress{}
	}
	b.agg, e.agg = e.agg, nil
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	return b
}

func (e *flashEmitter) send(b flashBatch) {
	e.emitMu.Lock()
	defer e.emitMu.Unlock()
	if b.plan != nil {
		runtime.EventsEmit(e.a.ctx, flashFilePlanEvent, *b.plan)
	}
	if len(b.files) > 0 {
		runtime.EventsEmit(e.a.ctx, flashFileEvent, b.files)
	}
	if b.agg != nil {
		runtime.EventsEmit(e.a.ctx, flashCopyProgressEvent, *b.agg)
	}
}

func (e *flashEmitter) timerFlush() {
	e.mu.Lock()
	e.timer = nil
	b := e.takeLocked()
	e.mu.Unlock()
	e.send(b)
}

func (e *flashEmitter) update(step string, p CopyProgress) {
	now := time.Now()
	e.mu.Lock()

	// A new step or a new plan starts a new list: what the previous one left
	// pending goes out first, so events keep their order.
	var prev flashBatch
	if step != e.curStep || p.Plan != nil {
		prev = e.takeLocked()
	}
	if step != e.curStep {
		e.curStep = step
		e.est.reset(now, p.DoneBytes)
	}

	// Every call feeds the estimator, however rarely the UI hears about it.
	etaSeconds := e.est.sample(now, p.DoneBytes, p.TotalBytes)
	rate := e.est.rate()
	final := p.DoneFiles == p.TotalFiles
	if final {
		etaSeconds, rate = 0, 0
	}
	e.agg = &FlashCopyProgress{
		Letter: e.letter, Step: step,
		Done: p.DoneFiles, Total: p.TotalFiles, DoneBytes: p.DoneBytes, TotalBytes: p.TotalBytes,
		EtaSeconds: etaSeconds, RateBytesPerSec: rate,
	}
	if p.File != "" {
		e.files[p.File] = FlashFileProgress{Letter: e.letter, RelPath: p.File, Done: p.FileDone, Total: p.FileTotal}
	}

	var now2 flashBatch
	switch {
	case p.Plan != nil:
		now2 = e.takeLocked()
		now2.plan = &FlashFilePlan{Letter: e.letter, Step: step, Files: p.Plan}
	case final:
		now2 = e.takeLocked()
	case e.timer == nil:
		e.timer = time.AfterFunc(flashEmitInterval, e.timerFlush)
	}
	e.mu.Unlock()

	if prev.agg != nil || len(prev.files) > 0 {
		e.send(prev)
	}
	if now2.plan != nil || now2.agg != nil || len(now2.files) > 0 {
		e.send(now2)
	}
}

// DriveInfo is one removable drive offered by the Write to Flash Drive
// dialog.
type DriveInfo struct {
	Letter     string `json:"letter"`
	Label      string `json:"label"`
	TotalBytes uint64 `json:"totalBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
}

// ListDrivesResult is ListRemovableDrives' outcome.
type ListDrivesResult struct {
	Drives []DriveInfo `json:"drives"`
	Error  string      `json:"error"`
}

// IsRunningFromRemovableDrive reports whether this exact running PDT.exe
// sits on a removable (USB flash) drive - the toolbar's Flash Drive button
// is disabled whenever this is true, since Windows refuses to let a running
// exe overwrite its own file ("The process cannot access the file because
// it is being used by another process" - confirmed live) and there's no
// sensible reason to stamp a portable copy out onto more drives from
// another portable copy anyway; only an installed copy on a technician's
// laptop is a real source.
func (a *App) IsRunningFromRemovableDrive() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return flashdrive.IsRemovableDrive(exe)
}

// ListRemovableDrives lists every currently-mounted USB flash drive, for the
// Write to Flash Drive dialog's checklist.
func (a *App) ListRemovableDrives() ListDrivesResult {
	drives, err := flashdrive.EnumRemovableDrives()
	if err != nil {
		return ListDrivesResult{Error: err.Error()}
	}
	out := make([]DriveInfo, 0, len(drives))
	for _, d := range drives {
		out = append(out, DriveInfo{Letter: d.Letter, Label: d.Label, TotalBytes: d.TotalBytes, FreeBytes: d.FreeBytes})
	}
	return ListDrivesResult{Drives: out}
}

// BatchDriveResult is the per-drive outcome shared by FormatDrives and
// WritePortablePDT - each drive succeeds or fails independently, so one bad
// drive (write-protected, unplugged mid-operation) doesn't abort the rest.
type BatchDriveResult struct {
	Succeeded []string          `json:"succeeded"`
	Failed    map[string]string `json:"failed"`
	// Notes are extra lines worth logging that aren't a per-drive success or
	// failure (today: the post-write PDT.app check - see macappflash.go).
	Notes []FlashNote `json:"notes"`
}

// FormatDrives quick-formats every listed drive letter as exFAT. Destructive
// and irreversible - the frontend is expected to have already shown its own
// explicit "this erases everything" warning naming these exact drives before
// ever calling this.
func (a *App) FormatDrives(letters []string) BatchDriveResult {
	// Succeeded starts as []string{}, not nil, for the same reason
	// ManufacturersWithDrivers' own comment explains (a nil slice marshals
	// to JSON `null`) - the frontend already guards every read of this
	// specific field with `|| []`, but there's no reason to rely on that.
	result := BatchDriveResult{Succeeded: []string{}, Failed: map[string]string{}, Notes: []FlashNote{}}
	for _, letter := range letters {
		if err := flashdrive.FormatExFAT(letter); err != nil {
			result.Failed[letter] = err.Error()
			continue
		}
		result.Succeeded = append(result.Succeeded, letter)
	}
	return result
}

// beginFlashSync starts a cancelable child of a.ctx and stores it as the
// currently-running Sync/Write-to-Flash-Drive transfer (see
// flashSyncMu/flashSyncCancel's own doc comment) - the same
// derive-a-child-context-so-CancelFlashSync-only-touches-this-one-call
// pattern Deploy/StopDeploy already use. The returned done func clears that
// stored cancel and releases ctx's own resources; callers defer it.
func (a *App) beginFlashSync() (ctx context.Context, done func()) {
	ctx, cancel := context.WithCancel(a.ctx)
	a.flashSyncMu.Lock()
	a.flashSyncCancel = cancel
	a.flashSyncMu.Unlock()
	return ctx, func() {
		a.flashSyncMu.Lock()
		a.flashSyncCancel = nil
		a.flashSyncMu.Unlock()
		cancel()
	}
}

// CancelFlashSync cancels the currently-running Sync/Write-to-Flash-Drive
// transfer, if any (a no-op otherwise). Unlike StopDeploy's own "let the
// current step finish" semantics - a Deploy step half-applied to a real
// printer object is a state worth avoiding - an interrupted file copy has no
// equivalent concern, so this stops as close to immediately as possible: the
// one file each worker is mid-copying when Cancel lands is aborted and its
// partial destination content deleted (see copyTreeMerge/copyFile), and no
// further file starts. Any drive not yet reached in a multi-drive batch is
// reported as failed/canceled rather than silently skipped.
func (a *App) CancelFlashSync() {
	a.flashSyncMu.Lock()
	cancel := a.flashSyncCancel
	a.flashSyncMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// WritePortablePDT copies a portable PDT install - the running executable
// plus this laptop's own Drivers/ and Configs/ folders (driversRoot()/
// configsRoot(), the same "next to the executable" locations PDT already
// reads them from) - onto every listed drive letter. This is what actually
// turns "PDT installed on the technician's laptop" into "a portable copy on
// a flash drive": the copied exe finds its own Drivers/Configs right beside
// it exactly like it does today when run directly from a flash drive.
//
// includeDrivers (the dialog's "Include Drivers Repo" checkbox, checked by
// default with the Local source - Ken, 2026-09-20) controls whether the
// Drivers repo is copied too: a full local repo can take about 20 minutes over
// USB 3.0, so the tech may untick it or pick Cloud. Left out, the drive still gets an empty Drivers
// folder scaffold - the exe decides it's running portably by finding a
// Drivers folder right beside itself.
//
// driversSource ("local", the default and "" too, or "cloud") picks where an
// included Drivers repo comes from, exactly like the Sync dialog's combo:
// this laptop's own Drivers folder, or the shared cloud repository
// downloaded straight onto the drive (cloudflash.go).
func (a *App) WritePortablePDT(letters []string, includeDrivers bool, driversSource string) BatchDriveResult {
	<-a.ready
	result := BatchDriveResult{Succeeded: []string{}, Failed: map[string]string{}, Notes: []FlashNote{}}

	var cloudDrivers func(context.Context, string, func(CopyProgress)) error
	if includeDrivers && driversSource == driversSourceCloud {
		// Fail once, up front, before anything is written to any drive.
		if _, err := a.cloudSyncConfig(); err != nil {
			result.Failed["*"] = err.Error()
			return result
		}
		cloudDrivers = func(ctx context.Context, letter string, progress func(CopyProgress)) error {
			conflicts, err := a.syncCloudDriversToDrive(ctx, letter, progress)
			if conflicts > 0 {
				result.Notes = append(result.Notes, FlashNote{Level: "WARN", Text: fmt.Sprintf("%d file(s) on %s differ in size from the cloud copy and were left untouched - resolve them in Cloud Sync.", conflicts, letter)})
			}
			return err
		}
	}

	exePath, err := os.Executable()
	if err != nil {
		result.Failed["*"] = fmt.Sprintf("could not determine this executable's own path: %v", err)
		return result
	}
	exeName := filepath.Base(exePath)
	exeData, err := os.ReadFile(exePath)
	if err != nil {
		result.Failed["*"] = fmt.Sprintf("could not read %q: %v", exePath, err)
		return result
	}

	ctx, done := a.beginFlashSync()
	defer done()

	for _, letter := range letters {
		if ctx.Err() != nil {
			result.Failed[letter] = ctx.Err().Error()
			continue
		}
		if err := writePortablePDTTo(ctx, letter, exeName, exeData, includeDrivers, cloudDrivers, a.newFlashCopyProgressFunc(letter)); err != nil {
			result.Failed[letter] = err.Error()
			continue
		}
		result.Succeeded = append(result.Succeeded, letter)
	}
	a.ensureReleaseAppsOnDrives(ctx, &result)
	return result
}

// ensureReleaseAppsOnDrives is WritePortablePDT's last step: once the drives
// are written, and only if this computer has Internet, make sure each one
// carries a current copy of the app for the OTHER platform it'll be carried to
// (Ken, 2026-09-20) - the release is fetched once, then:
//   - PDT.app for Mac endpoints (macappflash.go), always; and
//   - PDT.exe for Windows endpoints (winexeflash.go), when running on a Mac -
//     a Windows write already copies its own PDT.exe, but a Mac has none to copy.
//
// Entirely best-effort: the write itself already succeeded, so every outcome
// here - offline, GitHub unreachable, a failed download - is just a note in
// result.Notes, never a Failed entry.
func (a *App) ensureReleaseAppsOnDrives(ctx context.Context, result *BatchDriveResult) {
	if len(result.Succeeded) == 0 || ctx.Err() != nil {
		return
	}
	onMac := goruntime.GOOS == "darwin"
	rel, err := update.FetchLatest(repoSlug())
	if err != nil {
		what := "PDT.app for macOS"
		if onMac {
			what = "PDT.app for macOS and PDT.exe for Windows"
		}
		result.Notes = append(result.Notes, FlashNote{Level: "WARN", Text: fmt.Sprintf("Skipped the %s check - couldn't reach GitHub for the latest release (%v). Connect to the Internet and Write to Flash Drive again, or copy them on by hand.", what, err)})
		return
	}

	if src, err := macAppSourceFrom(rel); err != nil {
		result.Notes = append(result.Notes, FlashNote{Level: "WARN", Text: fmt.Sprintf("Skipped the PDT.app for macOS check: %v", err)})
	} else {
		defer src.cleanup()
		for _, letter := range result.Succeeded {
			if ctx.Err() != nil {
				return
			}
			note, err := ensureMacAppOnDrive(ctx, letter, src, a.newFlashCopyProgressFunc(letter))
			if err != nil {
				result.Notes = append(result.Notes, FlashNote{Level: "WARN", Text: fmt.Sprintf("Could not put a current PDT.app on %s: %v", letter, err)})
				continue
			}
			result.Notes = append(result.Notes, note)
		}
	}

	if !onMac {
		return
	}
	src, err := newWinExeSource(rel)
	if err != nil {
		result.Notes = append(result.Notes, FlashNote{Level: "WARN", Text: fmt.Sprintf("Skipped the PDT.exe for Windows check: %v", err)})
		return
	}
	defer src.cleanup()
	for _, letter := range result.Succeeded {
		if ctx.Err() != nil {
			return
		}
		note, err := ensureWinExeOnDrive(ctx, letter, src, a.newFlashCopyProgressFunc(letter))
		if err != nil {
			result.Notes = append(result.Notes, FlashNote{Level: "WARN", Text: fmt.Sprintf("Could not put a current PDT.exe on %s: %v", letter, err)})
			continue
		}
		result.Notes = append(result.Notes, note)
	}
}

// SyncToFlashDrives copies this laptop's own Drivers folder and/or Configs
// folder onto every listed drive letter - the toolbar's Sync button, for
// topping up a flash drive that already has a portable PDT copy on it with
// whatever has shown up locally since, without rewriting the exe or the
// 7-Zip tools. The dialog's Drivers/Configs checkboxes (Ken, 2026-09-20)
// pick which; at least one must be true. syncDriversTo already extracts
// anything newly-copied on the destination itself (see its own doc
// comment), so the flash drive is immediately ready to use without needing
// to be plugged into another computer first just to trigger that.
//
// driversSource ("local" - the default, also "" - or "cloud") picks where the
// Drivers come from: this laptop's own Drivers folder, or the shared cloud
// repository downloaded straight onto the drive (see cloudflash.go).
func (a *App) SyncToFlashDrives(letters []string, includeDrivers, includeConfigs bool, driversSource string) BatchDriveResult {
	<-a.ready
	result := BatchDriveResult{Succeeded: []string{}, Failed: map[string]string{}, Notes: []FlashNote{}}
	if !includeDrivers && !includeConfigs {
		result.Failed["*"] = "nothing selected to sync - check Drivers and/or Configs"
		return result
	}
	fromCloud := includeDrivers && driversSource == driversSourceCloud
	if fromCloud {
		// Fail once, up front, with the same clear "set up Cloud Sync first"
		// message the Cloud Sync dialog gives, rather than once per drive.
		if _, err := a.cloudSyncConfig(); err != nil {
			result.Failed["*"] = err.Error()
			return result
		}
	}

	ctx, done := a.beginFlashSync()
	defer done()

	for _, letter := range letters {
		if ctx.Err() != nil {
			result.Failed[letter] = ctx.Err().Error()
			continue
		}
		progress := a.newFlashCopyProgressFunc(letter)
		var errs []error
		if fromCloud {
			conflicts, err := a.syncCloudDriversToDrive(ctx, letter, func(p CopyProgress) { progress("Drivers (cloud)", p) })
			if err != nil {
				errs = append(errs, err)
			}
			if conflicts > 0 {
				result.Notes = append(result.Notes, FlashNote{Level: "WARN", Text: fmt.Sprintf("%d file(s) on %s differ in size from the cloud copy and were left untouched - resolve them in Cloud Sync.", conflicts, letter)})
			}
		} else if includeDrivers {
			if err := syncDriversTo(ctx, letter, func(p CopyProgress) { progress("Drivers", p) }); err != nil {
				errs = append(errs, err)
			}
		}
		if includeConfigs {
			if err := syncConfigsTo(ctx, letter, func(p CopyProgress) { progress("Configs", p) }); err != nil {
				errs = append(errs, err)
			}
		}
		if err := errors.Join(errs...); err != nil {
			result.Failed[letter] = err.Error()
			continue
		}
		result.Succeeded = append(result.Succeeded, letter)
	}
	return result
}

// syncConfigsTo copies this laptop's Configs folder onto letter's own
// Configs folder, merging - the Configs counterpart of syncDriversTo. Creates
// the folder even when there's nothing local to copy, matching what
// writePortablePDTTo guarantees for a freshly-written drive.
func syncConfigsTo(ctx context.Context, letter string, onProgress func(CopyProgress)) error {
	dest := filepath.Join(letter, "Configs")
	src := configsRoot()
	if samePath(dest, src) {
		return nil // same self-truncation hazard syncDriversTo documents
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("creating Configs: %w", err)
	}
	if !dirExists(src) {
		return nil
	}
	if err := copyTreeMerge(ctx, dest, src, onProgress); err != nil {
		return fmt.Errorf("copying Configs: %w", err)
	}
	return nil
}

// SyncFromFlashDrive copies letter's own Drivers and/or Configs folder onto
// this laptop's (driversRoot()/configsRoot()) - the reverse direction of
// SyncToFlashDrives, for pulling in whatever another technician's own sync
// run left on a shared flash drive since this laptop last saw it (Configs
// too now - Ken, 2026-09-20). Single-drive rather than batched like the
// to-flash-drive direction: pulling from more than one flash drive into the
// same destination in one call would make "which drive's copy of a
// same-named file wins" an unanswerable question, so the frontend has the
// technician pick exactly one source drive at a time. Shares
// flashSyncCancel with the to-flash-drive direction and WritePortablePDT
// (see beginFlashSync) - only one such transfer is ever expected to be
// running at once, and Cancel should stop whichever one that is.
func (a *App) SyncFromFlashDrive(letter string, includeDrivers, includeConfigs bool) error {
	<-a.ready
	if !includeDrivers && !includeConfigs {
		return errors.New("nothing selected to sync - check Drivers and/or Configs")
	}

	ctx, done := a.beginFlashSync()
	defer done()

	progress := a.newFlashCopyProgressFunc(letter)
	var errs []error
	pull := func(step, src, dest string) {
		if samePath(src, dest) || !dirExists(src) {
			return
		}
		if err := copyTreeMerge(ctx, dest, src, func(p CopyProgress) { progress(step, p) }); err != nil {
			errs = append(errs, err)
		}
	}
	if includeDrivers {
		pull("Drivers", filepath.Join(letter, "Drivers"), driversRoot())
	}
	if includeConfigs {
		pull("Configs", filepath.Join(letter, "Configs"), configsRoot())
	}
	return errors.Join(errs...)
}

// writePortablePDTTo copies exeData to letter, then this laptop's own
// Drivers, Configs, and 7-Zip tools folders alongside it - a fully
// self-contained portable copy that needs nothing else to work on another
// computer. Both Drivers and Configs are guaranteed to exist afterward even
// if the source had nothing to copy - a technician's local Configs folder
// often doesn't exist yet (nothing saved or captured there so far), and
// their local Drivers folder is very rarely fully populated for every
// manufacturer PDT knows about. ensureDriversScaffold (already
// unconditional/idempotent - see its own doc comment) fills in whichever
// manufacturer folders the copy didn't already bring along, the same way it
// does for a brand-new install's own empty Drivers folder. Uses
// copyTreeMerge, not os.CopyFS, specifically so re-running this against a
// flash drive that already has content on it (a repeat Write to Flash
// Drive, or Sync) doesn't fail outright on the first already-existing file
// it finds - confirmed live as a real bug (see copyTreeMerge's own doc
// comment).
// stepProgressFunc reports progress for one named copy step ("Drivers",
// "Configs", "7-Zip tools") - files/bytes processed so far within that step
// specifically, each step restarting its own count from zero. nil is a
// valid, no-op value (existing tests, and anything that doesn't need to show
// a progress dialog, pass nil throughout).
type stepProgressFunc func(step string, progress CopyProgress)

// cloudDrivers, when non-nil, replaces the local Drivers copy with a download
// from the cloud repository (see WritePortablePDT's driversSource); nil means
// copy this laptop's own Drivers folder. Only consulted when includeDrivers.
func writePortablePDTTo(ctx context.Context, letter, exeName string, exeData []byte, includeDrivers bool, cloudDrivers func(context.Context, string, func(CopyProgress)) error, progress stepProgressFunc) error {
	flashdrive.DisableIndexing(letter)
	if err := os.WriteFile(filepath.Join(letter, exeName), exeData, 0o755); err != nil {
		return fmt.Errorf("writing %s: %w", exeName, err)
	}

	// Each step below is attempted regardless of whether an earlier one hit
	// a partial failure - copyTreeMerge itself is already best-effort
	// per-file (see its own doc comment for the real bug that motivated
	// that), but this function used to still throw away everything after
	// the first step that returned any error at all, which meant one bad
	// file part-way through Drivers previously skipped Configs and tools
	// entirely too, on top of whatever Drivers itself already skipped. A
	// canceled ctx makes each of these near-instant no-ops rather than
	// skipping them outright, for the same reason: simpler than threading a
	// "did an earlier step get canceled" flag through, and copyTreeMerge
	// already returns fast once ctx.Err() is set.
	var errs []error
	driversStep := "Drivers"
	if cloudDrivers != nil {
		driversStep = "Drivers (cloud)"
	}
	driversProgress := func(p CopyProgress) {
		if progress != nil {
			progress(driversStep, p)
		}
	}
	if includeDrivers {
		var err error
		if cloudDrivers != nil {
			err = cloudDrivers(ctx, letter, driversProgress)
		} else {
			err = syncDriversTo(ctx, letter, driversProgress)
		}
		if err != nil {
			errs = append(errs, err)
		}
	} else {
		// No Drivers copy requested - but still lay down the empty
		// Drivers folder (and the platform's own manufacturer scaffold
		// inside it, the same postSyncDriversHook a real sync ends with),
		// since its mere presence beside the exe is what makes PDT treat
		// itself as a portable copy, and it gives Cloud Sync / a later
		// Sync somewhere to land.
		driversDest := filepath.Join(letter, "Drivers")
		if err := os.MkdirAll(driversDest, 0o755); err != nil {
			errs = append(errs, fmt.Errorf("creating Drivers: %w", err))
		} else if err := postSyncDriversHook(driversDest); err != nil {
			errs = append(errs, err)
		}
	}

	configsDest := filepath.Join(letter, "Configs")
	if dirExists(configsRoot()) {
		configsProgress := func(p CopyProgress) {
			if progress != nil {
				progress("Configs", p)
			}
		}
		if err := copyTreeMerge(ctx, configsDest, configsRoot(), configsProgress); err != nil {
			errs = append(errs, fmt.Errorf("copying Configs: %w", err))
		}
	}
	if err := os.MkdirAll(configsDest, 0o755); err != nil {
		errs = append(errs, fmt.Errorf("creating Configs: %w", err))
	}

	// Written straight from this build's own embedded copy (sevenzipassets.go),
	// not copied from sevenZipToolsDir()'s per-machine cache - that cache is a
	// Windows-only concept (macOS never populates or even defines one), but
	// every platform's build embeds the same 7-Zip assets, so a flash drive
	// gets a real Windows tools/7zip folder regardless of which platform
	// created it.
	if err := writeSevenZipAssets(filepath.Join(letter, "tools", "7zip")); err != nil {
		errs = append(errs, fmt.Errorf("writing 7-Zip tools: %w", err))
	} else if progress != nil {
		progress("7-Zip tools", CopyProgress{DoneFiles: 1, TotalFiles: 1, DoneBytes: 1, TotalBytes: 1})
	}
	return errors.Join(errs...)
}

// syncDriversTo copies this laptop's own Drivers folder onto letter, then
// runs postSyncDriversHook (platform-specific - see app_windows.go/
// app_darwin.go) for whatever platform-specific finishing touch a freshly-
// copied Drivers folder needs before it's immediately usable from the flash
// drive itself, without being plugged into another computer first. The
// shared step between writePortablePDTTo (full "Write to Flash Drive") and
// the toolbar's Sync button (drivers only, no exe/Configs/tools, for topping
// up a flash drive that already exists).
func syncDriversTo(ctx context.Context, letter string, onProgress func(CopyProgress)) error {
	flashdrive.DisableIndexing(letter)
	driversDest := filepath.Join(letter, "Drivers")
	src := driversRoot()
	if samePath(driversDest, src) {
		// Sync stays available even when PDT itself is running from a flash
		// drive (unlike Write to Flash Drive, which is disabled outright in
		// that case - see IsRunningFromRemovableDrive), so a technician can
		// sync one portable copy's Drivers onto a *different* one they've
		// also plugged in. Picking that exact same drive as the sync target
		// would make src and driversDest identical, which - copying a tree
		// onto itself via copyTreeMerge's own os.OpenFile(O_TRUNC) - would
		// truncate a source file while still reading it. A no-op instead.
		return nil
	}
	var errs []error
	if dirExists(src) {
		if err := copyTreeMerge(ctx, driversDest, src, onProgress); err != nil {
			errs = append(errs, fmt.Errorf("copying Drivers: %w", err))
		}
	}
	if err := postSyncDriversHook(driversDest); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
