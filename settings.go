package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"PDT/internal/cloudsync"
	"PDT/internal/driver"
)

// Settings is the whole persisted-preferences document - its own JSON file
// (rather than folding into config.SavedConfig) since these are app-wide
// preferences, not something that travels with a particular printer list.
type Settings struct {
	SaveFileBasePath   string            `json:"saveFileBasePath"`
	DriversBasePath    string            `json:"driversBasePath"`
	PreinstallBasePath string            `json:"preinstallBasePath"`
	ManufacturerURLs   map[string]string `json:"manufacturerUrls"`
	ManufacturerOrder  []string          `json:"manufacturerOrder"`
	CloudSync          CloudSyncSettings `json:"cloudSync"`

	// CloudSyncSecretKey is write-only, never persisted to settings.json and
	// never echoed back by GetSettings - SaveSettings reads it, stores it in
	// the OS keychain (see internal/cloudsync.SaveSecretKey), and clears it
	// back to "" before writing s to disk or keeping it as a.settings, the
	// same "type the new value, never see it again" shape a password-change
	// form normally has. Left "" on a save means "leave whatever secret is
	// already stored alone" - Settings can be saved for any other reason
	// (renaming a manufacturer URL, say) without re-entering the R2 key
	// every time.
	CloudSyncSecretKey string `json:"cloudSyncSecretKey,omitempty"`
	// CloudSyncHasSecret is populated fresh on every GetSettings/SaveSettings
	// response from the OS keychain itself (see internal/cloudsync.HasSecretKey)
	// - whatever value it holds when read back from settings.json on disk is
	// ignored, so a stale copy sitting there from an old save can't lie
	// about whether a secret is actually configured.
	CloudSyncHasSecret bool `json:"cloudSyncHasSecret"`
}

// CloudSyncSettings is the non-secret half of the Cloud Sync (R2) config -
// everything needed to reach the bucket except the Secret Access Key itself,
// which lives in the OS keychain instead (see Settings.CloudSyncSecretKey).
type CloudSyncSettings struct {
	// Endpoint is the R2 S3-compatible endpoint, e.g.
	// "https://<account-id>.r2.cloudflarestorage.com" - no bucket path on
	// the end (Bucket below is passed separately to every API call).
	Endpoint string `json:"endpoint"`
	Bucket   string `json:"bucket"`
	// Prefix scopes this tool to one folder within Bucket rather than the
	// whole bucket - Ken's own bucket has a single "Drivers" folder in it,
	// but nothing here assumes that's the only thing ever stored in a
	// shared bucket.
	Prefix      string `json:"prefix"`
	AccessKeyID string `json:"accessKeyId"`
	// ConcurrentTransfers bounds how many files SyncCloud transfers at
	// once - see SyncCloud's own doc comment for how this interacts with
	// starting the next queued file once the current one reaches 95%
	// (Ken's own request: actual simultaneous transfers can transiently
	// run a little over this near a handoff). Always >=1 - SaveSettings
	// clamps anything else, since 0 would mean no transfer could ever
	// start at all.
	ConcurrentTransfers int `json:"concurrentTransfers"`
}

// defaultConcurrentTransfers is Cloud Sync's own default worker count -
// Ken's own explicit choice, not derived from anything (unlike
// copyTreeWorkers' own "measured against real USB hardware" reasoning,
// there's no equivalent real-world measurement against R2 yet).
const defaultConcurrentTransfers = 3

