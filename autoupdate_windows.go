package main

import (
	"sync"
	"time"
)

// Automatic update checks (Settings > About's "check automatically"
// checkboxes) - the Windows half: both PDT's own update and the bundled
// 7-Zip's, since both self-update mechanisms are Windows-only here (7-Zip
// has no bundled-tool equivalent on macOS at all). See autoupdate_darwin.go
// for the macOS half, PDT's own update only.

var autoUpdateOnce sync.Once

// AutoUpdateChecks runs whichever automatic update checks are due - the
// frontend calls it once at startup, after its own listeners are wired, and
// shows whatever it reports. It's pull-based (the frontend asks, rather than
// the backend pushing an event) so a fast check can't finish before the
// frontend is ready to hear about it. Only the first call in a process does
// anything, so a webview reload can't re-check.
//
// Skipped entirely when this exact PDT.exe runs from a removable (flash)
// drive: a portable copy is deliberately frozen, and a technician plugging it
// into a customer's machine shouldn't have it phoning home, or offering to
// overwrite itself on read-only media.
//
// A check's time is recorded only when its lookup actually succeeded, so a
// failed one (no internet at a customer site) is retried at the next launch
// instead of waiting out a whole Weekly/Monthly interval.
func (a *App) AutoUpdateChecks() AutoUpdateResult {
	var out AutoUpdateResult
	autoUpdateOnce.Do(func() { out = a.runAutoUpdateChecks() })
	return out
}

func (a *App) runAutoUpdateChecks() AutoUpdateResult {
	<-a.ready
	var out AutoUpdateResult
	if a.IsRunningFromRemovableDrive() {
		return out
	}

	now := time.Now()
	state := loadUpdateCheckState()
	pdtDue, sevenZipDue := dueUpdateChecks(a.settings, state, now)

	var wg sync.WaitGroup
	if pdtDue {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out.PDT = AutoUpdateOutcome{Checked: true, Result: a.CheckForUpdate()}
		}()
	}
	if sevenZipDue {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out.SevenZip = AutoUpdateOutcome{Checked: true, Result: a.CheckSevenZipUpdate()}
		}()
	}
	wg.Wait()

	// LatestVersion is only set once the lookup itself succeeded (see
	// CheckForUpdate/CheckSevenZipUpdate) - Error alone can't tell "couldn't
	// reach the server" from "found an update but no downloadable asset".
	recorded := false
	if out.PDT.Checked && out.PDT.Result.LatestVersion != "" {
		state.PDT, recorded = now, true
	}
	if out.SevenZip.Checked && out.SevenZip.Result.LatestVersion != "" {
		state.SevenZip, recorded = now, true
	}
	if recorded {
		_ = saveUpdateCheckState(state) // best-effort: a failed write only means one extra check next launch
	}
	return out
}
