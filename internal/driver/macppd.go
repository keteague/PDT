package driver

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ppdNickNameRe matches a PPD's own *NickName line - the human-readable
// product name a technician would actually recognize (e.g. "Canon iR-ADV
// C5840/5850"), confirmed against a real Canon PPD to be far more reliable
// for model matching than the PPD's own filename: Canon's real filenames are
// cryptic codes ("CNPZUIRAC5840ZU.ppd.gz") that share no matchable substring
// or even in-order character sequence with how a technician would actually
// type the model ("iR-ADV C5840") - FuzzyMatchScore's subsequence fallback
// specifically fails on the "-" and " " characters neither the filename nor
// most abbreviated codes ever contain, so filename-based matching isn't just
// weaker here, it's a hard zero for a manufacturer shaped like this one.
var ppdNickNameRe = regexp.MustCompile(`(?m)^\*NickName:\s*"([^"]*)"`)

// ppdModelNameRe is the fallback when a PPD has no *NickName at all (seen on
// some vendors' PPDs, which only declare *ModelName) - same field, same
// convention, just a different PPD keyword some manufacturers prefer.
var ppdModelNameRe = regexp.MustCompile(`(?m)^\*ModelName:\s*"([^"]*)"`)

// ReadPPDNickName reads ppdPath's own *NickName (falling back to
// *ModelName) - transparently gzip-decompressing if the path ends in
// ".gz", the form every PPD under /Library/Printers/PPDs/Contents/Resources
// - and returns ("", false) for anything unreadable or lacking both fields,
// rather than erroring: a PPD this can't identify by name is still usable,
// just not something model-matching can use as positive evidence.
func ReadPPDNickName(ppdPath string) (string, bool) {
	f, err := os.Open(ppdPath)
	if err != nil {
		return "", false
	}
	defer f.Close()

	var r io.Reader = f
	if strings.HasSuffix(strings.ToLower(ppdPath), ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return "", false
		}
		defer gz.Close()
		r = gz
	}

	// PPDs are small plain-text files (confirmed against real Canon/Kyocera
	// PPDs: well under 500KB even uncompressed) - capped defensively rather
	// than trusting an arbitrary file's declared size.
	data, err := io.ReadAll(io.LimitReader(r, 4<<20))
	if err != nil {
		return "", false
	}
	if m := ppdNickNameRe.FindSubmatch(data); m != nil {
		return string(m[1]), true
	}
	if m := ppdModelNameRe.FindSubmatch(data); m != nil {
		return string(m[1]), true
	}
	return "", false
}

// ppdEntry is one PPD found inside an expanded package payload - its own
// path (inside the scratch expand dir - gone once the caller's temp dir is
// removed, so only its Base() is safe to keep around) alongside its
// *NickName. packagePPDEntries' own return type; MacPPDVariant's own
// Filename field is filepath.Base of one of these.
type ppdEntry struct {
	Path     string
	NickName string
}

// subPackageResult is one sub-package that actually contributed at least
// one PPD during extractPPDsFromExpandedPkg - its own name (e.g.
// "Canon_Family_Printer_Device.pkg" - a distribution archive's real PPDs
// turned out to live in just one of five sub-packages, confirmed live; the
// other four are icons/profiles/core-binary/accounting-manager payloads
// with none) and declared version (readPackageInfoVersion - macmount.go,
// "" if it has none).
// Purely informational - MacCatalogDB's own provenance record of "which
// package, specifically, produced these entries" - never used to decide
// whether re-inspection is needed (see MacPackageRef's own doc comment for
// why only the outermost file's identity is checked for that).
type subPackageResult struct {
	Name    string
	Version string
}

