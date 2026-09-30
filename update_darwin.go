package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"PDT/internal/driver"
	"PDT/internal/update"
)

// PDT's own self-update mechanism (About tab's Check for Updates/Update Now)
// - the macOS half. See update_windows.go for the Windows half and its own
// doc comment for how the two mechanisms differ: Windows swaps one running
// .exe file in place; this replaces the whole running PDT.app *bundle*
// (Info.plist, Resources, the executable itself - a directory tree, not a
// single file). Rather than build that from scratch, ApplyUpdate reuses
// macappflash.go's macAppSource/extractMacAppZip - originally built to stage
// a current PDT.app onto a flash drive from a technician's (often Windows)
// laptop, but the download-then-safely-swap-a-bundle-into-place mechanics
// are exactly what a self-update needs too, just retargeted at this app's
// own install location instead of a drive.

// CheckForUpdate queries this project's GitHub Releases for a version newer
// than AppVersion - mirrors update_windows.go's own CheckForUpdate exactly,
// except the asset it looks for is the macOS app zip (macAppAssetRe, see
// macappflash.go) rather than a bare updateAssetName exe.
func (a *App) CheckForUpdate() UpdateCheckResult {
	rel, err := update.FetchLatest(repoSlug())
	if err != nil {
		return UpdateCheckResult{CurrentVersion: AppVersion, Error: err.Error()}
	}

	latest := strings.TrimPrefix(rel.TagName, "v")
	result := UpdateCheckResult{CurrentVersion: AppVersion, LatestVersion: latest, ReleaseURL: rel.HTMLURL}
	if driver.CompareVersions(latest, AppVersion) > 0 {
		result.Available = true
		if asset := rel.AssetMatching(macAppAssetRe); asset != nil {
			result.AssetURL = asset.DownloadURL
		} else {
			result.Error = fmt.Sprintf("release %s has no macOS app zip (PDT-macOS-<version>.zip) asset", rel.TagName)
		}
	}
	return result
}

// macAppBundlePath returns this running process's own PDT.app bundle path
// (e.g. "/Applications/PDT.app"), derived by walking up from
// os.Executable()'s own ".../PDT.app/Contents/MacOS/PDT" - Wails' standard
// macOS bundle layout - rather than assuming a fixed install location, since
// PDT.app can sit anywhere the user put it (Applications, Desktop, a dev
// build's own build/bin, a flash drive...). Errors if the running exe isn't
// actually three levels inside a same-named bundle (a bare `go run`/`go
// test` binary during development, say, or - deliberately not special-cased
// further than the error itself - a flash-drive copy, which AutoUpdateChecks
// already refuses to touch and a manual check would rather report clearly
// than silently misbehave on).
func macAppBundlePath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	// .../PDT.app/Contents/MacOS/PDT -> up three: MacOS, Contents, PDT.app.
	appPath := filepath.Dir(filepath.Dir(filepath.Dir(exePath)))
	if filepath.Base(appPath) != macAppBundleName {
		return "", fmt.Errorf("not running from inside a %s bundle (found %s)", macAppBundleName, exePath)
	}
	return appPath, nil
}

// ApplyUpdate downloads assetURL (from a prior CheckForUpdate result) and
// extracts it into a fresh PDT.app right beside the currently-running one,
// via extractMacAppZip - its own staging-then-atomic-rename (see its doc
// comment) means a failed or canceled download/extraction never leaves the
// running app half-replaced, the same guarantee Windows' rename-aside trick
// gives ApplyUpdate there. Relaunches the new bundle with `open` (rather than
// exec'ing Contents/MacOS/PDT directly, so it starts as a proper independent
// GUI app - its own process group, Dock icon, etc. - exactly like a normal
// double-click would) and quits this process. newVersion is accepted only to
// match ApplyUpdate's cross-platform signature - unlike Windows' registry
// DisplayVersion update, the freshly-extracted bundle's own Info.plist
// already carries the right version, nothing else needs telling.
func (a *App) ApplyUpdate(assetURL, newVersion string) ApplyUpdateResult {
	appPath, err := macAppBundlePath()
	if err != nil {
		return ApplyUpdateResult{Error: err.Error()}
	}
	parentDir := filepath.Dir(appPath)

	src := &macAppSource{version: newVersion, assetURL: assetURL, download: update.Download}
	defer src.cleanup()
	zipPath, err := src.zip()
	if err != nil {
		return ApplyUpdateResult{Error: err.Error()}
	}
	if err := extractMacAppZip(context.Background(), zipPath, parentDir, nil); err != nil {
		return ApplyUpdateResult{Error: err.Error()}
	}
	reapplyLocalDevSigningIfAvailable(appPath)
	if err := exec.Command("open", appPath).Start(); err != nil {
		return ApplyUpdateResult{Error: "update installed, but failed to relaunch: " + err.Error()}
	}
	a.ui.Quit()
	return ApplyUpdateResult{}
}

// localDevSigningIdentity is build-mac.sh's own "PDT Local Dev" identity
// name - a self-signed cert generated and trusted once, by hand, on a single
// machine (issue #9's own setup steps: a plain ad-hoc-signed build gets
// killed by AMFI - AppleMobileFileIntegrityError -423 - the moment a
// privileged command like Deploy's own admin prompt actually starts, and
// there's no paid Apple Developer ID to sign around it with yet). Does NOT
// generalize to any other machine.
const localDevSigningIdentity = "PDT Local Dev"

// reapplyLocalDevSigningIfAvailable re-signs appPath with
// localDevSigningIdentity if (and only if) this exact machine already has
// that identity trusted - build-mac.sh's own local rebuild does the same
// check for the same reason. Every GitHub release .app is only ever ad-hoc
// signed (release.yml's own comment: build-mac.sh's extra re-sign step can't
// run in CI), so without this, ApplyUpdate would silently replace a
// technician's own locally re-signed PDT.app with an ad-hoc one on every
// self-update - reintroducing issue #9's AMFI crash on the very next Deploy,
// on the one machine that had already worked around it. Best-effort and
// silent either way: a missing identity (every machine except Ken's current
// one) or a codesign failure just leaves the freshly-extracted bundle
// ad-hoc-signed, exactly like a plain release download - never worse than
// that, so this never fails ApplyUpdate itself over a signing nicety.
func reapplyLocalDevSigningIfAvailable(appPath string) {
	out, err := exec.Command("security", "find-identity", "-v", "-p", "codesigning").Output()
	if err != nil || !strings.Contains(string(out), `"`+localDevSigningIdentity+`"`) {
		return
	}
	_ = exec.Command("codesign", "--force", "--deep", "--sign", localDevSigningIdentity, "--options", "runtime", appPath).Run()
}
