package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows/registry"

	"PDT/internal/driver"
	"PDT/internal/update"
)

// PDT's own self-update mechanism (About tab's Check for Updates/Update Now)
// - Windows-only for now (see this port's own "explicitly out of scope"
// list): replacing a running .exe in place by renaming it aside
// (internal/update.Apply) has no macOS analog, and a real mac self-update
// would mean replacing the whole .app bundle - its own design pass, not
// attempted here.

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
// installs it in place of the running executable, best-effort updates the
// Inno Setup uninstall entry's DisplayVersion to newVersion (see
// updateInstalledVersionInRegistry), relaunches, and quits this process -
// see internal/update's doc comment for how replacing a running .exe works
// on Windows with no separate installer. Runs inline with no progress
// reporting since it's one small exe download, not a multi-minute operation
// like Deploy.
func (a *App) ApplyUpdate(assetURL, newVersion string) ApplyUpdateResult {
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
	updateInstalledVersionInRegistry(newVersion)
	if err := exec.Command(exePath).Start(); err != nil {
		return ApplyUpdateResult{Error: "update installed, but failed to relaunch: " + err.Error()}
	}
	runtime.Quit(a.ctx)
	return ApplyUpdateResult{}
}

// innoSetupUninstallKeyPath is where pdt.iss's own [Setup] AppId
// (`{40FB3E79-C3DC-4C78-A969-35012251BD36}`, braces included - Inno Setup's
// own "{{" in that script is its escape for a literal "{") ends up under
// Uninstall - Inno Setup always names its own uninstall registry key
// "<AppId>_is1". Kept in sync with pdt.iss by hand; nothing enforces this
// automatically, so if that GUID is ever regenerated, this needs updating
// too.
const innoSetupUninstallKeyPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\{40FB3E79-C3DC-4C78-A969-35012251BD36}_is1`

// updateInstalledVersionInRegistry best-effort updates the Inno Setup
// uninstall entry's DisplayVersion to newVersion after a successful
// self-update (ApplyUpdate) - without this, Windows' own Programs and
// Features / appwiz.cpl keeps showing whatever version was last actually
// installed, even though the running exe underneath it is now newer;
// confirmed live that appwiz.cpl's own Version column only ever reflects
// this one registry value; it has no idea the exe itself changed. Tries both
// CURRENT_USER (an unelevated install) and LOCAL_MACHINE (an elevated one)
// since exactly one will actually have this key - PDT itself always runs
// elevated regardless of which mode it was installed under (see the
// README's own elevation note), so it can reach whichever hive actually has
// it. A portable/flash-drive copy - never installed via the Inno Setup
// installer at all - has neither key; silently a no-op there, not an error,
// same reasoning as every other best-effort step in ApplyUpdate/update.Apply.
func updateInstalledVersionInRegistry(newVersion string) {
	setRegistryDisplayVersion(registry.CURRENT_USER, innoSetupUninstallKeyPath, newVersion)
	setRegistryDisplayVersion(registry.LOCAL_MACHINE, innoSetupUninstallKeyPath, newVersion)
}

// setRegistryDisplayVersion sets path's DisplayVersion value to newVersion
// under root, silently doing nothing if path doesn't exist there (wrong
// hive for how this copy was installed, or a portable copy with no
// uninstall entry at all) or can't be written to. Split out from
// updateInstalledVersionInRegistry so it can be unit-tested directly against
// a throwaway key rather than this project's own real uninstall entry.
func setRegistryDisplayVersion(root registry.Key, path, newVersion string) {
	k, err := registry.OpenKey(root, path, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.SetStringValue("DisplayVersion", newVersion)
}
