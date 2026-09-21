package main

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"PDT/internal/driver"
	"PDT/internal/openprinting"
)

// Events the OpenPrinting sync dialog listens for, in the order they occur:
// "listing" while the site's folders are checked, one "plan" naming
// everything about to be downloaded, then "file" (one file's bytes) and
// "total" (the whole batch, with rate and ETA) as data arrives.
const (
	openPrintingListingEvent = "openprinting-sync-listing"
	openPrintingPlanEvent    = "openprinting-sync-plan"
	openPrintingFileEvent    = "openprinting-sync-file"
	openPrintingTotalEvent   = "openprinting-sync-total"
)

// OpenPrintingListing is openPrintingListingEvent's payload.
type OpenPrintingListing struct {
	Manufacturer string `json:"manufacturer"`
}

// OpenPrintingPlanFile is one entry of OpenPrintingPlan.
type OpenPrintingPlanFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// OpenPrintingPlan is openPrintingPlanEvent's payload.
type OpenPrintingPlan struct {
	Files      []OpenPrintingPlanFile `json:"files"`
	TotalBytes int64                  `json:"totalBytes"`
}

// OpenPrintingFileProgress is openPrintingFileEvent's payload.
type OpenPrintingFileProgress struct {
	Path  string `json:"path"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

// OpenPrintingTotalProgress is openPrintingTotalEvent's payload.
type OpenPrintingTotalProgress struct {
	DoneFiles       int     `json:"doneFiles"`
	TotalFiles      int     `json:"totalFiles"`
	DoneBytes       int64   `json:"doneBytes"`
	TotalBytes      int64   `json:"totalBytes"`
	RateBytesPerSec float64 `json:"rateBytesPerSec"`
	EtaSeconds      int     `json:"etaSeconds"`
}

// OpenPrintingSyncResult is what SyncOpenPrintingPPDs returns.
type OpenPrintingSyncResult struct {
	Downloaded int      `json:"downloaded"`
	Skipped    int      `json:"skipped"`
	Failed     int      `json:"failed"`
	Errors     []string `json:"errors"`
	Canceled   bool     `json:"canceled"`
	Error      string   `json:"error"`
}

// ppdSync guards the single in-flight OpenPrinting sync and its cancel func.
var ppdSync struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

// SyncOpenPrintingPPDs mirrors the OpenPrinting PPD library
// (https://www.openprinting.org/download/PPD/) into this computer's
// Drivers/macOS/OpenPrinting/<Manufacturer>/ folders for every manufacturer
// PDT supports, keeping each file's original modification time and skipping
// whatever is already current. It downloads only - nothing local is deleted -
// and a canceled or failed transfer never leaves a partial file behind (see
// openprinting.download). Callers should refresh the driver catalog
// afterwards (RefreshDriverCatalog) so the new PPDs show up as macOS
// driver/model candidates.
func (a *App) SyncOpenPrintingPPDs() OpenPrintingSyncResult {
	<-a.ready
	var out OpenPrintingSyncResult

	ppdSync.mu.Lock()
	if ppdSync.cancel != nil {
		ppdSync.mu.Unlock()
		out.Errors = []string{}
		out.Error = "an OpenPrinting sync is already running"
		return out
	}
	ctx, cancel := context.WithCancel(a.ctx)
	ppdSync.cancel = cancel
	ppdSync.mu.Unlock()
	defer func() {
		ppdSync.mu.Lock()
		ppdSync.cancel = nil
		ppdSync.mu.Unlock()
		cancel()
	}()

	var (
		est        etaEstimator
		lastTotal  time.Time
		lastFile   = map[string]time.Time{}
		estStarted bool
		progMu     sync.Mutex // guards the estimator/maps below
	)
	res, err := openprinting.Sync(ctx, openprinting.Options{
		Manufacturers: driver.Manufacturers,
		DestRoot:      filepath.Join(driversRoot(), "macOS", "OpenPrinting"),
		Progress: func(p openprinting.Progress) {
			progMu.Lock()
			defer progMu.Unlock()
			now := time.Now()
			switch p.Phase {
			case "listing":
				runtime.EventsEmit(a.ctx, openPrintingListingEvent, OpenPrintingListing{Manufacturer: p.Manufacturer})
			case "plan":
				files := make([]OpenPrintingPlanFile, len(p.Plan))
				for i, f := range p.Plan {
					files[i] = OpenPrintingPlanFile{Path: f.Path, Size: f.Size}
				}
				estStarted = false
				runtime.EventsEmit(a.ctx, openPrintingPlanEvent, OpenPrintingPlan{Files: files, TotalBytes: p.TotalBytes})
			case "transfer":
				if !estStarted {
					estStarted = true
					est.reset(now, 0)
				}
				eta := est.sample(now, p.DoneBytes, p.TotalBytes)
				finalFile := p.FileTotal > 0 && p.FileDone >= p.FileTotal
				if finalFile || now.Sub(lastFile[p.File]) >= 150*time.Millisecond {
					lastFile[p.File] = now
					runtime.EventsEmit(a.ctx, openPrintingFileEvent, OpenPrintingFileProgress{Path: p.File, Done: p.FileDone, Total: p.FileTotal})
				}
				allDone := p.DoneFiles == p.TotalFiles
				if allDone || now.Sub(lastTotal) >= 150*time.Millisecond {
					lastTotal = now
					rate := est.rate()
					if allDone {
						eta, rate = 0, 0
					}
					runtime.EventsEmit(a.ctx, openPrintingTotalEvent, OpenPrintingTotalProgress{
						DoneFiles: p.DoneFiles, TotalFiles: p.TotalFiles, DoneBytes: p.DoneBytes, TotalBytes: p.TotalBytes,
						RateBytesPerSec: rate, EtaSeconds: eta,
					})
				}
			}
		},
	})
	out.Downloaded, out.Skipped, out.Failed, out.Errors = res.Downloaded, res.Skipped, res.Failed, res.Errors
	if out.Errors == nil {
		out.Errors = []string{}
	}
	switch {
	case errors.Is(err, context.Canceled):
		out.Canceled = true
	case err != nil:
		out.Error = err.Error()
	}
	return out
}

// CancelOpenPrintingSync stops a running SyncOpenPrintingPPDs (a no-op when
// none is running). Files already downloaded are kept; the one(s) in flight
// are discarded, never left half-written.
func (a *App) CancelOpenPrintingSync() {
	ppdSync.mu.Lock()
	cancel := ppdSync.cancel
	ppdSync.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// OpenOpenPrintingPPDPage opens the OpenPrinting PPD library in the user's
// browser (the Settings > OpenPrinting tab's link).
func (a *App) OpenOpenPrintingPPDPage() {
	runtime.BrowserOpenURL(a.ctx, openprinting.DefaultBaseURL)
}
