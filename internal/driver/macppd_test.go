package driver

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildTestPayload creates a real gzip-compressed cpio "Payload" file at
// payloadPath containing exactly the given files (name -> content), using
// the system `cpio`/`gzip` tools directly - the same real format
// pkgutil --expand leaves every sub-package's own Payload in, confirmed via
// `file` elsewhere in this codebase. Lets extractPPDsFromExpandedPkgFiltered
// be tested against real archive bytes without needing a real downloaded
// vendor package.
func buildTestPayload(t *testing.T, payloadPath string, files map[string]string) {
	t.Helper()
	srcDir := t.TempDir()
	var names []string
	for name, content := range files {
		p := filepath.Join(srcDir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("writing fixture file %s: %v", name, err)
		}
		names = append(names, name)
	}

	cpioCmd := exec.Command("cpio", "-o", "--quiet")
	cpioCmd.Dir = srcDir
	cpioCmd.Stdin = strings.NewReader(strings.Join(names, "\n") + "\n")
	cpioOut, err := cpioCmd.Output()
	if err != nil {
		t.Fatalf("building test cpio archive: %v", err)
	}

	gzipCmd := exec.Command("gzip", "-c")
	gzipCmd.Stdin = bytes.NewReader(cpioOut)
	gzOut, err := gzipCmd.Output()
	if err != nil {
		t.Fatalf("gzip-compressing test payload: %v", err)
	}
	if err := os.WriteFile(payloadPath, gzOut, 0o644); err != nil {
		t.Fatalf("writing test Payload: %v", err)
	}
}

// TestExtractPPDsFromExpandedPkg_CaseInsensitiveExtensions guards a real bug
// found live: cpio's own glob matching is case-sensitive, and a real
// Kyocera "Web Build" download mixes both extension cases in the same
// sub-package (confirmed live: 124 real PPDs named "*.ppd", 336 named
// "*.PPD" - a lowercase-only cpio pattern silently dropped 73% of Kyocera's
// own real model coverage, only noticed because the indexed model count
// came back suspiciously low against the real BOM's own 460-file count).
func TestExtractPPDsFromExpandedPkg_CaseInsensitiveExtensions(t *testing.T) {
	expandDir := t.TempDir()
	subPkg := filepath.Join(expandDir, "Test.pkg")
	if err := os.MkdirAll(subPkg, 0o755); err != nil {
		t.Fatal(err)
	}
	buildTestPayload(t, filepath.Join(subPkg, "Payload"), map[string]string{
		"lowercase.ppd": "*NickName: \"Lowercase Model\"\n",
		"UPPERCASE.PPD": "*NickName: \"Uppercase Model\"\n",
		"not-a-ppd.txt": "irrelevant\n",
	})

	destDir := t.TempDir()
	subs := extractPPDsFromExpandedPkgFiltered(expandDir, destDir, nil)
	if len(subs) != 1 {
		t.Fatalf("got %d sub-package results, want 1: %+v", len(subs), subs)
	}

	if _, err := os.Stat(filepath.Join(destDir, "Test.pkg", "lowercase.ppd")); err != nil {
		t.Errorf("lowercase.ppd was not extracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "Test.pkg", "UPPERCASE.PPD")); err != nil {
		t.Errorf("UPPERCASE.PPD was not extracted (the real bug this guards against): %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "Test.pkg", "not-a-ppd.txt")); err == nil {
		t.Errorf("not-a-ppd.txt was extracted, want it excluded")
	}
}
