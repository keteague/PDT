package main

import (
	"fmt"
	"sort"

	"PDT/internal/driver"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// catalogLogEvent is the Wails event name the frontend's own Log panel
// listens for (main.js: EventsOn('catalog-log', ...)) - the channel a
// background catalog task (today: the OpenPrinting nickname cache - see
// buildOpenPrintingCatalogAsync) uses to append lines to the Log panel from
// a goroutine that runs after a.ready has already closed, with no
// synchronous bound-method return value to piggyback on the way
// RefreshDriverCatalog's own CatalogStatus does.
const catalogLogEvent = "catalog-log"

// CatalogLogEntry is one catalogLogEvent payload - Level matches logStatus's
// own first argument in main.js (e.g. "INFO", "OK"), Text is the
// already-composed message.
type CatalogLogEntry struct {
	Level string `json:"level"`
	Text  string `json:"text"`
}

// logCatalog always emits, regardless of the Verbose checkbox - GitHub
// issue #16 follow-up (2026-09-19): Ken's own explicit ask, once he learned
// the OpenPrinting cataloging step runs in the background, was for a plain
// "this is happening / this finished" log entry - separate from (and never
// gated by) the *extra* per-manufacturer/per-file detail verboseNormal/
// verboseDebug control below.
func (a *App) logCatalog(level, text string) {
	if a.ctx == nil {
		return // no live frontend to emit to (e.g. a unit test constructing *App directly)
	}
	runtime.EventsEmit(a.ctx, catalogLogEvent, CatalogLogEntry{Level: level, Text: text})
}

// verboseNormal/verboseDebug gate the toolbar's own Verbose checkbox +
// Normal/Debug combobox (GitHub issue #16 follow-up, 2026-09-19) - see
// Settings.VerboseLoggingDisabled/LogLevel's own doc comments. Debug implies
// Normal's own detail too (a strictly more verbose mode, not a separate
// unrelated stream): a caller that wants "at least Normal" detail checks
// verboseNormal() alone and gets it under Debug too; a caller that wants
// Debug-only detail checks verboseDebug() alone.
func (a *App) verboseNormal() bool {
	return !a.settings.VerboseLoggingDisabled
}
func (a *App) verboseDebug() bool {
	return !a.settings.VerboseLoggingDisabled && a.settings.LogLevel == "Debug"
}

// buildOpenPrintingCatalogAsync runs driver.BuildOpenPrintingNickNames and
// folds its result into catalog.OpenPrintingNickNames under catalogMu,
// logging along the way (GitHub issue #16 follow-up, 2026-09-19) - shared by
// both platforms' own loadCatalog: app_windows.go runs this from its own
// already-backgrounded mac-catalog goroutine; app_darwin.go spawns a
// dedicated goroutine for it, since darwin's own loadCatalog is otherwise
// entirely synchronous (blocks a.ready) and this shouldn't add to that.
//
// Callers must never invoke this at all when running from removable media -
// Ken's own explicit ask: this shouldn't run when PDT is running from a
// flash drive, not just skip its own disk write the way persist=false
// elsewhere in this codebase does. A technician's field flash drive is
// exactly the slow-storage, minimize-startup-work scenario this project
// already treats specially everywhere else (driver.BuildCatalogNoExtract,
// the same reasoning) - both call sites gate the call itself on
// !isRemovable, not just pass persist=false through.
func (a *App) buildOpenPrintingCatalogAsync(catalog driver.MacCatalog, macRoot string) {
	mfgCount := 0
	for _, paths := range catalog.OpenPrintingPPDs {
		if len(paths) > 0 {
			mfgCount++
		}
	}
	if mfgCount == 0 {
		return // nothing to catalog at all - no log noise for a Drivers folder with no OpenPrinting content
	}

	a.logCatalog("INFO", "Checking OpenPrinting catalog...")
	nickNames, reports := driver.BuildOpenPrintingNickNames(catalog, macRoot, true)

	for _, r := range reports {
		if !a.verboseNormal() {
			continue
		}
		verb := "Checking"
		if r.WasNew {
			verb = "Creating"
		}
		switch {
		case len(r.Added) == 0 && len(r.Removed) == 0:
			a.logCatalog("INFO", fmt.Sprintf("%s OpenPrinting catalog for %s: up to date (%d cached).", verb, r.Manufacturer, r.Unchanged))
		default:
			a.logCatalog("INFO", fmt.Sprintf("%s OpenPrinting catalog for %s: %d new/changed, %d removed, %d unchanged.", verb, r.Manufacturer, len(r.Added), len(r.Removed), r.Unchanged))
		}
		if !a.verboseDebug() {
			continue
		}
		added := make([]string, 0, len(r.Added))
		for path := range r.Added {
			added = append(added, path)
		}
		sort.Strings(added)
		for _, path := range added {
			nick := r.Added[path]
			if nick == "" {
				nick = "(no *NickName/*ModelName found)"
			}
			a.logCatalog("INFO", fmt.Sprintf("  + %s -> %s", path, nick))
		}
		for _, path := range r.Removed {
			a.logCatalog("INFO", fmt.Sprintf("  - %s (no longer present)", path))
		}
	}

	a.catalogMu.Lock()
	a.macCatalog.OpenPrintingNickNames = nickNames
	a.catalogMu.Unlock()

	a.logCatalog("OK", "OpenPrinting catalog check complete.")
}