// packagePPDEntries expands pkgPath's own structure and returns every
// .ppd/.ppd.gz found inside, alongside its own *NickName - the shared
// primitive behind both PackagePPDNickNames (Package/model score matching)
// and indexFamilyPackage (macmodel.go's own build-once model index), so
// both read the exact same real payload the same way.
//
// Uses `pkgutil --expand` (not `--expand-full`) plus selective `cpio`
// extraction, not a full expand-and-walk - a real, confirmed-live
// bottleneck fixed here. `--expand-full` fully decompresses every
// sub-package's *entire* Payload to real files on disk - driver binaries, a
// dozen languages of README/license text, icons, everything - just so the
// old version of this function could walk it looking for *.ppd(.gz).
// Timed against a real Canon UFR II package: 6.4s and 255MB written, for a
// package whose actual PPDs total 25MB. `pkgutil --expand` alone unpacks
// only each sub-package's own Bom/PackageInfo/Scripts structure (0.1s) and
// leaves Payload as what it actually is on disk - a plain gzip-compressed
// cpio archive (confirmed via `file`: "gzip compressed data", no dependency
// on a more exotic format like pbzx) - which the system `cpio` tool can
// extract selectively: `cpio -idm "*.ppd" "*.ppd.gz"` pulls out only the
// matching entries (0.2-0.4s, 25MB - a ~20x wall-clock cut on the dominant
// cost of building the mac model index, timed the same way).
func packagePPDEntries(pkgPath string) ([]ppdEntry, []subPackageResult, error) {
	return packagePPDEntriesFiltered(pkgPath, nil)
}

// packagePPDEntriesFiltered is packagePPDEntries, with an optional extra
// step between expanding and extracting: given the freshly-expanded tree,
// restrict computes which sub-package(s) selective PPD extraction is
// allowed to draw from (nil restrict, or a false ok, means "every
// sub-package with a Payload" - every manufacturer except Kyocera today).
// See kyoceraRestrictSubPackages (mackyocera.go) for the one real caller
// and why it's needed.
func packagePPDEntriesFiltered(pkgPath string, restrict func(expandDir string) (allow map[string]bool, ok bool)) ([]ppdEntry, []subPackageResult, error) {
	return packagePPDEntriesFilteredFallback(pkgPath, restrict, nil)
}

// packagePPDEntriesFilteredFallback is packagePPDEntriesFiltered, with a
// second optional manufacturer-specific hook: ppdFallback, tried on a
// sub-package only when the fast, extension-based cpio glob finds nothing
// in it at all - see macSubPackagePPDFallback for the callers and why Ricoh
// and Xerox specifically need one (their own real PPDs carry no recognized
// extension at all, or - for Ricoh's one legacy bundle - a bare ".gz").
func packagePPDEntriesFilteredFallback(pkgPath string, restrict func(expandDir string) (allow map[string]bool, ok bool), ppdFallback ppdExtractionFallback) ([]ppdEntry, []subPackageResult, error) {
	tmpDir, err := os.MkdirTemp("", "pdt-ppdinspect-*")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(tmpDir)

	expandDir := filepath.Join(tmpDir, "expand")
	if err := exec.Command("pkgutil", "--expand", pkgPath, expandDir).Run(); err != nil {
		return nil, nil, fmt.Errorf("expanding %s: %w", pkgPath, err)
	}

	var allow map[string]bool
	if restrict != nil {
		if a, ok := restrict(expandDir); ok {
			allow = a
		}
	}

	extractDir := filepath.Join(tmpDir, "ppds")
	subs := extractPPDsFromExpandedPkgFiltered(expandDir, extractDir, allow, ppdFallback)

	var entries []ppdEntry
	_ = filepath.WalkDir(extractDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		lower := strings.ToLower(path)
		hasRecognizedSuffix := strings.HasSuffix(lower, ".ppd") || strings.HasSuffix(lower, ".ppd.gz")
		// A recognized suffix is trusted outright (the fast, common case -
		// every real Canon/Kyocera PPD hits this); anything else (a fallback
		// extraction's own output - see ppdExtractionFallback) only counts
		// once its actual content is confirmed real, never by name alone.
		if !hasRecognizedSuffix && !looksLikeRealPPD(path) {
			return nil
		}
		if name, ok := ReadPPDNickName(path); ok {
			entries = append(entries, ppdEntry{Path: path, NickName: name})
		}
		return nil
	})
	return entries, subs, nil
}

// looksLikeRealPPD reports whether path's own content starts with a real
// PPD's own `*PPD-Adobe` header - transparently gzip-decompressing first
// when path's own first two bytes are the gzip magic number, regardless of
// what extension (if any) the file's own name carries. The content-based
// fallback extractPPDsFromExpandedPkgFiltered falls back to once a
// manufacturer's own real PPDs can't be recognized by extension at all
// (confirmed against two real, different Ricoh shapes: modern "Web Build"-
// style downloads name PPDs with no extension whatsoever, a legacy
// Apple-distributed bundle names them "<model>.gz" with no ".ppd" anywhere) -
// never trusted by name alone, exactly the discipline this codebase already
// applies everywhere else a vendor's own naming can't be trusted.
func looksLikeRealPPD(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	magic := make([]byte, 2)
	if _, err := io.ReadFull(f, magic); err != nil {
		return false
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false
	}

	var r io.Reader = f
	if magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return false
		}
		defer gz.Close()
		r = gz
	}
	head := make([]byte, 32)
	n, _ := io.ReadFull(r, head)
	return bytes.HasPrefix(head[:n], []byte("*PPD-Adobe"))
}

