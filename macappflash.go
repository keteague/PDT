package main

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"PDT/internal/driver"
	"PDT/internal/update"
)

// After a portable PDT is written to a flash drive, PDT tops up the macOS
// side too (Ken, 2026-09-20): a technician's laptop is usually Windows, and
// only its own PDT.exe gets copied to the drive - but the same drive is
// meant to be carried to Mac endpoints, where it needs PDT.app. When the
// laptop has Internet, the drive's PDT.app is checked against the latest
// GitHub release and downloaded/replaced if it's missing or older.

const (
	macAppBundleName = "PDT.app"
	// macAppAssetName is the release asset .github/workflows/release.yml
	// builds and uploads: `ditto -c -k --sequesterRsrc --keepParent` of
	// PDT.app, so every real entry sits under a top-level "PDT.app/".
	macAppAssetName = "PDT-macOS.zip"
)

// FlashNote is one informational/warning line a flash-drive operation wants
// logged beyond plain per-drive success/failure - Level is a logStatus level
// ("INFO", "OK", "WARN").
type FlashNote struct {
	Level string `json:"level"`
	Text  string `json:"text"`
}

// macAppSource is the latest release's macOS asset, downloaded lazily and at
// most once no matter how many drives need it.
type macAppSource struct {
	version  string
	assetURL string
	// download is update.Download in production; a field so tests can supply
	// a local zip instead of touching the network.
	download func(url, destPath string) (string, error)

	tmpDir  string
	zipPath string
	dlErr   error
	tried   bool
}

func (s *macAppSource) zip() (string, error) {
	if s.tried {
		return s.zipPath, s.dlErr
	}
	s.tried = true
	dir, err := os.MkdirTemp("", "pdt-macapp-*")
	if err != nil {
		s.dlErr = err
		return "", err
	}
	s.tmpDir = dir
	path, err := s.download(s.assetURL, filepath.Join(dir, macAppAssetName))
	if err != nil {
		s.dlErr = fmt.Errorf("downloading %s: %w", macAppAssetName, err)
		return "", s.dlErr
	}
	s.zipPath = path
	return path, nil
}

func (s *macAppSource) cleanup() {
	if s.tmpDir != "" {
		os.RemoveAll(s.tmpDir)
	}
}

// fetchMacAppSource asks GitHub for the latest release. A non-nil error means
// there's no usable Internet connection (or GitHub, or a release, or its
// macOS asset) - the caller reports that as a note and moves on; the drive
// write itself already succeeded.
func fetchMacAppSource() (*macAppSource, error) {
	rel, err := update.FetchLatest(repoSlug())
	if err != nil {
		return nil, err
	}
	return macAppSourceFrom(rel)
}

// macAppSourceFrom picks the macOS asset out of an already-fetched release.
func macAppSourceFrom(rel update.Release) (*macAppSource, error) {
	asset := rel.Asset(macAppAssetName)
	if asset == nil {
		return nil, fmt.Errorf("release %s has no %s asset", rel.TagName, macAppAssetName)
	}
	return &macAppSource{
		version:  strings.TrimPrefix(rel.TagName, "v"),
		assetURL: asset.DownloadURL,
		download: update.Download,
	}, nil
}

var (
	plistShortVersionRe = regexp.MustCompile(`<key>CFBundleShortVersionString</key>\s*<string>([^<]*)</string>`)
	plistBundleVersion  = regexp.MustCompile(`<key>CFBundleVersion</key>\s*<string>([^<]*)</string>`)
)

// macAppInstalledVersion reads an existing PDT.app's version out of its
// Info.plist (Wails writes info.productVersion into both keys). ok is false
// when the bundle, its plist, or a version in it can't be found - treated as
// "not usable", so it gets replaced.
func macAppInstalledVersion(appPath string) (version string, ok bool) {
	data, err := os.ReadFile(filepath.Join(appPath, "Contents", "Info.plist"))
	if err != nil {
		return "", false
	}
	for _, re := range []*regexp.Regexp{plistShortVersionRe, plistBundleVersion} {
		if m := re.FindSubmatch(data); m != nil {
			if v := strings.TrimSpace(string(m[1])); v != "" {
				return v, true
			}
		}
	}
	return "", false
}

