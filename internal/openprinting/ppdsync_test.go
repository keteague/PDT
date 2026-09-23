package openprinting

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func indexPage(rows ...string) string {
	return `<html><body><table>
<tr><th><a href="?C=N;O=D">Name</a></th><th><a href="?C=M;O=A">Last modified</a></th></tr>
<tr><td valign="top"><img src="/icons/back.gif"></td><td><a href="/download/PPD/">Parent Directory</a></td><td>&nbsp;</td><td align="right">  - </td><td>&nbsp;</td></tr>
` + strings.Join(rows, "\n") + `</table></body></html>`
}

func fileRow(name, mod, size string) string {
	return fmt.Sprintf(`<tr><td valign="top"><img src="/icons/unknown.gif"></td><td><a href="%s">%s</a></td><td align="right">%s  </td><td align="right">%s</td><td>&nbsp;</td></tr>`, name, name, mod, size)
}

func dirRow(name, mod string) string {
	return fmt.Sprintf(`<tr><td valign="top"><img src="/icons/folder.gif"></td><td><a href="%s/">%s/</a></td><td align="right">%s  </td><td align="right">  - </td><td>&nbsp;</td></tr>`, name, name, mod)
}

func TestParseListing(t *testing.T) {
	page := indexPage(dirRow("en", "2021-09-02 18:59"), fileRow("A%20B.ppd", "2021-09-02 18:59", "164K"))
	got := ParseListing(page)
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(got), got)
	}
	if !got[0].IsDir || got[0].Name != "en" {
		t.Errorf("first entry = %+v", got[0])
	}
	if got[1].Name != "A B.ppd" || got[1].Size != "164K" || got[1].Modified != "2021-09-02 18:59" {
		t.Errorf("second entry = %+v", got[1])
	}
}

func newTestSite(t *testing.T, uaSeen *atomic.Value, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	lastMod := time.Date(2021, 9, 2, 18, 59, 0, 0, time.UTC)
	mux := http.NewServeMux()
	mux.HandleFunc("/PPD/", func(w http.ResponseWriter, r *http.Request) {
		uaSeen.Store(r.Header.Get("User-Agent"))
		switch r.URL.Path {
		case "/PPD/":
			fmt.Fprint(w, indexPage(dirRow("KONICA_MINOLTA", "2021-09-02 18:59"), dirRow("Canon", "2021-09-02 18:59"), dirRow("Dell", "2021-09-02 18:59")))
		case "/PPD/KONICA_MINOLTA/":
			fmt.Fprint(w, indexPage(fileRow("KM_C258.ppd", "2021-09-02 18:59", "10K"), fileRow("ReadMe.htm", "2021-09-02 18:59", "23K"), dirRow("en", "2021-09-02 18:59"), dirRow("de", "2021-09-02 18:59")))
		case "/PPD/KONICA_MINOLTA/en/":
			fmt.Fprint(w, indexPage(fileRow("KM_en.ppd", "2021-09-02 18:59", "10K"), fileRow("KM_C258.ppd", "2021-09-02 18:59", "10K")))
		case "/PPD/KONICA_MINOLTA/de/":
			fmt.Fprint(w, indexPage(fileRow("KM_de.ppd", "2021-09-02 18:59", "10K")))
		case "/PPD/Canon/":
			fmt.Fprint(w, indexPage(fileRow("Canon_iR.ppd.gz", "2021-09-02 18:59", "5K")))
		case "/PPD/KONICA_MINOLTA/KM_C258.ppd", "/PPD/KONICA_MINOLTA/en/KM_en.ppd", "/PPD/Canon/Canon_iR.ppd.gz":
			hits.Add(1)
			w.Header().Set("Last-Modified", lastMod.Format(http.TimeFormat))
			fmt.Fprint(w, "*PPD-Adobe: 4.3\n")
		default:
			http.NotFound(w, r)
		}
	})
	return httptest.NewServer(mux)
}