// removeNonPPDFiles deletes every regular file under root whose own content
// doesn't pass looksLikeRealPPD - a content-based extraction fallback (see
// ppdExtractionFallback) trusts a real signal (a declared install-location,
// a matched path fragment) to decide *where* to look, never to decide that
// everything found there is automatically real; this is the fallback's own
// cleanup step so destDir never carries unverified content past its own
// return, the same "verify, don't guess" discipline applied everywhere else
// a vendor's own naming can't be trusted.
func removeNonPPDFiles(root string) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !looksLikeRealPPD(path) {
			_ = os.Remove(path)
		}
		return nil
	})
}

// extractPPDsFromExpandedPkg walks expandDir (a pkgutil --expand tree - one
// or more sub-packages, each its own directory with a Payload file: either
// pkgPath itself for a single-component package, or several nested
// <Name>.pkg directories for a distribution/product archive, which is the
// shape every real Canon download turned out to be - see
// packagePPDEntries' own doc comment) and, for every Payload found,
// decompresses it (plain gzip) and extracts just its *.ppd/*.ppd.gz entries
// via cpio, into destDir/<sub-package name>/ - returning every sub-package
// that actually yielded at least one file (see subPackageResult's own doc
// comment). Every sub-package's Payload is tried independently and a
// failure on one (cpio finding nothing, a Payload that isn't actually gzip,
// etc.) is skipped rather than aborting the others - the same "index what's
// readable, degrade for the rest" spirit BuildMacCatalog itself already has
// for a corrupt/unreadable file. Always best-effort: destDir may end up
// with nothing in it, which packagePPDEntries' own caller already treats as
// "no PPDs found", not an error.
func extractPPDsFromExpandedPkg(expandDir, destDir string) []subPackageResult {
	return extractPPDsFromExpandedPkgFiltered(expandDir, destDir, nil, nil)
}

// ppdExtractionFallback is tried, for one sub-package, only once the fast
// extension-based cpio glob below finds nothing in it at all - pkgDir is
// that sub-package's own expanded directory (its PackageInfo lives there),
// payloadPath its own Payload file, destDir where a real match should end up
// (same directory the normal glob path would have used). Returns whether it
// found and extracted anything. See macSubPackagePPDFallback (macricoh.go)
// for the one real dispatcher and why only Ricoh needs one today - nil for
// every other manufacturer, at zero extra cost (the fallback branch below is
// simply never taken).
type ppdExtractionFallback func(pkgDir, payloadPath, destDir string) bool

// extractPPDsFromExpandedPkgFiltered is extractPPDsFromExpandedPkg, with two
// optional extra hooks. allow restricts which sub-package directory *names*
// get walked at all - nil means every sub-package with a Payload, same as
// before (every manufacturer except Kyocera today). Kyocera's own real "Web
// Build" package ships the identical PPD set duplicated across 3
// sub-packages (a baseline installer plus two that only patch a default
// value into an otherwise byte-identical copy afterward, confirmed live) -
// without this, every model would get indexed 3 times over with completely
// duplicate variants. See kyoceraRestrictSubPackages (mackyocera.go) for the
// one real caller. ppdFallback is tried on a sub-package only when the fast
// glob path finds nothing in it - see ppdExtractionFallback's own doc
// comment.
func extractPPDsFromExpandedPkgFiltered(expandDir, destDir string, allow map[string]bool, ppdFallback ppdExtractionFallback) []subPackageResult {
	var subs []subPackageResult
	_ = filepath.WalkDir(expandDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "Payload" {
			return nil
		}
		pkgDir := filepath.Dir(path)
		if allow != nil && !allow[filepath.Base(pkgDir)] {
			return nil
		}
		f, ferr := os.Open(path)
		if ferr != nil {
			return nil
		}
		defer f.Close()
		gz, gzerr := gzip.NewReader(f)
		if gzerr != nil {
			return nil
		}
		defer gz.Close()

		name := filepath.Base(pkgDir)
		sub := filepath.Join(destDir, name)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return nil
		}
		// Four patterns, not two - cpio's own glob matching is
		// case-sensitive, and a real Kyocera "Web Build" download mixes
		// both extension cases in the same sub-package (confirmed live:
		// 124 real PPDs named "*.ppd", 336 named "*.PPD" - lowercase-only
		// patterns silently dropped 73% of Kyocera's own real model
		// coverage, discovered only once the model count came back
		// suspiciously low against the real BOM's own 460-file count).
		cmd := exec.Command("cpio", "-idm", "--quiet", "*.ppd", "*.PPD", "*.ppd.gz", "*.PPD.gz")
		cmd.Dir = sub
		cmd.Stdin = gz
		_ = cmd.Run()

		if !dirHasAnyFile(sub) && ppdFallback != nil {
			ppdFallback(pkgDir, path, sub)
		}

		if dirHasAnyFile(sub) {
			version, _ := readPackageInfoVersion(filepath.Join(pkgDir, "PackageInfo"))
			subs = append(subs, subPackageResult{Name: name, Version: version})
		}
		return nil
	})
	return subs
}

