package cloudsync

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// slowMultipartServer fakes just enough of S3's multipart-upload API for
// uploadMultipart to run against: an empty ListMultipartUploads (nothing to
// resume), NewMultipartUpload, a slow-reading PutObjectPart (see
// slowUploadServer's own comment for why the artificial delay matters), and
// AbortMultipartUpload.
func slowMultipartServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodGet && q.Has("uploads"):
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListMultipartUploadsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Bucket>test-bucket</Bucket>
  <KeyMarker></KeyMarker>
  <UploadIdMarker></UploadIdMarker>
  <NextKeyMarker></NextKeyMarker>
  <NextUploadIdMarker></NextUploadIdMarker>
  <MaxUploads>1000</MaxUploads>
  <IsTruncated>false</IsTruncated>
</ListMultipartUploadsResult>`)
		case r.Method == http.MethodPost && q.Has("uploads"):
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<InitiateMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Bucket>test-bucket</Bucket>
  <Key>Drivers/src.bin</Key>
  <UploadId>test-upload-id</UploadId>
</InitiateMultipartUploadResult>`)
		case r.Method == http.MethodPut && q.Has("partNumber"):
			buf := make([]byte, 32*1024)
			for {
				_, err := r.Body.Read(buf)
				if err != nil {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
		case r.Method == http.MethodDelete && q.Has("uploadId"):
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
}

// TestUploadMultipart_CancelDuringPartUploadReturnsPromptly is
// TestUpload_CancelDuringSimplePutObjectReturnsPromptly's own multipart
// counterpart - a real Drivers folder has large installer files too (at or
// above MultipartThreshold), which take the entirely different
// uploadMultipart code path (NewMultipartUpload/PutObjectPart/
// AbortMultipartUpload instead of a single PutObject).
func TestUploadMultipart_CancelDuringPartUploadReturnsPromptly(t *testing.T) {
	srv := slowMultipartServer(t)
	defer srv.Close()
	core := testCore(t, srv)

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	data := make([]byte, MultipartThreshold+1024*1024) // just over the multipart threshold
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
	case <-time.After(3 * time.Second):
		t.Fatal("upload never reported any progress - test setup itself is broken")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Upload (multipart) did not return within 3s of ctx being canceled - this is the real hang reported live")
	}
	if gotErr == nil {
		t.Error("expected Upload to return a non-nil error after cancellation")
	}
	t.Logf("Upload (multipart) returned: %v", gotErr)
}