func TestSync_MirrorsFlattensKeepsTimestampsAndSkipsUnchanged(t *testing.T) {
	var ua atomic.Value
	var hits atomic.Int32
	srv := newTestSite(t, &ua, &hits)
	defer srv.Close()

	dest := t.TempDir()
	// A pre-existing folder spelled differently must be reused, not duplicated.
	if err := os.MkdirAll(filepath.Join(dest, "KonicaMinolta"), 0o755); err != nil {
		t.Fatal(err)
	}
	opt := Options{BaseURL: srv.URL + "/PPD/", Manufacturers: []string{"Canon", "Konica Minolta", "Ricoh"}, DestRoot: dest}

	res, err := Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Downloaded != 3 || res.Failed != 0 {
		t.Fatalf("first run: %+v", res)
	}
	if !strings.Contains(ua.Load().(string), "Mozilla/5.0") {
		t.Errorf("user agent = %q, want a browser-style agent", ua.Load())
	}
	want := time.Date(2021, 9, 2, 18, 59, 0, 0, time.UTC)
	for _, rel := range []string{"KonicaMinolta/KM_C258.ppd", "KonicaMinolta/KM_en.ppd", "Canon/Canon_iR.ppd.gz"} {
		info, err := os.Stat(filepath.Join(dest, rel))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if !info.ModTime().UTC().Equal(want) {
			t.Errorf("%s mtime = %v, want %v", rel, info.ModTime().UTC(), want)
		}
	}
	for _, rel := range []string{"KonicaMinolta/KM_de.ppd", "KonicaMinolta/ReadMe.htm", "Konica Minolta"} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err == nil {
			t.Errorf("%s should not exist", rel)
		}
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dest, "*", "*.part")); len(leftovers) > 0 {
		t.Errorf("leftover .part files: %v", leftovers)
	}

	before := hits.Load()
	res, err = Sync(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Downloaded != 0 || res.Skipped != 3 {
		t.Errorf("second run: %+v, want everything skipped", res)
	}
	if hits.Load() != before {
		t.Errorf("second run re-downloaded files")
	}

	// A deleted local file is fetched again even though the state matches.
	_ = os.Remove(filepath.Join(dest, "Canon", "Canon_iR.ppd.gz"))
	res, _ = Sync(context.Background(), opt)
	if res.Downloaded != 1 || res.Skipped != 2 {
		t.Errorf("third run: %+v", res)
	}
}

