package cloudsync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/minio/minio-go/v7"

	"PDT/internal/driver"
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
//
// ctx is checked between pages, not passed into ListObjectsV2 itself -
// minio-go v7.3.0's Core.ListObjectsV2 (unlike every other Core method used
// elsewhere in this package) takes no context.Context parameter at all, so
// there's no way to abort a single in-flight page request. This was a real
// live bug: SyncCloud's own Cancel button re-runs BuildPlan (this function)
// at the very start of every sync, before any per-file transfer (which DOES
// respect ctx - see transfer.go) even begins, so hitting Cancel while a
// large shared bucket's listing was still paging left the button frozen on
// "Canceling..." until the entire multi-page listing finished on its own,
// with no way to interrupt it. Checking ctx.Done() right after each page
// lands (rather than mid-request, which isn't possible here) bounds that
// wait to however long one more page's own request-response cycle takes,
// not however many pages remain - the same "can't cancel the network call
// itself, but never let that freeze the button indefinitely" reasoning
// cloudSyncCancelGracePeriod (cloudsync_app.go) already applies to the
// per-file transfer join, just one level further out.
// isIgnoredRemotePath applies the same dotfile/dotfolder exclusion
// walkLocal enforces locally (see driver.IsIgnoredDotEntry) to a remote
// object's own full relative key - listRemote has no per-directory walk to
// hook a single-entry check into the way walkLocal does, so it checks every
// path segment instead. Once a PdtInfCacheDirName segment is seen, every
// segment under it is let through unfiltered rather than checked further -
// the same "and its contents" exception IsIgnoredDotEntry applies to one
// entry name, extended across the rest of a full path.
func isIgnoredRemotePath(rel string) bool {
	segs := strings.Split(rel, "/")
	// A per-manufacturer catalog.<mfg>.json is excluded outright, checked
	// first and independent of the loop below - see IsCatalogFileName's own
	// doc comment: unlike PdtInfCacheDirName's contents, it bakes in a fresh
	// per-build timestamp and the building machine's own view of each source
	// archive's modTime, so two technicians' independently-rebuilt catalogs
	// for the identical driver repo are never byte-identical and would
	// otherwise show a permanent, never-resolvable Cloud Sync conflict.
	// Cheaply rebuilt locally instead; each machine just keeps its own copy.
	if driver.IsCatalogFileName(segs[len(segs)-1]) {
		return true
	}
	for _, seg := range segs {
		if seg == driver.PdtInfCacheDirName {
			return false
		}
		if driver.IsIgnoredDotEntry(seg) {
			return true
		}
	}
	return false
}

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
			// Ignore any dotfile/dotfolder (.DS_Store, a stray .git, etc.)
			// already sitting in the bucket from before this exclusion
			// existed - walkLocal never lists one locally anymore (see
			// driver.IsIgnoredDotEntry), so leaving one listed here would
			// just make it look like a "Download" instead of correctly
			// disappearing from the tree entirely. PdtInfCacheDirName and
			// its contents are the one exception - real content, not
			// clutter - so isIgnoredRemotePath lets those through.
			if isIgnoredRemotePath(rel) {
				continue
			}
			out[rel] = remoteObject{size: obj.Size}
		}
		if !result.IsTruncated {
			return out, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
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
//
// A manual recursive walk (via os.ReadDir per directory), not a single
// filepath.WalkDir - needed so driver.ExtractedSiblingDirs can see a whole
// directory's sibling list at once to decide what to skip, the same reason
// copytree.go's own collectCopyJobs is shaped this way. An archive's own
// extracted sibling folder (Foo.zip -> Foo/) is a derived, re-creatable
// artifact - Cloud Sync must not upload it any more than flash-drive Sync
// does (GitHub issue #10: a real Drivers folder was 5.4GB/22,570 files with
// both extracted siblings and the .inf-only cache kept forever, vs.
// ~1.5GB/~25 files for just the archives). Skipping the directory outright,
// rather than filtering its contents out after the fact, also avoids paying
// the walk cost for whatever's inside it. Every other dotfile/dotfolder is
// skipped the same way, except PdtInfCacheDirName itself - see
// driver.IsIgnoredDotEntry's own doc comment for why that one's real
// content, not clutter, and travels with the rest of the folder.
func listLocal(root string) map[string]int64 {
	out := map[string]int64{}
	walkLocal(root, "", out)
	return out
}

func walkLocal(absDir, relDir string, out map[string]int64) {
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return
	}
	extractedSiblings := driver.ExtractedSiblingDirs(absDir, entries)
	for _, entry := range entries {
		if entry.IsDir() && extractedSiblings[entry.Name()] {
			continue
		}
		if driver.IsIgnoredDotEntry(entry.Name()) {
			continue
		}
		// catalog.<mfg>.json is excluded from Sync entirely - see
		// isIgnoredRemotePath's own doc comment (same reasoning, applied to
		// the local side of the same comparison).
		if !entry.IsDir() && driver.IsCatalogFileName(entry.Name()) {
			continue
		}
		childAbs := filepath.Join(absDir, entry.Name())
		childRel := entry.Name()
		if relDir != "" {
			childRel = relDir + "/" + entry.Name()
		}
		if entry.IsDir() {
			walkLocal(childAbs, childRel, out)
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out[childRel] = info.Size()
	}
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