// extractAllFromPayload re-opens payloadPath fresh (whatever reader found
// this sub-package's own fast glob came up empty already consumed the
// stream) and cpio-extracts every entry into destDir, with no name
// filtering at all - safe only when the whole Payload is already known
// (from some other real signal, not a guess - see ppdExtractionFallback) to
// contain nothing but real PPDs.
func extractAllFromPayload(payloadPath, destDir string) {
	f, err := os.Open(payloadPath)
	if err != nil {
		return
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return
	}
	defer gz.Close()
	cmd := exec.Command("cpio", "-idm", "--quiet")
	cmd.Dir = destDir
	cmd.Stdin = gz
	_ = cmd.Run()
}

// listPayloadEntries re-opens payloadPath fresh and lists every entry's own
// path via `cpio -it` - decompresses the whole stream but never writes a
// file to disk, far cheaper than a real extraction pass (the same
// distinction packagePPDEntries' own doc comment already draws between
// `--expand-full` and selective extraction).
func listPayloadEntries(payloadPath string) []string {
	f, err := os.Open(payloadPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil
	}
	defer gz.Close()
	cmd := exec.Command("cpio", "-it", "--quiet")
	cmd.Stdin = gz
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	entries := make([]string, 0, len(lines))
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			entries = append(entries, l)
		}
	}
	return entries
}

// extractPathContainingFromPayload re-opens payloadPath fresh and
// cpio-extracts only the entries whose own path contains fragment - found by
// a first, cheap listPayloadEntries pass, then requested from cpio by exact
// name (cpio accepts literal names, not just globs) rather than a blind
// "extract everything," which would also pull down every unrelated file
// sharing the same Payload (confirmed necessary against a real legacy Ricoh
// bundle whose PPDs sit in the same Payload as hundreds of unrelated
// driver-framework/PDE-plugin files - see macricoh.go).
func extractPathContainingFromPayload(payloadPath, fragment, destDir string) {
	var matches []string
	for _, e := range listPayloadEntries(payloadPath) {
		if strings.Contains(e, fragment) {
			matches = append(matches, e)
		}
	}
	if len(matches) == 0 {
		return
	}
	f, err := os.Open(payloadPath)
	if err != nil {
		return
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return
	}
	defer gz.Close()
	args := append([]string{"-idm", "--quiet"}, matches...)
	cmd := exec.Command("cpio", args...)
	cmd.Dir = destDir
	cmd.Stdin = gz
	_ = cmd.Run()
}

// dirHasAnyFile reports whether root (recursively) contains at least one
// regular file - extractPPDsFromExpandedPkg's own "did this sub-package's
// cpio extraction actually find anything" check, since cpio itself exits 0
// either way (confirmed live: no error, no output, just nothing extracted,
// for a sub-package whose Payload has no *.ppd(.gz) entries at all).
func dirHasAnyFile(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			found = true
		}
		return nil
	})
	return found
}