// TestSync_SkipFileExcludesMatches guards Options.SkipFile (Ken, 2026-09-23):
// a real Japan-market OpenPrinting PPD (Sharp's own confirmed "-jp" filename
// convention, decidable from the name alone, unlike its *NickName content)
// should never even be downloaded, not just hidden from the dropdowns
// afterward - openprintingsync.go (package main) wires this to
// driver.IsExcludedOpenPrintingFilename, but this package stays agnostic of
// that rule, so the test supplies an equivalent plain filename check itself.
func TestSync_SkipFileExcludesMatches(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/PPD/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/PPD/":
			fmt.Fprint(w, indexPage(dirRow("Sharp", "2021-09-02 18:59")))
		case "/PPD/Sharp/":
			fmt.Fprint(w, indexPage(
				fileRow("Sharp-MX-2300FG-ps.ppd", "2021-09-02 18:59", "10K"),
				fileRow("Sharp-MX-2300FG-ps-jp.ppd", "2021-09-02 18:59", "10K"),
			))
		case "/PPD/Sharp/Sharp-MX-2300FG-ps.ppd":
			fmt.Fprint(w, "*PPD-Adobe: 4.3\n")
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dest := t.TempDir()
	skip := func(name string) bool { return strings.Contains(strings.ToLower(name), "-jp.") }
	res, err := Sync(context.Background(), Options{BaseURL: srv.URL + "/PPD/", Manufacturers: []string{"Sharp"}, DestRoot: dest, SkipFile: skip})
	if err != nil {
		t.Fatal(err)
	}
	if res.Downloaded != 1 || res.Failed != 0 {
		t.Fatalf("got %+v, want exactly the non-jp file downloaded", res)
	}
	if _, err := os.Stat(filepath.Join(dest, "Sharp", "Sharp-MX-2300FG-ps.ppd")); err != nil {
		t.Errorf("the non-jp file should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "Sharp", "Sharp-MX-2300FG-ps-jp.ppd")); err == nil {
		t.Error("the -jp file should never have been downloaded")
	}
}

func TestSync_CancelStops(t *testing.T) {
	var ua atomic.Value
	var hits atomic.Int32
	srv := newTestSite(t, &ua, &hits)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Sync(ctx, Options{BaseURL: srv.URL + "/PPD/", Manufacturers: []string{"Canon"}, DestRoot: t.TempDir()})
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
}

// A cancel that lands while a file is mid-transfer must leave nothing behind:
// no partial file under the real name, no .part file, and no state entry.
func TestSync_CancelMidTransferLeavesNoPartialFile(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/PPD/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/PPD/":
			fmt.Fprint(w, indexPage(dirRow("Canon", "2021-09-02 18:59")))
		case "/PPD/Canon/":
			fmt.Fprint(w, indexPage(fileRow("Big.ppd", "2021-09-02 18:59", "1K")))
		case "/PPD/Canon/Big.ppd":
			w.Header().Set("Content-Length", "100000")
			w.Write(make([]byte, 5000))
			w.(http.Flusher).Flush()
			<-r.Context().Done() // stall until the client gives up
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dest := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sawBytes := false
	res, err := Sync(ctx, Options{
		BaseURL: srv.URL + "/PPD/", Manufacturers: []string{"Canon"}, DestRoot: dest,
		Progress: func(p Progress) {
			if p.Phase == "transfer" && p.FileDone > 0 && !sawBytes {
				sawBytes = true
				cancel()
			}
		},
	})
	if err == nil || !sawBytes {
		t.Fatalf("expected a mid-transfer cancel: err=%v sawBytes=%v", err, sawBytes)
	}
	if res.Downloaded != 0 {
		t.Errorf("Downloaded = %d, want 0", res.Downloaded)
	}
	entries, _ := os.ReadDir(filepath.Join(dest, "Canon"))
	for _, e := range entries {
		t.Errorf("unexpected file left behind after cancel: %s", e.Name())
	}
	if b, _ := os.ReadFile(filepath.Join(dest, StateFileName)); strings.Contains(string(b), "Big.ppd") {
		t.Errorf("canceled file was recorded in the state file: %s", b)
	}
}

// A .part file left by a killed run is cleaned up by the next sync.
func TestSync_SweepsStalePartialFiles(t *testing.T) {
	var ua atomic.Value
	var hits atomic.Int32
	srv := newTestSite(t, &ua, &hits)
	defer srv.Close()
	dest := t.TempDir()
	stale := filepath.Join(dest, "Canon", "Old.ppd.part")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), Options{BaseURL: srv.URL + "/PPD/", Manufacturers: []string{"Canon"}, DestRoot: dest}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("stale .part file should have been removed")
	}
}

// The plan lists exactly what will download, and the running totals reach the
// full byte count with the real sizes replacing the listing's estimates.
func TestSync_ReportsPlanAndTotals(t *testing.T) {
	var ua atomic.Value
	var hits atomic.Int32
	srv := newTestSite(t, &ua, &hits)
	defer srv.Close()
	var plan []PlanFile
	var last Progress
	_, err := Sync(context.Background(), Options{
		BaseURL: srv.URL + "/PPD/", Manufacturers: []string{"Canon", "Konica Minolta"}, DestRoot: t.TempDir(),
		Progress: func(p Progress) {
			switch p.Phase {
			case "plan":
				plan = p.Plan
			case "transfer":
				last = p
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 3 {
		t.Fatalf("plan = %+v, want 3 files", plan)
	}
	if last.DoneFiles != 3 || last.TotalFiles != 3 || last.DoneBytes != last.TotalBytes || last.DoneBytes != 3*int64(len("*PPD-Adobe: 4.3\n")) {
		t.Errorf("final totals = %+v", last)
	}
}

func TestParseListedSize(t *testing.T) {
	for in, want := range map[string]int64{"164K": 164 * 1024, "1.5M": 1572864, "512": 512, "": 0, "-": 0} {
		if got := parseListedSize(in); got != want {
			t.Errorf("parseListedSize(%q) = %d, want %d", in, got, want)
		}
	}
}