// ensureMacAppOnDrive makes sure driveRoot has a PDT.app at least as new as
// src.version, downloading and extracting one if not. Returns the note to
// log; a non-nil error means the app is still missing/outdated on the drive.
func ensureMacAppOnDrive(ctx context.Context, driveRoot string, src *macAppSource, progress stepProgressFunc) (FlashNote, error) {
	appPath := filepath.Join(driveRoot, macAppBundleName)
	installed, has := macAppInstalledVersion(appPath)
	if has && driver.CompareVersions(installed, src.version) >= 0 {
		return FlashNote{Level: "OK", Text: fmt.Sprintf("PDT.app on %s is current (v%s).", driveRoot, installed)}, nil
	}

	zipPath, err := src.zip()
	if err != nil {
		return FlashNote{}, err
	}
	if err := extractMacAppZip(ctx, zipPath, driveRoot, progress); err != nil {
		return FlashNote{}, err
	}
	if has {
		return FlashNote{Level: "OK", Text: fmt.Sprintf("Updated PDT.app on %s from v%s to v%s.", driveRoot, installed, src.version)}, nil
	}
	return FlashNote{Level: "OK", Text: fmt.Sprintf("Added PDT.app v%s to %s.", src.version, driveRoot)}, nil
}

// extractMacAppZip extracts zipPath's PDT.app/... entries into
// driveRoot/PDT.app, staged in a sibling folder first so a failed or
// canceled extraction never leaves the drive with a half-written app - the
// existing one (if any) is only removed once the new one is fully in place.
// The __MACOSX resource-fork sidecar folder and AppleDouble "._" files a
// Mac-built zip can carry are skipped. Any symlink entry is recreated where
// the drive's filesystem allows it (exFAT/FAT can't - a Wails bundle has none
// of its own, so that's not expected to matter).
func extractMacAppZip(ctx context.Context, zipPath, driveRoot string, progress stepProgressFunc) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", filepath.Base(zipPath), err)
	}
	defer r.Close()

	prefix := macAppBundleName + "/"
	var entries []*zip.File
	var totalBytes int64
	for _, f := range r.File {
		name := filepath.ToSlash(f.Name)
		if !strings.HasPrefix(name, prefix) || strings.HasPrefix(filepath.Base(name), "._") {
			continue
		}
		entries = append(entries, f)
		totalBytes += int64(f.UncompressedSize64)
	}
	if len(entries) == 0 {
		return fmt.Errorf("%s contains no %s", filepath.Base(zipPath), macAppBundleName)
	}

	staging := filepath.Join(driveRoot, ".PDT.app.new")
	os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", staging, err)
	}
	fail := func(err error) error {
		os.RemoveAll(staging)
		return err
	}

	var doneFiles int
	var doneBytes int64
	report := func() {
		if progress != nil {
			progress("PDT.app (macOS)", CopyProgress{DoneFiles: doneFiles, TotalFiles: len(entries), DoneBytes: doneBytes, TotalBytes: totalBytes})
		}
	}
	report()

	for _, f := range entries {
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		rel := strings.TrimPrefix(filepath.ToSlash(f.Name), prefix)
		if rel == "" {
			continue
		}
		target := filepath.Join(staging, filepath.FromSlash(rel))
		if !withinDir(staging, target) {
			return fail(fmt.Errorf("%s: entry %q would extract outside the destination", filepath.Base(zipPath), f.Name))
		}

		mode := f.Mode()
		switch {
		case f.FileInfo().IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fail(err)
			}
			continue
		case mode&os.ModeSymlink != 0:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fail(err)
			}
			src, err := f.Open()
			if err != nil {
				return fail(err)
			}
			linkTarget, err := io.ReadAll(io.LimitReader(src, 4096))
			src.Close()
			if err != nil {
				return fail(err)
			}
			_ = os.Symlink(string(linkTarget), target) // best-effort - see doc comment
		default:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fail(err)
			}
			if err := writeZipEntry(f, target); err != nil {
				return fail(err)
			}
		}
		doneFiles++
		doneBytes += int64(f.UncompressedSize64)
		report()
	}

	appPath := filepath.Join(driveRoot, macAppBundleName)
	if err := os.RemoveAll(appPath); err != nil {
		return fail(fmt.Errorf("removing the old %s: %w", macAppBundleName, err))
	}
	if err := os.Rename(staging, appPath); err != nil {
		return fail(fmt.Errorf("moving the new %s into place: %w", macAppBundleName, err))
	}
	return nil
}

func writeZipEntry(f *zip.File, target string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	// 0o755 for everything: the drive is exFAT (no per-file permissions), and
	// macOS shows every file on such a volume as executable anyway - what
	// matters is that Contents/MacOS/PDT isn't written read-only.
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

// withinDir reports whether target is dir itself or inside it - guards
// against a zip entry using ".." to write outside the staging folder.
func withinDir(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
