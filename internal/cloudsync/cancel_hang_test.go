package cloudsync

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// slowUploadServer accepts a PUT and reads its body in small, deliberately
// delayed chunks - real network transfer of anything but a tiny file takes
// real wall-clock time, and this recreates that window so a cancellation
// fired mid-upload has something real to interrupt, rather than racing
// against a transfer that's already finished before ctx.Done() is ever
// checked.
func slowUploadServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusOK)
			return
		}
		buf := make([]byte, 32*1024)
		for {
			_, err := r.Body.Read(buf)
			if err != nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}))
}

func testCore(t *testing.T, srv *httptest.Server) *minio.Core {
	t.Helper()
	core, err := minio.NewCore(srv.Listener.Addr().String(), &minio.Options{
		Creds:  credentials.NewStaticV4("id", "secret", ""),
		Secure: false,
		Region: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	return core
}

// TestUpload_CancelDuringSimplePutObjectReturnsPromptly is the direct
// regression test for a real bug reported live: canceling an in-progress
// Cloud Sync upload left the Cancel button stuck on "Canceling..." forever.
// A 2-second bound here is generous - if this ever fails, Upload isn't
// actually honoring ctx cancellation somewhere in the request it's making.
func TestUpload_CancelDuringSimplePutObjectReturnsPromptly(t *testing.T) {
	srv := slowUploadServer(t)
	defer srv.Close()
	core := testCore(t, srv)

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	data := make([]byte, 2*1024*1024) // 2MiB - comfortably under MultipartThreshold
	if err := os.WriteFile(srcPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	var gotErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		gotErr = Upload(ctx, nil, core, "test-bucket", "Drivers/", "src.bin", srcPath, func(doneBytes, total int64) {
			select {
			case <-started:
			default:
				close(started)
			}
		})
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("upload never reported any progress - test setup itself is broken")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Upload did not return within 2s of ctx being canceled - this is the real hang reported live")
	}
	if gotErr == nil {
		t.Error("expected Upload to return a non-nil error after cancellation")
	}
	t.Logf("Upload returned: %v", gotErr)
	_ = io.Discard
}
