package driver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// hdiutilMount is one currently-mounted image, parsed from `hdiutil info`'s
// own plain-text output (see parseHdiutilInfo). mountPoint is "" for an
// attached image with no real /Volumes mount (rare in practice for anything
// this codebase itself ever mounts, but handled rather than assumed away).
type hdiutilMount struct {
	imagePath  string
	mountPoint string
}

// parseHdiutilInfo parses `hdiutil info`'s own plain-text output (no -plist
// - this file lists every currently-mounted image system-wide, a different
// shape from mountPointRe's own single-image `attach -plist` output) into
// one hdiutilMount per "====...====" - delimited block. Confirmed against
// real output (2026-09-16): each block is a run of "key : value" lines
// (image-path is the only one this cares about) followed by one or more
// tab-separated device lines, the last field of which - when present - is
// the real /Volumes mount point (`/dev/disk4s1\t<scheme>\t/Volumes/X`); a
// bare partition-map/scheme line has only two fields and is skipped. Never
// errors - a genuinely malformed or empty block just yields a zero-value
// entry, which ReconcileStaleMounts's own matching rules below will never
// treat as PDT-owned anyway (an empty imagePath matches nothing).
func parseHdiutilInfo(output string) []hdiutilMount {
	var mounts []hdiutilMount
	var cur hdiutilMount
	haveBlock := false
	flush := func() {
		if haveBlock {
			mounts = append(mounts, cur)
		}
		cur = hdiutilMount{}
		haveBlock = false
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "====") {
			flush()
			continue
		}
		if strings.HasPrefix(line, "image-path") {
			if i := strings.Index(line, ":"); i >= 0 {
				cur.imagePath = strings.TrimSpace(line[i+1:])
				haveBlock = true
			}
			continue
		}
		if strings.HasPrefix(line, "/dev/") {
			fields := strings.Split(line, "\t")
			if len(fields) >= 3 {
				cur.mountPoint = strings.TrimSpace(fields[2])
			}
		}
	}
	flush()
	return mounts
}

