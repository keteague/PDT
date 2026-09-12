package driver

import (
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
	tmpDir, err := os.MkdirTemp("", "pdt-ppdinspect-*")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(tmpDir)

	expandDir := filepath.Join(tmpDir, "expand")
	if err := exec.Command("pkgutil", "--expand", pkgPath, expandDir).Run(); err != nil {
		return nil, nil, fmt.Errorf("expanding %s: %w", pkgPath, err)
	}

	extractDir := filepath.Join(tmpDir, "ppds")
	subs := extractPPDsFromExpandedPkg(expandDir, extractDir)

	var entries []ppdEntry
	_ = filepath.WalkDir(extractDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		lower := strings.ToLower(path)
		if !strings.HasSuffix(lower, ".ppd") && !strings.HasSuffix(lower, ".ppd.gz") {
			return nil
		}
		if name, ok := ReadPPDNickName(path); ok {
			entries = append(entries, ppdEntry{Path: path, NickName: name})
		}
		return nil
	})
	return entries, subs, nil
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
	var subs []subPackageResult
	_ = filepath.WalkDir(expandDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "Payload" {
			return nil
		}
		pkgDir := filepath.Dir(path)
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
		cmd := exec.Command("cpio", "-idm", "--quiet", "*.ppd", "*.ppd.gz")
		cmd.Dir = sub
		cmd.Stdin = gz
		_ = cmd.Run()

		if dirHasAnyFile(sub) {
			version, _ := readPackageInfoVersion(filepath.Join(pkgDir, "PackageInfo"))
			subs = append(subs, subPackageResult{Name: name, Version: version})
		}
		return nil
	})
	return subs
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
// packages (see ResolveMacFamily) to actually install.
func PackagePPDNickNames(pkgPath string) ([]string, error) {
	entries, _, err := packagePPDEntries(pkgPath)
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
func PackageBestModelScore(pkgPath, model string) (nickName string, score int, ok bool) {
	if model == "" {
		return "", -1, false
	}
	names, err := PackagePPDNickNames(pkgPath)
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
