package main

import (
	"sync"
	"time"
)

// Automatic update checks (Settings > About's "check automatically"
// checkbox) - the macOS half, PDT's own update only. See
// autoupdate_windows.go for the Windows half, which also covers the bundled
// 7-Zip tool - macOS has no equivalent bundled dependency to check, so
// AutoUpdateResult.SevenZip is always left at its zero value (Checked:
// false) here rather than ever being set.

var autoUpdateOnce sync.Once

// AutoUpdateChecks runs whichever automatic update checks are due - see
// autoupdate_windows.go's own AutoUpdateChecks doc comment for the shared
// "frontend calls this once at startup, pull-based, only the first call in a
// process does anything" reasoning, which applies identically here.
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
	pdtDue, _ := dueUpdateChecks(a.settings, state, now)
	if !pdtDue {
		return out
	}

	out.PDT = AutoUpdateOutcome{Checked: true, Result: a.CheckForUpdate()}
	// LatestVersion is only set once the lookup itself succeeded (see
	// CheckForUpdate) - Error alone can't tell "couldn't reach the server"
	// from "found an update but no downloadable asset" (see
	// autoupdate_windows.go's own identical comment).
	if out.PDT.Result.LatestVersion != "" {
		state.PDT = now
		_ = saveUpdateCheckState(state) // best-effort: a failed write only means one extra check next launch
	}
	return out
}
