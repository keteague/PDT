package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"PDT/internal/driver"
	"PDT/internal/update"
)

const sevenZipRepoSlug = "ip7z/7zip"

// sevenZipInstallerAssetRe matches 7-Zip's own x64 GUI installer asset name,
// e.g. "7z2603-x64.exe" - the version digits change every release, so this
// can't be looked up by exact name the way PDT's own "PDT.exe" release
// asset is.
var sevenZipInstallerAssetRe = regexp.MustCompile(`^7z\d+-x64\.exe$`)

// sevenZipVersionRe pulls the version number out of 7z.exe's own startup
// banner ("7-Zip 26.03 (x64) : Copyright ...") - confirmed against a real
// install this is the first line `7z.exe i` prints, and simpler than reading
// the PE version resource for a value this codebase only ever needs to
// display or compare, never act on programmatically beyond that.
var sevenZipVersionRe = regexp.MustCompile(`7-Zip\s+(\d+\.\d+)`)

// sevenZipToolsDir is the stable per-machine cache folder
// ensureSevenZipExtracted writes 7z.exe/7z.dll/License.txt into - also
// this exact computer's own cache existing - unlike Write to Flash Drive
// (flashdrive.go), which writes the embedded 7-Zip assets straight onto the
// flash drive itself (writeSevenZipAssets, sevenzipassets.go) rather than
// going through this per-machine cache at all. "" if os.UserCacheDir()
// itself fails, same as ensureSevenZipExtracted's own best-effort handling
// of that.
func sevenZipToolsDir() string {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(cacheDir, "PDT", "tools", "7zip")
}

// ensureSevenZipExtracted writes this build's embedded 7z.exe/7z.dll/License
// out to a stable per-machine cache folder and points driver.SevenZipPath at
// the result, so BuildCatalog can auto-extract a self-extracting RAR package
// it finds. Best-effort: any failure just leaves SevenZipPath unset, which
// skips that auto-extraction entirely rather than failing startup over it -
// the rest of the catalog scan doesn't depend on it.
func ensureSevenZipExtracted() {
	destDir := sevenZipToolsDir()
	if destDir == "" {
		return
	}
	if err := writeSevenZipAssets(destDir); err != nil {
		return
	}
	driver.SevenZipPath = filepath.Join(destDir, "7z.exe")
}

// currentSevenZipVersion runs the cached 7z.exe's own "i" (info) command and
// parses its startup banner for the version number - the only version
// currently in play, since it's whatever's actually cached and being used,
// regardless of what shipped embedded in this particular PDT build.
func currentSevenZipVersion() (string, error) {
	if driver.SevenZipPath == "" {
		return "", fmt.Errorf("7-Zip isn't available")
	}
	out, _ := exec.Command(driver.SevenZipPath, "i").CombinedOutput()
	m := sevenZipVersionRe.FindSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("could not determine the installed 7-Zip version")
	}
	return string(m[1]), nil
}

// GetSevenZipVersion returns the currently-cached 7z.exe's own version
// (e.g. "26.03"), or "" if it can't be determined - Settings > About shows
// this next to the credit line, populated on open rather than baked into
// GetAppInfo since it depends on whatever's actually cached, not a constant
// tied to this particular PDT build.
func (a *App) GetSevenZipVersion() string {
	<-a.ready
	v, _ := currentSevenZipVersion()
	return v
}

const sevenZipHomepageURL = "https://www.7-zip.org/"

// OpenSevenZipHomepage opens 7-Zip's own website in the system default
// browser - the About tab's credit link.
func (a *App) OpenSevenZipHomepage() {
	runtime.BrowserOpenURL(a.ctx, sevenZipHomepageURL)
}

