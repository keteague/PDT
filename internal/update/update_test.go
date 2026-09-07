package update

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestRelease_Asset(t *testing.T) {
	rel := Release{Assets: []ReleaseAsset{
		{Name: "PDT.exe", DownloadURL: "https://example.com/PDT.exe"},
		{Name: "checksums.txt", DownloadURL: "https://example.com/checksums.txt"},
	}}

	if a := rel.Asset("PDT.exe"); a == nil || a.DownloadURL != "https://example.com/PDT.exe" {
		t.Fatalf("Asset(PDT.exe) = %+v, want a match", a)
	}
	if a := rel.Asset("missing.zip"); a != nil {
		t.Fatalf("Asset(missing.zip) = %+v, want nil", a)
	}
}

func TestRelease_AssetMatching(t *testing.T) {
	rel := Release{Assets: []ReleaseAsset{
		{Name: "7z2603-x64.exe", DownloadURL: "https://example.com/7z2603-x64.exe"},
		{Name: "7z2603-extra.7z", DownloadURL: "https://example.com/7z2603-extra.7z"},
	}}
	re := regexp.MustCompile(`^7z\d+-x64\.exe$`)
	if a := rel.AssetMatching(re); a == nil || a.Name != "7z2603-x64.exe" {
		t.Fatalf("AssetMatching = %+v, want a match on 7z2603-x64.exe", a)
	}

	noMatch := regexp.MustCompile(`^nothing-like-this\.exe$`)
	if a := rel.AssetMatching(noMatch); a != nil {
		t.Fatalf("AssetMatching(no match) = %+v, want nil", a)
	}
}

func TestFetchLatest_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := fetchLatestFrom(srv.URL)
	if err == nil {
		t.Fatal("expected an error for a 404 response, got nil")
	}
}

func TestFetchLatest_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != userAgent {
			t.Errorf("User-Agent = %q, want %q", got, userAgent)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"tag_name": "v0.2.0",
			"html_url": "https://github.com/keteague/PDT/releases/tag/v0.2.0",
			"assets": [{"name": "PDT.exe", "browser_download_url": "https://example.com/PDT.exe"}]
		}`))
	}))
	defer srv.Close()

	rel, err := fetchLatestFrom(srv.URL)
	if err != nil {
		t.Fatalf("fetchLatestFrom: %v", err)
	}
	if rel.TagName != "v0.2.0" {
		t.Errorf("TagName = %q, want v0.2.0", rel.TagName)
	}
	if a := rel.Asset("PDT.exe"); a == nil || a.DownloadURL != "https://example.com/PDT.exe" {
		t.Errorf("Asset(PDT.exe) = %+v", a)
	}
}

func TestDownloadAndApply(t *testing.T) {
	const newContent = "new exe bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(newContent))
	}))
	defer srv.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "PDT.exe")
	if err := os.WriteFile(exePath, []byte("old exe bytes"), 0o755); err != nil {
		t.Fatal(err)
	}

	tmpPath, err := Download(srv.URL, exePath)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if got, _ := os.ReadFile(tmpPath); string(got) != newContent {
		t.Fatalf("downloaded content = %q, want %q", got, newContent)
	}

	if err := Apply(exePath, tmpPath); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, _ := os.ReadFile(exePath); string(got) != newContent {
		t.Fatalf("exePath content after Apply = %q, want %q", got, newContent)
	}
	if _, err := os.Stat(exePath + ".old"); err != nil {
		t.Fatalf("expected a %s.old backup to exist: %v", exePath, err)
	}

	CleanupOldExe(exePath)
	if _, err := os.Stat(exePath + ".old"); !os.IsNotExist(err) {
		t.Fatalf("CleanupOldExe did not remove the .old file (err=%v)", err)
	}
}
