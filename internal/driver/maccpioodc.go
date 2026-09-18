package driver

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// cpioOdcMagic is the "odc" (POSIX portable ASCII, aka "old character")
// cpio variant's own fixed 6-byte magic - the real shape a macOS installer
// package's Payload is actually archived in (confirmed live, GitHub issue
// #3 Phase 3, against a real Kyocera Payload: this codebase's own existing
// `cpio -idm`/`cpio -it` calls never pass a -H format flag, so they've
// always relied on cpio's own format autodetection reading whichever of
// several magic values is actually present - "newc" (SVR4, hex fields, the
// initially assumed format here) was the wrong guess; the real bytes start
// "070707", not "070701").
const cpioOdcMagic = "070707"

// cpioOdcTrailer is the sentinel entry name that ends a cpio stream, in
// every real cpio variant including this one.
const cpioOdcTrailer = "TRAILER!!!"

// cpioOdcFieldWidths are the "odc" header's own 10 fields immediately
// following the 6-byte magic (dev, ino, mode, uid, gid, nlink, rdev, mtime,
// namesize, filesize, in that order) - all fixed-width ASCII OCTAL digits
// (not hex, unlike "newc"), with NO padding or alignment anywhere in the
// whole format: the header, filename, and file data are all simply
// byte-packed back to back. Confirmed by mechanically parsing a real
// Payload's own first three entries (a "." directory entry, then a real
// "./Kyocera CS 255.ppd" file entry with a plausible real file size) against
// exactly this layout before trusting it - magic itself is read separately,
// not counted in this slice.
var cpioOdcFieldWidths = []int{6, 6, 6, 6, 6, 6, 6, 11, 6, 11}

const (
	cpioOdcFieldMode     = 2
	cpioOdcFieldNamesize = 8
	cpioOdcFieldFilesize = 9
)

// cpioOdcHeaderSize is the fixed size of an "odc" header record (not
// counting the magic, which is read separately, or the filename that
// follows) - sum of cpioOdcFieldWidths.
var cpioOdcHeaderSize = func() int {
	n := 0
	for _, w := range cpioOdcFieldWidths {
		n += w
	}
	return n
}()

// cpioModeTypeMask/cpioModeDir isolate a cpio entry's own file-type bits
// (the upper bits of the standard Unix st_mode value the mode field
// carries) - S_IFDIR, needed to tell a directory entry (no data at all,
// just a name to mkdir) from a real file entry. Format-agnostic - the same
// mode encoding every real cpio variant carries.
const (
	cpioModeTypeMask = 0o170000
	cpioModeDir      = 0o040000
)

// cpioOdcEntry is one parsed "odc" header, name already read - FileSize/
// IsDir are the two fields cpioOdcList/cpioOdcExtract actually need; every
// other field (dev/ino/uid/gid/nlink/rdev/mtime) is parsed (parsing all 11
// is no harder than parsing just the two needed) but never used.
type cpioOdcEntry struct {
	Name     string
	FileSize int64
	IsDir    bool
}

// readCpioOdcHeader reads one "odc" header + filename from r - no alignment
// padding to consume anywhere, unlike "newc". Returns io.EOF once the
// stream is genuinely exhausted (not once TRAILER!!! is read - callers
// check for that themselves).
func readCpioOdcHeader(r io.Reader) (*cpioOdcEntry, error) {
	magic := make([]byte, 6)
	if _, err := io.ReadFull(r, magic); err != nil {
		return nil, err
	}
	if string(magic) != cpioOdcMagic {
		return nil, fmt.Errorf("not a cpio %q stream (got magic %q)", cpioOdcMagic, magic)
	}

	fields := make([]byte, cpioOdcHeaderSize)
	if _, err := io.ReadFull(r, fields); err != nil {
		return nil, err
	}
	pos := 0
	values := make([]string, len(cpioOdcFieldWidths))
	for i, w := range cpioOdcFieldWidths {
		values[i] = string(fields[pos : pos+w])
		pos += w
	}
	mode, err := strconv.ParseInt(values[cpioOdcFieldMode], 8, 32)
	if err != nil {
		return nil, fmt.Errorf("parsing cpio mode: %w", err)
	}
	fileSize, err := strconv.ParseInt(values[cpioOdcFieldFilesize], 8, 64)
	if err != nil {
		return nil, fmt.Errorf("parsing cpio filesize: %w", err)
	}
	nameSize, err := strconv.ParseInt(values[cpioOdcFieldNamesize], 8, 64)
	if err != nil {
		return nil, fmt.Errorf("parsing cpio namesize: %w", err)
	}

	nameBuf := make([]byte, nameSize)
	if _, err := io.ReadFull(r, nameBuf); err != nil {
		return nil, err
	}
	name := ""
	if nameSize > 0 {
		name = string(nameBuf[:nameSize-1]) // drop the trailing NUL
	}

	return &cpioOdcEntry{
		Name:     name,
		FileSize: fileSize,
		IsDir:    mode&cpioModeTypeMask == cpioModeDir,
	}, nil
}

