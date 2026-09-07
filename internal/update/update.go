// Package update checks GitHub Releases for a newer version of something and
// can download it - written for PDT's own self-update (see App.CheckForUpdate/
// ApplyUpdate), where "applying" means replacing the running executable's own
// file directly (confirmed on a real machine before writing this: Windows
// lets a running .exe's file be renamed out of the way while it keeps
// executing from the renamed file, which is enough to drop a new exe in at
// the original path with no helper process or installer needed), but generic
// enough that FetchLatest/Download are reused as-is for checking and
// downloading updates to the bundled 7z.exe/7z.dll too (see sevenzip.go) -
// GitHub's release API doesn't care whose repository it is.
package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"
)

const userAgent = "PDT-update-check"

// Release is the subset of GitHub's release API response this package uses.
type Release struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Assets  []ReleaseAsset `json:"assets"`
}

// ReleaseAsset is one file attached to a release.
type ReleaseAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
}

// Asset returns the release asset named exactly name, or nil if none matches.
func (r Release) Asset(name string) *ReleaseAsset {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

// AssetMatching returns the first release asset whose name matches re, or
// nil if none does - for a release whose asset filenames embed a version
// number and so can't be looked up by exact name the way Asset can (e.g.
// 7-Zip's own releases name their x64 installer "7z2603-x64.exe", where
// "2603" changes every release).
func (r Release) AssetMatching(re *regexp.Regexp) *ReleaseAsset {
	for i := range r.Assets {
		if re.MatchString(r.Assets[i].Name) {
			return &r.Assets[i]
		}
	}
	return nil
}

// FetchLatest queries GitHub's "latest release" endpoint for repo
// ("owner/name"). Returns a plain, user-presentable error for the common
// case of no releases having been published yet (a bare 404 otherwise).
func FetchLatest(repo string) (Release, error) {
	return fetchLatestFrom("https://api.github.com/repos/" + repo + "/releases/latest")
}

// fetchLatestFrom is FetchLatest against an arbitrary URL, split out so tests
// can point it at an httptest server instead of the real GitHub API.
func fetchLatestFrom(url string) (Release, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Release{}, fmt.Errorf("no releases have been published yet")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Release{}, fmt.Errorf("github returned %s: %s", resp.Status, string(body))
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return Release{}, err
	}
	return rel, nil
}

// Download fetches url's body to a new "<destPath>.download" file (same
// directory as destPath, so Apply's later rename is same-volume) and returns
// its path. The caller removes it on any subsequent failure; Apply consumes
// it on success.
func Download(url, destPath string) (tmpPath string, err error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: %s", resp.Status)
	}

	tmpPath = destPath + ".download"
	out, err := os.Create(tmpPath)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return "", err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}

// Apply installs newPath in place of the running executable at exePath, by
// renaming exePath out of the way first rather than overwriting it directly -
// an in-place overwrite of a running .exe fails on Windows, but renaming it
// away succeeds and the process keeps running unaffected from its
// now-renamed file until it next exits. The renamed-away file
// (exePath+".old") is left behind for CleanupOldExe to remove on the next
// launch, once this process has actually exited and it's no longer in use.
func Apply(exePath, newPath string) error {
	oldPath := exePath + ".old"
	os.Remove(oldPath) // best-effort: a leftover .old from an update that was never cleaned up
	if err := os.Rename(exePath, oldPath); err != nil {
		return fmt.Errorf("could not move aside the running executable: %w", err)
	}
	if err := os.Rename(newPath, exePath); err != nil {
		os.Rename(oldPath, exePath) // best-effort restore
		return fmt.Errorf("could not install the downloaded update: %w", err)
	}
	return nil
}

// CleanupOldExe removes a leftover exePath+".old" from a previous update, if
// present. Best-effort, meant to be called once at startup: by the time a
// process is running at all, any previous process that left a ".old" behind
// has necessarily already exited, so the file is guaranteed unused.
func CleanupOldExe(exePath string) {
	os.Remove(exePath + ".old")
}