// installedAppDataDir, defaultDriversBasePath, defaultSaveFileBasePath: see
// settings_windows.go/settings_darwin.go - installedAppDataDir is genuinely
// platform-specific (%LocalAppData%\PDT vs ~/Library/Application
// Support/PDT), but the two defaultXBasePath functions themselves are split
// only because of a real bug found live: the portable-copy case used to
// return a literal ".\Drivers" - Windows' own separator baked into the
// string - which meant nothing as a relative path once handed to
// filepath.Join on macOS (it doesn't split on the backslash at all), so it
// created a folder literally *named* ".\Drivers" right next to the running
// .app's own executable instead of a "Drivers" subfolder - severely enough
// that it broke `wails build`'s own codesign step once that malformed folder
// existed inside the bundle. Both platforms' portable case now returns the
// same bare "Drivers"/"Configs" literal (no leading ".\" or "./" on either
// side) rather than each echoing its own OS's separator convention -
// deliberate: it means Settings' own displayed base path never shows a
// platform-specific character at all, so a technician moving the same flash
// drive between a Windows and a macOS machine never sees it look "wrong" on
// one side or the other.

// defaultPreinstallBasePath is Documents\Preinstall under the current user's
// home directory on THIS computer - created on demand by the OS folder-
// picker dialog if it doesn't already exist, never eagerly by this tool
// itself. Unlike SaveFileBasePath, this is never exe-relative: it names a
// technician's own site-survey notes folder, which lives on their laptop
// regardless of which flash drive PDT happens to be running from.
func defaultPreinstallBasePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Documents", "Preinstall")
}

// defaultManufacturerURLs seeds the Settings > External Sites tab - each
// manufacturer's own driver download/support page, for the Defaults panel's
// "Check for Updates" button to open. No vendor here exposes an API to check
// the latest driver version automatically, so this only ever opens the page
// for a human to look at - these are just the starting points, editable in
// Settings since a vendor can relocate its own download page at any time.
func defaultManufacturerURLs() map[string]string {
	return map[string]string{
		"Canon":          "https://www.usa.canon.com/support/software-and-drivers",
		"HP":             "https://support.hp.com/ee-en/drivers/hp-universal-print-driver-series-for-windows/503548",
		"Kyocera":        "https://www.kyoceradocumentsolutions.us/en/support/downloads.html",
		"Ricoh":          "https://support.ricoh.com/bb/html/dr_ut_e/rc3/model/p_i/p_i.htm?lang=en",
		"Sharp":          "https://global.sharp/restricted/print/select.html?view=2",
		"Toshiba":        "https://business.toshiba.com/",
		"Xerox":          "https://www.support.xerox.com/en-us/product/global-printer-driver/downloads?language=en",
		"Konica Minolta": "https://onyxweb.mykonicaminolta.com/OneStopProductSupport?appMode=Public&target=Drivers",
		"Lexmark":        "https://www.lexmark.com/en_us/technical-support/universal-print-driver-support.html",
	}
}

// reconcileManufacturerOrder returns a complete permutation of every
// manufacturer in driver.Manufacturers: saved's entries first (in the order
// the user last dragged them into, dropping any that no longer name a real
// manufacturer), then any manufacturer not present in saved appended in
// driver.Manufacturers' own declared order - so a manufacturer added to a
// later PDT version (or one a user hasn't dragged yet) still shows up in the
// Settings > General reorder list instead of silently vanishing from it.
func reconcileManufacturerOrder(saved []string) []string {
	valid := make(map[string]bool, len(driver.Manufacturers))
	for _, m := range driver.Manufacturers {
		valid[m] = true
	}

	seen := make(map[string]bool, len(driver.Manufacturers))
	out := make([]string, 0, len(driver.Manufacturers))
	for _, m := range saved {
		if valid[m] && !seen[m] {
			out = append(out, m)
			seen[m] = true
		}
	}
	for _, m := range driver.Manufacturers {
		if !seen[m] {
			out = append(out, m)
		}
	}
	return out
}

// settingsFilePath is under the OS's standard per-user config directory
// (%AppData% on Windows) rather than next to the executable - unlike the
// Drivers folder, these are personal preferences that shouldn't get
// clobbered by reinstalling/moving the app, and every user of a shared
// install should get their own.
func settingsFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "PDT", "settings.json"), nil
}

