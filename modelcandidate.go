package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"PDT/internal/driver"
)

// ModelCandidate is one entry in the Driver modal's Model dropdown. Source is
// a multi-line tooltip: every driver package (relative to the Drivers folder)
// that offers this model - so several near-identical entries, or one model
// present in more than one driver version, can be told apart. GitHub issue
// #16 follow-up (Ken, 2026-09-20).
type ModelCandidate struct {
	Label  string `json:"label"`
	Source string `json:"source"`
}

const modelCandidateMaxSourceLines = 8

// modelCandidatesWithSource builds the Model dropdown's list from every
// source that can name a model for manufacturer: the Windows driver catalog's
// own per-model index (Kyocera's .inf-derived one - winCatalog/winModelIndex
// are nil/empty on a native macOS build, which has no Windows catalog), the
// macOS driver catalog's model index, and - for a manufacturer with neither
// (HP, whose macOS side is a single app and whose Windows side is a Universal
// driver) - the OpenPrinting PPDs' own real model names. Same model reached
// through more than one source is one entry whose tooltip lists all of them.
func modelCandidatesWithSource(macCatalog driver.MacCatalog, macIndex driver.MacModelIndex, winCatalog driver.Catalog, winModelIndex map[string]map[string][]string, manufacturer, filterText string) []ModelCandidate {
	root := driversRoot()
	rel := func(p string) string {
		if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
		return p
	}

	sources := map[string][]string{}
	var order []string
	add := func(name, line string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := sources[name]; !ok {
			sources[name] = nil
			order = append(order, name)
		}
		for _, existing := range sources[name] {
			if existing == line {
				return
			}
		}
		if line != "" {
			sources[name] = append(sources[name], line)
		}
	}

	if winCatalog != nil {
		for _, name := range driver.Models(winModelIndex, manufacturer, "") {
			add(name, "")
			for _, d := range driver.CandidateDetails(winCatalog, winModelIndex, manufacturer, name, "") {
				for _, src := range d.Sources {
					add(name, rel(src))
				}
			}
		}
	}
	for _, name := range driver.MacModels(macIndex, manufacturer, "") {
		add(name, "")
		for _, src := range driver.MacModelSourcePaths(macIndex, manufacturer, name) {
			add(name, rel(src))
		}
	}
	for _, path := range macCatalog.OpenPrintingPPDs[manufacturer] {
		name := macCatalog.OpenPrintingNickNames[manufacturer][path]
		if name == "" {
			name = strings.ReplaceAll(strings.TrimSuffix(strings.TrimSuffix(filepath.Base(path), ".gz"), ".ppd"), "_", " ")
		}
		add(driver.NormalizeModelName(name), rel(path))
	}

	type scored struct {
		name  string
		score int
	}
	var kept []scored
	for _, name := range order {
		s := 0
		if filterText != "" {
			if s = driver.FuzzyMatchScore(name, filterText); s < 0 {
				continue
			}
		}
		kept = append(kept, scored{name, s})
	}
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].score != kept[j].score {
			return kept[i].score > kept[j].score
		}
		return kept[i].name < kept[j].name
	})

	out := make([]ModelCandidate, len(kept))
	for i, k := range kept {
		lines := sources[k.name]
		if len(lines) > modelCandidateMaxSourceLines {
			extra := len(lines) - modelCandidateMaxSourceLines
			lines = append(append([]string{}, lines[:modelCandidateMaxSourceLines]...), fmt.Sprintf("...and %d more", extra))
		}
		out[i] = ModelCandidate{Label: k.name, Source: strings.Join(lines, "\n")}
	}
	return out
}
