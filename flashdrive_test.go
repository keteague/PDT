package main

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"

	"PDT/internal/driver"
)

// TestWritePortablePDTTo_AlwaysCreatesConfigsAndFullDriversScaffold guards
// against two real gaps found live: a technician's local Configs folder
// often doesn't exist yet (nothing saved/captured there so far), which used
// to leave the flash drive with no Configs folder at all; and their local
// Drivers folder is very rarely fully populated for every manufacturer PDT
// knows about, which used to leave the flash drive missing folders for
// whichever manufacturers weren't already downloaded locally.
//
// The full-manufacturer-scaffold assertion at the bottom is Windows-only: it
// exercises postSyncDriversHook's own ensureDriversScaffold call
// (app_windows.go), which has no macOS equivalent yet (see app_darwin.go's
// own doc comment - there's no macOS Drivers-folder scaffold built yet at
// all) - skipped there rather than the whole test, since the Configs-folder
// and real-content-copied assertions above it are platform-independent and
// still worth running on every platform.
func TestWritePortablePDTTo_AlwaysCreatesConfigsAndFullDriversScaffold(t *testing.T) {
	oldDrivers, oldConfigs := currentDriversBasePath, currentConfigsBasePath
	defer func() {
		currentDriversBasePath, currentConfigsBasePath = oldDrivers, oldConfigs
	}()

	sourceDrivers := t.TempDir()
	// Only Canon has anything real locally - every other manufacturer is
	// deliberately absent, the common case on a technician's own laptop.
	canonDir := filepath.Join(sourceDrivers, "Windows", "11", "Canon")
	if err := os.MkdirAll(canonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonDir, "real-driver.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	currentDriversBasePath = sourceDrivers

	// No Configs folder at all yet - the common "nothing saved yet" case.
	currentConfigsBasePath = filepath.Join(t.TempDir(), "Configs-does-not-exist")

	dest := t.TempDir()
	if err := writePortablePDTTo(context.Background(), dest, "PDT.exe", []byte("fake exe bytes"), true, nil, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dest, "PDT.exe")); err != nil {
		t.Errorf("expected PDT.exe to be written: %v", err)
	}
	if info, err := os.Stat(filepath.Join(dest, "Configs")); err != nil || !info.IsDir() {
		t.Errorf("expected a Configs folder to exist even though the source had none: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "Drivers", "Windows", "11", "Canon", "real-driver.zip")); err != nil {
		t.Errorf("expected Canon's real local content to be copied: %v", err)
	}
	if goruntime.GOOS != "windows" {
		return
	}
	for _, mfg := range driver.Manufacturers {
		if mfg == "Canon" {
			continue
		}
		folder := filepath.Join(dest, "Drivers", "Windows", "11", mfg)
		if mfg == "Konica Minolta" {
			folder = filepath.Join(dest, "Drivers", "Windows", "11", "KonicaMinolta")
		}
		if info, err := os.Stat(folder); err != nil || !info.IsDir() {
			t.Errorf("expected %s to be scaffolded on the flash drive even though it had nothing locally: %v", mfg, err)
		}
	}
}

// TestWritePortablePDTTo_RepeatWriteDoesNotDropRealDrivers guards against the
// exact real bug that motivated switching from os.CopyFS to copyTreeMerge:
// os.CopyFS documents that it "will not overwrite existing files" and "stops
// at and returns the first error encountered" - so writing to a flash drive
// that already had anything under Drivers\ (a prior Write to Flash Drive, or
// even just its own scaffolded Archive\README.txt files) made the copy fail
// immediately, silently copying none of the real driver files after
// whichever path it choked on first. Confirmed live.
func TestWritePortablePDTTo_RepeatWriteDoesNotDropRealDrivers(t *testing.T) {
	oldDrivers, oldConfigs := currentDriversBasePath, currentConfigsBasePath
	defer func() {
		currentDriversBasePath, currentConfigsBasePath = oldDrivers, oldConfigs
	}()

	sourceDrivers := t.TempDir()
	canonDir := filepath.Join(sourceDrivers, "Windows", "11", "Canon")
	if err := os.MkdirAll(canonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonDir, "real-driver.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	currentDriversBasePath = sourceDrivers
	currentConfigsBasePath = filepath.Join(t.TempDir(), "Configs-does-not-exist")

	dest := t.TempDir()
	if err := writePortablePDTTo(context.Background(), dest, "PDT.exe", []byte("fake exe bytes"), true, nil, nil); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	// Second write to the SAME already-populated destination - this is what
	// os.CopyFS choked on.
	if err := writePortablePDTTo(context.Background(), dest, "PDT.exe", []byte("fake exe bytes, updated"), true, nil, nil); err != nil {
		t.Fatalf("second write to an already-populated destination failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, "Drivers", "Windows", "11", "Canon", "real-driver.zip")); err != nil {
		t.Errorf("expected Canon's real content to still be present after a second write: %v", err)
	}
}

// TestWritePortablePDTTo_CopiesSevenZipTools guards the "USB drive should
// include tools (7-zip) in case it's needed" request - a portable copy
// should be fully self-contained, not rely on the destination computer
// already having 7z.exe cached from some other PDT install. Written
// straight from this build's own embedded copy (writeSevenZipAssets,
// sevenzipassets.go) rather than any per-machine cache, so this is fully
// deterministic - every platform's build embeds the same assets, including
// a macOS build, which has no such cache to depend on at all.
func TestWritePortablePDTTo_CopiesSevenZipTools(t *testing.T) {
	oldDrivers, oldConfigs := currentDriversBasePath, currentConfigsBasePath
	defer func() {
		currentDriversBasePath, currentConfigsBasePath = oldDrivers, oldConfigs
	}()
	currentDriversBasePath = t.TempDir()
	currentConfigsBasePath = filepath.Join(t.TempDir(), "Configs-does-not-exist")

	dest := t.TempDir()
	if err := writePortablePDTTo(context.Background(), dest, "PDT.exe", []byte("fake exe bytes"), true, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"7z.exe", "7z.dll", "License.txt"} {
		if _, err := os.Stat(filepath.Join(dest, "tools", "7zip", name)); err != nil {
			t.Errorf("expected %s to be written onto the flash drive: %v", name, err)
		}
	}
}

// TestSyncDriversTo_SkipsWhenTargetIsTheSameAsSource guards against a real
// hazard: Sync stays available even when PDT itself is running from a flash
// drive (unlike Write to Flash Drive), so a technician could pick that exact
// same drive as the sync target. copyTreeMerge would then be asked to copy a
// directory tree onto itself - truncating a source file while still reading
// it (its own os.OpenFile uses O_TRUNC). This must be a silent no-op
// instead.
func TestSyncDriversTo_SkipsWhenTargetIsTheSameAsSource(t *testing.T) {
	oldDrivers := currentDriversBasePath
	defer func() { currentDriversBasePath = oldDrivers }()

	root := t.TempDir()
	driversDir := filepath.Join(root, "Drivers")
	if err := os.MkdirAll(filepath.Join(driversDir, "Windows", "11", "Canon"), 0o755); err != nil {
		t.Fatal(err)
	}
	realFile := filepath.Join(driversDir, "Windows", "11", "Canon", "real-driver.zip")
	if err := os.WriteFile(realFile, []byte("original content"), 0o644); err != nil {
		t.Fatal(err)
	}
	currentDriversBasePath = driversDir

	// root's own Drivers subfolder IS driversRoot() - syncing "to root" would
	// make the destination and source identical.
	if err := syncDriversTo(context.Background(), root, nil); err != nil {
		t.Fatalf("expected a same-path sync to be a silent no-op, got error: %v", err)
	}

	data, err := os.ReadFile(realFile)
	if err != nil || string(data) != "original content" {
		t.Errorf("expected the source file to be completely untouched: data=%q err=%v", data, err)
	}
}

// TestEtaEstimator_NoEstimateBeforeMinElapsed guards the "don't flash a
// wrong number immediately" gate - a rate measured over a fraction of a
// second is unreliable.
func TestEtaEstimator_NoEstimateBeforeMinElapsed(t *testing.T) {
	var est etaEstimator
	start := time.Now()
	est.reset(start, 0)

	if got := est.sample(start.Add(time.Second), 10_000_000, 100_000_000); got != 0 {
		t.Errorf("sample() before etaMinElapsed = %d, want 0 (not known yet)", got)
	}
}

// TestEtaEstimator_ZeroOnceTotalReached guards against reporting a stale
// nonzero ETA once there's nothing left to copy.
func TestEtaEstimator_ZeroOnceTotalReached(t *testing.T) {
	var est etaEstimator
	start := time.Now()
	est.reset(start, 0)
	now := start
	for i := 0; i < 10; i++ {
		now = now.Add(time.Second)
		est.sample(now, int64(i+1)*10_000_000, 100_000_000)
	}
	if got := est.sample(now, 100_000_000, 100_000_000); got != 0 {
		t.Errorf("sample() once doneBytes reached totalBytes = %d, want 0", got)
	}
}

// TestEtaEstimator_UsesThirtySecondAverage: the estimate is the bytes
// remaining divided by the average rate over the LAST 30 seconds - history
// older than that must not count, and the rate within it is a plain average
// (not weighted toward the most recent second).
func TestEtaEstimator_UsesThirtySecondAverage(t *testing.T) {
	var est etaEstimator
	start := time.Now()
	est.reset(start, 0)

	const totalBytes = 10_000_000_000
	var done int64
	now := start
	// 60s at 10MB/s, then 30s at 1MB/s: the window now only sees the slow phase.
	for i := 0; i < 60; i++ {
		now = now.Add(time.Second)
		done += 10_000_000
		est.sample(now, done, totalBytes)
	}
	for i := 0; i < 30; i++ {
		now = now.Add(time.Second)
		done += 1_000_000
		est.sample(now, done, totalBytes)
	}
	if r := est.rate(); r < 0.95e6 || r > 1.05e6 {
		t.Errorf("rate() = %.0f B/s, want ~1,000,000 (the last 30s only)", r)
	}
	got := est.sample(now, done, totalBytes)
	want := int(float64(totalBytes-done) / 1_000_000)
	if got < want*95/100 || got > want*105/100 {
		t.Errorf("ETA = %ds, want ~%ds (remaining bytes / 30s average rate)", got, want)
	}
}

// TestEtaEstimator_AveragesBurstsInsideTheWindow: 15s at 10MB/s then 15s of
// nothing averages to 5MB/s over the 30s window - a stall lowers the rate
// rather than being ignored.
func TestEtaEstimator_AveragesBurstsInsideTheWindow(t *testing.T) {
	var est etaEstimator
	start := time.Now()
	est.reset(start, 0)
	var done int64
	now := start
	for i := 0; i < 15; i++ {
		now = now.Add(time.Second)
		done += 10_000_000
		est.sample(now, done, 1_000_000_000)
	}
	for i := 0; i < 15; i++ {
		now = now.Add(time.Second)
		est.sample(now, done, 1_000_000_000)
	}
	if r := est.rate(); r < 4.7e6 || r > 5.3e6 {
		t.Errorf("rate() = %.0f B/s, want ~5,000,000", r)
	}
}

// TestEtaEstimator_BoundedHistory: however often progress is reported, the
// window keeps a bounded number of samples.
func TestEtaEstimator_BoundedHistory(t *testing.T) {
	var est etaEstimator
	start := time.Now()
	est.reset(start, 0)
	now := start
	for i := 0; i < 200_000; i++ {
		now = now.Add(time.Millisecond)
		est.sample(now, int64(i)*1000, 1<<40)
	}
	if len(est.samples) > 400 {
		t.Errorf("window holds %d samples, want a few hundred at most", len(est.samples))
	}
}

// TestWritePortablePDTTo_ExcludesDriversWhenNotRequested: with includeDrivers
// false (the dialog's default - Ken, 2026-09-20) nothing from the laptop's
// Drivers folder is copied, but the drive still gets an empty Drivers folder
// so the exe on it still recognizes itself as portable.
func TestWritePortablePDTTo_ExcludesDriversWhenNotRequested(t *testing.T) {
	oldDrivers, oldConfigs := currentDriversBasePath, currentConfigsBasePath
	defer func() {
		currentDriversBasePath, currentConfigsBasePath = oldDrivers, oldConfigs
	}()

	sourceDrivers := t.TempDir()
	canonDir := filepath.Join(sourceDrivers, "Windows", "11", "Canon")
	if err := os.MkdirAll(canonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonDir, "real-driver.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	currentDriversBasePath = sourceDrivers
	currentConfigsBasePath = filepath.Join(t.TempDir(), "Configs-does-not-exist")

	dest := t.TempDir()
	if err := writePortablePDTTo(context.Background(), dest, "PDT.exe", []byte("fake exe bytes"), false, nil, nil); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Stat(filepath.Join(dest, "Drivers")); err != nil || !info.IsDir() {
		t.Fatalf("expected an (empty) Drivers folder on the drive even when not copying drivers: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "Drivers", "Windows", "11", "Canon", "real-driver.zip")); !os.IsNotExist(err) {
		t.Errorf("expected the laptop's driver package to NOT be copied, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "PDT.exe")); err != nil {
		t.Errorf("expected PDT.exe to still be written: %v", err)
	}
}

// TestSyncConfigsTo_MergesAndCreatesConfigsFolder: the sync dialog's Configs
// checkbox (Ken, 2026-09-20) copies this laptop's Configs onto the drive,
// merging with what's already there, and leaves a Configs folder even when
// the laptop has nothing to copy.
func TestSyncConfigsTo_MergesAndCreatesConfigsFolder(t *testing.T) {
	oldConfigs := currentConfigsBasePath
	defer func() { currentConfigsBasePath = oldConfigs }()

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "14545-1.json"), []byte("cfg"), 0o644); err != nil {
		t.Fatal(err)
	}
	currentConfigsBasePath = src

	drive := t.TempDir()
	existing := filepath.Join(drive, "Configs")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "other-tech.json"), []byte("theirs"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := syncConfigsTo(context.Background(), drive, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(existing, "14545-1.json")); err != nil {
		t.Errorf("expected the laptop's config to be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(existing, "other-tech.json")); err != nil {
		t.Errorf("a sync must merge, not wipe what's already on the drive: %v", err)
	}

	// Nothing local to copy: the folder is still created.
	currentConfigsBasePath = filepath.Join(t.TempDir(), "does-not-exist")
	empty := t.TempDir()
	if err := syncConfigsTo(context.Background(), empty, nil); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(empty, "Configs")); err != nil || !info.IsDir() {
		t.Errorf("expected an empty Configs folder to be created: %v", err)
	}
}

// With a cloud source supplied, writePortablePDTTo uses it INSTEAD of copying
// the laptop's own Drivers folder.
func TestWritePortablePDTTo_UsesCloudDriversSourceInsteadOfLocalCopy(t *testing.T) {
	oldDrivers, oldConfigs := currentDriversBasePath, currentConfigsBasePath
	defer func() {
		currentDriversBasePath, currentConfigsBasePath = oldDrivers, oldConfigs
	}()
	sourceDrivers := t.TempDir()
	canonDir := filepath.Join(sourceDrivers, "Windows", "11", "Canon")
	if err := os.MkdirAll(canonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonDir, "local-only.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	currentDriversBasePath = sourceDrivers
	currentConfigsBasePath = filepath.Join(t.TempDir(), "Configs-does-not-exist")

	dest := t.TempDir()
	called := 0
	cloud := func(ctx context.Context, letter string, progress func(CopyProgress)) error {
		called++
		if letter != dest {
			t.Errorf("cloud source got letter %q, want %q", letter, dest)
		}
		return nil
	}
	if err := writePortablePDTTo(context.Background(), dest, "PDT.exe", []byte("fake exe bytes"), true, cloud, nil); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Errorf("expected the cloud source to be used once, got %d", called)
	}
	if _, err := os.Stat(filepath.Join(dest, "Drivers", "Windows", "11", "Canon", "local-only.zip")); !os.IsNotExist(err) {
		t.Errorf("the laptop's own Drivers must NOT be copied when the cloud is the source, got err=%v", err)
	}
}
