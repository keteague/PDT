package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"PDT/internal/config"
	"PDT/internal/driver"
	"PDT/internal/printer"
	pdtwin "PDT/internal/printer/windows"
	"PDT/internal/update"
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

	catalog    driver.Catalog
	modelIndex map[string]map[string][]string
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

	// Best-effort cleanup of a previous update's renamed-aside old exe (see
	// internal/update.Apply) - by the time this process is running at all,
	// whatever process left that file behind has necessarily already exited.
	if exe, err := os.Executable(); err == nil {
		update.CleanupOldExe(exe)
	}

	ensureSevenZipExtracted()

	// A freshly-installed copy's Drivers folder (installedAppDataDir(),
	// picked by defaultDriversBasePath() when there's no portable copy's
	// Drivers folder to inherit) starts out completely empty - scaffold the
	// standard manufacturer subfolders so the Defaults panel's own
	// Manufacturer dropdown isn't just blank on first launch, and so
	// there's an obvious, ready-to-use place to drop driver packages into.
	// A no-op once anything already exists there (see its own doc comment).
	_ = ensureDriversScaffold(driversRoot())

	catalog, err := driver.BuildCatalog(driversRoot())
	if err != nil {
		a.catalogErr = err
		close(a.ready)
		return
	}
	a.catalog = catalog
	a.modelIndex = driver.BuildModelIndex(catalog)
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
		DefaultDirectory:     currentPath,
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

// OpenRepoURL opens this project's GitHub page in the system default
// browser - the About tab's repo link.
func (a *App) OpenRepoURL() {
	runtime.BrowserOpenURL(a.ctx, appRepoURL)
}

// updateAssetName is the exact GitHub release asset name CheckForUpdate looks
// for - the release process for this app is to build with `wails build` and
// upload build/bin/PDT.exe under this same name to each GitHub release.
const updateAssetName = "PDT.exe"

// repoSlug is appRepoURL in GitHub API "owner/name" form.
func repoSlug() string {
	return strings.TrimPrefix(appRepoURL, "https://github.com/")
}

