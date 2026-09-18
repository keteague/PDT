package driver

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// isDmgLikePath reports whether path is a plain .dmg, or a gzip-compressed
// .dmg.gz - the real shape Toshiba's own current download ships as
// (confirmed live, 2026-09-13: `hdiutil attach` does NOT auto-detect a plain
// gzip wrapper on its own - "image not recognized" - unlike the already-
// handled nested-.dmg-inside-a-.dmg case, which is a real UDIF image at
// every level). Checked by suffix, not filepath.Ext (which would only ever
// see the trailing ".gz" on a compound ".dmg.gz" name).
func isDmgLikePath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".dmg") || strings.HasSuffix(lower, ".dmg.gz")
}

// decompressGzipToTemp gunzip-decompresses path into a caller-owned temp
// file, returning its path plus a cleanup that removes it - openDmg's own
// helper for a ".dmg.gz" input on darwin (macdmgopen_darwin.go), since
// `hdiutil attach` needs a real, already-decompressed UDIF image on disk to
// open at all. Pure Go (compress/gzip, no OS dependency) - shared as-is by
// the Windows openDmg (macdmgopen_windows.go, GitHub issue #3 Phase 1),
// which needs the identical decompression step before handing a real file
// to 7z.exe.
func decompressGzipToTemp(path string) (tmpPath string, cleanup func(), err error) {
	noop := func() {}
	f, err := os.Open(path)
	if err != nil {
		return "", noop, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", noop, err
	}
	defer gz.Close()

	tmp, err := os.CreateTemp("", "pdt-mac-dmg-*.dmg")
	if err != nil {
		return "", noop, err
	}
	defer tmp.Close()
	if _, err := io.Copy(tmp, gz); err != nil {
		os.Remove(tmp.Name())
		return "", noop, err
	}
	return tmp.Name(), func() { os.Remove(tmp.Name()) }, nil
}

// findFirstByExt walks root looking for the first file whose extension
// matches ext (case-insensitive), skipping Archive/etc/__MACOSX segments and
// "._"-prefixed AppleDouble resource-fork stub files - the same convention
// scanMacPackages/collectByExt already apply, needed here too now that a
// caller can be searching inside a temp directory resolveMacZipSource just
// extracted a real vendor .zip into (confirmed live: a real Canon download's
// own zip carries a __MACOSX/._<name>.dmg stub alongside the real .dmg -
// without this, a search that happened to visit the stub first would return
// a ~300-byte junk file instead of the real 80+ MB one; relying on
// alphabetical WalkDir ordering to avoid that by luck is not a real
// guarantee). Returns "" if none found.
func findFirstByExt(root, ext string) string {
	found := ""
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if d.IsDir() {
			if strings.EqualFold(d.Name(), "etc") || strings.EqualFold(d.Name(), "Archive") || d.Name() == "__MACOSX" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), "._") {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ext) {
			found = path
		}
		return nil
	})
	return found
}

// collectByExt walks root collecting every file whose extension matches one
// of exts (case-insensitive) - findFirstByExt's own "first match" traversal
// (same Archive/etc-skipping convention), but every match instead of just
// one. Also skips __MACOSX dirs and "._"-prefixed AppleDouble stub files, the
// same convention maccatalog.go's own scanMacPackages/scanOpenPrintingPPDs
// already apply when walking a real vendor image.
func collectByExt(root string, exts ...string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.EqualFold(d.Name(), "etc") || strings.EqualFold(d.Name(), "Archive") || d.Name() == "__MACOSX" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), "._") {
			return nil
		}
		lower := strings.ToLower(path)
		for _, ext := range exts {
			if strings.HasSuffix(lower, strings.ToLower(ext)) {
				out = append(out, path)
				return nil
			}
		}
		return nil
	})
	return out
}

