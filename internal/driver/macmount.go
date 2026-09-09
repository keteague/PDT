package driver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// mountPointRe pulls the mount-point string out of `hdiutil attach -plist`'s
// XML output. Confirmed against a real vendor image (Kyocera's macOS
// driver .dmg): only the system-entities dict for the actual mountable
// volume carries a <key>mount-point</key>, sibling partition-map/scheme
// entries never do - a plain line-pair regex is enough, no need for a full
// plist parser for this one field.
var mountPointRe = regexp.MustCompile(`(?s)<key>mount-point</key>\s*<string>(.*?)</string>`)

// mountDmg attaches path read-only and not in the Finder (-nobrowse), and
// returns its mount point plus a detach func that unmounts it - always call
// detach once done, even on a later error, so a failed driver install
// doesn't leave a mounted volume behind.
func mountDmg(path string) (mountPoint string, detach func() error, err error) {
	out, err := exec.Command("hdiutil", "attach", "-nobrowse", "-readonly", "-plist", path).Output()
	if err != nil {
		return "", nil, fmt.Errorf("mounting %s: %w", path, err)
	}
	m := mountPointRe.FindSubmatch(out)
	if m == nil {
		return "", nil, fmt.Errorf("mounting %s: no mountable volume found in hdiutil output", path)
	}
	mountPoint = string(m[1])
	detach = func() error {
		return exec.Command("hdiutil", "detach", mountPoint, "-quiet").Run()
	}
	return mountPoint, detach, nil
}

// findFirstByExt walks root looking for the first file whose extension
// matches ext (case-insensitive), skipping Archive/etc segments the same way
// scanMacPackages does. Returns "" if none found.
func findFirstByExt(root, ext string) string {
	found := ""
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if d.IsDir() {
			if strings.EqualFold(d.Name(), "etc") || strings.EqualFold(d.Name(), "Archive") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ext) {
			found = path
		}
		return nil
	})
	return found
}

// LocatePkg resolves path (a .pkg or .dmg found by BuildMacCatalog) to an
// actual, on-disk .pkg ready for `installer -pkg`/`pkgutil --expand-full`. A
// .pkg passes through unchanged with a no-op cleanup. A .dmg is mounted, and
// the first .pkg found inside is used; if none is found directly, the first
// nested .dmg is mounted in turn (one level - confirmed sufficient against
// the README's own "some wrap a nested .dmg" note, and every real vendor
// image inspected so far nests at most once) and searched the same way.
// cleanup unmounts everything this call mounted, in reverse order - always
// call it, even after an error, in case an outer mount succeeded before an
// inner step failed.
func LocatePkg(path string) (pkgPath string, cleanup func(), err error) {
	if strings.EqualFold(filepath.Ext(path), ".pkg") {
		return path, func() {}, nil
	}
	if !strings.EqualFold(filepath.Ext(path), ".dmg") {
		return "", func() {}, fmt.Errorf("%s is neither a .pkg nor a .dmg", path)
	}

	var detaches []func() error
	cleanup = func() {
		for i := len(detaches) - 1; i >= 0; i-- {
			_ = detaches[i]()
		}
	}

	mountPoint, detach, err := mountDmg(path)
	if err != nil {
		return "", cleanup, err
	}
	detaches = append(detaches, detach)

	if pkg := findFirstByExt(mountPoint, ".pkg"); pkg != "" {
		return pkg, cleanup, nil
	}

	if nested := findFirstByExt(mountPoint, ".dmg"); nested != "" {
		nestedMount, nestedDetach, err := mountDmg(nested)
		if err != nil {
			return "", cleanup, err
		}
		detaches = append(detaches, nestedDetach)
		if pkg := findFirstByExt(nestedMount, ".pkg"); pkg != "" {
			return pkg, cleanup, nil
		}
	}

	return "", cleanup, fmt.Errorf("no .pkg found inside %s", path)
}

// PackageLabel returns a best-effort human-readable identifier for the
// package at path - for logging/display only, NOT a comparable version.
//
// Confirmed against a real vendor package (Kyocera's macOS driver, a
// distribution-style metapackage with 14 component sub-packages): there is
// no reliable, single product-version field anywhere in a macOS installer
// package the way a Windows .inf's DriverVer= line gives one for the whole
// driver. Every component <pkg-ref>'s own "version" attribute in the
// Distribution script was boilerplate "1.0" across the board, and each
// sub-package's own PackageInfo carried version="0"; the only genuinely
// informative string was the package's own filename (which happened to embed
// a build date - "Kyocera OS 13+ Web build 2025.12.01" - vendor naming
// convention, not something to rely on generally). So this tries a flat
// package's own top-level PackageInfo version attribute first (works for a
// simple, single-component .pkg), and falls back to the filename (without
// its extension) when that's missing/unhelpful - callers needing to pick the
// newest of several candidates should sort by file modification time, not by
// this label (see ResolveMac).
func PackageLabel(pkgPath string) string {
	tmpDir, err := os.MkdirTemp("", "pdt-pkginfo-*")
	if err == nil {
		defer os.RemoveAll(tmpDir)
		if err := exec.Command("pkgutil", "--expand-full", pkgPath, filepath.Join(tmpDir, "expand")).Run(); err == nil {
			if v, ok := readPackageInfoVersion(filepath.Join(tmpDir, "expand", "PackageInfo")); ok {
				return v
			}
		}
	}
	base := filepath.Base(pkgPath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// Requires a literal space before `version="` (not just \b) so this matches
// the pkg-info element's own "version" attribute without also matching the
// tail of a "generator-version"/"format-version" attribute - confirmed
// necessary against a real PackageInfo file, whose generator-version="..."
// comes before version="..." and would otherwise be matched first.
var packageInfoVersionRe = regexp.MustCompile(`<pkg-info\b[^>]* version="([^"]*)"`)

// readPackageInfoVersion reads a flat .pkg's own top-level PackageInfo file
// (present only for a single-component package, not a distribution-style
// metapackage - see PackageLabel's doc comment) and returns its version
// attribute, if present and not the "0" placeholder version="0" typically
// carries for a sub-component with no real version of its own.
func readPackageInfoVersion(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	m := packageInfoVersionRe.FindSubmatch(data)
	if m == nil {
		return "", false
	}
	v := strings.TrimSpace(string(m[1]))
	if v == "" || v == "0" {
		return "", false
	}
	return v, true
}
