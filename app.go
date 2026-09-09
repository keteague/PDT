package main

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"PDT/internal/config"
	"PDT/internal/driver"
	"PDT/internal/printer"
)

const deployProgressEvent = "deploy-progress"

// App is the Wails-bound backend: every exported method on *App is callable
// from the frontend. Wails' binding layer only supports a method returning
// (T) or (T, error) - never more outputs - and a bare Go `error` value
// doesn't JSON-marshal its message at all (most concrete error types have no
// exported fields), so every method below that can fail in a way the
// frontend needs to see (a canceled file dialog, a failed deploy row) wraps
// its result in a plain DTO instead of returning a second/third raw value.
type App struct {
	ctx context.Context

	// ready is closed once startup has finished populating catalog/
	// modelIndex/settings below. Every method that reads them blocks on it
	// first (<-a.ready) - found necessary in practice, not just defensive:
	// Wails does not actually block the frontend's own script from running
	// until OnStartup returns, and BuildCatalog scanning a real Drivers
	// folder (particularly with zip extraction - see
	// driver.ensureZipsExtracted) easily takes longer than the frontend
	// needs to fire its first catalog-dependent call, which would otherwise
	// silently see catalog/modelIndex still at their nil zero value.
	ready chan struct{}

	// catalogMu guards catalog/modelIndex/macCatalog/catalogErr below. Built
	// once at startup and read-only from then on for most of this app's
	// life, but RefreshDriverCatalog (the toolbar's Refresh button) can now
	// replace them live, without restarting PDT - a real lock, not just the
	// one-time <-a.ready happens-before startup already relied on, is what
	// keeps that safe against a DriverCandidates/Deploy call landing at the
	// same moment.
	//
	// catalog/modelIndex (Windows' .inf-driver-name-shaped catalog, plus its
	// Kyocera model index) and macCatalog (macOS' installer-package-shaped
	// catalog - internal/driver.MacCatalog) both live on every build of this
	// struct, but only the one this platform's own loadCatalog
	// (app_windows.go/app_darwin.go) actually populates is ever non-zero -
	// the same way internal/printer/windows and internal/printer/darwin are
	// two packages that never both link into the same binary.
	catalogMu  sync.RWMutex
	catalog    driver.Catalog
	modelIndex map[string]map[string][]string
	macCatalog driver.MacCatalog
	catalogErr error
	settings   Settings

	// deployCancel is set for the duration of a running Deploy call (nil
	// otherwise) - guarded by deployMu since StopDeploy can be called from a
	// different goroutine than the one running Deploy itself. Canceling it
	// is exactly what printer.DeployAllWithProgress already documents as its
	// only early-exit path: the row currently in progress still finishes
	// (safer than aborting a printer/port/driver change half-applied), but
	// no further row starts.
	deployMu     sync.Mutex
	deployCancel context.CancelFunc
}

func NewApp() *App {
	return &App{ready: make(chan struct{})}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.settings = loadSettings()
	currentDriversBasePath = a.settings.DriversBasePath
	currentConfigsBasePath = a.settings.SaveFileBasePath

	// Whatever this platform needs done once, before the driver catalog is
	// scanned (Windows: 7-Zip extraction, stale-update cleanup, the Drivers
	// scaffold; macOS: nothing yet - see app_windows.go/app_darwin.go).
	a.platformStartup()

	if err := a.loadCatalog(driversRoot()); err != nil {
		a.catalogMu.Lock()
		a.catalogErr = err
		a.catalogMu.Unlock()
		close(a.ready)
		return
	}
	close(a.ready)
}

// GetSettings returns the current persisted preferences.
func (a *App) GetSettings() Settings {
	<-a.ready
	return a.settings
}