// cpioOdcList streams r's own header records only (skipping every entry's
// file data, never decompressing "into" it) and returns every real entry's
// name, in stream order - darwin's own `cpio -it` equivalent
// (maccpio_darwin.go's own cpioListEntries), for cpioListEntries on Windows
// (maccpio_windows.go).
func cpioOdcList(r io.Reader) ([]string, error) {
	br := bufio.NewReader(r)
	var names []string
	for {
		entry, err := readCpioOdcHeader(br)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if entry.Name == cpioOdcTrailer {
			break
		}
		if !entry.IsDir {
			names = append(names, entry.Name)
		}
		if _, err := io.CopyN(io.Discard, br, entry.FileSize); err != nil {
			return nil, err
		}
	}
	return names, nil
}

// cpioOdcExtract streams r's own header records, writing each real file
// entry whose own Name satisfies match into destDir/<Name> (creating parent
// directories as needed) - match == nil extracts everything (darwin's own
// plain `cpio -idm` with no glob patterns at all, cpioExtractAll's own real
// call shape). A directory entry only ever gets its own empty directory
// created (mkdir -p semantics, same as real cpio -idm) - carries no data of
// its own to skip or write.
func cpioOdcExtract(r io.Reader, destDir string, match func(name string) bool) error {
	br := bufio.NewReader(r)
	for {
		entry, err := readCpioOdcHeader(br)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if entry.Name == cpioOdcTrailer {
			break
		}
		want := match == nil || match(entry.Name)

		if entry.IsDir {
			if want {
				_ = os.MkdirAll(filepath.Join(destDir, filepath.FromSlash(entry.Name)), 0o755)
			}
			continue
		}
		if !want {
			if _, err := io.CopyN(io.Discard, br, entry.FileSize); err != nil {
				return err
			}
			continue
		}

		dest := filepath.Join(destDir, filepath.FromSlash(entry.Name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		out, err := os.Create(dest)
		if err != nil {
			return err
		}
		if _, err := io.CopyN(out, br, entry.FileSize); err != nil {
			out.Close()
			return err
		}
		out.Close()
	}
	return nil
}

// cpioGlobMatch reports whether name matches pattern using real cpio's own
// fnmatch()-without-FNM_PATHNAME semantics: "*" matches any run of bytes,
// INCLUDING "/" - unlike Go's own path.Match/filepath.Match, which never let
// "*" cross a path separator (confirmed via grep: no existing helper in this
// codebase already does this correctly). "?" matches any single byte;
// everything else is a literal match. Needed because every real cpio glob
// pattern already in use across this package - "*/"+ppdFilename,
// "*/Recipe/"+base+".bundle/*" (maccanonselective.go),
// "*.ppd"/"*.PPD"/"*.ppd.gz"/"*.PPD.gz" (macppd.go) - relies on exactly that
// crossing behavior against a real Payload's own entry names (e.g.
// "./UFRII_LT_LIPS_LX_Installer.pkg/Recipe/CNPZUIFC5150ZU.bundle/Contents/...").
func cpioGlobMatch(pattern, name string) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			for len(pattern) > 0 && pattern[0] == '*' {
				pattern = pattern[1:]
			}
			if len(pattern) == 0 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if cpioGlobMatch(pattern, name[i:]) {
					return true
				}
			}
			return false
		case '?':
			if len(name) == 0 {
				return false
			}
			pattern, name = pattern[1:], name[1:]
		default:
			if len(name) == 0 || pattern[0] != name[0] {
				return false
			}
			pattern, name = pattern[1:], name[1:]
		}
	}
	return len(name) == 0
}

// cpioMatchAny returns a cpioOdcExtract-shaped match func admitting any name
// that cpioGlobMatch accepts against at least one of patterns - the
// Windows-side cpioExtractGlob's own real matcher (maccpio_windows.go), the
// direct analog of real cpio's own "-idm pattern1 pattern2 ..." accepting
// any one of several patterns.
func cpioMatchAny(patterns []string) func(name string) bool {
	return func(name string) bool {
		for _, p := range patterns {
			if cpioGlobMatch(p, name) {
				return true
			}
		}
		return false
	}
}
