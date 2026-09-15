// Package cloudsync talks to a Cloudflare R2 bucket (S3-compatible) to keep
// this technician's local Drivers folder and the team's shared cloud copy in
// sync - the multi-technician equivalent of flash-drive Sync (see
// copytree.go), but between a laptop and a shared bucket every technician
// reads from and writes to, rather than between a laptop and a physically
// plugged-in drive.
package cloudsync

import (
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MultipartThreshold is the file size at or above which Upload switches from
// a single PutObject to a resumable multipart upload - below it, a plain
// PutObject is already fast enough that resumability buys nothing, and a
// real Drivers folder's own bimodal size distribution (many tiny driver
// files, a handful of huge installers) means most files never pay multipart
// overhead at all.
const MultipartThreshold = 32 * 1024 * 1024

// PartSize is how large each multipart upload part is, except the last.
// S3's own multipart API requires every part but the last to be at least
// 5MiB and permits at most 10,000 parts per upload - 16MiB keeps real driver
// installers (rarely more than a few GB) well within maxParts while still
// being large enough that per-part HTTP overhead stays a small fraction of
// total transfer time.
const PartSize = 16 * 1024 * 1024

// maxParts mirrors S3's own hard limit - PartSizeFor grows past PartSize for
// a file large enough that PartSize alone would exceed it, rather than
// simply failing on a very large real driver package.
const maxParts = 10000

// mtimeMetaKey is the custom object metadata key Upload/Download use to
// round-trip a file's original modification time through R2 - S3-compatible
// storage always stamps LastModified as the upload time, not the source
// file's own mtime, so without this every file downloaded by another
// technician would show today's date instead of when it was actually
// downloaded/built. Becomes the "X-Amz-Meta-Mtime" header on the wire;
// minio-go returns it back via ObjectInfo.UserMetadata with that prefix
// already stripped. RFC3339Nano text (not a raw Unix timestamp) so it reads
// sensibly if ever inspected directly in the R2 dashboard.
const mtimeMetaKey = "mtime"

// PartSizeFor returns the part size Upload should use for a file of size
// totalSize - PartSize, unless that many parts would exceed maxParts, in
// which case the part size grows just enough to stay within it.
func PartSizeFor(totalSize int64) int64 {
	if totalSize <= 0 {
		return PartSize
	}
	if (totalSize+PartSize-1)/PartSize <= maxParts {
		return PartSize
	}
	return (totalSize + maxParts - 1) / maxParts
}

// Config is one technician's R2 connection settings - Endpoint/Bucket/
// AccessKeyID come from Settings' own Cloud Sync fields (plaintext, not
// secret), SecretAccessKey from the OS keychain (see LoadSecretKey) rather
// than ever being written to Settings' own plaintext settings.json.
type Config struct {
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
}

// NewCore builds an S3-compatible client for cfg - minio.Core specifically
// (not the higher-level minio.Client it embeds), since Upload/Download need
// Core's own low-level multipart primitives (NewMultipartUpload/
// PutObjectPart/ListObjectParts/CompleteMultipartUpload) to implement real
// resumability - the higher-level Client's own PutObject auto-multiparts
// large files but exposes no way to discover or continue an upload another
// run already started.
//
// Region is "auto" - Cloudflare R2's own documented value; R2 doesn't have
// AWS-style regions, but the S3 API this library speaks requires the field
// to be non-empty for request signing.
func NewCore(cfg Config) (*minio.Core, error) {
	return minio.NewCore(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: true,
		Region: "auto",
	})
}