// SaveSettings persists s to disk and makes it the app's current settings.
// An empty SaveFileBasePath, or an empty URL for any manufacturer, is
// replaced with its own default rather than saved as literally empty, so
// clearing a field and saving can't leave a file dialog with no starting
// directory, or "Check for Updates" with nowhere to go.
func (a *App) SaveSettings(s Settings) (Settings, error) {
	<-a.ready
	if s.SaveFileBasePath == "" {
		s.SaveFileBasePath = defaultSaveFileBasePath()
	}
	if s.DriversBasePath == "" {
		s.DriversBasePath = defaultDriversBasePath()
	}
	if s.PreinstallBasePath == "" {
		s.PreinstallBasePath = defaultPreinstallBasePath()
	}
	if s.ManufacturerURLs == nil {
		s.ManufacturerURLs = map[string]string{}
	}
	for mfg, defaultURL := range defaultManufacturerURLs() {
		if s.ManufacturerURLs[mfg] == "" {
			s.ManufacturerURLs[mfg] = defaultURL
		}
	}
	s.ManufacturerOrder = reconcileManufacturerOrder(s.ManufacturerOrder)
	if err := saveSettingsToDisk(s); err != nil {
		return Settings{}, err
	}
	a.settings = s
	currentDriversBasePath = s.DriversBasePath
	currentConfigsBasePath = s.SaveFileBasePath
	return a.settings, nil
}

// PickFolder prompts for a directory, starting from currentPath, for the
// Settings panel's "Save File Base Path" browse button.
func (a *App) PickFolder(currentPath string) (PathResult, error) {
	path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                "Select Save File Base Path",
		DefaultDirectory:     resolveExeRelative(currentPath),
		CanCreateDirectories: true,
	})
	if err != nil || path == "" {
		return PathResult{Canceled: path == ""}, err
	}
	return PathResult{Path: path}, nil
}

// OpenManufacturerURL opens manufacturer's configured External Sites URL
// (Settings) in the system default browser - the "Check for Updates" button.
// No vendor exposes an API to actually check the latest driver version, so
// this only ever hands the page to a human to look at themselves.
func (a *App) OpenManufacturerURL(manufacturer string) {
	<-a.ready
	url := a.settings.ManufacturerURLs[manufacturer]
	if url == "" {
		return
	}
	runtime.BrowserOpenURL(a.ctx, url)
}

// AppInfo is Settings' About tab content.
type AppInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Author  string `json:"author"`
	RepoURL string `json:"repoUrl"`
}

func (a *App) GetAppInfo() AppInfo {
	return AppInfo{Name: appDisplayName, Version: AppVersion, Author: appAuthor, RepoURL: appRepoURL}
}

// Platform reports this build's OS ("windows" or "darwin") - the frontend's
// one feature-detection signal for showing/hiding platform-specific UI
// (Spooler/Flash Drive/DEVMODE capture/SNMP-port fields on Windows; a
// simpler grid shape with no Driver combobox on macOS - see
// frontend/src/main.js).
func (a *App) Platform() string {
	return goruntime.GOOS
}

// OpenRepoURL opens this project's GitHub page in the system default
// browser - the About tab's repo link.
func (a *App) OpenRepoURL() {
	runtime.BrowserOpenURL(a.ctx, appRepoURL)
}

// currentDriversBasePath/currentConfigsBasePath cache the live Settings
// values driversRoot()/configsRoot() below actually resolve to - set once
// from a.settings in startup(), and refreshed in SaveSettings() so a change
// takes effect immediately for anything that resolves its folder mid-
// session (DEVMODE capture, Export Configs, Write to Flash Drive). The
// driver *catalog* itself only rescans when explicitly asked to - at
// startup, or via the toolbar's Refresh button (RefreshDriverCatalog) - so a
// changed DriversBasePath needs one of those two before it's actually
// reflected, not automatically on save; Settings' own tooltip says so.
var currentDriversBasePath string
var currentConfigsBasePath string

