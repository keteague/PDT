package cloudsync

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"

	"PDT/internal/driver"
)

// partialSuffix marks a download still in progress - never mistaken for the
// real, complete file it's building toward. Kept across runs (unlike a
// canceled transfer's own cleanup) specifically so a later Sync can resume
// it via a Range request starting at this file's own current size.
const partialSuffix = driver.PartialDownloadSuffix

// PauseGate is Sync's own pause/resume switch, shared by every in-flight
// Upload/Download call for one Sync run - unlike ctx cancellation (Cancel:
// stop now, delete whatever was in transit), pausing blocks each transfer
// between reads without erroring or losing any already-transferred bytes,
// so Resume can pick up mid-file exactly where Pause left it, not just
// mid-tree. Zero value is ready to use (starts unpaused).
type PauseGate struct {
	mu     sync.Mutex
	paused bool
	resume chan struct{}
}

// SetPaused pauses or resumes every transfer currently waiting on g.
func (g *PauseGate) SetPaused(paused bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if paused == g.paused {
		return
	}
	g.paused = paused
	if paused {
		g.resume = make(chan struct{})
	} else {
		close(g.resume)
	}
}

// IsPaused reports g's current state.
func (g *PauseGate) IsPaused() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.paused
}

// wait blocks while g is paused, returning early with ctx's own error if ctx
// is canceled while waiting - so Cancel always wins over a stuck Pause.
func (g *PauseGate) wait(ctx context.Context) error {
	g.mu.Lock()
	if !g.paused {
		g.mu.Unlock()
		return nil
	}
	ch := g.resume
	g.mu.Unlock()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// pausingReader wraps r so every Read first honors gate (if any - nil is a
// valid "no pause support" value) and ctx cancellation, then reports
// however many bytes actually moved via onRead - the one mechanism both
// Upload (reading local part data to send) and Download (reading response
// bytes to write) share for progress plus pause/cancel responsiveness.
type pausingReader struct {
	ctx    context.Context
	gate   *PauseGate
	r      io.Reader
	onRead func(n int)
}

func (p *pausingReader) Read(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	if p.gate != nil {
		if err := p.gate.wait(p.ctx); err != nil {
			return 0, err
		}
	}
	n, err := p.r.Read(b)
	if n > 0 && p.onRead != nil {
		p.onRead(n)
	}
	return n, err
}

// Upload copies localPath to bucket's prefix+relPath object, attaching
// localPath's own modification time as object metadata (see mtimeMetaKey)
// so Download can restore it later. Files at or above MultipartThreshold go
// through uploadMultipart, which can resume a prior interrupted attempt
// (Cancel, a lost network connection, or PDT simply being closed
// mid-upload) rather than re-uploading from the very first byte.
func Upload(ctx context.Context, gate *PauseGate, core *minio.Core, bucket, prefix, relPath, localPath string, onProgress func(done, total int64)) error {
	key := prefix + relPath
	info, err := os.Stat(localPath)
	if err != nil {
		return err
	}
	size := info.Size()
	mtimeVal := info.ModTime().UTC().Format(time.RFC3339Nano)

	if size < MultipartThreshold {
		f, err := os.Open(localPath)
		if err != nil {
			return err
		}
		defer f.Close()
		var done int64
		reader := &pausingReader{ctx: ctx, gate: gate, r: f, onRead: func(n int) {
			done += int64(n)
			if onProgress != nil {
				onProgress(done, size)
			}
		}}
		_, err = core.PutObject(ctx, bucket, key, reader, size, "", "", minio.PutObjectOptions{
			UserMetadata: map[string]string{mtimeMetaKey: mtimeVal},
		})
		return err
	}

	return uploadMultipart(ctx, gate, core, bucket, key, localPath, size, mtimeVal, onProgress)
}

// partState is what listAndValidateParts already knows about one
// already-uploaded part, without re-uploading it.
type partState struct {
	ETag string
	Size int64
}

func uploadMultipart(ctx context.Context, gate *PauseGate, core *minio.Core, bucket, key, localPath string, size int64, mtimeVal string, onProgress func(done, total int64)) error {
	partSize := PartSizeFor(size)
	totalParts := int((size + partSize - 1) / partSize)

	uploadID, completed, err := resumeOrCreateUpload(ctx, core, bucket, key, mtimeVal, size, partSize, totalParts)
	if err != nil {
		return err
	}

	var doneBytes int64
	for _, p := range completed {
		doneBytes += p.Size
	}
	if onProgress != nil {
		onProgress(doneBytes, size)
	}

	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	completeParts := make([]minio.CompletePart, 0, totalParts)
	for partNumber := 1; partNumber <= totalParts; partNumber++ {
		if existing, ok := completed[partNumber]; ok {
			completeParts = append(completeParts, minio.CompletePart{PartNumber: partNumber, ETag: existing.ETag})
			continue
		}
		thisSize := expectedPartSize(partNumber, size, partSize, totalParts)
		if _, err := f.Seek(partSize*int64(partNumber-1), io.SeekStart); err != nil {
			return err
		}
		reader := &pausingReader{ctx: ctx, gate: gate, r: io.LimitReader(f, thisSize), onRead: func(n int) {
			doneBytes += int64(n)
			if onProgress != nil {
				onProgress(doneBytes, size)
			}
		}}
		objPart, err := core.PutObjectPart(ctx, bucket, key, uploadID, partNumber, reader, thisSize, minio.PutObjectPartOptions{})
		if err != nil {
			if ctx.Err() != nil {
				// Cancel: an incomplete multipart upload otherwise sits in
				// the bucket indefinitely (and R2, like S3, bills for its
				// storage) - abort it outright rather than leaving it
				// resumable, since Cancel's own contract is "delete the
				// file that was in transit," not "pause it." Uses a fresh
				// context - ctx is already canceled, so the abort call
				// itself needs its own short-lived one to actually reach
				// the server.
				abortCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				_ = core.AbortMultipartUpload(abortCtx, bucket, key, uploadID)
				cancel()
			}
			// Any other failure (a transient network error, or Pause
			// itself never produces one) leaves the multipart upload as-is
			// - the next Sync run's own resumeOrCreateUpload finds it and
			// continues from the parts already uploaded.
			return err
		}
		completeParts = append(completeParts, minio.CompletePart{PartNumber: partNumber, ETag: objPart.ETag})
	}

	_, err = core.CompleteMultipartUpload(ctx, bucket, key, uploadID, completeParts, minio.PutObjectOptions{})
	return err
}

// resumeOrCreateUpload finds an existing incomplete multipart upload for key
// and validates its already-uploaded parts against the part plan the
// CURRENT local file (size/partSize) implies. A local file that changed
// since that upload was started produces a plan that no longer matches what
// was actually uploaded - resuming it anyway would silently splice old and
// new content together into one corrupted object, so a mismatch aborts the
// stale upload and starts a fresh one instead of trusting it.
func resumeOrCreateUpload(ctx context.Context, core *minio.Core, bucket, key, mtimeVal string, size, partSize int64, totalParts int) (string, map[int]partState, error) {
	uploadID, err := findExistingUpload(ctx, core, bucket, key)
	if err != nil {
		return "", nil, err
	}
	if uploadID != "" {
		parts, ok, err := listAndValidateParts(ctx, core, bucket, key, uploadID, size, partSize, totalParts)
		if err != nil {
			return "", nil, err
		}
		if ok {
			return uploadID, parts, nil
		}
		_ = core.AbortMultipartUpload(ctx, bucket, key, uploadID)
	}
	newID, err := core.NewMultipartUpload(ctx, bucket, key, minio.PutObjectOptions{
		UserMetadata: map[string]string{mtimeMetaKey: mtimeVal},
	})
	if err != nil {
		return "", nil, err
	}
	return newID, map[int]partState{}, nil
}

// findExistingUpload returns the most recently initiated incomplete
// multipart upload for exactly key, if any - aborting every OTHER
// incomplete upload found for that same key along the way (an earlier
// abandoned attempt that got superseded by a fresh one some other run,
// otherwise left sitting in the bucket accruing storage cost forever).
func findExistingUpload(ctx context.Context, core *minio.Core, bucket, key string) (string, error) {
	var best string
	var bestTime time.Time
	keyMarker, uploadIDMarker := "", ""
	for {
		result, err := core.ListMultipartUploads(ctx, bucket, key, keyMarker, uploadIDMarker, "", 1000)
		if err != nil {
			return "", err
		}
		for _, u := range result.Uploads {
			if u.Key != key {
				continue
			}
			if best != "" && u.Initiated.Before(bestTime) {
				_ = core.AbortMultipartUpload(ctx, bucket, key, u.UploadID)
				continue
			}
			if best != "" {
				_ = core.AbortMultipartUpload(ctx, bucket, key, best)
			}
			best, bestTime = u.UploadID, u.Initiated
		}
		if !result.IsTruncated {
			return best, nil
		}
		keyMarker, uploadIDMarker = result.NextKeyMarker, result.NextUploadIDMarker
	}
}

func listAndValidateParts(ctx context.Context, core *minio.Core, bucket, key, uploadID string, size, partSize int64, totalParts int) (map[int]partState, bool, error) {
	parts := map[int]partState{}
	marker := 0
	for {
		result, err := core.ListObjectParts(ctx, bucket, key, uploadID, marker, 1000)
		if err != nil {
			return nil, false, err
		}
		for _, p := range result.ObjectParts {
			want := expectedPartSize(p.PartNumber, size, partSize, totalParts)
			if want < 0 || p.Size != want {
				return nil, false, nil
			}
			parts[p.PartNumber] = partState{ETag: p.ETag, Size: p.Size}
		}
		if !result.IsTruncated {
			return parts, true, nil
		}
		marker = result.NextPartNumberMarker
	}
}

// expectedPartSize is the size uploadMultipart's own deterministic part plan
// assigns to partNumber (1-based) for a file of size split into partSize-
// sized parts, except the last part, which gets whatever remainder is left
// over - or -1 for a partNumber outside [1, totalParts].
func expectedPartSize(partNumber int, size, partSize int64, totalParts int) int64 {
	if partNumber < 1 || partNumber > totalParts {
		return -1
	}
	if partNumber < totalParts {
		return partSize
	}
	return size - partSize*int64(totalParts-1)
}

// Download copies bucket's prefix+relPath object to localPath, resuming a
// prior interrupted attempt via an HTTP Range request if a .pdt-partial
// sidecar from one is already present, and restoring the object's own
// stored mtime (see mtimeMetaKey/Upload) once the transfer completes.
// expectedSize is the object's real full size (from BuildPlan's own
// listing) - not derived from this call's own response, since a ranged
// GET's Content-Length reflects only the requested remainder, not the whole
// object.
func Download(ctx context.Context, gate *PauseGate, core *minio.Core, bucket, prefix, relPath, localPath string, expectedSize int64, onProgress func(done, total int64)) error {
	key := prefix + relPath
	partialPath := localPath + partialSuffix

	var offset int64
	if info, err := os.Stat(partialPath); err == nil {
		offset = info.Size()
	}
	if offset >= expectedSize {
		// A partial as large as (or larger than) the real object can't be a
		// valid resume point - there is nothing left to range-request, or the
		// object changed - so start over rather than trust it.
		offset = 0
		_ = os.Remove(partialPath)
	}

	opts := minio.GetObjectOptions{}
	if offset > 0 {
		if err := opts.SetRange(offset, 0); err != nil {
			return err
		}
	}

	reader, objInfo, _, err := core.GetObject(ctx, bucket, key, opts)
	if err != nil {
		return err
	}
	defer reader.Close()

	if err := os.MkdirAll(filepath.Dir(partialPath), 0o755); err != nil {
		return err
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	out, err := os.OpenFile(partialPath, flags, 0o644)
	if err != nil {
		return err
	}

	doneBytes := offset
	if onProgress != nil {
		onProgress(doneBytes, expectedSize)
	}
	pr := &pausingReader{ctx: ctx, gate: gate, r: reader, onRead: func(n int) {
		doneBytes += int64(n)
		if onProgress != nil {
			onProgress(doneBytes, expectedSize)
		}
	}}
	_, copyErr := io.Copy(out, pr)
	closeErr := out.Close()

	if copyErr != nil || closeErr != nil {
		// Whatever arrived is kept on ctx cancel too (Ken, 2026-09-20), like
		// Pause or a transient error: the next Sync resumes from this
		// partial's size rather than re-downloading it.
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	if doneBytes != expectedSize {
		return fmt.Errorf("downloaded %d bytes, expected %d", doneBytes, expectedSize)
	}

	mtime := time.Now()
	if v := objInfo.UserMetadata[mtimeMetaKey]; v != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, v); err == nil {
			mtime = parsed
		}
	}
	if err := os.Rename(partialPath, localPath); err != nil {
		return err
	}
	return os.Chtimes(localPath, mtime, mtime)
}