// loadSettings reads the persisted settings file, falling back to defaults
// for a missing file, a corrupt one, or any field left blank in it - this is
// preferences, not user data, so silently recovering to sane defaults beats
// failing app startup over it. Each manufacturer's URL falls back to its own
// default independently (rather than the whole map being all-or-nothing), so
// a settings file saved before a manufacturer existed, or with just one URL
// blanked out, still gets sensible defaults for the rest.
func loadSettings() Settings {
	s := Settings{
		SaveFileBasePath:   defaultSaveFileBasePath(),
		DriversBasePath:    defaultDriversBasePath(),
		PreinstallBasePath: defaultPreinstallBasePath(),
		ManufacturerURLs:   defaultManufacturerURLs(),
		ManufacturerOrder:  reconcileManufacturerOrder(nil),
		CloudSync:          defaultCloudSyncSettings(),
	}
	path, err := settingsFilePath()
	if err != nil {
		return withCloudSyncHasSecret(s)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return withCloudSyncHasSecret(s)
	}
	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err != nil {
		return withCloudSyncHasSecret(s)
	}
	if loaded.SaveFileBasePath != "" {
		s.SaveFileBasePath = loaded.SaveFileBasePath
	}
	if loaded.DriversBasePath != "" {
		s.DriversBasePath = loaded.DriversBasePath
	}
	if loaded.PreinstallBasePath != "" {
		s.PreinstallBasePath = loaded.PreinstallBasePath
	}
	for mfg, url := range loaded.ManufacturerURLs {
		if url != "" {
			s.ManufacturerURLs[mfg] = url
		}
	}
	s.ManufacturerOrder = reconcileManufacturerOrder(loaded.ManufacturerOrder)
	if loaded.CloudSync.Endpoint != "" {
		s.CloudSync.Endpoint = loaded.CloudSync.Endpoint
	}
	if loaded.CloudSync.Bucket != "" {
		s.CloudSync.Bucket = loaded.CloudSync.Bucket
	}
	if loaded.CloudSync.Prefix != "" {
		s.CloudSync.Prefix = loaded.CloudSync.Prefix
	}
	if loaded.CloudSync.AccessKeyID != "" {
		s.CloudSync.AccessKeyID = loaded.CloudSync.AccessKeyID
	}
	if loaded.CloudSync.ConcurrentTransfers > 0 {
		s.CloudSync.ConcurrentTransfers = loaded.CloudSync.ConcurrentTransfers
	}
	return withCloudSyncHasSecret(s)
}

// withCloudSyncHasSecret sets s.CloudSyncHasSecret from the OS keychain
// itself - always recomputed, never trusted from whatever settings.json
// happened to have on disk (see Settings.CloudSyncHasSecret's own doc
// comment).
func withCloudSyncHasSecret(s Settings) Settings {
	s.CloudSyncHasSecret = cloudsync.HasSecretKey()
	s.CloudSyncSecretKey = ""
	return s
}

// defaultCloudSyncSettings seeds the account/bucket Ken's own PDT bucket
// already uses - "Drivers" is the one folder in it (mirroring driversRoot()'s
// own name), so a fresh install only ever needs an Access Key ID and Secret
// Access Key typed in before Cloud Sync works, not the whole endpoint/bucket
// pair re-entered by hand.
func defaultCloudSyncSettings() CloudSyncSettings {
	return CloudSyncSettings{
		Endpoint:            "https://60a85894a285594161353b7731fa385f.r2.cloudflarestorage.com",
		Bucket:              "pdt",
		Prefix:              "Drivers/",
		ConcurrentTransfers: defaultConcurrentTransfers,
	}
}

func saveSettingsToDisk(s Settings) error {
	// Never let the plaintext secret reach disk, even defensively - callers
	// (SaveSettings) already clear it before calling this, but a secret
	// belongs in the OS keychain and nowhere else, so this is enforced here
	// too rather than trusted to every future caller getting that right.
	s.CloudSyncSecretKey = ""
	path, err := settingsFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