// driversRoot returns the Drivers folder PDT actually uses - Settings'
// "Drivers Base Path", defaulting to defaultDriversBasePath() if that's
// somehow still unset (shouldn't happen; loadSettings always seeds it) -
// resolved against this exe's own current location if it's a relative path
// (see resolveExeRelative).
func driversRoot() string {
	if currentDriversBasePath != "" {
		return resolveExeRelative(currentDriversBasePath)
	}
	return resolveExeRelative(defaultDriversBasePath())
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// configsRoot returns the Configs folder PDT actually uses - Settings'
// "Configuration Files Base Path", defaulting to defaultSaveFileBasePath()
// if somehow still unset - resolved against this exe's own current location
// if it's a relative path (see resolveExeRelative).
func configsRoot() string {
	if currentConfigsBasePath != "" {
		return resolveExeRelative(currentConfigsBasePath)
	}
	return resolveExeRelative(defaultSaveFileBasePath())
}

// resolveExeRelative resolves a relative Settings base path (".\Drivers",
// ".\Configs" - see defaultDriversBasePath/defaultSaveFileBasePath's own
// portable-copy case) against the directory this exe is *currently* running
// from, rather than wherever it happened to be the last time Settings was
// saved. This is what actually makes a portable/flash-drive copy's
// DriversBasePath/SaveFileBasePath stay correct across computers and drive
// letters: an absolute path (an installed copy's %LocalAppData%\PDT\...,
// or anything a user explicitly picked via Browse) is returned unchanged,
// since only a relative one depends on "relative to what" in the first
// place. Every consumer of a Settings base path - driversRoot/configsRoot
// above, PickFolder's starting directory, Open/Save Configuration's own
// DefaultDirectory, OpenDriversBasePathInExplorer - goes through this so
// none of them accidentally resolve a relative path against the process's
// own current working directory instead (not guaranteed to be the exe's own
// directory, unlike what every double-click/shortcut launch happens to give
// it in practice).
func resolveExeRelative(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	exe, err := os.Executable()
	if err != nil {
		return path
	}
	return filepath.Join(filepath.Dir(exe), path)
}

// CatalogStatus reports whether the driver catalog loaded at startup, and
// its error if not - the frontend surfaces this once on load rather than
// silently showing zero drivers with no explanation. HasDrivers is a
// separate, non-error signal: a brand-new install's freshly-scaffolded
// Drivers folder loads just fine (OK: true) but legitimately has zero
// manufacturers with any driver package present yet - the frontend uses
// HasDrivers to show a first-run "go get some drivers" banner rather than
// treating that state as a load failure.
type CatalogStatus struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error"`
	HasDrivers bool   `json:"hasDrivers"`
}

// GetCatalogStatus, RefreshDriverCatalog: see drivercatalog_windows.go/
// drivercatalog_darwin.go - both platform-specific (Windows' driver.Catalog
// vs macOS' driver.MacCatalog).

// Manufacturers is every row's dropdown offers - the full list of
// manufacturers PDT knows about (same set as AllManufacturers), ordered per
// the user's own Settings > General drag/drop preference
// (a.settings.ManufacturerOrder) rather than AllManufacturers' fixed
// alphabetical order. Deliberately not filtered to manufacturers with a
// local driver present: a brand-new install has none yet, and still needs
// to offer every manufacturer here so the Defaults panel's "pick a
// Manufacturer, then Check for Updates" bootstrap workflow (see the
// no-drivers banner in init()) works before any driver has been downloaded.
// A manufacturer with nothing in the local Drivers folder simply offers no
// Driver candidates yet (see DriverCandidates) - not itself an error.
func (a *App) Manufacturers() []string {
	<-a.ready
	return applyManufacturerOrder(append([]string(nil), driver.Manufacturers...), a.settings.ManufacturerOrder)
}

// AllManufacturers is every manufacturer PDT knows about - the same set
// Manufacturers returns, just always alphabetical rather than the user's
// custom drag/drop order. Settings > External Sites uses this instead of
// Manufacturers since it's about finding a specific manufacturer's URL to
// edit, not deployment convenience.
func (a *App) AllManufacturers() []string {
	out := append([]string(nil), driver.Manufacturers...)
	sort.Strings(out)
	return out
}

// applyManufacturerOrder reorders items (already filtered to whatever's
// actually relevant - e.g. manufacturers with local drivers present) to match
// order's sequence. An item in items but not (yet) in order is appended at
// the end in items' own original order, so a manufacturer added after the
// user last customized their order doesn't just vanish from the dropdown.
func applyManufacturerOrder(items []string, order []string) []string {
	pos := make(map[string]int, len(order))
	for i, m := range order {
		pos[m] = i
	}
	out := append([]string(nil), items...)
	sort.SliceStable(out, func(i, j int) bool {
		pi, iok := pos[out[i]]
		pj, jok := pos[out[j]]
		switch {
		case iok && jok:
			return pi < pj
		case iok:
			return true
		default:
			return false
		}
	})
	return out
}

