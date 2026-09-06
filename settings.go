package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"PDT/internal/driver"
)

// Settings is the whole persisted-preferences document - its own JSON file
// (rather than folding into config.SavedConfig) since these are app-wide
// preferences, not something that travels with a particular printer list.
type Settings struct {
	SaveFileBasePath  string            `json:"saveFileBasePath"`
	ManufacturerURLs  map[string]string `json:"manufacturerUrls"`
	ManufacturerOrder []string          `json:"manufacturerOrder"`
}

// defaultSaveFileBasePath is Documents\Preinstall under the current user's
// home directory - created on demand by the OS folder-picker/save dialog if
// it doesn't already exist, never eagerly by this tool itself.
func defaultSaveFileBasePath() string {
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
		SaveFileBasePath:  defaultSaveFileBasePath(),
		ManufacturerURLs:  defaultManufacturerURLs(),
		ManufacturerOrder: reconcileManufacturerOrder(nil),
	}
	path, err := settingsFilePath()
	if err != nil {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err != nil {
		return s
	}
	if loaded.SaveFileBasePath != "" {
		s.SaveFileBasePath = loaded.SaveFileBasePath
	}
	for mfg, url := range loaded.ManufacturerURLs {
		if url != "" {
			s.ManufacturerURLs[mfg] = url
		}
	}
	s.ManufacturerOrder = reconcileManufacturerOrder(loaded.ManufacturerOrder)
	return s
}

func saveSettingsToDisk(s Settings) error {
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
