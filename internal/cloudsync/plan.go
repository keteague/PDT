package cloudsync

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"

	"github.com/minio/minio-go/v7"
)

// Action is what BuildPlan decided a given relative path needs.
type Action string

const (
	// ActionUpload: present locally, missing from the bucket.
	ActionUpload Action = "upload"
	// ActionDownload: present in the bucket, missing locally.
	ActionDownload Action = "download"
	// ActionSynced: present on both sides with the same size - nothing to
	// do. Same size-as-proxy-for-identical reasoning copyTreeMerge already
	// uses locally (see its own doc comment): driver packages are
	// downloaded once and never silently modified in place afterward, so a
	// size match is already as good a signal as a full content hash here,
	// without needing to read every byte of a 14GB+ shared catalog on every
	// sync.
	ActionSynced Action = "synced"
	// ActionConflict: present on both sides with DIFFERENT sizes - the one
	// case BuildPlan refuses to guess about. Silently preferring either
	// side risks destroying real content for every other technician
	// sharing this bucket, so a conflict is surfaced for a human to look at
	// rather than auto-resolved in either direction; Sync leaves it
	// completely untouched (neither uploads nor downloads it) until the
	// mismatch is resolved by hand.
	ActionConflict Action = "conflict"
)

// PlanItem is one relative path's own local-vs-remote comparison.
type PlanItem struct {
	RelPath    string `json:"relPath"` // slash-separated, e.g. "Canon/26-Tahoe/UFRII.pkg.zip"
	Action     Action `json:"action"`
	LocalSize  int64  `json:"localSize"`  // 0 if not present locally
	RemoteSize int64  `json:"remoteSize"` // 0 if not present in the bucket
}

// remoteObject is BuildPlan's own trimmed-down view of one bucket listing
// entry - just enough to diff against the local tree.
type remoteObject struct {
	size int64
}

// listRemote lists every object under prefix in bucket, paginating via
// ListObjectsV2's own continuation token until the full listing is in hand -
// a real Drivers folder is tens of thousands of files, comfortably more than
// one page's worth (1000 keys), so a caller that only read the first page
// would silently treat everything past it as "missing locally" and try to
// download all of it. Keys come back with prefix stripped, using forward
// slashes, matching PlanItem.RelPath's own shape.
func listRemote(ctx context.Context, core *minio.Core, bucket, prefix string) (map[string]remoteObject, error) {
	out := map[string]remoteObject{}
	token := ""
	for {
		result, err := core.ListObjectsV2(bucket, prefix, "", token, "", 1000)
		if err != nil {
			return nil, fmt.Errorf("listing %s/%s: %w", bucket, prefix, err)
		}
		for _, obj := range result.Contents {
			rel := obj.Key
			if len(rel) >= len(prefix) && rel[:len(prefix)] == prefix {
				rel = rel[len(prefix):]
			}
			if rel == "" {
				continue
			}
			out[rel] = remoteObject{size: obj.Size}
		}
		if !result.IsTruncated {
			return out, nil
		}
		token = result.NextContinuationToken
	}
}

// listLocal walks root (driversRoot() on the caller's side) into the same
// relPath -> size shape as listRemote, for BuildPlan to diff against
// directly. Empty map (not an error) for a root that doesn't exist yet - a
// technician syncing for the very first time, before their own Drivers
// folder has anything in it, should see every remote file as a download
// candidate, not fail outright.
func listLocal(root string) map[string]int64 {
	out := map[string]int64{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		out[filepath.ToSlash(rel)] = info.Size()
		return nil
	})
	return out
}

// BuildPlan compares localRoot's own file tree against every object under
// prefix in bucket and returns one PlanItem per relative path that exists on
// either side, sorted by RelPath for a stable, predictable tree view.
func BuildPlan(ctx context.Context, core *minio.Core, bucket, prefix, localRoot string) ([]PlanItem, error) {
	remote, err := listRemote(ctx, core, bucket, prefix)
	if err != nil {
		return nil, err
	}
	return diff(listLocal(localRoot), remote), nil
}

// diff is BuildPlan's own comparison logic, split out from the network
// calls (listLocal/listRemote) above it purely so it can be unit-tested
// against plain maps without a real bucket to talk to.
func diff(local map[string]int64, remote map[string]remoteObject) []PlanItem {
	relPaths := make(map[string]bool, len(remote)+len(local))
	for rel := range remote {
		relPaths[rel] = true
	}
	for rel := range local {
		relPaths[rel] = true
	}

	items := make([]PlanItem, 0, len(relPaths))
	for rel := range relPaths {
		localSize, hasLocal := local[rel]
		remoteObj, hasRemote := remote[rel]
		item := PlanItem{RelPath: rel, LocalSize: localSize, RemoteSize: remoteObj.size}
		switch {
		case hasLocal && !hasRemote:
			item.Action = ActionUpload
		case hasRemote && !hasLocal:
			item.Action = ActionDownload
		case localSize == remoteObj.size:
			item.Action = ActionSynced
		default:
			item.Action = ActionConflict
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].RelPath < items[j].RelPath })
	return items
}
