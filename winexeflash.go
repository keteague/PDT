package main

import (
	"bytes"
	"context"
	"debug/pe"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"PDT/internal/driver"
	"PDT/internal/update"
)

// The mirror image of macappflash.go (Ken, 2026-09-20): when Write to Flash
// Drive runs on a Mac, the drive is meant to be carried to Windows endpoints
// too, but a Mac has no PDT.exe of its own to copy. So after the write, if
// there's Internet, the drive's PDT.exe is checked against the latest GitHub
// release and downloaded/replaced if it's missing or older.

const (
	winExeName = "PDT.exe"
	// winExeAssetName is the release asset release.yml uploads under its own
	// exact name (the in-app updater looks for the same one).
	winExeAssetName = "PDT.exe"
)

// peVersionKeys are the version-resource strings tried, in order. Wails
// writes info.productVersion into both.
var peVersionKeys = []string{"ProductVersion", "FileVersion"}

// peInstalledVersion reads the version out of the Windows executable at
// path, from its VERSIONINFO resource. Only the small .rsrc section is read,
// not the whole (tens of MB) file - this runs against a USB drive. ok is
// false when the file, its resource section, or a version in it can't be
// found - treated as "not usable", so it gets replaced.
func peInstalledVersion(path string) (version string, ok bool) {
	f, err := pe.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	sec := f.Section(".rsrc")
	if sec == nil {
		return "", false
	}
	data, err := sec.Data()
	if err != nil {
		return "", false
	}
	return versionFromResource(data)
}

// versionFromResource finds a StringFileInfo "ProductVersion"/"FileVersion"
// entry in the raw bytes of a resource section. Each entry is the key as
// NUL-terminated UTF-16LE, zero padding to a 4-byte boundary, then the value
// as NUL-terminated UTF-16LE.
func versionFromResource(data []byte) (string, bool) {
	for _, key := range peVersionKeys {
		needle := utf16LE(key + "\x00")
		from := 0
		for {
			i := bytes.Index(data[from:], needle)
			if i < 0 {
				break
			}
			pos := from + i + len(needle)
			from = pos
			for pos+1 < len(data) && data[pos] == 0 && data[pos+1] == 0 {
				pos += 2 // padding
			}
			var units []uint16
			for pos+1 < len(data) {
				u := uint16(data[pos]) | uint16(data[pos+1])<<8
				if u == 0 {
					break
				}
				units = append(units, u)
				pos += 2
			}
			if v := strings.TrimSpace(string(utf16.Decode(units))); v != "" && v[0] >= '0' && v[0] <= '9' {
				return v, true
			}
		}
	}
	return "", false
}

func utf16LE(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 0, len(units)*2)
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

// winExeSource is the latest release's PDT.exe, downloaded lazily and at most
// once no matter how many drives need it.
type winExeSource struct {
	version  string
	assetURL string
	// download is update.Download in production; a field so tests can supply
	// a local file instead of touching the network.
	download func(url, destPath string) (string, error)

	tmpDir string
	exe    string
	dlErr  error
	tried  bool
}

func newWinExeSource(rel update.Release) (*winExeSource, error) {
	asset := rel.Asset(winExeAssetName)
	if asset == nil {
		return nil, fmt.Errorf("release %s has no %s asset", rel.TagName, winExeAssetName)
	}
	return &winExeSource{
		version:  strings.TrimPrefix(rel.TagName, "v"),
		assetURL: asset.DownloadURL,
		download: update.Download,
	}, nil
}

func (s *winExeSource) file() (string, error) {
	if s.tried {
		return s.exe, s.dlErr
	}
	s.tried = true
	dir, err := os.MkdirTemp("", "pdt-winexe-*")
	if err != nil {
		s.dlErr = err
		return "", err
	}
	s.tmpDir = dir
	path, err := s.download(s.assetURL, filepath.Join(dir, winExeAssetName))
	if err != nil {
		s.dlErr = fmt.Errorf("downloading %s: %w", winExeAssetName, err)
		return "", s.dlErr
	}
	s.exe = path
	return path, nil
}

func (s *winExeSource) cleanup() {
	if s.tmpDir != "" {
		os.RemoveAll(s.tmpDir)
	}
}

// ensureWinExeOnDrive makes sure driveRoot has a PDT.exe at least as new as
// src.version, downloading and copying one over if not. Returns the note to
// log; a non-nil error means the exe is still missing/outdated on the drive.
// The new file is staged next to the old one and renamed into place, so a
// failed or canceled copy never leaves a truncated PDT.exe on the drive.
func ensureWinExeOnDrive(ctx context.Context, driveRoot string, src *winExeSource, progress stepProgressFunc) (FlashNote, error) {
	exePath := filepath.Join(driveRoot, winExeName)
	installed, has := peInstalledVersion(exePath)
	if has && driver.CompareVersions(installed, src.version) >= 0 {
		return FlashNote{Level: "OK", Text: fmt.Sprintf("PDT.exe on %s is current (v%s).", driveRoot, installed)}, nil
	}

	downloaded, err := src.file()
	if err != nil {
		return FlashNote{}, err
	}
	info, err := os.Stat(downloaded)
	if err != nil {
		return FlashNote{}, err
	}
	staging := filepath.Join(driveRoot, ".PDT.exe.new")
	os.Remove(staging)
	buf := make([]byte, copyBufferSize)
	var done int64
	report := func(files int) {
		if progress != nil {
			progress("PDT.exe (Windows)", CopyProgress{DoneFiles: files, TotalFiles: 1, DoneBytes: done, TotalBytes: info.Size()})
		}
	}
	report(0)
	err = copyFileProgress(ctx, staging, downloaded, info.ModTime(), buf, func(n int64) {
		done += n
		report(0)
	})
	if err != nil {
		os.Remove(staging)
		return FlashNote{}, err
	}
	if err := os.Rename(staging, exePath); err != nil {
		os.Remove(staging)
		return FlashNote{}, fmt.Errorf("moving the new %s into place: %w", winExeName, err)
	}
	report(1)
	if has {
		return FlashNote{Level: "OK", Text: fmt.Sprintf("Updated PDT.exe on %s from v%s to v%s.", driveRoot, installed, src.version)}, nil
	}
	return FlashNote{Level: "OK", Text: fmt.Sprintf("Added PDT.exe v%s to %s.", src.version, driveRoot)}, nil
}
