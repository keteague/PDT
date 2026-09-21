package update

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// A trimmed copy of the real page's shape: the newest release first (absolute
// GitHub links), then older ones with relative links.
const sevenZipPageFixture = `<html><body>
<P><B>Download 7-Zip 26.03 (2026-09-03) for Windows</B>:</P>
<TABLE>
<TR><TD><A href="https://github.com/ip7z/7zip/releases/download/26.03/7z2603-x64.exe">Download</A></TD><TD>64-bit x64</TD></TR>
<TR><TD><A href="https://github.com/ip7z/7zip/releases/download/26.03/7z2603.exe">Download</A></TD><TD>32-bit x86</TD></TR>
<TR><TD><A href="https://github.com/ip7z/7zip/releases/download/26.03/7z2603-arm64.exe">Download</A></TD><TD>ARM64</TD></TR>
</TABLE>
<P><B>Download 7-Zip 23.01 (2023-06-20)</B>:</P>
<TR><TD><A href="a/7z2301-x64.exe">Download</A></TD></TR>
</body></html>`

func TestParseSevenZipPage_TakesNewestSectionOnly(t *testing.T) {
	rel, err := parseSevenZipPage(sevenZipPageFixture, "https://www.7-zip.org/download.html")
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "26.03" || rel.Date != "2026-09-03" {
		t.Errorf("version/date = %q/%q", rel.Version, rel.Date)
	}
	if rel.InstallerURL != "https://github.com/ip7z/7zip/releases/download/26.03/7z2603-x64.exe" {
		t.Errorf("installer URL = %q", rel.InstallerURL)
	}
}

func TestParseSevenZipPage_ResolvesRelativeLinks(t *testing.T) {
	page := `<B>Download 7-Zip 27.00 (2027-01-01) for Windows</B><A href="a/7z2700-x64.exe">Download</A>`
	rel, err := parseSevenZipPage(page, "https://www.7-zip.org/download.html")
	if err != nil {
		t.Fatal(err)
	}
	if rel.InstallerURL != "https://www.7-zip.org/a/7z2700-x64.exe" {
		t.Errorf("installer URL = %q", rel.InstallerURL)
	}
}

func TestParseSevenZipPage_LayoutChangeIsAnErrorNotAGuess(t *testing.T) {
	if _, err := parseSevenZipPage("<html>nothing useful</html>", "https://x/"); err == nil {
		t.Error("expected an error when no version heading exists")
	}
	// A heading with no x64 link in its own section must not borrow an older
	// section's link.
	page := `<B>Download 7-Zip 27.00 (2027-01-01) for Windows</B> no links here
<B>Download 7-Zip 26.03 (2026-09-03) for Windows</B><A href="7z2603-x64.exe">x</A>`
	if _, err := parseSevenZipPage(page, "https://x/"); err == nil {
		t.Error("expected an error when the newest section has no installer link")
	}
}

func TestFetchSevenZipLatestFrom_ServerAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("no User-Agent sent")
		}
		fmt.Fprint(w, sevenZipPageFixture)
	}))
	defer srv.Close()

	rel, err := fetchSevenZipLatestFrom(srv.URL + "/download.html")
	if err != nil || rel.Version != "26.03" {
		t.Fatalf("got %+v, %v", rel, err)
	}
	if _, err := fetchSevenZipLatestFrom(srv.URL + "/missing"); err == nil {
		t.Error("expected an error for a 404")
	}
}

// Opt-in check against the real site (PDT_LIVE_TESTS=1).
func TestLive_SevenZipDownloadPage(t *testing.T) {
	if os.Getenv("PDT_LIVE_TESTS") == "" {
		t.Skip("set PDT_LIVE_TESTS=1 to hit 7-zip.org")
	}
	rel, err := FetchSevenZipLatest()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("latest: %+v", rel)
	if rel.Version == "" || rel.InstallerURL == "" {
		t.Errorf("incomplete: %+v", rel)
	}
}
