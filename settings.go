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
	// DirectDownloadURLs: manufacturer -> platform ("Windows"/"macOS",
	// driver.DirectDownloadPlatformWindows/Mac) -> family label (e.g.
	// "PCL6", "UFR II" - driver.DirectDownloadFamiliesFor) -> a direct
	// download URL for that exact driver - Settings > Direct Downloads
	// (GitHub issue #19). Unlike ManufacturerURLs (one general download
	// *page* per manufacturer, for a human to browse), each of these is
	// meant to be a real, direct link to the file itself - what a future
	// per-driver "Check for Update" button (issue #18) or site-survey
	// export (issue #15) would fetch or print without a human navigating a
	// vendor's own site first.
	DirectDownloadURLs map[string]map[string]map[string]string `json:"directDownloadUrls"`
	CloudSync          CloudSyncSettings                       `json:"cloudSync"`

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
	// VerboseLoggingDisabled turns off the extra Normal/Debug-level detail
	// background catalog work (e.g. the OpenPrinting nickname cache - see
	// buildOpenPrintingCatalogAsync) emits into the Log panel via the
	// toolbar's own Verbose checkbox (GitHub issue #16 follow-up,
	// 2026-09-19). false (the zero value) is deliberately "enabled" - Ken's
	// own explicit ask: verbose logging in Normal mode is the default a
	// tech should see without opting in, and this negative polarity is what
	// lets a settings.json that predates this field default to enabled
	// rather than silently starting disabled (same reasoning/fix as
	// printer.PrinterRow's own WindowsDisabled).
	VerboseLoggingDisabled bool `json:"verboseLoggingDisabled"`
	// LogLevel is "Normal" or "Debug" - the toolbar's own combobox next to
	// the Verbose checkbox. Any other value (blank - an old settings.json,
	// or a not-yet-recognized one) is treated as "Normal", never as
	// invalid - see loadSettings/SaveSettings' own normalization.
	LogLevel string `json:"logLevel"`
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

// defaultPreinstallBasePath is "Documents/Preinstall" - resolved against the
// current user's own home directory at read time (see resolveHomeRelative,
// app.go), never baked into an absolute string here. Unlike SaveFileBasePath
// (exe-relative), this is home-relative: it names a technician's own
// site-survey notes folder, which lives on their laptop regardless of which
// flash drive PDT happens to be running from - but it must still never be a
// literal absolute path, since that would only ever be correct for whichever
// one computer (and OS) it was first resolved on. Ken's own explicit ask
// (2026-09-18): a tech using both a Windows and a macOS laptop needs the same
// configured value to resolve correctly on either one - %UserProfile% on
// Windows, $HOME on macOS - the same reasoning resolveExeRelative's own doc
// comment already gives for DriversBasePath/SaveFileBasePath, just anchored
// on the user's home directory instead of the exe's own. Forward-slash
// stored (matching relToDriversRoot's own convention, GitHub issue #13) so
// the identical value means the same thing regardless of which OS resolves
// it. A folder-picker result (always a real, OS-resolved absolute path) is
// left exactly as the tech chose it, never collapsed back to this relative
// form - "techs can point it wherever they want" (Ken's own words) is a
// deliberate, explicit choice this never second-guesses.
func defaultPreinstallBasePath() string {
	return "Documents/Preinstall"
}

// defaultManufacturerURLs seeds the Settings > Download Centers tab - each
// manufacturer's own driver download/support page, for the Defaults panel's
// "Download Center" button to open. No vendor here exposes an API to check
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

