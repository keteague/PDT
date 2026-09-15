package cloudsync

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// pagingForeverServer is a fake ListObjectsV2 endpoint that always reports
// IsTruncated=true with a fresh continuation token - it never runs out of
// pages on its own. If listRemote ever failed to check ctx between pages, a
// test against this server would spin forever rather than return promptly
// once canceled - the exact live bug this guards against (see listRemote's
// own doc comment): SyncCloud re-runs BuildPlan at the very start of every
// sync, before any per-file transfer (which does respect ctx - see
// cancel_hang_test.go) even begins, so Cancel landing while a large shared
// bucket's listing was still paging had no way to interrupt it.
func pagingForeverServer(t *testing.T, requestCount *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(requestCount, 1)
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>test-bucket</Name>
  <Prefix>Drivers/</Prefix>
  <IsTruncated>true</IsTruncated>
  <NextContinuationToken>next</NextContinuationToken>
  <Contents><Key>Drivers/file.zip</Key><Size>10</Size></Contents>
</ListBucketResult>`)
	}))
}

// TestListRemote_CancelBetweenPagesReturnsPromptly is the direct regression
// test for the real "Cancel gets stuck in a Canceling state" bug reported
// live: a bucket listing that's still paging when Cancel is hit used to
// never notice ctx at all (minio-go's ListObjectsV2 takes no context
// parameter, so there was nothing checking it). pagingForeverServer never
// stops offering another page on its own, so this can only pass if
// listRemote itself bails out once ctx is canceled.
func TestListRemote_CancelBetweenPagesReturnsPromptly(t *testing.T) {
	var requestCount int32
	srv := pagingForeverServer(t, &requestCount)
	defer srv.Close()
	core := testCore(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(75*time.Millisecond, cancel)

	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		_, err = listRemote(ctx, core, "test-bucket", "Drivers/")
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("listRemote did not return within 2s of ctx being canceled - this is the real hang reported live")
	}
	if err == nil {
		t.Fatal("expected a non-nil error once ctx was canceled mid-listing")
	}
	t.Logf("listRemote returned after %d page requests: %v", atomic.LoadInt32(&requestCount), err)
}
