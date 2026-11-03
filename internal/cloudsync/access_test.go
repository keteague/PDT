package cloudsync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fakeS3 answers CreateMultipartUpload (POST ?uploads) with status/body,
// counts AbortMultipartUpload (DELETE ?uploadId=) calls, and 200s anything
// else.
func fakeS3(t *testing.T, status int, body string, aborts *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodPost && q.Has("uploads"):
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		case r.Method == http.MethodDelete && q.Has("uploadId"):
			atomic.AddInt32(aborts, 1)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProbeWriteAccess(t *testing.T) {
	const accessDenied = `<?xml version="1.0" encoding="UTF-8"?><Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`
	const initiated = `<?xml version="1.0" encoding="UTF-8"?><InitiateMultipartUploadResult><Bucket>pdt</Bucket><Key>Drivers/.pdt-access-probe</Key><UploadId>abc123</UploadId></InitiateMultipartUploadResult>`
	const internalErr = `<?xml version="1.0" encoding="UTF-8"?><Error><Code>InternalError</Code><Message>boom</Message></Error>`

	cases := []struct {
		name       string
		status     int
		body       string
		wantWrite  bool
		wantErr    bool
		wantAborts int32
	}{
		{"read-only key is refused", http.StatusForbidden, accessDenied, false, false, 0},
		{"read-write key is allowed and the probe is aborted", http.StatusOK, initiated, true, false, 1},
		{"inconclusive error never hides uploads", http.StatusInternalServerError, internalErr, true, true, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var aborts int32
			srv := fakeS3(t, c.status, c.body, &aborts)
			core := testCore(t, srv)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			canWrite, err := ProbeWriteAccess(ctx, core, "pdt", "Drivers/")
			if canWrite != c.wantWrite || (err != nil) != c.wantErr {
				t.Fatalf("got canWrite=%v err=%v, want canWrite=%v err?=%v", canWrite, err, c.wantWrite, c.wantErr)
			}
			if got := atomic.LoadInt32(&aborts); got != c.wantAborts {
				t.Errorf("abort calls = %d, want %d", got, c.wantAborts)
			}
		})
	}
}

func TestIsAccessDenied(t *testing.T) {
	if IsAccessDenied(nil) {
		t.Error("nil is not AccessDenied")
	}
	if !IsAccessDenied(ErrReadOnly) {
		t.Error("ErrReadOnly should count as AccessDenied")
	}
}
