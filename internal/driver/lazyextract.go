package driver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ExtractionCache fully extracts driver packages on demand, right before a
// Deploy needs the real files a .inf's own SourceDisksFiles/CopyFiles
// sections reference - StageInf (internal/printer/windows/
// driverinstall_windows.go) requires them physically present alongside the
// .inf, a hard Win32 constraint (SetupCopyOEMInfW), not a PDT design choice.
// GitHub issue #10's catalog rework stopped extracting eagerly at every
// catalog scan (see ensureZipInfsExtracted's own doc comment) so this only
// ever happens for a package actually being installed.
//
// Where the extraction lives (Ken, 2026-09-20): in the system temp folder
// (%TEMP% on Windows, the per-user temp dir on macOS), never in the Drivers
// repo. It used to be written as a persistent sibling of the archive, which
// left unwanted extracted folders all over the repo (and wrote to the
// Drivers drive - a flash drive, when running portably - during every
// deploy).
//
// Lifetime: one ExtractionCache is meant to live for one whole Deploy run.
// Every row in that run that needs the same package shares its one
// extraction, so it is kept until the run is over; Close then deletes all of
// it. (A crash or forced quit skips Close - SweepStaleExtractions clears
// those leftovers at the next startup.)
//
// Reuses the exact same per-format extraction functions
// (extractZip/extractMsi+expandCompressedSiblings/extractSfxArchive/
// extractKyoceraExe) the old eager helpers used - zero change to the
// extraction logic itself, just where its output goes.
type ExtractionCache struct {
	mu   sync.Mutex
	root string            // lazily created temp dir holding every extraction
	n    int               // numbers extraction folders so names never collide
	done map[string]string // archive path -> its extracted dir
}

// NewExtractionCache returns an empty cache; nothing touches the disk until
// the first Ensure.
func NewExtractionCache() *ExtractionCache {
	return &ExtractionCache{done: map[string]string{}}
}

// extractionRootPrefix names the per-run folder in the temp dir; also what
// SweepStaleExtractions looks for.
const extractionRootPrefix = "pdt-extract-"

// Ensure returns the directory archivePath (a real archive path -
// ArchEntry.ArchivePath, resolved via a real driver selection) is extracted
// into, extracting it first if this cache hasn't already. Files in that
// directory sit at the same relative paths as inside the archive, so a
// .inf's own InfRelPath resolves against it directly.
func (c *ExtractionCache) Ensure(archivePath string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ensureLocked(archivePath)
}

// Close deletes everything this cache extracted. Safe to call more than once
// and on a cache that never extracted anything.
func (c *ExtractionCache) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.root != "" {
		os.RemoveAll(c.root)
		c.root = ""
	}
	c.done = map[string]string{}
}

func (c *ExtractionCache) newDest(name string) (string, error) {
	if c.root == "" {
		root, err := os.MkdirTemp("", extractionRootPrefix+"*")
		if err != nil {
			return "", err
		}
		c.root = root
	}
	c.n++
	dest := filepath.Join(c.root, fmt.Sprintf("%d-%s", c.n, strings.TrimSuffix(name, filepath.Ext(name))))
	return dest, os.MkdirAll(dest, 0o755)
}

func (c *ExtractionCache) ensureLocked(archivePath string) (string, error) {
	origKey := filepath.Clean(archivePath)
	if dir, ok := c.done[origKey]; ok {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir, nil
		}
		delete(c.done, origKey) // removed out from under us - extract again
	}

	// archivePath living inside .pdt-infcache means it's a NESTED archive an
	// earlier .inf-only pass revealed from inside some outer archive - a
	// real, live example: Lexmark's own package is an outer self-extracting
	// RAR whose selective .inf-only extraction (extractInfsFromSfxArchive)
	// also pulls out several inner .msi files by design (needed to then find
	// *their* own .inf entries in turn - see ensureMsiInfsExtracted's own
	// ordering comment in catalog.go), so ArchEntry.ArchivePath for a driver
	// found that way points at one of those inner .msi files, which already
	// sits inside .pdt-infcache, holding only the cached .inf - none of the
	// companion files a real deploy needs alongside it (confirmed live:
	// StageInf failed with "The system cannot find the file specified").
	//
	// So the OUTER archive is fully extracted first (recursively, so any
	// nesting depth works), and archivePath is re-resolved to its own real
	// position inside that extraction - the same relative path .inf-only
	// extraction's own selective 7z filter (-r, recursive) preserved against
	// the cache, so it lines up with where a real, unfiltered extraction
	// puts the same file.
	if strings.Contains(archivePath, string(filepath.Separator)+PdtInfCacheDirName+string(filepath.Separator)) {
		if outerArchive, markerDir := findSourceArchive(filepath.Dir(archivePath), ""); outerArchive != "" {
			outerDir, err := c.ensureLocked(outerArchive)
			if err != nil {
				return "", fmt.Errorf("extracting outer archive %s for nested %s: %w", outerArchive, archivePath, err)
			}
			if rel, relErr := filepath.Rel(markerDir, archivePath); relErr == nil {
				archivePath = filepath.Join(outerDir, rel)
			}
		}
	}

	name := filepath.Base(archivePath)
	var extract func(archivePath, destDir string) error
	destName := name
	if m := kyoceraExeNameRe.FindStringSubmatch(name); m != nil {
		extract = extractKyoceraExe
		destName = "KXDriver_" + m[1]
	} else {
		switch strings.ToLower(filepath.Ext(name)) {
		case ".zip":
			extract = func(src, dst string) error {
				if err := extractZip(src, dst); err != nil {
					return err
				}
				flattenRedundantWrapperDir(dst)
				return nil
			}
		case ".msi":
			extract = func(src, dst string) error {
				if err := extractMsi(src, dst); err != nil {
					return err
				}
				expandCompressedSiblings(dst)
				flattenRedundantWrapperDir(dst)
				return nil
			}
		case ".exe":
			extract = func(src, dst string) error {
				if err := extractSfxArchive(src, dst); err != nil {
					return err
				}
				flattenRedundantWrapperDir(dst)
				return nil
			}
		default:
			return "", fmt.Errorf("don't know how to extract %s", archivePath)
		}
	}

	dest, err := c.newDest(destName)
	if err != nil {
		return "", err
	}
	if err := extract(archivePath, dest); err != nil {
		os.RemoveAll(dest)
		return "", err
	}
	c.done[origKey] = dest
	c.done[filepath.Clean(archivePath)] = dest
	return dest, nil
}

// SweepStaleExtractions deletes extraction folders in the temp directory
// that are older than maxAge - what a run that never got to call Close (a
// crash, or ForceQuit) leaves behind. Called once at startup; the age limit
// keeps it away from another PDT instance's live extraction.
func SweepStaleExtractions(maxAge time.Duration) {
	tmp := os.TempDir()
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), extractionRootPrefix) {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			os.RemoveAll(filepath.Join(tmp, e.Name()))
		}
	}
}
