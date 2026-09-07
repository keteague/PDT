package main

import (
	"embed"
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

// sevenZipAssets embeds a fixed, versioned copy of 7-Zip's own 7z.exe/7z.dll
// (plus its license text) - the only thing on this machine confirmed to
// correctly extract Lexmark's self-extracting RAR driver package. See the
// README's "Lexmark" section for why: Go's standard library has no RAR
// reader at all, the one pure-Go library evaluated (nwaples/rardecode)
// silently corrupts exactly the files this needs, and 7-Zip's own
// easily-redistributable "Extra" console package doesn't include RAR support
// either (confirmed directly - it errors "Cannot open the file as archive")
// - only the full 7z.dll does. Redistribution is permitted under 7-Zip's own
// license (LGPL + an "unRAR restriction" limited to barring use of the code
// to build a RAR *compressor* - not redistribution of the decoder), provided
// the license text travels with the binaries, which is why License.txt is
// embedded and extracted alongside them.
//
//go:embed third_party/7zip/7z.exe third_party/7zip/7z.dll third_party/7zip/License.txt
var sevenZipAssets embed.FS

// ensureSevenZipExtracted writes this build's embedded 7z.exe/7z.dll/License
// out to a stable per-machine cache folder and points driver.SevenZipPath at
// the result, so BuildCatalog can auto-extract a self-extracting RAR package
// it finds. Best-effort: any failure just leaves SevenZipPath unset, which
// skips that auto-extraction entirely rather than failing startup over it -
// the rest of the catalog scan doesn't depend on it.
func ensureSevenZipExtracted() {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return
	}
	destDir := filepath.Join(cacheDir, "PDT", "tools", "7zip")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return
	}

	for _, name := range []string{"7z.exe", "7z.dll", "License.txt"} {
		if err := extractEmbeddedIfStale("third_party/7zip/"+name, filepath.Join(destDir, name)); err != nil {
			return
		}
	}
	driver.SevenZipPath = filepath.Join(destDir, "7z.exe")
}

// extractEmbeddedIfStale writes embeddedPath's content to destPath, skipping
// the write if destPath already exists with the same size - cheap enough to
// call on every startup without rewriting ~2.5MB each time, while still
// picking up a newer bundled 7z.exe/7z.dll after PDT itself is updated (see
// internal/update).
func extractEmbeddedIfStale(embeddedPath, destPath string) error {
	data, err := sevenZipAssets.ReadFile(embeddedPath)
	if err != nil {
		return err
	}
	if info, statErr := os.Stat(destPath); statErr == nil && info.Size() == int64(len(data)) {
		return nil
	}
	return os.WriteFile(destPath, data, 0o755)
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

// CheckSevenZipUpdate queries 7-Zip's own GitHub Releases (ip7z/7zip, where
// 7-Zip's development now lives) for a version newer than the one currently
// cached - Settings > About's "Check for 7-Zip Updates" button. Reuses
// internal/update's FetchLatest (written for PDT's own releases, but generic
// enough that this needs no update-package changes beyond AssetMatching, for
// 7-Zip's own release asset names embedding a version number that PDT's
// exact-name Asset lookup can't match).
func (a *App) CheckSevenZipUpdate() UpdateCheckResult {
	<-a.ready
	current, err := currentSevenZipVersion()
	if err != nil {
		return UpdateCheckResult{Error: err.Error()}
	}

	rel, err := update.FetchLatest(sevenZipRepoSlug)
	if err != nil {
		return UpdateCheckResult{CurrentVersion: current, Error: err.Error()}
	}

	result := UpdateCheckResult{CurrentVersion: current, LatestVersion: rel.TagName, ReleaseURL: rel.HTMLURL}
	if driver.CompareVersions(rel.TagName, current) > 0 {
		result.Available = true
		if asset := rel.AssetMatching(sevenZipInstallerAssetRe); asset != nil {
			result.AssetURL = asset.DownloadURL
		} else {
			result.Error = fmt.Sprintf("release %s has no x64 installer asset to download", rel.TagName)
		}
	}
	return result
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
