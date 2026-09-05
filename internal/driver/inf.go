package driver

import (
	"bufio"
	"os"
	"regexp"
	"strings"
	"time"
)

// Sections holds an INF file's [SectionName] -> lines mapping: every
// non-blank, non-comment line under the most recently seen [Section] header,
// verbatim (callers split key=value themselves). A line starting with ';'
// after trimming is treated as a comment - real INF files can also carry
// inline ';' comments, which this (like the original PowerShell tool) does
// not attempt to strip; fidelity to the working original matters more here
// than handling INF syntax this tool has never needed to handle.
type Sections map[string][]string

var sectionHeaderRe = regexp.MustCompile(`^\[(.+)\]$`)

// ParseInfSections ports Get-InfSections.
func ParseInfSections(path string) (Sections, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sections := Sections{}
	current := ""
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if m := sectionHeaderRe.FindStringSubmatch(line); m != nil {
			current = strings.TrimSpace(m[1])
			if _, ok := sections[current]; !ok {
				sections[current] = nil
			}
			continue
		}
		if current != "" {
			sections[current] = append(sections[current], line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return sections, nil
}

// findSectionCI returns the exact key in sections matching name
// case-insensitively (INF section names are conventionally case-insensitive;
// PowerShell's default -eq/-match string comparisons are too, which the
// original tool relied on throughout - mirrored here explicitly since Go map
// keys are case-sensitive).
func findSectionCI(sections Sections, name string) (string, bool) {
	for k := range sections {
		if strings.EqualFold(k, name) {
			return k, true
		}
	}
	return "", false
}

var driverVerRe = regexp.MustCompile(`(?i)^DriverVer\s*=\s*([^,]+),\s*([0-9.]+)`)

// infDateLayouts: an INF's DriverVer date is specified by Microsoft's INF
// format as always mm/dd/yyyy regardless of system locale; the extra layouts
// are defensive fallbacks only, mirroring [datetime]::TryParse's leniency.
var infDateLayouts = []string{"01/02/2006", "1/2/2006", "01-02-2006", "2006-01-02"}

func parseInfDate(s string) time.Time {
	for _, layout := range infDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// InfDriverVersion ports Get-InfDriverVersion.
func InfDriverVersion(sections Sections) (time.Time, string) {
	key, ok := findSectionCI(sections, "Version")
	if !ok {
		return time.Time{}, "0.0"
	}
	for _, line := range sections[key] {
		if m := driverVerRe.FindStringSubmatch(line); m != nil {
			date := parseInfDate(strings.TrimSpace(m[1]))
			version := strings.TrimSpace(m[2])
			if version == "" {
				version = "0.0"
			}
			return date, version
		}
	}
	return time.Time{}, "0.0"
}

// InfInfo is the result of scanning one INF for its printer driver names and
// declared version - ports Get-DriverNamesFromInf's return shape.
type InfInfo struct {
	Names   []string
	Date    time.Time
	Version string
}

var (
	classLineRe   = regexp.MustCompile(`(?i)^Class\s*=\s*(.+)$`)
	stringsLineRe = regexp.MustCompile(`(?i)^([A-Za-z0-9_.]+)\s*=\s*(.*)$`)
	mfgLineValRe  = regexp.MustCompile(`=\s*(.+)$`)
	tokenRefRe    = regexp.MustCompile(`^%(.+)%$`)
	modelLineRe   = regexp.MustCompile(`^(?:"([^"]*)"|%([A-Za-z0-9_]+)%)\s*=`)
	suffixedNameRe = regexp.MustCompile(`^(.+) \(v[\d.]+\)$`)
	guidRe        = regexp.MustCompile(`^\{[0-9A-Fa-f]{8}-[0-9A-Fa-f-]{27}\}$`)
)

// DriverNamesFromInf ports Get-DriverNamesFromInf, including its two
// vendor-specific quirks: skipping non-printer companion INFs (Class != Printer,
// e.g. WinUSB co-installers bundled alongside a printer package) and dropping
// HP-style "<name> (vX.Y.Z)" alias entries when the same INF also declares the
// unsuffixed base name (see Create-Printers.ps1 lines 254-323 for the original
// and its rationale).
//
// Deliberate simplification vs. the original: names are deduplicated as they're
// collected (a `seen` set) rather than left as a duplicate-containing list for
// the caller to later `Select-Object -Unique`. This makes the alias-drop step
// unconditionally correct - the original's List.Remove() removes only the
// first matching duplicate, which could theoretically leave a stray suffixed
// alias behind if the same suffixed name line appeared more than once in one
// INF (plausible: the same friendly name recurs once per matched hardware ID).
// Deduplicating first sidesteps that edge case entirely rather than reproducing it.
func DriverNamesFromInf(path string) (InfInfo, error) {
	sections, err := ParseInfSections(path)
	if err != nil {
		return InfInfo{}, err
	}
	empty := InfInfo{Date: time.Time{}, Version: "0.0"}

	if versionKey, ok := findSectionCI(sections, "Version"); ok {
		for _, line := range sections[versionKey] {
			if m := classLineRe.FindStringSubmatch(line); m != nil {
				if !strings.EqualFold(strings.TrimSpace(m[1]), "Printer") {
					return empty, nil
				}
				break
			}
		}
	}

	date, version := InfDriverVersion(sections)

	strs := map[string]string{}
	if stringsKey, ok := findSectionCI(sections, "Strings"); ok {
		for _, line := range sections[stringsKey] {
			if m := stringsLineRe.FindStringSubmatch(line); m != nil {
				strs[m[1]] = strings.Trim(strings.TrimSpace(m[2]), `"`)
			}
		}
	}

	mfgKey, ok := findSectionCI(sections, "Manufacturer")
	if !ok {
		return empty, nil
	}

	baseNames := map[string]bool{}
	for _, line := range sections[mfgKey] {
		m := mfgLineValRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		base := strings.TrimSpace(strings.Split(m[1], ",")[0])
		base = strings.Trim(base, `"`)
		if tm := tokenRefRe.FindStringSubmatch(base); tm != nil {
			if v, ok := strs[tm[1]]; ok {
				base = v
			}
		}
		if base != "" {
			baseNames[strings.ToLower(base)] = true
		}
	}

	var names []string
	seen := map[string]bool{}
	for secName, lines := range sections {
		lowerSec := strings.ToLower(secName)
		isModel := false
		for base := range baseNames {
			if lowerSec == base || strings.HasPrefix(lowerSec, base+".") {
				isModel = true
				break
			}
		}
		if !isModel {
			continue
		}
		for _, line := range lines {
			m := modelLineRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			var friendly string
			if m[1] != "" {
				friendly = m[1]
			} else if m[2] != "" {
				friendly = strs[m[2]]
			}
			friendly = strings.TrimSpace(friendly)
			if friendly == "" || guidRe.MatchString(friendly) {
				continue
			}
			if !seen[friendly] {
				seen[friendly] = true
				names = append(names, friendly)
			}
		}
	}

	baseNameSet := map[string]bool{}
	for _, n := range names {
		baseNameSet[n] = true
	}
	var filtered []string
	for _, n := range names {
		if m := suffixedNameRe.FindStringSubmatch(n); m != nil && baseNameSet[m[1]] {
			continue
		}
		filtered = append(filtered, n)
	}

	return InfInfo{Names: filtered, Date: date, Version: version}, nil
}