// LocateLoosePPDs resolves path (like LocatePkg) - transparently extracting
// it first, into a throwaway temp directory, if it's a .zip (see
// resolveMacZipSource) - then mounts it and returns every loose
// *.ppd/*.ppd.gz file found inside - for a package shape with no installer
// .pkg at all (confirmed live against Canon's own "PPD" bucket download:
// PPDv5.50_mac.zip wraps one PPDv5.50_mac.dmg, which itself wraps one nested
// mac-ppd-*.dmg, itself a plain folder-per-model tree of *.PPD.gz files with
// no *cupsFilter line in any of them - real Generic PostScript PPDs, not a
// proprietary driver, so there's no installer to look for at all). Same
// one-level-of-nesting mount convention as LocatePkg - checks the outer
// mount first, then one level of nested .dmg - but unlike LocatePkg, every
// nested .dmg found gets mounted and collected, not just the first
// (GitHub issue #6: Canon's own "PPD" bucket nests one .dmg per locale
// variant side by side - PS_PPD/mac-ppd-v550-uken-16.dmg and
// PS_PPD/mac-ppd-v550-usen-16.dmg, confirmed live, both containing the same
// real model's own PPD under the same real filename - mounting only
// "whichever happened to mount first" silently dropped every other
// locale's own PPDs. Matches the older flat-folder-per-locale version of
// this same package's own already-correct "collect everything" behavior -
// the older PPDv5.35_mac.dmg has both locales' folders sitting right at
// the top level, no nesting at all, so collectByExt's own first call
// already found both). One bad nested .dmg (fails to mount) doesn't block
// the others - best-effort,
// same discipline every other multi-item walk in this codebase already
// holds itself to. cleanup unmounts everything this call mounted AND
// removes any temp extraction directory resolveMacZipSource created;
// always call it, even after an error.
func LocateLoosePPDs(path string) (ppdPaths []string, cleanup func(), err error) {
	realPath, zipCleanup, zerr := resolveMacZipSource(path)
	if zerr != nil {
		return nil, func() {}, zerr
	}
	ppdPaths, innerCleanup, err := locateLoosePPDsFromRealPath(realPath)
	return ppdPaths, func() { innerCleanup(); zipCleanup() }, err
}

func locateLoosePPDsFromRealPath(path string) (ppdPaths []string, cleanup func(), err error) {
	if !isDmgLikePath(path) {
		return nil, func() {}, fmt.Errorf("%s is not a .dmg", path)
	}

	var detaches []func() error
	cleanup = func() {
		for i := len(detaches) - 1; i >= 0; i-- {
			_ = detaches[i]()
		}
	}

	mountPoint, detach, err := openDmg(path)
	if err != nil {
		return nil, cleanup, err
	}
	detaches = append(detaches, detach)

	if found := collectByExt(mountPoint, ".ppd", ".ppd.gz"); len(found) > 0 {
		return found, cleanup, nil
	}

	var nestedPPDs []string
	for _, nested := range collectByExt(mountPoint, ".dmg") {
		nestedMount, nestedDetach, mountErr := openDmg(nested)
		if mountErr != nil {
			// Best-effort: one bad nested .dmg (a locale variant that
			// happens not to mount) shouldn't block collecting PPDs from
			// the others.
			continue
		}
		detaches = append(detaches, nestedDetach)
		nestedPPDs = append(nestedPPDs, collectByExt(nestedMount, ".ppd", ".ppd.gz")...)
	}
	if len(nestedPPDs) > 0 {
		return nestedPPDs, cleanup, nil
	}

	return nil, cleanup, fmt.Errorf("no loose PPD files found inside %s", path)
}

// InspectDmg is a debug-only wrapper around the package-internal openDmg
// seam (macdmgopen_darwin.go/macdmgopen_windows.go) - opens path (a real
// mount via hdiutil on darwin, a 7z.exe extraction on Windows) and returns
// every .pkg/.dmg found directly inside it. No real codepath uses this -
// LocatePkg/LocatePkgWithChain/LocateLoosePPDs call openDmg directly and go
// straight on to resolve a real .pkg or PPDs rather than just listing what's
// there. Exists purely for cmd/pdtdebug's own "macdmg" subcommand (GitHub
// issue #3, Phase 1's own "give yourself a test hook before wiring into the
// UI" step) to hand-test openDmg against real vendor .dmg files before
// anything downstream depends on it.
func InspectDmg(path string) (found []string, err error) {
	root, cleanup, err := openDmg(path)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	found = append(collectByExt(root, ".pkg"), collectByExt(root, ".dmg")...)
	return found, nil
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
	pkgPath, _, cleanup, err = LocatePkgWithChain(path)
	return pkgPath, cleanup, err
}