// PackagePPDNickNames returns the *NickName of every PPD pkgPath's own
// payload would install, without installing anything - lets deploy-time
// model matching happen *before* deciding which of a manufacturer's several
// packages (see ResolveMacFamily) to actually install. manufacturer selects
// the same content-based extraction fallback BuildMacModelIndex's own
// catalog build already applies (macSubPackagePPDFallback) - without it, a
// manufacturer whose real PPDs need that fallback (Ricoh) would score every
// one of its own packages as having no PPDs at all here, even though the
// catalog-driven index (built the same way) finds them correctly.
func PackagePPDNickNames(pkgPath, manufacturer string) ([]string, error) {
	entries, _, err := packagePPDEntriesFilteredFallback(pkgPath, nil, macSubPackagePPDFallback(manufacturer))
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.NickName
	}
	return names, nil
}

// CachePPDFile copies srcPath (a PPD file, typically still on a mounted
// volume LocateLoosePPDs' own caller will unmount once it returns) into
// cacheDir/filename, creating cacheDir if needed - the permanent local copy
// a loose (no-installer) family's MacPPDVariant.LooseCachedPPDPath points at,
// made once during BuildMacModelIndex rather than re-mounting the source
// .dmg at deploy time. Overwrites any existing file at the destination -
// idempotent, a later catalog refresh re-copying the same PPD is a no-op in
// effect, not an error.
func CachePPDFile(srcPath, cacheDir, filename string) (string, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(cacheDir, filename)
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return "", err
	}
	return dest, nil
}

// PackageBestModelScore is PackagePPDNickNames plus FuzzyMatchScore in one
// call - the convenience ResolveMacFamily actually uses: how well (if at
// all) pkgPath's own payload supports model, and the best-matching PPD's own
// NickName for logging. ok is false when the package has no PPD at all
// (e.g. an installer that registers no classic PPD), model is empty, or
// nothing inside scores a match.
func PackageBestModelScore(pkgPath, manufacturer, model string) (nickName string, score int, ok bool) {
	if model == "" {
		return "", -1, false
	}
	names, err := PackagePPDNickNames(pkgPath, manufacturer)
	if err != nil {
		return "", -1, false
	}
	best, bestScore := "", -1
	for _, name := range names {
		if s := FuzzyMatchScore(name, model); s > bestScore {
			best, bestScore = name, s
		}
	}
	if bestScore < 0 {
		return "", -1, false
	}
	return best, bestScore, true
}

// PPDPathForDefaults extracts just ppdFilename out of the real, already-
// located driver package at pkgPath into a caller-owned temp directory
// (removed via the returned cleanup once the caller is done reading it) - a
// batched deploy plan (planRicohBatchRow/planSharpBatchRow,
// canonbatch_darwin.go) needs this to compute print defaults ahead of its
// one privileged call, for a manufacturer whose install is never selective:
// a plain full `installer -pkg` run places every real PPD at once, so
// there's no already-staged copy of just this one model to read defaults
// from the way Canon/Kyocera's own selective extraction leaves behind as a
// side effect. Reuses the exact same detection logic proven live in
// BuildMacModelIndex (extractPPDsFromExpandedPkgFiltered plus manufacturer's
// own content-based fallback, if any - macSubPackagePPDFallback) against a
// temp dir this function owns outright, rather than one gone by the time
// packagePPDEntriesFiltered's own caller sees it. Generalized (2026-09-13)
// from what was originally Ricoh-only (RicohPPDPathForDefaults) once Sharp
// needed the identical mechanism - manufacturer only matters for picking the
// right macSubPackagePPDFallback, nil for both Ricoh and Sharp today.
func PPDPathForDefaults(manufacturer, pkgPath, ppdFilename string) (ppdPath string, cleanup func(), err error) {
	noop := func() {}
	tmpDir, err := os.MkdirTemp("", "pdt-ppddefaults-*")
	if err != nil {
		return "", noop, err
	}
	cleanup = func() { os.RemoveAll(tmpDir) }

	expandDir := filepath.Join(tmpDir, "expand")
	if err := exec.Command("pkgutil", "--expand", pkgPath, expandDir).Run(); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("expanding %s: %w", pkgPath, err)
	}
	extractDir := filepath.Join(tmpDir, "ppds")
	extractPPDsFromExpandedPkgFiltered(expandDir, extractDir, nil, macSubPackagePPDFallback(manufacturer))

	var found string
	_ = filepath.WalkDir(extractDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Base(path) == ppdFilename {
			found = path
		}
		return nil
	})
	if found == "" {
		cleanup()
		return "", noop, fmt.Errorf("could not find %q in %s's own real payload", ppdFilename, pkgPath)
	}
	return found, cleanup, nil
}

