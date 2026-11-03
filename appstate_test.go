package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"PDT/internal/cloudsync"
)

// isolateSplitDirs gives the legacy (%AppData%) and new (%LocalAppData%)
// locations separate temp folders - the real Windows layout - and resets
// the keychain mock and the seeded flag.
func isolateSplitDirs(t *testing.T) (legacyPDT, newPDT string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("the legacy and installed state folders only differ on Windows")
	}
	roaming, local := t.TempDir(), t.TempDir()
	t.Setenv("AppData", roaming)
	t.Setenv("LOCALAPPDATA", local)
	_ = cloudsync.DeleteSecretKey()
	guestCloudSyncSeeded = false
	t.Cleanup(func() { _ = cloudsync.DeleteSecretKey(); guestCloudSyncSeeded = false })
	return filepath.Join(roaming, "PDT"), filepath.Join(local, "PDT")
}

func withPortableExeDir(t *testing.T, dir string) {
	t.Helper()
	prev := portableExeDir
	portableExeDir = func() string { return dir }
	t.Cleanup(func() { portableExeDir = prev })
}

func writeJSON(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A brand-new installed copy gets the read-only guest account.
func TestLoadSettings_FreshInstallGetsGuestCloudSync(t *testing.T) {
	isolateSplitDirs(t)
	withPortableExeDir(t, "")

	s := loadSettings()

	if s.CloudSync.AccessKeyID != guestCloudAccessKeyID || !guestCloudSyncSeeded {
		t.Fatalf("expected the guest key ID, got %q (seeded=%v)", s.CloudSync.AccessKeyID, guestCloudSyncSeeded)
	}
	if secret, _ := cloudsync.LoadSecretKey(); secret != guestCloudSecretKey {
		t.Error("expected the guest secret in the keychain")
	}
	if !s.CloudSyncHasSecret {
		t.Error("expected CloudSyncHasSecret")
	}
}

// An existing installation is never touched - even one with no Cloud Sync
// account at all - and its old %AppData% settings are carried over.
func TestLoadSettings_ExistingInstallIsNotSeededAndIsMigrated(t *testing.T) {
	legacy, newDir := isolateSplitDirs(t)
	withPortableExeDir(t, "")
	writeJSON(t, filepath.Join(legacy, "settings.json"), `{"logLevel":"Debug","cloudSync":{"accessKeyId":""}}`)
	writeJSON(t, filepath.Join(legacy, "cloudsync-state.json"), `{"deselected":{"a.zip":true},"seen":{}}`)

	s := loadSettings()

	if guestCloudSyncSeeded || s.CloudSync.AccessKeyID != "" {
		t.Errorf("existing install must not get the guest key, got %q", s.CloudSync.AccessKeyID)
	}
	if secret, _ := cloudsync.LoadSecretKey(); secret != "" {
		t.Error("keychain must stay empty")
	}
	if s.LogLevel != "Debug" {
		t.Errorf("settings not carried over from %%AppData%%: LogLevel=%q", s.LogLevel)
	}
	for _, name := range []string{"settings.json", "cloudsync-state.json"} {
		if !fileExists(filepath.Join(newDir, name)) {
			t.Errorf("%s not copied to %%LocalAppData%%\\PDT", name)
		}
		if !fileExists(filepath.Join(legacy, name)) {
			t.Errorf("%s removed from %%AppData%%\\PDT - must be copied, not moved", name)
		}
	}
}

// An existing installation whose settings already live in the new place is
// left alone too, real key and all.
func TestLoadSettings_ExistingAccountUntouched(t *testing.T) {
	_, newDir := isolateSplitDirs(t)
	withPortableExeDir(t, "")
	writeJSON(t, filepath.Join(newDir, "settings.json"), `{"cloudSync":{"accessKeyId":"techs-own-key"}}`)
	if err := cloudsync.SaveSecretKey("techs-own-secret"); err != nil {
		t.Fatal(err)
	}

	s := loadSettings()

	if s.CloudSync.AccessKeyID != "techs-own-key" || guestCloudSyncSeeded {
		t.Errorf("expected the technician's own key kept, got %q (seeded=%v)", s.CloudSync.AccessKeyID, guestCloudSyncSeeded)
	}
	if secret, _ := cloudsync.LoadSecretKey(); secret != "techs-own-secret" {
		t.Errorf("secret replaced: %q", secret)
	}
}

// No settings file anywhere, but a secret already in the keychain (an
// earlier install's account): never replaced, never paired with the guest ID.
func TestLoadSettings_FreshSettingsButKeychainSecretNotSeeded(t *testing.T) {
	isolateSplitDirs(t)
	withPortableExeDir(t, "")
	if err := cloudsync.SaveSecretKey("earlier-install-secret"); err != nil {
		t.Fatal(err)
	}

	s := loadSettings()

	if guestCloudSyncSeeded || s.CloudSync.AccessKeyID != "" {
		t.Errorf("expected no guest key, got %q", s.CloudSync.AccessKeyID)
	}
	if secret, _ := cloudsync.LoadSecretKey(); secret != "earlier-install-secret" {
		t.Errorf("secret replaced: %q", secret)
	}
}

// A portable copy keeps its state on the flash drive, never reads the PC's
// files (not even on the tech's own laptop), and never gets the guest key.
func TestLoadSettings_PortableCopyUsesItsOwnSettings(t *testing.T) {
	legacy, newDir := isolateSplitDirs(t)
	drive := t.TempDir()
	withPortableExeDir(t, drive)
	pcSettings := `{"logLevel":"Debug","driversBasePath":"C:\\Laptop\\Drivers"}`
	writeJSON(t, filepath.Join(legacy, "settings.json"), pcSettings)
	writeJSON(t, filepath.Join(newDir, "settings.json"), pcSettings)

	s := loadSettings()

	if s.LogLevel == "Debug" || s.DriversBasePath == `C:\Laptop\Drivers` {
		t.Errorf("portable copy read the PC's settings: LogLevel=%q Drivers=%q", s.LogLevel, s.DriversBasePath)
	}
	if guestCloudSyncSeeded {
		t.Error("portable copy must not get the guest key")
	}
	if err := saveSettingsToDisk(s); err != nil {
		t.Fatal(err)
	}
	onDrive := filepath.Join(drive, portableSettingsDirName)
	if !fileExists(filepath.Join(onDrive, "settings.json")) {
		t.Errorf("expected settings saved on the drive in %s", onDrive)
	}
	if p, _ := cloudSyncStateFilePath(); filepath.Dir(p) != onDrive {
		t.Errorf("Cloud Sync state should live on the drive, got %s", p)
	}
	if p, _ := updateCheckStateFilePath(); filepath.Dir(p) != onDrive {
		t.Errorf("update-check state should live on the drive, got %s", p)
	}
}