// defaultDirectDownloadURLs seeds Settings > Direct Downloads (GitHub issue
// #19) - a real, direct file URL per manufacturer/platform/family
// (driver.DirectDownloadFamiliesFor names which families exist for each).
// Ken's own real, hand-verified links (2026-09-18) for "the primary vendors
// I work with... we can add more later" - adding a manufacturer or family
// later is a driver.directDownloadFamilies entry plus one more URL here,
// not a structural change.
func defaultDirectDownloadURLs() map[string]map[string]map[string]string {
	return map[string]map[string]map[string]string{
		"Canon": {
			driver.DirectDownloadPlatformWindows: {
				"PCL6":   "https://downloads.canon.com/sss2026/drivers/Generic_Plus_PCL6_v3.50.zip",
				"PS":     "https://downloads.canon.com/sss2026/drivers/Generic_Plus_PS3_v3.50.zip",
				"UFR II": "https://downloads.canon.com/sss2026/drivers/Generic_Plus_UFRII_v3.50.zip",
			},
			driver.DirectDownloadPlatformMac: {
				"PPD":    "https://downloads.canon.com/sss2026/drivers/PPDv5.50_mac.zip",
				"PS":     "https://downloads.canon.com/sss2026/drivers/PS_v4.17.24_mac.zip",
				"UFR II": "https://downloads.canon.com/sss2026/drivers/UFRII_v10.19.25_mac.zip",
			},
		},
		"Kyocera": {
			driver.DirectDownloadPlatformWindows: {
				"KX": "https://www.kyoceradocumentsolutions.us/content/dam/download-center-americas-cf/us/drivers/drivers/KX_Print_Driver_exe.download.exe",
			},
			driver.DirectDownloadPlatformMac: {
				"KPDL": "https://www.kyoceradocumentsolutions.us/content/dam/download-center-americas-cf/us/drivers/drivers/Mac56_2024_05_16_KDC_en_zip.download.dmg",
			},
		},
		"Ricoh": {
			driver.DirectDownloadPlatformWindows: {
				"PCL6": "https://support.ricoh.com/bb/pub_e/dr_ut_e/0001346/0001346104/V44500/z07106L24.exe",
				"PS":   "https://support.ricoh.com/bb/pub_e/dr_ut_e/0001346/0001346099/V44500/z07102L24.exe",
			},
			driver.DirectDownloadPlatformMac: {
				"PPD": "https://updates.cdn-apple.com/2021/macos/071-46900-20211101-39324856-757E-4475-BAEC-0B2CA2488F71/RicohPrinterDrivers.dmg",
			},
		},
		"Sharp": {
			driver.DirectDownloadPlatformWindows: {
				"UD3 PCL6": "https://global.sharp/restricted/print/mfpdl/sites/default/files/Global_Download_Data/19098/UD3_07_PCL6_2510a.zip",
			},
			driver.DirectDownloadPlatformMac: {
				"PPD": "https://global.sharp/restricted/print/mfpdl/sites/default/files/Global_Download_Data/18415/MX-C55c_2512a_MacPS.dmg",
			},
		},
		// Konica Minolta/HP/Lexmark/Toshiba/Xerox added 2026-09-20 (Ken's own
		// real, hand-verified links, GitHub issue #19's own remaining scope)
		// - see driver.directDownloadFamilies' own doc comment for each
		// family's Label/Tokens provenance.
		"Konica Minolta": {
			driver.DirectDownloadPlatformWindows: {
				"PCL & PS": "https://onyxweb.mykonicaminolta.com/OneStopProductSupport?appMode=public&productId=2275&categoryId=1&subCategoryId=ft17",
			},
			driver.DirectDownloadPlatformMac: {
				"PS": "https://onyxweb.mykonicaminolta.com/OneStopProductSupport?appMode=public&productId=2275&categoryId=1&subCategoryId=ft0",
			},
		},
		"HP": {
			driver.DirectDownloadPlatformWindows: {
				"PCL6": "https://ftp.hp.com/pub/softlib/software13/printers/UPD/upd-pcl6-win11-x64-8.2.0.26819.zip",
				"PS":   "https://ftp.hp.com/pub/softlib/software13/printers/UPD/upd-ps-win11-x64-8.2.0.26819.zip",
			},
			driver.DirectDownloadPlatformMac: {
				"HP Easy Start": "https://ftp.hp.com/pub/softlib/software12/HP_Quick_Start/osx/HP_Easy_Start.app.zip",
			},
		},
		"Lexmark": {
			driver.DirectDownloadPlatformWindows: {
				"PCL6 & PS": "https://downloads.lexmark.com/downloads/drivers/Lexmark_Universal_v2_UD1_Installation_Package_06092026.exe",
			},
			driver.DirectDownloadPlatformMac: {
				"Color": "https://downloads.lexmark.com/downloads/drivers/Lexmark_UC1_PrinterSoftware_04022026.dmg",
				"Mono":  "https://downloads.lexmark.com/downloads/drivers/Lexmark_UM1_PrinterSoftware_11202025.dmg",
			},
		},
		"Toshiba": {
			driver.DirectDownloadPlatformWindows: {
				"PCL6 & PS": "https://business.toshiba.com/downloads/KB/f1Ulds/18128/eb4-ebn-Uni-3264bit-7212483517.zip",
			},
			driver.DirectDownloadPlatformMac: {
				"Color": "https://business.toshiba.com/downloads/KB/f1Ulds/21838/TOSHIBA_ColorMFP.dmg.gz",
				"Mono":  "https://business.toshiba.com/downloads/KB/f1Ulds/21840/TOSHIBA_MonoMFP.dmg.gz",
			},
		},
		"Xerox": {
			driver.DirectDownloadPlatformWindows: {
				"PCL": "https://download.support.xerox.com/pub/drivers/GLOBALPRINTDRIVER/drivers/win10x64/ar/UNIV_5.1076.4.0_PCL6_x64.zip",
				"PS":  "https://download.support.xerox.com/pub/drivers/VLC8000W/drivers/win10x64/ar/UNIV_5.1076.4.0_PS_x64.zip",
			},
			driver.DirectDownloadPlatformMac: {
				"PPD": "https://download.support.xerox.com/pub/drivers/ALB80XX/drivers/macOS13/en_GB/XeroxDrivers_5.19.3_2562.dmg",
			},
		},
	}
}