// CheckSevenZipUpdate looks for a 7-Zip version newer than the one currently
// cached - Settings > About's "Check for 7-Zip Updates" button. The source is
// 7-Zip's own download page (www.7-zip.org, Ken 2026-09-20): its newest
// Windows section names the latest version and links the x64 installer. If
// the page can't be read or its layout has changed, it falls back to
// 7-Zip's GitHub Releases (ip7z/7zip), which hosts the same installers.
func (a *App) CheckSevenZipUpdate() UpdateCheckResult {
	<-a.ready
	current, err := currentSevenZipVersion()
	if err != nil {
		return UpdateCheckResult{Error: err.Error()}
	}

	latest, assetURL, releaseURL, err := latestSevenZip()
	if err != nil {
		return UpdateCheckResult{CurrentVersion: current, Error: err.Error()}
	}

	result := UpdateCheckResult{CurrentVersion: current, LatestVersion: latest, ReleaseURL: releaseURL}
	if driver.CompareVersions(latest, current) > 0 {
		result.Available = true
		if assetURL != "" {
			result.AssetURL = assetURL
		} else {
			result.Error = fmt.Sprintf("7-Zip %s has no x64 installer to download", latest)
		}
	}
	return result
}

// latestSevenZip returns the newest 7-Zip version, its x64 installer URL
// ("" if none could be found) and the page to send the user to - from the
// 7-Zip website, else from GitHub Releases.
func latestSevenZip() (version, installerURL, pageURL string, err error) {
	rel, siteErr := update.FetchSevenZipLatest()
	if siteErr == nil {
		return rel.Version, rel.InstallerURL, rel.PageURL, nil
	}
	gh, ghErr := update.FetchLatest(sevenZipRepoSlug)
	if ghErr != nil {
		return "", "", "", fmt.Errorf("could not check for a 7-Zip update: %v (GitHub fallback: %v)", siteErr, ghErr)
	}
	if asset := gh.AssetMatching(sevenZipInstallerAssetRe); asset != nil {
		installerURL = asset.DownloadURL
	}
	return gh.TagName, installerURL, gh.HTMLURL, nil
}

// UpdateSevenZip downloads assetURL (7-Zip's own x64 GUI installer - a
// self-extracting 7z archive in its own right, confirmed directly: 7z.exe
// can list and extract files from it without ever running it as an
// installer), pulls just 7z.exe/7z.dll/License.txt back out of it using the
// currently-cached 7z.exe, and overwrites the cached copies with the result.
// Extracts to a scratch folder first and only overwrites the real cached
// files once that fully succeeds, so a bad download or a failed extraction
// never leaves the existing, working 7z.exe/7z.dll touched.
func (a *App) UpdateSevenZip(assetURL string) ApplyUpdateResult {
	<-a.ready
	if driver.SevenZipPath == "" {
		return ApplyUpdateResult{Error: "7-Zip isn't available to perform the update"}
	}
	cacheDir := filepath.Dir(driver.SevenZipPath)

	installerPath, err := update.Download(assetURL, filepath.Join(cacheDir, "7z-installer"))
	if err != nil {
		return ApplyUpdateResult{Error: err.Error()}
	}
	defer os.Remove(installerPath)

	extractDir := filepath.Join(cacheDir, "update-extract")
	os.RemoveAll(extractDir)
	defer os.RemoveAll(extractDir)

	names := []string{"7z.exe", "7z.dll", "License.txt"}
	args := append([]string{"e", installerPath, "-o" + extractDir, "-y"}, names...)
	if out, err := exec.Command(driver.SevenZipPath, args...).CombinedOutput(); err != nil {
		return ApplyUpdateResult{Error: fmt.Sprintf("extracting the new 7-Zip failed: %v: %s", err, out)}
	}

	// Make sure the new 7z.exe actually runs before it replaces the working
	// one - a bad download that still extracted must not brick extraction.
	if out, err := exec.Command(filepath.Join(extractDir, "7z.exe"), "i").CombinedOutput(); err != nil || sevenZipVersionRe.Find(out) == nil {
		return ApplyUpdateResult{Error: fmt.Sprintf("the downloaded 7-Zip didn't run correctly, so it was not installed (%v)", err)}
	}

	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(extractDir, name))
		if err != nil {
			return ApplyUpdateResult{Error: fmt.Sprintf("reading extracted %s: %v", name, err)}
		}
		if err := os.WriteFile(filepath.Join(cacheDir, name), data, 0o755); err != nil {
			return ApplyUpdateResult{Error: fmt.Sprintf("installing new %s: %v", name, err)}
		}
	}
	return ApplyUpdateResult{}
}
