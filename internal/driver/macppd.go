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

// PackagePPDNickNames returns the *NickName of every PPD pkgPath's own
// payload would install, without installing anything - pkgutil --expand-full
// (already used by PackageLabel) fully decompresses a package's Payload,
// landing PPDs at the exact same Library/Printers/PPDs/Contents/Resources
// path a real install would use (confirmed live against a real Canon
// package), so this is safe, read-only inspection: mount/locate the pkg (see
// LocatePkg), expand it to a scratch directory, read every .ppd/.ppd.gz
// found anywhere in it. Lets deploy-time model matching happen *before*
// deciding which of a manufacturer's several packages (see
// ResolveMacFamily) to actually install.
func PackagePPDNickNames(pkgPath string) ([]string, error) {
	tmpDir, err := os.MkdirTemp("", "pdt-ppdinspect-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	expandDir := filepath.Join(tmpDir, "expand")
	if err := exec.Command("pkgutil", "--expand-full", pkgPath, expandDir).Run(); err != nil {
		return nil, fmt.Errorf("expanding %s: %w", pkgPath, err)
	}

	var names []string
	_ = filepath.WalkDir(expandDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		lower := strings.ToLower(path)
		if !strings.HasSuffix(lower, ".ppd") && !strings.HasSuffix(lower, ".ppd.gz") {
			return nil
		}
		if name, ok := ReadPPDNickName(path); ok {
			names = append(names, name)
		}
		return nil
	})
	return names, nil
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
