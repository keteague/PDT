package update

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// SevenZipDownloadPage is 7-Zip's own download page - the source of truth for
// "what is the latest 7-Zip" (Ken, 2026-09-20).
const SevenZipDownloadPage = "https://www.7-zip.org/download.html"

// SevenZipRelease is what the download page advertises for Windows.
type SevenZipRelease struct {
	Version      string // e.g. "26.03"
	Date         string // e.g. "2026-09-03"
	InstallerURL string // the x64 installer, e.g. .../7z2603-x64.exe
	PageURL      string // the page it was read from
}

var (
	// "Download 7-Zip 26.03 (2026-09-03) for Windows" - the first such
	// heading is the newest release; older versions follow it.
	sevenZipHeadingRe = regexp.MustCompile(`(?i)Download\s+7-Zip\s+(\d+\.\d+)\s+\((\d{4}-\d{2}-\d{2})\)\s+for\s+Windows`)
	// The x64 GUI installer's link, e.g. href="https://github.com/.../7z2603-x64.exe"
	// or a relative href="a/7z2301-x64.exe".
	sevenZipInstallerLinkRe = regexp.MustCompile(`(?i)href="([^"]*7z\d+-x64\.exe)"`)
)

// FetchSevenZipLatest reads 7-Zip's download page and returns the newest
// Windows release it lists, with its x64 installer link resolved to an
// absolute URL. The installer is a self-extracting archive: an existing
// 7z.exe can pull 7z.exe/7z.dll straight out of it without running it.
func FetchSevenZipLatest() (SevenZipRelease, error) {
	return fetchSevenZipLatestFrom(SevenZipDownloadPage)
}

// fetchSevenZipLatestFrom is FetchSevenZipLatest against an arbitrary URL, so
// tests can point it at an httptest server.
func fetchSevenZipLatestFrom(pageURL string) (SevenZipRelease, error) {
	req, err := http.NewRequest(http.MethodGet, pageURL, nil)
	if err != nil {
		return SevenZipRelease{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "+userAgent)
	req.Header.Set("Accept", "text/html")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return SevenZipRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return SevenZipRelease{}, fmt.Errorf("7-zip.org returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return SevenZipRelease{}, err
	}
	return parseSevenZipPage(string(body), pageURL)
}

// parseSevenZipPage extracts the newest release from the download page's
// HTML: the first "Download 7-Zip X.YY (date) for Windows" heading, and the
// first x64 installer link after it (so a later, older section's links are
// never picked up for a newer version).
func parseSevenZipPage(page, pageURL string) (SevenZipRelease, error) {
	loc := sevenZipHeadingRe.FindStringSubmatchIndex(page)
	if loc == nil {
		return SevenZipRelease{}, fmt.Errorf("couldn't find the latest version on the 7-Zip download page (its layout may have changed)")
	}
	version := page[loc[2]:loc[3]]
	date := page[loc[4]:loc[5]]

	rest := page[loc[1]:]
	// Stop at the next release's heading so we only look inside this section.
	if next := sevenZipHeadingRe.FindStringIndex(rest); next != nil {
		rest = rest[:next[0]]
	}
	m := sevenZipInstallerLinkRe.FindStringSubmatch(rest)
	if m == nil {
		return SevenZipRelease{}, fmt.Errorf("7-Zip %s is listed but its x64 installer link wasn't found on the download page", version)
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return SevenZipRelease{}, err
	}
	ref, err := url.Parse(strings.TrimSpace(html.UnescapeString(m[1])))
	if err != nil {
		return SevenZipRelease{}, err
	}
	return SevenZipRelease{Version: version, Date: date, InstallerURL: base.ResolveReference(ref).String(), PageURL: pageURL}, nil
}