// Models, DriverCandidates, DefaultDriverFor: Windows-only bound methods -
// see drivercatalog_windows.go. All three are Driver-combobox concepts
// (a per-manufacturer driver-name index, fuzzy-ranked candidate labels, a
// pre-selected default driver name) with nothing analogous on macOS, where a
// manufacturer's driver package resolves automatically
// (internal/printer/darwin's deploy_darwin.go) and the frontend's mac row
// shape has a plain Model text field instead of a Driver combobox at all -
// not stubbed out here since nothing on a darwin build ever calls them.

// PathResult is a file dialog's outcome: Canceled is true (with Path empty)
// if the user dismissed the dialog without choosing a file.
type PathResult struct {
	Canceled bool   `json:"canceled"`
	Path     string `json:"path"`
}

var csvFilter = runtime.FileFilter{DisplayName: "CSV Files (*.csv)", Pattern: "*.csv"}
var jsonFilter = runtime.FileFilter{DisplayName: "JSON Files (*.json)", Pattern: "*.json"}

// NewCsvTemplate prompts for a save path and writes a blank CSV (header row
// only) there - the entire backend of the original tool's "New CSV" button.
func (a *App) NewCsvTemplate() (PathResult, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "New CSV",
		DefaultFilename: "printers.csv",
		Filters:         []runtime.FileFilter{csvFilter},
	})
	if err != nil || path == "" {
		return PathResult{Canceled: path == ""}, err
	}
	if err := config.WriteTemplate(path); err != nil {
		return PathResult{}, err
	}
	return PathResult{Path: path}, nil
}

// ImportResult is ImportCsv's outcome.
type ImportResult struct {
	Canceled bool                 `json:"canceled"`
	Rows     []printer.PrinterRow `json:"rows"`
}

// ImportCsv prompts for a CSV file and parses it into rows.
func (a *App) ImportCsv() (ImportResult, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Import CSV",
		Filters: []runtime.FileFilter{csvFilter},
	})
	if err != nil || path == "" {
		return ImportResult{Canceled: path == ""}, err
	}
	rows, err := config.ImportCSV(path)
	if err != nil {
		return ImportResult{}, err
	}
	return ImportResult{Rows: rows}, nil
}

// OpenConfigResult is OpenConfiguration's outcome.
type OpenConfigResult struct {
	Canceled bool               `json:"canceled"`
	Config   config.SavedConfig `json:"config"`
}

// OpenConfiguration prompts for a saved JSON configuration and loads it.
func (a *App) OpenConfiguration() (OpenConfigResult, error) {
	<-a.ready
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Open Configuration",
		DefaultDirectory: configsRoot(),
		Filters:          []runtime.FileFilter{jsonFilter},
	})
	if err != nil || path == "" {
		return OpenConfigResult{Canceled: path == ""}, err
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		return OpenConfigResult{}, err
	}
	return OpenConfigResult{Config: cfg}, nil
}

// SaveConfiguration prompts for a save path and writes cfg there as JSON.
// The suggested filename is cfg.SalesChainId (already restricted to
// filesystem-safe characters by the frontend's own SalesChain ID input
// validation) when set, falling back to a generic name otherwise.
func (a *App) SaveConfiguration(cfg config.SavedConfig) (PathResult, error) {
	<-a.ready
	defaultFilename := "printers.json"
	if cfg.SalesChainID != "" {
		defaultFilename = cfg.SalesChainID + ".json"
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:            "Save Configuration",
		DefaultDirectory: configsRoot(),
		DefaultFilename:  defaultFilename,
		Filters:          []runtime.FileFilter{jsonFilter},
	})
	if err != nil || path == "" {
		return PathResult{Canceled: path == ""}, err
	}
	if err := config.SaveConfig(path, cfg); err != nil {
		return PathResult{}, err
	}
	return PathResult{Path: path}, nil
}