// mergeDirectDownloadURLs merges loaded onto dst one family URL at a time
// (mirroring ManufacturerURLs' own per-manufacturer independent fallback in
// loadSettings below) - a settings.json saved before a manufacturer/
// platform/family existed, or with just one URL blanked out, still gets
// sensible defaults for the rest rather than losing a whole manufacturer's
// worth of URLs to one edited field.
func mergeDirectDownloadURLs(dst, loaded map[string]map[string]map[string]string) {
	for mfg, platforms := range loaded {
		for platform, families := range platforms {
			for family, url := range families {
				if url == "" {
					continue
				}
				if dst[mfg] == nil {
					dst[mfg] = map[string]map[string]string{}
				}
				if dst[mfg][platform] == nil {
					dst[mfg][platform] = map[string]string{}
				}
				dst[mfg][platform][family] = url
			}
		}
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
		DirectDownloadURLs: defaultDirectDownloadURLs(),
		CloudSync:          defaultCloudSyncSettings(),
		LogLevel:           "Normal",
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
	// VerboseLoggingDisabled passes straight through (not gated on
	// non-zero, unlike every string/map field above) - "explicitly true"
	// and "explicitly false" both need to survive a save, and false is
	// already this struct's own default, so there's no meaningful "blank
	// means absent" case to detect here at all, unlike PreDatesMacDriverSplit's
	// own JSON-key-presence trick elsewhere in this codebase.
	s.VerboseLoggingDisabled = loaded.VerboseLoggingDisabled
	s.LogLevel = normalizeLogLevel(loaded.LogLevel)
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
	mergeDirectDownloadURLs(s.DirectDownloadURLs, loaded.DirectDownloadURLs)
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

// normalizeLogLevel is LogLevel's own "any unrecognized value means Normal"
// rule - shared by loadSettings (an old/corrupt settings.json) and
// SaveSettings (a defensive normalization at the one other place this
// crosses the Wails bridge), so the two can never drift apart on what
// counts as a valid value.
func normalizeLogLevel(level string) string {
	if level == "Debug" {
		return "Debug"
	}
	return "Normal"
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
