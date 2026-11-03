package cloudsync

import (
	"context"
	"errors"
	"time"

	"github.com/minio/minio-go/v7"
)

// probeKeyName is the object key ProbeWriteAccess starts (and immediately
// aborts) a multipart upload for, under the configured prefix. Dot-prefixed,
// so even an upload whose abort somehow failed could never surface in a plan
// (isIgnoredRemotePath drops dot segments) or collide with a real driver
// path.
const probeKeyName = ".pdt-access-probe"

// ErrReadOnly marks a refused write - returned by Upload's callers once
// IsAccessDenied identifies one, so a run can stop attempting further
// uploads instead of failing each one separately.
var ErrReadOnly = errors.New("read-only access: this Access Key can't upload to the bucket")

// ProbeWriteAccess reports whether cfg's key may write to bucket. S3/R2 has
// no call that just reports a key's permissions, so this asks for a real
// write - NewMultipartUpload - and aborts it at once if granted. An initiated-
// then-aborted multipart upload never becomes an object, so nothing appears
// in anyone's listing and no cleanup delete is needed. Two Class A requests
// per check.
//
// Returns (false, nil) for an explicit AccessDenied - a read-only key, e.g.
// the public guest key - and (true, nil) when the upload was allowed. Any
// other error (network, bad endpoint, wrong bucket) is returned as-is with
// canWrite true: an inconclusive probe must never hide uploads from a
// technician who really can write; the real transfer will report the real
// problem.
func ProbeWriteAccess(ctx context.Context, core *minio.Core, bucket, prefix string) (canWrite bool, err error) {
	key := prefix + probeKeyName
	uploadID, err := core.NewMultipartUpload(ctx, bucket, key, minio.PutObjectOptions{})
	if err != nil {
		if IsAccessDenied(err) {
			return false, nil
		}
		return true, err
	}
	abortCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = core.AbortMultipartUpload(abortCtx, bucket, key, uploadID)
	return true, nil
}

// IsAccessDenied reports whether err is the S3 "AccessDenied" refusal R2
// returns for a write attempted with a read-only key.
func IsAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrReadOnly) {
		return true
	}
	return minio.ToErrorResponse(err).Code == "AccessDenied"
}
