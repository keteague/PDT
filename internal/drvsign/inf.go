package drvsign

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf16"
)

var (
	infSectionRe = regexp.MustCompile(`^\[(.+)\]$`)
	// CatalogFile=, CatalogFile.NT=, CatalogFile.NTamd64= ... in [Version].
	infCatalogRe = regexp.MustCompile(`(?i)^CatalogFile(\.[A-Za-z0-9]+)?\s*=\s*(.+)$`)
)

// CatalogFiles returns the signed catalogs infPath's [Version] section
// names (CatalogFile and its architecture-decorated forms), resolved
// against the .inf's own folder and limited to ones that actually exist -
// the files whose signature Windows checks when the package is installed.
// Reads ANSI/UTF-8 and UTF-16 (with BOM) .inf files; vendors ship both.
func CatalogFiles(infPath string) ([]string, error) {
	raw, err := os.ReadFile(infPath)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(infPath)
	seen := map[string]bool{}
	var out []string
	inVersion := false
	for _, line := range strings.Split(decodeInf(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if m := infSectionRe.FindStringSubmatch(line); m != nil {
			inVersion = strings.EqualFold(strings.TrimSpace(m[1]), "Version")
			continue
		}
		if !inVersion {
			continue
		}
		m := infCatalogRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[2]
		if i := strings.Index(name, ";"); i >= 0 { // trailing comment
			name = name[:i]
		}
		name = strings.Trim(strings.TrimSpace(name), `"`)
		if name == "" {
			continue
		}
		path := findCaseInsensitive(dir, name)
		if path == "" || seen[strings.ToLower(path)] {
			continue
		}
		seen[strings.ToLower(path)] = true
		out = append(out, path)
	}
	return out, nil
}

// decodeInf turns an .inf's bytes into text: UTF-16 LE/BE by BOM, otherwise
// as-is (ANSI and UTF-8 agree on everything CatalogFile lines contain).
func decodeInf(b []byte) string {
	if len(b) >= 2 && (b[0] == 0xFF && b[1] == 0xFE || b[0] == 0xFE && b[1] == 0xFF) {
		le := b[0] == 0xFF
		b = b[2:]
		u := make([]uint16, len(b)/2)
		for i := range u {
			if le {
				u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
			} else {
				u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
			}
		}
		return strings.ReplaceAll(string(utf16.Decode(u)), "\r", "")
	}
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	return strings.ReplaceAll(string(b), "\r", "")
}

// findCaseInsensitive finds name in dir ignoring case (an .inf says
// KYOMITA.CAT for a file shipped as kyomita.cat - fine on Windows, not on a
// case-sensitive filesystem), or "" if it isn't there.
func findCaseInsensitive(dir, name string) string {
	direct := filepath.Join(dir, name)
	if _, err := os.Stat(direct); err == nil {
		return direct
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(e.Name(), name) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}