// DeployRowResult is one row's outcome as sent to the frontend - Error is ""
// on success (see the App doc comment for why this isn't a bare Go error).
type DeployRowResult struct {
	RowName string   `json:"rowName"`
	Log     []string `json:"log"`
	Error   string   `json:"error"`
}

func toDeployRowResult(r printer.DeployResult) DeployRowResult {
	errText := ""
	if r.Err != nil {
		errText = r.Err.Error()
	}
	return DeployRowResult{RowName: r.RowName, Log: r.Log, Error: errText}
}

// Deploy runs the full Deploy sequence for every row in order, emitting a
// "deploy-progress" event with each row's DeployRowResult as it finishes (a
// single HP row alone can take several minutes even with the NUL: workaround
// declined, so the frontend needs live per-row updates, not just a final
// batch result) and also returning every result once the whole run - or an
// earlier cancellation via StopDeploy - completes. A failure to create one
// row's printer object is fatal for that row alone; the run always
// continues to the next row regardless (see
// internal/printer.DeployAllWithProgress).
func (a *App) Deploy(rows []printer.PrinterRow, salesChainID, portNamePrefix string) []DeployRowResult {
	<-a.ready
	reqs := make([]printer.DeployRequest, len(rows))
	for i, r := range rows {
		reqs[i] = printer.DeployRequest{Row: r, SalesChainID: salesChainID, PortNamePrefix: portNamePrefix}
	}

	setTitleBarBusy()
	defer resetTitleBarColor()

	// A cancellable child of a.ctx, not a.ctx itself - a.ctx lives for the
	// whole app, so canceling it directly would take down every other
	// in-flight Wails call along with this one deploy. StopDeploy only ever
	// touches this derived context.
	ctx, cancel := context.WithCancel(a.ctx)
	a.deployMu.Lock()
	a.deployCancel = cancel
	a.deployMu.Unlock()
	defer func() {
		a.deployMu.Lock()
		a.deployCancel = nil
		a.deployMu.Unlock()
		cancel()
	}()

	deployer := a.newPlatformDeployer()
	results := printer.DeployAllWithProgress(ctx, deployer, reqs, a.confirm, func(r printer.DeployResult) {
		runtime.EventsEmit(a.ctx, deployProgressEvent, toDeployRowResult(r))
	})

	out := make([]DeployRowResult, len(results))
	for i, r := range results {
		out[i] = toDeployRowResult(r)
	}
	return out
}

// StopDeploy cancels the currently-running Deploy, if any (a no-op
// otherwise) - the row already in progress still finishes rather than being
// torn down mid-step, since aborting a printer/port/driver change half-
// applied would risk leaving that one printer object in a broken,
// partially-configured state; no further row starts afterward.
func (a *App) StopDeploy() {
	a.deployMu.Lock()
	cancel := a.deployCancel
	a.deployMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ForceQuit immediately terminates this process - the emergency escape
// hatch for "PDT is locked up mid-deploy". A single Win32 call blocked
// forever (a hung driver install, a printer-object call that never returns)
// has no safe way to be canceled from Go once it's started - context
// cancellation only ever helps at a checkpoint the code itself checks
// between steps, which is exactly the checkpoint that's unreachable if the
// app is genuinely hung. Deliberately bypasses every graceful-shutdown path
// (Wails' own runtime.Quit, deferred cleanup, Go's finalizers) rather than
// attempting any of them first - if the hang is real, anything that assumes
// the app is still responsive enough to participate in its own shutdown
// could just as easily hang too. Whatever was mid-flight is abandoned
// exactly as if the process had been ended from Task Manager, because this
// is that, from inside the app instead of outside it.
func (a *App) ForceQuit() {
	os.Exit(1)
}

// confirm implements printer.Confirm via a native OS Yes/No message box -
// the same kind of blocking modal confirmation Create-Printers.ps1 used
// (a WinForms MessageBox), just via Wails' cross-platform equivalent.
func (a *App) confirm(_ context.Context, title, message string) (bool, error) {
	result, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         title,
		Message:       message,
		Buttons:       []string{"Yes", "No"},
		DefaultButton: "Yes",
		CancelButton:  "No",
	})
	if err != nil {
		return false, err
	}
	return result == "Yes", nil
}