// LocatePkgWithChain is LocatePkg plus the chain of files it actually had to
// open to get there - path itself first (never the throwaway temp file
// resolveMacZipSource may have extracted it to internally, which has no
// meaningful identity of its own), then each nested .dmg it mounted along
// the way, ending with the resolved .pkg. Used by the catalog-building code
// (macmodel.go) to record exactly which files (and their own parents)
// produced a given set of indexed models - MacCatalogDB's own provenance
// chain. LocatePkg itself is a thin wrapper that just drops the chain;
// every one of its own existing callers is unaffected.
//
// Transparently extracts path first, into a throwaway temp directory, if
// it's a .zip (see resolveMacZipSource) - GitHub issue #11: a .zip is
// resolved fresh on every call, never left behind as a permanent sibling
// folder the way this function's callers used to rely on
// (ensureMacZipsExtracted, called eagerly by the catalog scan before this
// ever ran). cleanup unmounts everything AND removes that temp directory.
func LocatePkgWithChain(path string) (pkgPath string, chain []string, cleanup func(), err error) {
	realPath, zipCleanup, zerr := resolveMacZipSource(path)
	if zerr != nil {
		return "", nil, func() {}, zerr
	}
	pkgPath, innerChain, innerCleanup, err := locatePkgWithChainFromRealPath(realPath)
	cleanup = func() { innerCleanup(); zipCleanup() }
	if err != nil {
		return "", nil, cleanup, err
	}
	// innerChain[0] is realPath - either path itself (the common,
	// non-.zip case, where resolveMacZipSource was a no-op pass-through and
	// this is a no-op replacement) or a meaningless throwaway temp path;
	// replaced with path itself either way, since indexFamilyPackage's own
	// outerRef is built from pkg.Path directly and expects chain[0] to
	// duplicate it exactly (see its own doc comment).
	chain = append([]string{path}, innerChain[1:]...)
	return pkgPath, chain, cleanup, nil
}

func locatePkgWithChainFromRealPath(path string) (pkgPath string, chain []string, cleanup func(), err error) {
	if strings.EqualFold(filepath.Ext(path), ".pkg") {
		return path, []string{path}, func() {}, nil
	}
	if !isDmgLikePath(path) {
		return "", nil, func() {}, fmt.Errorf("%s is neither a .pkg nor a .dmg", path)
	}

	var detaches []func() error
	cleanup = func() {
		for i := len(detaches) - 1; i >= 0; i-- {
			_ = detaches[i]()
		}
	}

	mountPoint, detach, err := openDmg(path)
	if err != nil {
		return "", nil, cleanup, err
	}
	detaches = append(detaches, detach)

	if pkg := findFirstByExt(mountPoint, ".pkg"); pkg != "" {
		return pkg, []string{path, pkg}, cleanup, nil
	}

	if nested := findFirstByExt(mountPoint, ".dmg"); nested != "" {
		nestedMount, nestedDetach, err := openDmg(nested)
		if err != nil {
			return "", nil, cleanup, err
		}
		detaches = append(detaches, nestedDetach)
		if pkg := findFirstByExt(nestedMount, ".pkg"); pkg != "" {
			return pkg, []string{path, nested, pkg}, cleanup, nil
		}
	}

	return "", nil, cleanup, fmt.Errorf("no .pkg found inside %s", path)
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
	base = strings.TrimSuffix(base, filepath.Ext(base))
	// A ".dmg.gz" name (Toshiba's own real shape - see isDmgLikePath) only
	// has its trailing ".gz" stripped by the plain Ext-based trim above,
	// leaving ".dmg" in the label ("TOSHIBA_ColorMFP.dmg") - strip the
	// second suffix too, but only for this specific compound shape, never a
	// second blind Ext-based strip: a real vendor filename can carry
	// legitimate dots in its own version number (e.g.
	// "XeroxDrivers_5.19.3_2562.dmg", where filepath.Ext of the
	// single-.dmg-stripped result would wrongly find ".3_2562" as an
	// "extension" and mangle the label).
	if strings.HasSuffix(strings.ToLower(filepath.Base(pkgPath)), ".dmg.gz") {
		base = strings.TrimSuffix(base, ".dmg")
	}
	return base
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

// packageInfoInstallLocationRe matches a sub-package's own declared
// install-location attribute - present on every real Ricoh sub-package
// (e.g. `install-location="/Library/Printers/PPDs/Contents/Resources/"` on
// its own baseline "ppds.pkg"), absent on a package whose Payload instead
// bakes the real destination into each entry's own relative path (confirmed
// against a real legacy "RicohPrinterDrivers.pkg" - see macricoh.go).
var packageInfoInstallLocationRe = regexp.MustCompile(`<pkg-info\b[^>]* install-location="([^"]*)"`)

// readPackageInfoInstallLocation reads a sub-package's own declared
// install-location, ok false when the attribute is absent entirely (not
// just empty) - readPackageInfoVersion's own sibling, same file, same
// regex-over-plain-XML approach (no need for a full XML parser just for one
// attribute already proven reliable this way).
func readPackageInfoInstallLocation(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	m := packageInfoInstallLocationRe.FindSubmatch(data)
	if m == nil {
		return "", false
	}
	return string(m[1]), true
}