// UpdateCheckResult is CheckForUpdate's outcome.
type UpdateCheckResult struct {
	Available      bool   `json:"available"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	ReleaseURL     string `json:"releaseUrl"`
	AssetURL       string `json:"assetUrl"`
	Error          string `json:"error"`
}

// CheckForUpdate queries this project's GitHub Releases for a version newer
// than AppVersion - Settings > About's "Check for Updates" button. Comparison
// is numeric (driver.CompareVersions - a generic dot-separated numeric
// comparator despite living in the driver package, already exported for
// exactly this kind of reuse outside it), not string equality, so "0.1.0"
// isn't mistaken for older than "0.1.0" due to formatting. AssetURL is left
// empty (with Available still true) if the matching release has no
// updateAssetName asset to download - a release published without one is a
// process mistake worth surfacing, not silently ignoring.
func (a *App) CheckForUpdate() UpdateCheckResult {
	rel, err := update.FetchLatest(repoSlug())
	if err != nil {
		return UpdateCheckResult{CurrentVersion: AppVersion, Error: err.Error()}
	}

	latest := strings.TrimPrefix(rel.TagName, "v")
	result := UpdateCheckResult{CurrentVersion: AppVersion, LatestVersion: latest, ReleaseURL: rel.HTMLURL}
	if driver.CompareVersions(latest, AppVersion) > 0 {
		result.Available = true
		if asset := rel.Asset(updateAssetName); asset != nil {
			result.AssetURL = asset.DownloadURL
		} else {
			result.Error = fmt.Sprintf("release %s has no %s asset to download", rel.TagName, updateAssetName)
		}
	}
	return result
}

// ApplyUpdateResult is ApplyUpdate's outcome. Error is "" on success, in
// which case the app has already relaunched itself and this process is about
// to quit - there is nothing further for the frontend to do either way.
type ApplyUpdateResult struct {
	Error string `json:"error"`
}

// ApplyUpdate downloads assetURL (from a prior CheckForUpdate result),
// installs it in place of the running executable, relaunches it, and quits
// this process - see internal/update's doc comment for how replacing a
// running .exe works on Windows with no separate installer. Runs inline with
// no progress reporting since it's one small exe download, not a
// multi-minute operation like Deploy.
func (a *App) ApplyUpdate(assetURL string) ApplyUpdateResult {
	exePath, err := os.Executable()
	if err != nil {
		return ApplyUpdateResult{Error: err.Error()}
	}
	tmpPath, err := update.Download(assetURL, exePath)
	if err != nil {
		return ApplyUpdateResult{Error: err.Error()}
	}
	if err := update.Apply(exePath, tmpPath); err != nil {
		return ApplyUpdateResult{Error: err.Error()}
	}
	if err := exec.Command(exePath).Start(); err != nil {
		return ApplyUpdateResult{Error: "update installed, but failed to relaunch: " + err.Error()}
	}
	runtime.Quit(a.ctx)
	return ApplyUpdateResult{}
}

// currentDriversBasePath/currentConfigsBasePath cache the live Settings
// values driversRoot()/configsRoot() below actually resolve to - set once
// from a.settings in startup(), and refreshed in SaveSettings() so a change
// takes effect immediately for anything that resolves its folder mid-
// session (DEVMODE capture, Export Configs, Write to Flash Drive). The
// driver *catalog* itself is the one exception: BuildCatalog only ever runs
// once, at startup, so a changed DriversBasePath needs a PDT restart to
// actually rescan the new location - Settings' own tooltip says so.
var currentDriversBasePath string
var currentConfigsBasePath string

// driversRoot returns the Drivers folder PDT actually uses - Settings'
// "Drivers Base Path", defaulting to defaultDriversBasePath() if that's
// somehow still unset (shouldn't happen; loadSettings always seeds it).
func driversRoot() string {
	if currentDriversBasePath != "" {
		return currentDriversBasePath
	}
	return defaultDriversBasePath()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// configsRoot returns the Configs folder PDT actually uses - Settings'
// "Configuration Files Base Path", defaulting to defaultSaveFileBasePath()
// if somehow still unset.
func configsRoot() string {
	if currentConfigsBasePath != "" {
		return currentConfigsBasePath
	}
	return defaultSaveFileBasePath()
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

func (a *App) GetCatalogStatus() CatalogStatus {
	<-a.ready
	hasDrivers := len(driver.ManufacturersWithDrivers(a.catalog)) > 0
	if a.catalogErr != nil {
		return CatalogStatus{OK: false, Error: a.catalogErr.Error(), HasDrivers: hasDrivers}
	}
	return CatalogStatus{OK: true, HasDrivers: hasDrivers}
}

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

// Models lists the known models for manufacturer (Kyocera only - other
// manufacturers' driver names aren't model-specific; see driver.ModelFromDriverName).
func (a *App) Models(manufacturer string) []string {
	<-a.ready
	byModel, ok := a.modelIndex[manufacturer]
	if !ok {
		return nil
	}
	models := make([]string, 0, len(byModel))
	for m := range byModel {
		models = append(models, m)
	}
	sort.Strings(models)
	return models
}

// DriverCandidates lists selectable driver labels for a row's dropdown -
// plain names, or decorated "<name> (vVersion - date)" labels when more than
// one arch-compatible local version exists. filterText fuzzy-matches and
// re-ranks when non-empty (free-text typing in the dropdown).
func (a *App) DriverCandidates(manufacturer, model, filterText string) []string {
	<-a.ready
	return driver.Candidates(a.catalog, a.modelIndex, manufacturer, model, filterText)
}

// DefaultDriverFor is the Defaults panel's pre-selected driver name for
// manufacturer (e.g. Canon -> its UFR II driver), or "" if there's no such
// rule for manufacturer or no matching driver is present locally.
func (a *App) DefaultDriverFor(manufacturer string) string {
	<-a.ready
	return driver.DefaultDriverNameFor(a.catalog, manufacturer)
}

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
		DefaultDirectory: a.settings.SaveFileBasePath,
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
		DefaultDirectory: a.settings.SaveFileBasePath,
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

	deployer := pdtwin.NewDeployer(a.catalog, configsRoot())
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
