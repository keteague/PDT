package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// PreinstallFoldersResult is ListPreinstallFolders' outcome.
type PreinstallFoldersResult struct {
	Folders []string `json:"folders"`
	Error   string   `json:"error"`
}

// ListPreinstallFolders finds every subfolder directly under the configured
// Preinstall Base Path (Settings > General) whose name starts with
// "<salesChainID> - " - the "<SalesChainID> - <Client Company Name> -
// <Optional Street Address>" convention site-survey notes are filed under.
// More than one match (e.g. two site visits filed under the same SalesChain
// ID) is routine, not an error - ExportConfigs' frontend caller asks the
// tech which one to use when there's more than one.
func (a *App) ListPreinstallFolders(salesChainID string) PreinstallFoldersResult {
	<-a.ready
	base := a.settings.PreinstallBasePath
	folders, err := matchingPreinstallFolders(base, salesChainID)
	if err != nil {
		return PreinstallFoldersResult{Error: fmt.Sprintf("could not read Preinstall Base Path %q: %v", base, err)}
	}
	return PreinstallFoldersResult{Folders: folders}
}

// matchingPreinstallFolders lists every subfolder directly under base whose
// name starts with "<salesChainID> - ", returned as full paths. Parameterized
// on base (rather than reading a.settings itself) so this is testable
// against a t.TempDir() instead of the real, per-user Preinstall Base Path.
func matchingPreinstallFolders(base, salesChainID string) ([]string, error) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	prefix := salesChainID + " - "
	// []string{}, not "var folders []string" (a nil slice) - a nil slice
	// marshals to JSON `null`, and the frontend calls .length/.map() on this
	// result (listResult.folders) without a defensive `|| []` fallback, the
	// same class of bug fixed in ManufacturersWithDrivers (see its own
	// comment) - a SalesChain ID with no matching Preinstall subfolder at
	// all is a completely routine result here, not an error.
	folders := []string{}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			folders = append(folders, filepath.Join(base, e.Name()))
		}
	}
	sort.Strings(folders)
	return folders, nil
}

// matchingConfigFiles lists every file directly under dir (configsRoot() at
// every real call site; parameterized here so this pure-ish logic is
// testable against a t.TempDir() instead) whose name starts with
// salesChainID - covers both the saved JSON config itself
// (<SalesChainID>.json) and every captured DEVMODE/driver-data sidecar
// (<SalesChainID>-<PrinterName>...), matching the site-survey workflow's own
// "copy all <SalesChainID>* files" description.
func matchingConfigFiles(dir, salesChainID string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	// []string{}, not "var names []string" - see matchingPreinstallFolders'
	// own comment just above; CheckExportCollisions/ExportConfigs both
	// expose this as JSON to the frontend, which calls .length on it.
	names := []string{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), salesChainID) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// ExportCollisionResult is CheckExportCollisions' outcome.
type ExportCollisionResult struct {
	SourceFiles []string `json:"sourceFiles"`
	Colliding   []string `json:"colliding"`
	Error       string   `json:"error"`
}

// CheckExportCollisions previews an export: every Configs file that would be
// copied, and which of those already exist in destFolder's PDT subfolder -
// so the frontend only needs to ask Overwrite-vs-new-dated-subfolder when it
// would actually matter.
func (a *App) CheckExportCollisions(salesChainID, destFolder string) ExportCollisionResult {
	<-a.ready
	names, err := matchingConfigFiles(configsRoot(), salesChainID)
	if err != nil {
		return ExportCollisionResult{Error: err.Error()}
	}
	pdtDir := filepath.Join(destFolder, "PDT")
	// []string{}, not nil - see matchingPreinstallFolders' own comment above;
	// the frontend checks collisionResult.colliding.length directly.
	colliding := []string{}
	for _, name := range names {
		if fileExists(filepath.Join(pdtDir, name)) {
			colliding = append(colliding, name)
		}
	}
	return ExportCollisionResult{SourceFiles: names, Colliding: colliding}
}

// ExportResult is ExportConfigs' outcome.
type ExportResult struct {
	DestPath string   `json:"destPath"`
	Copied   []string `json:"copied"`
	Error    string   `json:"error"`
}

// ExportConfigs copies every Configs/<SalesChainID>* file to destFolder's PDT
// subfolder. mode "into" writes (and overwrites) directly into
// "<destFolder>\PDT"; mode "new" writes into a freshly timestamped
// "<destFolder>\PDT\<yyyy-mm-dd_hhmm>" instead, so declining an overwrite
// can't accidentally clobber anything.
func (a *App) ExportConfigs(salesChainID, destFolder, mode string) ExportResult {
	<-a.ready
	names, err := matchingConfigFiles(configsRoot(), salesChainID)
	if err != nil {
		return ExportResult{Error: err.Error()}
	}
	if len(names) == 0 {
		return ExportResult{Error: fmt.Sprintf("no Configs files found for SalesChain ID %q", salesChainID)}
	}

	destDir := filepath.Join(destFolder, "PDT")
	if mode == "new" {
		destDir = filepath.Join(destDir, time.Now().Format("2006-01-02_1504"))
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return ExportResult{Error: err.Error()}
	}

	srcDir := configsRoot()
	copied := make([]string, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(srcDir, name))
		if err != nil {
			return ExportResult{DestPath: destDir, Copied: copied, Error: fmt.Sprintf("reading %q: %v", name, err)}
		}
		if err := os.WriteFile(filepath.Join(destDir, name), data, 0o644); err != nil {
			return ExportResult{DestPath: destDir, Copied: copied, Error: fmt.Sprintf("writing %q: %v", name, err)}
		}
		copied = append(copied, name)
	}
	return ExportResult{DestPath: destDir, Copied: copied}
}
