package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"PDT/internal/config"
	"PDT/internal/driver"
	"PDT/internal/printer"
	pdtwin "PDT/internal/printer/windows"
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
}

func NewApp() *App {
	return &App{ready: make(chan struct{})}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.settings = loadSettings()

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
	if s.ManufacturerURLs == nil {
		s.ManufacturerURLs = map[string]string{}
	}
	for mfg, defaultURL := range defaultManufacturerURLs() {
		if s.ManufacturerURLs[mfg] == "" {
			s.ManufacturerURLs[mfg] = defaultURL
		}
	}
	if err := saveSettingsToDisk(s); err != nil {
		return Settings{}, err
	}
	a.settings = s
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

// driversRoot resolves the Drivers/ folder next to the running executable
// (matching the original tool's convention of Drivers/ next to the script) -
// falling back to ./Drivers under the working directory for `wails dev`,
// where the built binary lives under build/bin rather than the project root.
func driversRoot() string {
	if exe, err := os.Executable(); err == nil {
		if candidate := filepath.Join(filepath.Dir(exe), "Drivers"); dirExists(candidate) {
			return candidate
		}
	}
	return "Drivers"
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// CatalogStatus reports whether the driver catalog loaded at startup, and
// its error if not - the frontend surfaces this once on load rather than
// silently showing zero drivers with no explanation.
type CatalogStatus struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

func (a *App) GetCatalogStatus() CatalogStatus {
	<-a.ready
	if a.catalogErr != nil {
		return CatalogStatus{OK: false, Error: a.catalogErr.Error()}
	}
	return CatalogStatus{OK: true}
}

// Manufacturers is the fixed manufacturer list every row's dropdown offers.
func (a *App) Manufacturers() []string {
	return driver.Manufacturers
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
// earlier cancellation - completes. A failure to create one row's printer
// object is fatal for that row alone; the run always continues to the next
// row regardless (see internal/printer.DeployAllWithProgress).
func (a *App) Deploy(rows []printer.PrinterRow, salesChainID, portNamePrefix string) []DeployRowResult {
	<-a.ready
	reqs := make([]printer.DeployRequest, len(rows))
	for i, r := range rows {
		reqs[i] = printer.DeployRequest{Row: r, SalesChainID: salesChainID, PortNamePrefix: portNamePrefix}
	}

	deployer := pdtwin.NewDeployer(a.catalog)
	results := printer.DeployAllWithProgress(a.ctx, deployer, reqs, a.confirm, func(r printer.DeployResult) {
		runtime.EventsEmit(a.ctx, deployProgressEvent, toDeployRowResult(r))
	})

	out := make([]DeployRowResult, len(results))
	for i, r := range results {
		out[i] = toDeployRowResult(r)
	}
	return out
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
