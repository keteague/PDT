package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"PDT/internal/flashdrive"
)

// flashCopyProgressEvent is emitted throughout WritePortablePDT/
// SyncDriversToFlashDrives - the toolbar's copy-progress dialog listens for
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
}

// etaMinElapsed is how long a step has to run before newFlashCopyProgressFunc
// will report an ETA at all - a rate measured over the first fraction of a
// second (or the first handful of tiny files) is unreliable and would flash
// a wrong, confidence-destroying estimate at the very start of a copy;
// waiting this long trades a few seconds of "no estimate yet" for one that's
// actually stable.
const etaMinElapsed = 3 * time.Second

// etaRateTimeConstant is the "memory span" of the exponentially time-decayed
// bytes-per-second estimate newFlashCopyProgressFunc computes: roughly how
// far back in wall-clock time recent activity still meaningfully influences
// the current rate, with older activity fading out smoothly rather than
// being cut off sharply. See newFlashCopyProgressFunc's own doc comment for
// why a plain since-the-start average doesn't work.
const etaRateTimeConstant = 6 * time.Second

// etaEstimator tracks a time-decayed bytes-per-second rate for one copy
// step and turns it into a time-remaining estimate - a plain "bytes done /
// time elapsed since the step started" average swings wildly and is slow to
// recover, confirmed live: a real Drivers folder's file sizes are bimodal
// (long runs of tiny files, e.g. .cat/.inf, interrupted by a handful of huge
// installers), and per-file open/write/close overhead dominates for the
// tiny-file runs almost independent of their actual byte count - so a
// since-the-start average gets dragged down hard by a slow, overhead-bound
// run of small files, then stays wrong for a long time afterward even once
// a big file's real throughput starts coming in, because that average has
// to "unwind" every sample since the step began before it reflects current
// conditions at all.
//
// The fix is to weight recent samples far more than old ones: each call to
// sample decays a running (bytes, seconds) pair by
// e^(-realElapsed/etaRateTimeConstant) before adding this call's own delta -
// decaying by actual wall-clock time elapsed, not by call count, is what
// keeps this stable regardless of how bunched-up calls are (a burst of a
// thousand tiny files arriving within a few milliseconds barely decays the
// window at all, correctly treating them as one small contribution rather
// than shrinking the window's memory of whatever larger file came before
// it). Zero value is not ready to use - call reset first.
type etaEstimator struct {
	stepStart       time.Time
	lastSampleTime  time.Time
	lastSampleBytes int64
	windowBytes     float64
	windowSeconds   float64
}

// reset starts a new step's estimate from scratch at now, with doneBytes as
// that step's own starting point (normally 0, but need not be).
func (e *etaEstimator) reset(now time.Time, doneBytes int64) {
	e.stepStart = now
	e.lastSampleTime = now
	e.lastSampleBytes = doneBytes
	e.windowBytes = 0
	e.windowSeconds = 0
}

// sample feeds one new (now, doneBytes) observation into the decayed
// window and returns the current estimated seconds remaining until
// doneBytes reaches totalBytes - 0 ("not known yet") until at least
// etaMinElapsed has passed since reset, or once totalBytes is reached.
func (e *etaEstimator) sample(now time.Time, doneBytes, totalBytes int64) int {
	if dt := now.Sub(e.lastSampleTime).Seconds(); dt > 0 {
		decay := math.Exp(-dt / etaRateTimeConstant.Seconds())
		e.windowBytes = e.windowBytes*decay + float64(doneBytes-e.lastSampleBytes)
		e.windowSeconds = e.windowSeconds*decay + dt
		e.lastSampleTime = now
		e.lastSampleBytes = doneBytes
	}
	if doneBytes >= totalBytes || now.Sub(e.stepStart) < etaMinElapsed || e.windowSeconds <= 0 {
		return 0
	}
	rate := e.windowBytes / e.windowSeconds
	if rate <= 0 {
		return 0
	}
	return int(float64(totalBytes-doneBytes) / rate)
}

