package cloudsync

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"PDT/internal/driver"
)

// objectServer serves one object at /bucket/<key> with Range support. While
// stall is true it sends only the first stallAfter bytes of a request and then
// hangs until the client disconnects, imitating a transfer interrupted midway.
type objectServer struct {
	data       []byte
	mu         sync.Mutex
	stall      bool
	stallAfter int
	starts     []int64 // Range start of each GET, in order
}

func (o *objectServer) handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusOK)
		return
	}
	start := int64(0)
	if rg := r.Header.Get("Range"); strings.HasPrefix(rg, "bytes=") {
		spec := strings.TrimPrefix(rg, "bytes=")
		startStr, _, _ := strings.Cut(spec, "-")
		start, _ = strconv.ParseInt(startStr, 10, 64)
	}
	o.mu.Lock()
	o.starts = append(o.starts, start)
	stall, stallAfter := o.stall, o.stallAfter
	o.mu.Unlock()

	body := o.data[start:]
	h := w.Header()
	h.Set("ETag", `"abc123"`)
	h.Set("Last-Modified", time.Date(2021, 9, 2, 18, 59, 0, 0, time.UTC).Format(http.TimeFormat))
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	if start > 0 {
		h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(o.data)-1, len(o.data)))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	if stall {
		w.Write(body[:stallAfter])
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		return
	}
	w.Write(body)
}

// A Cancel mid-download keeps the partial file, and the next Download resumes
// from its size with a Range request instead of starting over.
func TestDownload_CancelKeepsPartialAndNextRunResumes(t *testing.T) {
	obj := &objectServer{data: bytes.Repeat([]byte("0123456789"), 5000), stall: true, stallAfter: 12000}
	srv := httptest.NewServer(http.HandlerFunc(obj.handler))
	defer srv.Close()
	core := testCore(t, srv)

	localPath := filepath.Join(t.TempDir(), "sub", "driver.zip")
	size := int64(len(obj.data))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := Download(ctx, &PauseGate{}, core, "bkt", "Drivers/", "sub/driver.zip", localPath, size, func(done, total int64) {
		if done >= 5000 {
			cancel()
		}
	})
	if err == nil {
		t.Fatal("expected the canceled download to return an error")
	}
	partial, statErr := os.Stat(localPath + partialSuffix)
	if statErr != nil {
		t.Fatalf("the partial file should be kept after Cancel: %v", statErr)
	}
	if partial.Size() < 5000 {
		t.Fatalf("partial holds %d bytes, want at least the 5000 that had arrived", partial.Size())
	}
	if _, err := os.Stat(localPath); err == nil {
		t.Fatal("the real file must not exist after a canceled download")
	}

	// Second run: server behaves; the download must resume, not restart.
	obj.mu.Lock()
	obj.stall = false
	obj.mu.Unlock()
	if err := Download(context.Background(), &PauseGate{}, core, "bkt", "Drivers/", "sub/driver.zip", localPath, size, nil); err != nil {
		t.Fatalf("resumed download failed: %v", err)
	}
	obj.mu.Lock()
	starts := append([]int64(nil), obj.starts...)
	obj.mu.Unlock()
	if len(starts) != 2 || starts[1] != partial.Size() {
		t.Errorf("GET range starts = %v, want the second to resume at %d", starts, partial.Size())
	}
	got, err := os.ReadFile(localPath)
	if err != nil || !bytes.Equal(got, obj.data) {
		t.Errorf("resumed file differs from the object (err=%v, %d vs %d bytes)", err, len(got), len(obj.data))
	}
	if _, err := os.Stat(localPath + partialSuffix); err == nil {
		t.Error("the .pdt-partial file should be gone once the download completes")
	}
}

// A leftover partial as big as the whole object is not a valid resume point
// (nothing to range-request) - it must be discarded and re-downloaded.
func TestDownload_FullSizePartialRestartsInsteadOfFailing(t *testing.T) {
	obj := &objectServer{data: bytes.Repeat([]byte("abcdefghij"), 300)}
	srv := httptest.NewServer(http.HandlerFunc(obj.handler))
	defer srv.Close()
	core := testCore(t, srv)

	localPath := filepath.Join(t.TempDir(), "driver.zip")
	if err := os.WriteFile(localPath+partialSuffix, obj.data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Download(context.Background(), &PauseGate{}, core, "bkt", "", "driver.zip", localPath, int64(len(obj.data)), nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(localPath)
	if !bytes.Equal(got, obj.data) {
		t.Error("file content mismatch after restart")
	}
}

// In-progress partials must never be listed as local files (they would show
// up as bogus uploads and be copied to flash drives).
func TestListLocal_SkipsPartialDownloads(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Canon"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Canon/real.zip", "Canon/big.zip" + driver.PartialDownloadSuffix} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := listLocal(root)
	if _, ok := got["Canon/real.zip"]; !ok {
		t.Errorf("real file missing from %v", got)
	}
	for k := range got {
		if strings.HasSuffix(k, driver.PartialDownloadSuffix) {
			t.Errorf("partial download listed as a local file: %s", k)
		}
	}
}