// ppdResourcesOnlyInstallLocation is the exact install-location a sub-
// package whose own Payload contains nothing BUT real PPDs declares -
// confirmed against all 9 real modern Ricoh "Web Build"-style downloads
// (identifier "com.RICOH.print.<model-group>.ppds.pkg" every time) - the
// cheap, manufacturer-agnostic signal pathFragmentPPDExtractionFallback uses
// to recognize "safe to extract this whole Payload verbatim" without
// needing to hard-code any one manufacturer's own sub-package naming
// convention. Real PPDs found this way carry no file extension whatsoever
// (confirmed live via `file`: genuine "PPD file, version 4.3" content under
// names like "RICOH IM C3000") - the fast, extension-based cpio glob every
// other manufacturer's real PPDs already match can never find them.
const ppdResourcesOnlyInstallLocation = "/Library/Printers/PPDs/Contents/Resources/"

// ppdResourcesPathFragment is the path fragment a real PPD lives under
// inside a package whose own PackageInfo declares a different (or no)
// install-location - the Payload bakes the real destination into each
// entry's own relative path instead (e.g.
// "./Library/Printers/PPDs/Contents/Resources/RICOH Aficio 3224C.gz",
// confirmed live for Ricoh's legacy "RicohPrinterDrivers.pkg" bundle, and
// "./Library/Printers/PPDs/Contents/Resources/Xerox C300 Color Printer.gz"
// for Xerox's own current, single, whole-driver package - install-location
// "/", not the PPD-only one above, since it also installs frameworks/
// filters/PDE plugins/a config utility app alongside the PPDs in the same
// Payload). pathFragmentPPDExtractionFallback falls back to finding PPDs by
// path instead of by declared destination whenever
// ppdResourcesOnlyInstallLocation doesn't match.
const ppdResourcesPathFragment = "/PPDs/Contents/Resources/"

// macSubPackagePPDFallback returns indexFamilyPackage's own content-based
// PPD-extraction fallback for a manufacturer, or nil for every manufacturer
// whose real PPDs are already found correctly by the fast, extension-based
// glob (everyone except Ricoh and Xerox today - both real, independently
// confirmed cases of a manufacturer naming its own real PPDs with no ".ppd"
// anywhere at all) - see ppdExtractionFallback's own doc comment for the
// exact contract.
func macSubPackagePPDFallback(manufacturer string) ppdExtractionFallback {
	switch manufacturer {
	case "Ricoh", "Xerox":
		return pathFragmentPPDExtractionFallback
	default:
		return nil
	}
}

// pathFragmentPPDExtractionFallback is tried only once the fast, extension-
// based cpio glob finds nothing in a given sub-package's own Payload. Two
// real shapes, confirmed live against both Ricoh and Xerox's own real
// packages: a sub-package whose own PackageInfo declares
// ppdResourcesOnlyInstallLocation itself (extract its whole Payload -
// already known, from that declaration, to contain nothing but real PPDs);
// anything else (a different or no declared install-location - e.g.
// Xerox's own current whole-driver package, install-location "/", or
// Ricoh's legacy bundle, no install-location declared at all) falls back to
// finding PPDs by path instead, extracting only the matched entries - never
// the whole Payload, which would also pull down hundreds (thousands, for
// Xerox's own current package - 6549 total payload entries, 178 real PPDs)
// of unrelated driver-framework/PDE-plugin/config-utility files sharing the
// very same Payload. Either way, nothing extracted here is trusted by name
// alone - packagePPDEntriesFilteredFallback's own caller content-sniffs
// every non-suffix-matched file (looksLikeRealPPD) before accepting it; a
// real, confirmed-live bonus for Xerox's own package specifically: the
// macOS `cpio` binary silently never writes out the AppleDouble resource-
// fork sidecar entries ("._Xerox <model>.gz") that share the very same
// path fragment as the real PPDs, so this fallback's own extraction never
// even sees them, not just filters them out afterward.
func pathFragmentPPDExtractionFallback(pkgDir, payloadPath, destDir string) bool {
	if loc, ok := readPackageInfoInstallLocation(filepath.Join(pkgDir, "PackageInfo")); ok && loc == ppdResourcesOnlyInstallLocation {
		extractAllFromPayload(payloadPath, destDir)
	} else {
		extractPathContainingFromPayload(payloadPath, ppdResourcesPathFragment, destDir)
	}
	removeNonPPDFiles(destDir)
	return dirHasAnyFile(destDir)
}