// newFlashCopyProgressFunc returns a stepProgressFunc that emits
// flashCopyProgressEvent for letter, throttled to at most once every 150ms
// per step - except the step's own final update (every file processed),
// always sent so the dialog never sits on a stale percentage once a step
// actually finishes. See etaEstimator for how the ETA itself is computed.
func (a *App) newFlashCopyProgressFunc(letter string) stepProgressFunc {
	var lastEmit time.Time
	var curStep string
	var est etaEstimator

	return func(step string, p CopyProgress) {
		now := time.Now()
		if step != curStep {
			curStep = step
			est.reset(now, p.DoneBytes)
		}
		// Every call feeds the estimator, regardless of the emit throttle
		// below - otherwise, whatever bytes/time occur between two
		// throttled emits would simply never be counted at all.
		etaSeconds := est.sample(now, p.DoneBytes, p.TotalBytes)

		final := p.DoneFiles == p.TotalFiles
		if !final && now.Sub(lastEmit) < 150*time.Millisecond {
			return
		}
		lastEmit = now
		if final {
			etaSeconds = 0
		}
		runtime.EventsEmit(a.ctx, flashCopyProgressEvent, FlashCopyProgress{
			Letter:     letter,
			Step:       step,
			Done:       p.DoneFiles,
			Total:      p.TotalFiles,
			DoneBytes:  p.DoneBytes,
			TotalBytes: p.TotalBytes,
			EtaSeconds: etaSeconds,
		})
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
	result := BatchDriveResult{Succeeded: []string{}, Failed: map[string]string{}}
	for _, letter := range letters {
		if err := flashdrive.FormatExFAT(letter); err != nil {
			result.Failed[letter] = err.Error()
			continue
		}
		result.Succeeded = append(result.Succeeded, letter)
	}
	return result
}

// WritePortablePDT copies a portable PDT install - the running executable
// plus this laptop's own Drivers/ and Configs/ folders (driversRoot()/
// configsRoot(), the same "next to the executable" locations PDT already
// reads them from) - onto every listed drive letter. This is what actually
// turns "PDT installed on the technician's laptop" into "a portable copy on
// a flash drive": the copied exe finds its own Drivers/Configs right beside
// it exactly like it does today when run directly from a flash drive.
func (a *App) WritePortablePDT(letters []string) BatchDriveResult {
	<-a.ready
	result := BatchDriveResult{Succeeded: []string{}, Failed: map[string]string{}}

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

	for _, letter := range letters {
		if err := writePortablePDTTo(letter, exeName, exeData, a.newFlashCopyProgressFunc(letter)); err != nil {
			result.Failed[letter] = err.Error()
			continue
		}
		result.Succeeded = append(result.Succeeded, letter)
	}
	return result
}

// SyncDriversToFlashDrives copies this laptop's own Drivers folder onto
// every listed drive letter - the toolbar's Sync button, for topping up a
// flash drive that already has a portable PDT copy on it with whatever new
// driver packages have shown up locally since, without rewriting the exe or
// touching Configs/tools at all. syncDriversTo already extracts anything
// newly-copied on the destination itself (see its own doc comment), so the
// flash drive is immediately ready to use without needing to be plugged
// into another computer first just to trigger that.
func (a *App) SyncDriversToFlashDrives(letters []string) BatchDriveResult {
	<-a.ready
	result := BatchDriveResult{Succeeded: []string{}, Failed: map[string]string{}}
	for _, letter := range letters {
		progress := a.newFlashCopyProgressFunc(letter)
		if err := syncDriversTo(letter, func(p CopyProgress) { progress("Drivers", p) }); err != nil {
			result.Failed[letter] = err.Error()
			continue
		}
		result.Succeeded = append(result.Succeeded, letter)
	}
	return result
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

func writePortablePDTTo(letter, exeName string, exeData []byte, progress stepProgressFunc) error {
	if err := os.WriteFile(filepath.Join(letter, exeName), exeData, 0o755); err != nil {
		return fmt.Errorf("writing %s: %w", exeName, err)
	}

	// Each step below is attempted regardless of whether an earlier one hit
	// a partial failure - copyTreeMerge itself is already best-effort
	// per-file (see its own doc comment for the real bug that motivated
	// that), but this function used to still throw away everything after
	// the first step that returned any error at all, which meant one bad
	// file part-way through Drivers previously skipped Configs and tools
	// entirely too, on top of whatever Drivers itself already skipped.
	var errs []error
	driversProgress := func(p CopyProgress) {
		if progress != nil {
			progress("Drivers", p)
		}
	}
	if err := syncDriversTo(letter, driversProgress); err != nil {
		errs = append(errs, err)
	}

	configsDest := filepath.Join(letter, "Configs")
	if dirExists(configsRoot()) {
		configsProgress := func(p CopyProgress) {
			if progress != nil {
				progress("Configs", p)
			}
		}
		if err := copyTreeMerge(configsDest, configsRoot(), configsProgress); err != nil {
			errs = append(errs, fmt.Errorf("copying Configs: %w", err))
		}
	}
	if err := os.MkdirAll(configsDest, 0o755); err != nil {
		errs = append(errs, fmt.Errorf("creating Configs: %w", err))
	}

	if dirExists(sevenZipToolsDir()) {
		toolsProgress := func(p CopyProgress) {
			if progress != nil {
				progress("7-Zip tools", p)
			}
		}
		if err := copyTreeMerge(filepath.Join(letter, "tools", "7zip"), sevenZipToolsDir(), toolsProgress); err != nil {
			errs = append(errs, fmt.Errorf("copying 7-Zip tools: %w", err))
		}
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
func syncDriversTo(letter string, onProgress func(CopyProgress)) error {
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
		if err := copyTreeMerge(driversDest, src, onProgress); err != nil {
			errs = append(errs, fmt.Errorf("copying Drivers: %w", err))
		}
	}
	if err := postSyncDriversHook(driversDest); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