// isPDTOwnedTempPath reports whether path lives under a "pdt-"-prefixed
// directory or file directly inside the OS temp dir - resolveMacZipSource's
// own extraction directories (pdt-maczip-*) and decompressGzipToTemp's own
// decompressed copies (pdt-mac-dmg-*), the two cases where this codebase
// mounts something that was never a real file under the Drivers folder to
// begin with.
func isPDTOwnedTempPath(path, tmpDir string) bool {
	rel, err := filepath.Rel(tmpDir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	first := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	return strings.HasPrefix(first, "pdt-")
}

// ReconcileStaleMounts detaches any real, currently-mounted volume this
// codebase's own mountDmg (macmount.go) would have created but never got a
// chance to unmount - GitHub issue #1: a real Canon/Toshiba package
// confirmed live (2026-09-16) still mounted from an earlier run, with
// mounting process IDs that had long since exited. Full audit of mountDmg/
// LocatePkgWithChain/LocateLoosePPDs and every one of their real call sites
// found no bug in the cleanup logic itself - every path correctly composes
// and calls its own detach func. The real cause: app.go's own startup()
// calls loadCatalog synchronously, mounting real packages along the way: if
// the whole process is terminated (a crash, force-quit, or - confirmed as
// the likely real-world trigger this session - an external SIGTERM/timeout
// arriving mid-catalog-build) before that call stack unwinds normally, no
// deferred cleanup ever runs - Go's defer only fires on a normal return
// within the same goroutine, not when the whole process is torn down out
// from under it. Rather than trying to make shutdown itself airtight
// against every termination cause (impossible against a hard crash or
// power loss), this sweeps for and cleans up whatever got left behind, once,
// at the START of the next run - called from platformStartup, before
// loadCatalog gets a chance to mount anything new.
//
// A mount only ever gets detached here when its own image-path is
// demonstrably this codebase's own: either isPDTOwnedTempPath (Canon's own
// zip-extraction detour, Toshiba's own gzip-decompression detour), or living
// directly under driversRoot (every other manufacturer's own real .dmg,
// mounted straight from its real location under the Drivers folder - no
// temp copy involved at all). A nested mount (LocatePkg's/LocateLoosePPDs'
// own "one level of nested .dmg" case - e.g. a locale-variant .dmg mounted
// from inside an already-mounted Canon volume) is recognized in a second
// pass: its own image-path lives under a /Volumes/<X> that IS the real
// mount point of an entry already confirmed PDT-owned in the first pass.
// Detaches nested-first (children before the parent they depend on) -
// detaching a volume before something still mounted from a file on it would
// leave that inner mount orphaned, reading from an image that's already
// gone.
//
// Best-effort throughout, matching platformStartup's own convention: a
// missing/misbehaving hdiutil, or a detach that itself fails (something
// else still has the volume open, say), is silently skipped rather than
// blocking startup or surfacing an error a technician has no real action to
// take on - the same low-urgency, "worth fixing properly, not urgently"
// severity the issue itself was filed with.
func ReconcileStaleMounts(driversRoot string) {
	out, err := exec.Command("hdiutil", "info").Output()
	if err != nil {
		return
	}
	mounts := parseHdiutilInfo(string(out))
	nested, outer := mountsToDetach(mounts, os.TempDir(), driversRoot)
	for _, mp := range nested {
		_ = exec.Command("hdiutil", "detach", mp, "-quiet").Run()
	}
	for _, mp := range outer {
		_ = exec.Command("hdiutil", "detach", mp, "-quiet").Run()
	}
}

// mountsToDetach is ReconcileStaleMounts's own decision logic, pulled out
// as a pure function so it's testable against real captured `hdiutil info`
// output without needing to actually mount/detach anything. Returns the
// real mount points to detach, split into nested (detach first - a mount
// whose own image-path lives under another owned entry's own mount point,
// so detaching the parent first would leave it reading from an
// already-vanished image) and outer (detach after). A mount with no real
// /Volumes mount point at all is skipped entirely - nothing to hand
// `hdiutil detach` in that case.
func mountsToDetach(mounts []hdiutilMount, tmpDir, driversRoot string) (nested, outer []string) {
	if len(mounts) == 0 {
		return nil, nil
	}
	owned := make([]bool, len(mounts))
	for i, m := range mounts {
		if m.imagePath == "" {
			continue
		}
		if isPDTOwnedTempPath(m.imagePath, tmpDir) {
			owned[i] = true
			continue
		}
		if driversRoot != "" {
			if rel, relErr := filepath.Rel(driversRoot, m.imagePath); relErr == nil && !strings.HasPrefix(rel, "..") {
				owned[i] = true
			}
		}
	}
	// Second pass: a nested mount whose own image-path lives under an
	// already-owned entry's own real mount point.
	for i, m := range mounts {
		if owned[i] || m.imagePath == "" {
			continue
		}
		for j, other := range mounts {
			if !owned[j] || other.mountPoint == "" {
				continue
			}
			if rel, relErr := filepath.Rel(other.mountPoint, m.imagePath); relErr == nil && !strings.HasPrefix(rel, "..") {
				owned[i] = true
				break
			}
		}
	}

	// Detach nested (owned-because-of-another-owned-mount-point) entries
	// before the outer ones they may depend on. A plain two-pass split is
	// enough - PDT itself never nests more than one level deep (the same
	// convention LocatePkg/LocateLoosePPDs already hold themselves to).
	for i, m := range mounts {
		if !owned[i] || m.mountPoint == "" {
			continue
		}
		isNested := false
		for j, other := range mounts {
			if j == i || other.mountPoint == "" {
				continue
			}
			if rel, relErr := filepath.Rel(other.mountPoint, m.imagePath); relErr == nil && !strings.HasPrefix(rel, "..") {
				isNested = true
				break
			}
		}
		if isNested {
			nested = append(nested, m.mountPoint)
		} else {
			outer = append(outer, m.mountPoint)
		}
	}
	return nested, outer
}
