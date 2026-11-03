package cloudsync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// r2ObjectServer serves one object the way R2 really does (confirmed live
// 2026-11-03): custom metadata comes back as canonical
// X-Amz-Meta-Uploaded-By / X-Amz-Meta-Mtime headers on HEAD and GET.
func r2ObjectServer(t *testing.T, body []byte, uploader, mtime string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Length", strconv.Itoa(len(body)))
		h.Set("Content-Type", "application/octet-stream")
		h.Set("ETag", `"abc"`)
		h.Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		h.Set("X-Amz-Meta-Uploaded-By", uploader)
		h.Set("X-Amz-Meta-Mtime", mtime)
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestUserMetaIgnoresCase(t *testing.T) {
	m := map[string]string{"Uploaded-By": "key1", "Mtime": "t"}
	if got := userMeta(m, uploaderMetaKey); got != "key1" {
		t.Errorf("userMeta(uploaded-by) = %q, want key1", got)
	}
	if got := userMeta(m, mtimeMetaKey); got != "t" {
		t.Errorf("userMeta(mtime) = %q, want t", got)
	}
	if got := userMeta(m, "missing"); got != "" {
		t.Errorf("userMeta(missing) = %q, want empty", got)
	}
}

// Regression: "Who?" reported every file as uploaded before uploaders were
// recorded, because the lookup missed R2's capitalized metadata keys.
func TestStatUploader_ReadsR2MetadataHeaders(t *testing.T) {
	srv := r2ObjectServer(t, []byte("data"), "efaf8e0987e9bd4cc495172d66a9944b", "2026-11-02T20:11:48Z")
	core := testCore(t, srv)

	got, err := StatUploader(context.Background(), core, "pdt", "Drivers/", "Windows/11/Kyocera/KXDriver_8.7.0917.zip")
	if err != nil {
		t.Fatal(err)
	}
	if got != "efaf8e0987e9bd4cc495172d66a9944b" {
		t.Errorf("StatUploader = %q, want the uploader's key ID", got)
	}
}

// Regression: Download stamped every file with the download time instead of
// restoring its original modification time, for the same reason.
func TestDownload_RestoresMtimeFromR2Metadata(t *testing.T) {
	body := []byte("driver package bytes")
	want := time.Date(2026, 11, 2, 20, 11, 48, 0, time.UTC)
	srv := r2ObjectServer(t, body, "key", want.Format(time.RFC3339Nano))
	core := testCore(t, srv)

	dest := filepath.Join(t.TempDir(), "Kyocera", "KXDriver.zip")
	if err := Download(context.Background(), &PauseGate{}, core, "pdt", "Drivers/", "Kyocera/KXDriver.zip", dest, int64(len(body)), nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(want) {
		t.Errorf("downloaded file mtime = %v, want %v", info.ModTime().UTC(), want)
	}
}
