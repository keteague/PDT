# Changelog

All notable changes to this project are documented here. This is a from-scratch Go/Wails rewrite of
`Create-Printers.ps1`; entries reference that original tool's own history where a decision or
limitation carries forward from it.

## 2026-09-06 - Bundle 7-Zip, auto-extract self-extracting RAR packages

### Added
- **Self-extracting RAR driver packages (Lexmark's own) are now auto-extracted**, the same way `.zip`
  packages already are. `internal/driver/rarsfx.go`: `isSelfExtractingRar` detects one by scanning for
  the actual RAR signature bytes (not a naming convention - `.zip`-extraction's convention of matching
  on file extension doesn't work here since a self-extracting archive is still just a `.exe`), and
  `ensureRarSfxExtracted` extracts a match into a sibling folder, same skip-if-already-extracted
  convention as `ensureZipsExtracted`.
- **Bundles `7z.exe` + `7z.dll`** (`third_party/7zip/`, embedded via `go:embed` in the new
  `sevenzip.go` and extracted once to `%LocalAppData%\PDT\tools\7zip\` at startup) to actually perform
  that extraction - reached only after ruling out every zero-dependency option:
  - Go's standard library has no RAR reader at all.
  - The one pure-Go RAR library evaluated, `nwaples/rardecode` (`/v2`), opens and lists the real
    Lexmark archive correctly but **silently corrupts exactly the `.msi` files this needs** - confirmed
    by feeding its output back to `msiexec`, which rejected it outright
    (`ERROR_INSTALL_PACKAGE_INVALID`, not just a checksum-mismatch false alarm). No known fix or newer
    version resolves this (checked the library's open GitHub issues directly - nothing matches this
    symptom), and no more-mature pure-Go alternative exists.
  - 7-Zip's own official, easily-redistributable "Extra" console-only package (`7za.exe`) does **not**
    include RAR support at all - confirmed directly by downloading it fresh and testing it against the
    real file (`Cannot open the file as archive`). Only the full `7z.dll` (from the full GUI install)
    has RAR support.
  - Confirmed instead that just `7z.exe` + `7z.dll` (~2.5MB total), copied out of a full 7-Zip install
    with no installer or registry entries, run completely standalone - tested in a bare, isolated
    folder with nothing else present. Redistribution is permitted under 7-Zip's own license (read
    directly from `7-zip.org/license.txt`, not a secondhand summary): the RAR-decoding code is LGPL
    plus an "unRAR restriction" that only bars using it to build a RAR *compressor*, not redistributing
    the decoder - `third_party/7zip/License.txt` ships alongside the binaries per that license's own
    terms.

### Verified
- New tests: `TestIsSelfExtractingRar` (RAR5 and older-format signatures, both at a nonzero offset
  mirroring a real SFX stub, plus a true-negative on an ordinary `.exe`),
  `TestEnsureRarSfxExtracted_NoOpWithoutSevenZipConfigured`,
  `TestEnsureRarSfxExtracted_SkipsAlreadyExtracted`. Full suite (`go build`/`vet`/`test`, `wails build`)
  clean.
- Live end to end against the real, unmodified `Lexmark_Universal_v2_UD1_Installation_Package_*.exe`
  sitting in the real Drivers folder: launched the built `PDT.exe` fresh, confirmed via temporary debug
  logging (removed before finishing) that `ensureRarSfxExtracted` correctly identified the file as a
  self-extracting RAR and extracted it successfully, then confirmed the resulting
  `InstallationPackage\...` folder tree appeared on disk with no manual intervention. (The `.msi` layer
  past that point is still a manual, documented step - see the README's Lexmark section.)

## 2026-09-06 - Lexmark actually works - yesterday's "known limitation" entry was wrong

### Fixed
- **The "Lexmark: known limitation" entry a few sections down was itself the result of two of my own
  mistakes, not a real Lexmark incompatibility** - corrected after redoing the whole investigation
  with a genuinely elevated session (see below) and finding driver install, printer creation,
  duplex/color, and APF all work correctly through PDT's existing, unmodified code. What actually
  happened:
  1. The PowerShell session used for yesterday's testing had silently lost its elevation partway
     through the conversation (confirmed via `whoami /groups` - Medium integrity, Administrators
     group present but "deny only", the signature of a UAC-filtered non-elevated token) - re-tested a
     known-good Canon driver install in that same session and it failed identically, proving the
     session itself was the problem, not Lexmark. Fixed once the user relaunched the terminal as
     Administrator.
  2. Separately, and more subtly: the extraction script guessed each compressed file's real extension
     from its compressed one (`.gd_` -> assumed `.gpd`, `.in_` -> assumed `.inf`) rather than asking
     `expand.exe` for the real embedded name (`expand -R`) - the guess was wrong for both. `.in_`
     actually decompresses to `.ini`, so the guess clobbered the real `.inf` (already correctly
     extracted by the earlier `msiexec /a` step) with unrelated `.ini` content sharing the same
     assumed filename - silently, since both are small text files. `.gd_` actually decompresses to
     `.gdl`, a file the driver's own `CopyFiles` section requires and which never got created at all
     under the wrong assumed `.gpd` name - `SetupCopyOEMInf` tolerated the missing file quietly enough
     at staging time to report success, but `AddPrinter` failed the moment anything tried to actually
     use the driver (`ERROR_CAN_NOT_COMPLETE`), confirmed independently through PDT's own code,
     native `Add-Printer`, and against both the `NUL:` port and a real Standard TCP/IP port (ruling
     out a port-specific cause).
  See the updated "Lexmark: self-extracting RAR + `.msi`-packaged drivers" section in the README for
  the corrected procedure using `expand -R`.

### Verified
- Full round-trip against the real package: `msiexec /a` extraction, `expand -R` decompression of
  every compressed sibling, `EnsureDriverInstalled` (stage + register - PDT's existing
  `driverinstall_windows.go`, no changes), `CreatePrinter` bound to `NUL:`, `SetDuplexAndColor` +
  `GetDevmode` round-trip, `SetAdvancedPrintingFeatures` - all succeeded. Cleaned up afterward
  (test printer deleted, driver package removed via `pnputil /delete-driver`, confirmed gone via
  `Get-Printer`/`Get-PrinterPort`/`pnputil /enum-drivers`) and confirmed nothing was left behind.

## 2026-09-06 - Manufacturer sort order, Lexmark support (metadata only)

### Added
- **Settings > General > Manufacturer sort order**: a plain HTML5 drag-and-drop list (no library)
  controlling the Manufacturer dropdown's order in both the Defaults panel and every grid row.
  `Settings.ManufacturerOrder` persists it; `reconcileManufacturerOrder` (`settings.go`) keeps it a
  valid permutation of `driver.Manufacturers` across saves - preserving the user's own ordering,
  dropping stale names, appending any manufacturer not yet ordered (e.g. one just added to the code)
  at the end rather than letting it silently disappear. `App.Manufacturers()` applies this order
  (`applyManufacturerOrder`); `App.AllManufacturers()` - Settings > External Sites - is deliberately
  exempt and always alphabetical instead.
- **Lexmark** added to `driver.Manufacturers` with a default-driver token rule (`Universal`, `v2` ->
  "Lexmark Universal v2") and a default update-check URL - the catalog-matching/metadata side is fully
  wired up like every other manufacturer. The driver package is a self-extracting RAR archive (not
  zip/7z - can't be auto-extracted the way `.zip` packages are); its `.msi`-embedded files need an MSI
  administrative install (`msiexec /a ... /qn TARGETDIR=...`) plus `expand.exe` to become real,
  correctly-named files `BuildCatalog` can parse.
  **Update, same day**: this entry originally reported actual deployment as a known-broken limitation
  ("the style of the INF is different than what was requested") - that finding was wrong, caused by
  two mistakes on my end (a session that had silently lost admin elevation, and a wrong guess at which
  real filename each compressed sibling file decompresses to), not a real Lexmark incompatibility. See
  the later "Lexmark actually works" entry above for the correction and the "Lexmark:
  self-extracting RAR + `.msi`-packaged drivers" section in the README for the corrected procedure -
  deployment is fully verified working through PDT's existing, unmodified install code.
- Considered and deliberately **excluded Brother**: no universal print driver compatible with most of
  its larger models, and its lineup targets home/small-office rather than fleet deployment.

### Verified
- New tests: `TestApplyManufacturerOrder`, `TestApplyManufacturerOrder_UnlistedItemAppendedAtEnd`,
  `TestReconcileManufacturerOrder_Nil`, `TestReconcileManufacturerOrder_PreservesCustomOrder`,
  `TestReconcileManufacturerOrder_DropsUnknownAndDuplicates` (first tests for the `main` package - it
  had none before), plus `TestDefaultDriverNameFor`'s Lexmark case. Full suite (`go build`/`vet`/`test`,
  `wails build`) clean.
- Screenshot/UI-Automation-confirmed live: with a custom order saved directly to `settings.json`
  (`["Sharp","Ricoh","Kyocera","HP","Canon"]`), the Defaults panel's Manufacturer dropdown correctly
  defaulted to Sharp (with Sharp's own default driver auto-populated), while Settings > External Sites
  still listed all manufacturers alphabetically regardless.
- The Lexmark investigation itself was hands-on against the real downloaded package on this machine,
  not guessed: confirmed the `Rar!` signature by scanning the raw `.exe`'s bytes; confirmed
  `msiexec /a` produces a correctly-named `.inf` and that `driver.DriverNamesFromInf` parses it
  correctly (extracting "Lexmark Universal v2"); confirmed the install failure independently through
  both `pnputil /add-driver` and PDT's own code; checked `setupapi.dev.log` and the `DevicePath`
  registry value to rule out stale driver-store state as the cause; confirmed no partial driver
  registration was left behind afterward (`pnputil /enum-drivers`).

## 2026-09-06 - Toshiba/Xerox/Konica Minolta support; External Sites reverted to plain fields

### Changed
- **External Sites reverted** to yesterday's plain always-visible text field per manufacturer (the
  link-plus-"Change"-modal redesign is gone) - the fixed-size modal made the original design's only
  real problem (a raw URL forcing the window wider) moot, and the simpler always-visible field was
  preferred once that was no longer a tradeoff. `.modal`'s fixed width is kept from that redesign.
- `.tab-panel`'s sizing changed from a hand-tuned `min-height` (needing re-tuning every time External
  Sites' manufacturer count changed - which it immediately did, twice, this same session) to a fixed
  `height` instead: every tab now reserves exactly the same space regardless of its own content,
  permanently, with `overflow-y: auto` for whichever tab has more content than that (now routinely
  External Sites, with 8 manufacturers) rather than ever growing the modal.

### Clarified (no code change)
- The Settings modal stays above the main window and moves with it because it's rendered as an
  in-page overlay (`.modal-backdrop`) inside the same native OS window as everything else - not a
  separate window at all - so there's no separate z-order or position to fall out of sync in the
  first place. Worth keeping in mind for any future change that touches window handles
  (`titlebar_windows.go`'s `findMainWindow`, etc.): this behavior depends on Settings never becoming
  an actual second OS window.

### Added
- **Toshiba, Xerox, and Konica Minolta** added to `driver.Manufacturers`, with default-driver token
  rules (`internal/driver/default.go`) and default update-check URLs (`settings.go`) - see the
  README's "Drivers folder layout" for the full per-manufacturer table.
- **Manufacturers without local drivers no longer appear as deployment options.** `App.Manufacturers()`
  now filters to `driver.ManufacturersWithDrivers(catalog)` (only manufacturers with at least one
  usable driver actually present); a new `App.AllManufacturers()` returns the full, unfiltered list
  for Settings > External Sites specifically, so a URL can still be configured for a manufacturer
  before its drivers are ever added.
- Generalized the "prefer the branded/versioned name over a generic alias" behavior (previously
  hardcoded to Ricoh only - `isUsableDriverName`) into a small per-manufacturer brand-token table
  (`vagueNameFilterBrand`) covering Ricoh, Xerox, and Konica Minolta.
- `DefaultDriverNameFor`'s tie-break, for when more than one catalog name matches a manufacturer's
  token rule, now prefers (in order): the newest INF-declared date, then - on an exact date tie, i.e.
  genuinely the same underlying driver registered under two names - whichever name actually shows a
  version number, then alphabetical only as a final deterministic fallback. Previously this was a
  bare alphabetical tie-break, which happened to work by accident for the original 5 manufacturers
  (each only ever had one name matching their tokens) but picked the *wrong* one once Konica Minolta
  was added: `"KONICA MINOLTA Universal PCL"` sorts before `"...Universal PCL v3.9.13"` since it's a
  literal string prefix of it, so the plain, non-versioned name was winning.
- `foldMatchIgnoringSpaces`: manufacturer folder names now match `driver.Manufacturers` ignoring
  spaces as well as case - found necessary immediately against the real Drivers folder, where
  "Konica Minolta" (this app's display name) sits on disk as `KonicaMinolta`, no space at all.

### Verified
- All three manufacturers tested against the **real, already-populated** local Drivers folder (not
  just synthetic fixtures) via a throwaway catalog-dump script: Toshiba resolves to
  `TOSHIBA Universal Printer 2`, Xerox to `Xerox GPD PCL6 V5.1076.4.0` (correctly preferring it over
  the also-present, unversioned `Xerox Global Print Driver PCL6`), and Konica Minolta - once the
  space-folding fix was in - to `KONICA MINOLTA Universal PCL v3.9.13` (correctly preferring it over
  the plain `KONICA MINOLTA Universal PCL`, an exact-date-tie case the real package actually has).
- New tests: `TestDefaultDriverNameFor_PrefersVersionedNameOnDateTie`,
  `TestBuildCatalog_ManufacturerFolderNameIgnoresSpaces`,
  `TestXeroxGenericAliasIsSelectableButNotDefault`, plus new `Toshiba`/`Xerox`/`KonicaMinolta` (no
  space, deliberately) fixtures under `internal/driver/testdata/`. Full suite (`go build`/`vet`/`test`,
  `wails build`) clean.
- Screenshot/UI-Automation-confirmed live: the Defaults panel's Manufacturer dropdown lists only
  Canon/HP/Kyocera/Ricoh/Sharp/Toshiba/Xerox on this machine (Konica Minolta correctly excluded before
  the space-fold fix, correctly included after); Settings > External Sites lists all 8 with the three
  new URLs pre-filled correctly, confirmed by reading each field's value directly via UI Automation.

## 2026-09-06 - Settings modal no longer resizes when switching tabs; External Sites redesigned

### Fixed
- **Real bug, reported from live use**: switching tabs in the Settings modal visibly resized the whole
  modal on every click - both dimensions, not just vertically as first fixed (see below). Two separate
  root causes:
  - *Vertical*: `.tab-panel` had a `max-height` cap but no lower bound, and the modal has no fixed
    size of its own - it sizes to whichever tab's content is showing. Fixed with a shared
    `.tab-panel` `min-height` (tuned to whichever tab is now tallest - see the External Sites redesign
    below, which changed that).
  - *Horizontal*: `.modal` had a `min-width` but likewise no upper bound, so External Sites' one long
    instructional sentence (a `<p>`, which by ordinary block shrink-to-fit sizing contributes its full
    unwrapped width when nothing constrains its ancestors) stretched the modal to roughly double the
    other tabs' width. Fixed by giving `.modal` a genuine fixed `width` instead of a `min-width`.
  - Confirmed via screenshot: the modal box is now pixel-identical (same edges, same Cancel/Save
    button position) across all three tabs.

### Changed
- **Settings > External Sites redesigned**: each manufacturer is now a clickable link (opens its
  configured URL via the same `OpenManufacturerURL` call Defaults' own "Check for Updates" button
  already used) plus a **Change** button that opens a small "Change URL" modal, instead of an
  always-visible text field showing the raw URL. Saving in that modal persists immediately (its own
  `SaveSettings` call) rather than staying pending for the outer Settings modal's own Save button -
  simplest way to keep the manufacturer link always accurate right after a change, with no separate
  "unsaved edit" state to track just for this one field. This was also what made the horizontal-resize
  bug possible to fully fix rather than just contain: a raw URL string in a fixed-width box either
  clips or forces the modal wider (whack-a-mole either way), and neither problem exists once the URL
  itself isn't shown inline at all.

### Verified
- `go build`/`go vet`/`go test` and `wails build` all clean.
- Screenshot-confirmed: identical modal size across all three tabs; the Change URL modal opens
  pre-filled with the current value, and a save round-trips correctly (confirmed directly by reading
  `%AppData%\PDT\settings.json` before and after, then restoring the real URL afterward).
- Caught and fixed a markup issue while testing the Change URL modal: writing `URL for
  <span id="siteUrlMfgName">` as two adjacent inline pieces directly inside a `flex-direction: column`
  label put them in *separate* flex items (one per contiguous inline run, per the flex layout spec),
  rendering "URL for" and the manufacturer name on two separate lines instead of one. Fixed by wrapping
  both in a single containing `<span>` so they're one flex item.
- Also caught mid-verification (unrelated to the app itself): a Bash heredoc used to restore
  `settings.json` after a manual test collapsed its `\\` path escapes to single backslashes,
  producing invalid JSON ("\U", "\K", "\D" aren't legal JSON escapes). Caught by reading the file back
  before moving on, rather than assuming the write matched what was typed; fixed by rewriting it with
  a tool that doesn't round-trip through shell string interpretation, then re-verified the app starts
  clean and reads it back correctly.

## 2026-09-06 - Check for Updates / self-update, repo made public

### Added
- **Settings > About > Check for Updates**: queries this project's GitHub Releases
  (`internal/update.FetchLatest`, `GET /repos/keteague/PDT/releases/latest`) and compares the
  release's tag against `AppVersion` (`driver.CompareVersions` - already a generic dot-separated
  numeric comparator despite living in the driver package, reused here rather than duplicated).
  Reports one of: an error (most commonly "no releases have been published yet", since none exist
  yet - confirmed live against the real, now-public repo), "you are running the latest version", or a
  newer version available with an **Update Now** button.
- **Update Now** (`App.ApplyUpdate`): downloads the newer release's `PDT.exe` asset and installs it in
  place of the running executable, then relaunches it and quits - see "Self-update" below for how that
  works with no separate installer.
- `internal/update`: the above as a standalone, unit-tested package (`FetchLatest`, `Download`,
  `Apply`, `CleanupOldExe`) independent of Wails/`app.go` plumbing.

### Changed
- The GitHub repo (`keteague/PDT`) is now public - confirmed nothing sensitive is actually tracked in
  it first: the only driver-related files under git are small `.INF` text fixtures used by unit tests
  (largest ~320KB), never the real `Drivers/` package tree (which was never committed to begin with -
  it's resolved next to the running exe at runtime, per "Drivers folder layout" in the README).

### Verified
- `go build`/`go vet`/`go test` (including new `internal/update` tests against an `httptest` server)
  and `wails build` all clean.
- **The core mechanism this whole feature depends on** - replacing a *running* Windows .exe's own
  file with no separate installer or helper process - confirmed directly against a real running copy
  of `PDT.exe` before writing any of the surrounding code: `Rename-Item` on the running exe's file
  succeeded while it kept running, a new file could then be written back at the original path, and the
  running (now-renamed-underneath-it) process was confirmed still alive and fully functional
  throughout. This is the Windows behavior `internal/update.Apply` relies on (rename the running exe
  aside, move the downloaded one into its place).
- Launching a `requireAdministrator`-manifested child process (`exec.Command(exePath).Start()`, used
  to relaunch after an update) from an already-elevated parent was separately confirmed not to trigger
  a second UAC prompt, incidentally, while testing the above (`Start-Process` launched `PDT.exe`
  immediately with no `-Verb RunAs` and no prompt/delay from an already-elevated shell).
- Live end-to-end through the real UI (Settings > About > Check for Updates) against the real,
  now-public GitHub API: correctly reported "no releases have been published yet" (accurate - none
  exist yet). The full download-and-apply path is exercised by `internal/update`'s own tests against a
  local `httptest` server; the live "Update Now" path itself needs an actual GitHub release to test
  against, which doesn't exist yet.

### Release process (new requirement for Check for Updates to find anything)
- Bump `AppVersion` (`version.go`) and `wails.json`'s `info.productVersion` together, `wails build`,
  then create a GitHub Release tagged `v<AppVersion>` (e.g. `v0.1.1`) with `build/bin/PDT.exe` uploaded
  as a release asset named exactly `PDT.exe` - `CheckForUpdate` looks for that exact asset name.

### Self-update mechanism (no installer)
`ApplyUpdate` downloads the new `PDT.exe` next to the running one, then calls `internal/update.Apply`:
rename the running exe to `PDT.exe.old`, move the downloaded file to the original `PDT.exe` name, then
relaunch it and quit. This works because Windows only needs a running executable's *name* freed to
place a new file there - it doesn't need the file itself closed, and the running process keeps
executing unaffected from its now-renamed-away file until it exits on its own a moment later. The
`.old` file is cleaned up the next time the app starts (`update.CleanupOldExe`, called from
`startup()`), once whatever process left it behind is guaranteed to have already exited. No installer,
no separate updater binary, and no extra UAC prompt (the relaunch inherits this already-elevated
process's token) - deliberately simpler than adding an NSIS/Inno installer pipeline, which would only
be worth it if this app ever needed Start Menu shortcuts or an uninstaller entry, neither of which it
does today (a single portable exe next to a `Drivers/` folder).

## 2026-09-06 - External Sites tab spacing, About tab verification

### Fixed
- **Real bug, reported from live use**: in Settings > External Sites, each manufacturer's field had
  its own internal 6px label-to-input gap (`.modal-field`'s own `gap`), but `#settingsSitesPanel`
  itself was a plain `<div>` with no gap between the `.modal-field` labels/entries - so one entry's
  input sat flush against the next entry's label, reading as though that label belonged to the field
  above it. Fixed by giving `#settingsSitesPanel` its own `display: flex; flex-direction: column; gap:
  14px` - wider than the 6px internal gap, so each entry still reads as its own group. Confirmed via
  screenshot after rebuilding: consistent spacing across all five manufacturer entries.

### Verified
- Found a reliable way to drive the Settings modal after two earlier automation attempts (coordinate
  clicks + Tab navigation) landed on the wrong control: WebView2 doesn't populate its UI Automation
  tree with named/typed elements until something first requests it, and the gear button's accessible
  name is its glyph (`⚙`), not "Settings" - locating and invoking elements by their actual accessible
  name via `System.Windows.Automation`'s `InvokePattern`, rather than simulated clicks/keystrokes at
  believed coordinates, opened the modal and switched tabs reliably.
- Using that method, the previously-unconfirmed **Settings > About** tab is now screenshot-confirmed
  correct: name, version, author, and the GitHub link all render as expected.

## 2026-09-05 - App icon, titlebar name/version, Settings > About

### Added
- `version.go`: `AppVersion`/`appDisplayName`/`appAuthor`/`appRepoURL` constants - `AppVersion` must be
  kept manually in sync with `wails.json`'s `info.productVersion` (Go can't read that value back out
  at runtime; it only feeds the compiled exe's Win32 version resource and the manifest template).
  `main.go`'s window `Title` is now `fmt.Sprintf("%s v%s", appDisplayName, AppVersion)` -
  "Printer Deployment Tool v0.1.0" - instead of the bare "PDT".
- `cmd/geniconassets`: a permanent repo utility (not a one-off script) that procedurally draws the
  app's printer-glyph icon and writes `build/appicon.png` (1024x1024, the Linux/frontend-facing icon)
  and `build/windows/icon.ico` (16/24/32/48/64/128/256, PNG-compressed frames per the modern
  Vista+ ICO format - no external image asset or design tool involved). Renders at 4x supersample and
  box-filter-downsamples to each target size (averaged in premultiplied-alpha space); an
  un-anti-aliased first pass looked jagged and illegible at 16px, visually confirmed fixed by
  comparing upscaled previews of both versions before finalizing. Re-run manually
  (`go run ./cmd/geniconassets`) if the icon design ever needs to change - it's not part of the normal
  build.
- **Settings > About** tab: version, author, and a clickable GitHub link (`GetAppInfo`,
  `OpenRepoURL` -> `runtime.BrowserOpenURL`), sourced from the same `version.go` constants as the
  titlebar.

### Verified
- Titlebar text confirmed via screenshot (`PrintWindow`) showing "Printer Deployment Tool v0.1.0"
  alongside the new icon.
- The compiled `PDT.exe`'s Explorer-associated icon (`[System.Drawing.Icon]::ExtractAssociatedIcon()`)
  confirmed screenshot-matching the new design.
- `go build`/`go vet`/`go test` and `wails build` all clean.

### Not verified at the time (see the 2026-09-06 entry above)
- The Settings > About tab's actual rendered content wasn't confirmed interactively this round -
  simulated keyboard navigation to reach the Settings gear button landed on an unrelated field
  instead. Screenshot-confirmed correct the next day, once UI Automation's `InvokePattern` replaced
  coordinate-based clicks/keystrokes as the way to drive the Settings modal.

## 2026-09-05 - Yellow titlebar while deploying

### Added
- `titlebar_windows.go`: the titlebar turns yellow for the duration of a `Deploy` run and always
  resets to the OS default when done, including on an unexpected error (`defer`) - ports
  `Create-Printers.ps1`'s own `DwmHelper` (`DwmSetWindowAttribute`/`DWMWA_CAPTION_COLOR`, Windows 11
  22000+ only; a harmless no-op on older Windows, same as that original tool's note). The one new
  piece: Wails doesn't expose the native window handle through its public runtime API the way the
  original tool already had `$form.Handle` in hand, so `findMainWindow` locates it the same way an
  external tool would - `EnumWindows` filtered to this process's own PID, then to the one visible
  top-level window (a real process was confirmed to also have several invisible helper windows -
  `GDI+ Window`, `MSCTFIME UI`, two `Default IME` - that the visibility check correctly excludes).
  Found once and cached, since the handle never changes for the life of the process.

### Verified
- The `findMainWindow` matching logic (PID filter + visibility check) replicated directly in
  PowerShell against a real running `PDT.exe`: exactly one visible window matched, titled "PDT", with
  the four invisible helper windows correctly excluded.
- `DwmSetWindowAttribute` called directly against that same real window handle - confirmed
  screenshot-visible yellow titlebar, then confirmed reset back to the default color - the exact
  sequence `Deploy` now runs automatically.

## 2026-09-05 - Request elevation on launch

### Added
- `build/windows/wails.exe.manifest`: `<trustInfo>` / `requestedExecutionLevel level="requireAdministrator"`.
  Every real operation this app performs (all printer/port work) already required Administrator, so
  it now requests elevation outright on every launch rather than starting unelevated and failing
  partway through a deploy - the same reasoning as `Create-Printers.ps1`'s own auto-elevation, but
  far simpler for a compiled exe: this is purely a manifest entry, Windows shows the UAC prompt
  automatically, with none of the re-exec-itself-as-admin trick a `.ps1` (which can't embed a
  manifest) needed. Unlike that original tool, there's no unelevated fallback mode - declining UAC
  means the process never starts at all, which is fine here since there's no genuinely useful
  degraded mode to fall back to. Confirmed embedded correctly by inspecting the compiled `PDT.exe`'s
  resources directly for the `requireAdministrator` string after rebuilding (an actual UAC prompt
  can't be produced/observed from an already-elevated session, since Windows only prompts when
  crossing from unelevated to elevated).

## 2026-09-05 - Print Defaults/Preferences fix, "Print spooled documents first", Save Configuration filename

### Fixed
- **Real bug, found from a live deployment report**: Canon UFR II printers deployed with Monochrome +
  1-sided checked were showing "Color (Auto)" and "Duplex, long edge" in their own Properties dialog.
  Root cause, confirmed against the real printer via three independent checks
  (`Get-PrintConfiguration`, .NET `System.Drawing.Printing.PrinterSettings`, and re-running
  `Set-PrintConfiguration` directly): raw DEVMODE (`SetDuplexAndColor`, this rewrite's only
  duplex/color mechanism until now) was already being set *correctly* - confirmed via
  `PrinterSettings`, the same mechanism the deploy sequence's own verification step uses - but a
  Canon UFR II printer's own Properties dialog and `Get-PrintConfiguration` read from a *separate*
  PrintTicket-based configuration store that DEVMODE alone never touches, and it was staying stale at
  whatever the driver's own install-time default was. Fixed by additionally calling
  `Set-PrintConfiguration` (`internal/printer/windows/printconfig_windows.go`,
  `SetPrintConfigurationViaShell`, shelling out to `powershell.exe`) after the existing DEVMODE step -
  confirmed by that same three-way check all agreeing afterward. Best-effort/`[WARN]`-only, same as
  the DEVMODE step it supplements, since DEVMODE alone already governs actual print behavior for most
  consumers.
- `PRINTER_ATTRIBUTE_DO_COMPLETE_FIRST` ("Print spooled documents first" on the Advanced tab) was
  researched and defined (`structs_windows.go`) during Phase 2 but never actually wired into
  anything - no deployed printer ever had it enabled. Added `SetPrintSpooledDocumentsFirst`
  (`apf_windows.go`, same `PRINTER_INFO_2.Attributes` mechanism as APF) and call it unconditionally
  (`enable: true`) for every row - this isn't a per-row checkbox like APF, just a fixed default every
  deployment should get. Verified via `Get-WmiObject Win32_Printer`'s `Attributes` bitmask against a
  real deployed printer.
- Save Configuration's suggested filename was always the generic `printers.json`, ignoring
  SalesChain ID entirely. `App.SaveConfiguration` now defaults to `<SalesChainID>.json` when
  SalesChain ID is set (already restricted to filesystem-safe characters by the frontend's own input
  validation), falling back to `printers.json` only when it's blank.

## 2026-09-05 - SalesChain ID rejection feedback (flash + beep)

### Added
- SalesChain ID now flashes red and plays a short synthesized beep on any keystroke that actually
  gets rejected - a disallowed character stripped, or a reserved device name (`PRN`, `COM1`, ...)
  reverted - never for an ordinary edit, since re-sanitizing an already-valid string is always a
  no-op. Detected by comparing the field's raw value against what `setSalesChainId` actually applied;
  they only ever differ when this keystroke was rejected. The beep is a couple of Web Audio nodes
  torn down right after (`playInvalidDing`), not an embedded audio asset, and fails silently if audio
  is unavailable rather than blocking the rejection feedback on it.

## 2026-09-05 - Check for Updates, Settings > External Sites, zip-packaged drivers

### Added
- **Check for Updates** button in the Defaults panel: opens the selected manufacturer's configured
  driver-download page in the system browser (`OpenManufacturerURL` -> `runtime.BrowserOpenURL`).
  Deliberately not automated version-checking - no vendor exposes an API for that, and scraping five
  different download portals individually would be fragile and high-maintenance; this just saves
  hunting down the URL each time.
- **Settings > External Sites** tab: one editable URL per manufacturer (seeded with the pages
  provided - Canon, HP, Kyocera, Ricoh, Sharp - via `defaultManufacturerURLs` in `settings.go`),
  persisted alongside Save File Base Path in the same `settings.json`. Each manufacturer's URL falls
  back to its own default independently if left blank, both on load and on save.
- `internal/driver/zip.go`: `.zip`-packaged driver downloads are now extracted automatically before
  each manufacturer folder is scanned - confirmed necessary against a real package (Sharp's UD3
  driver ships as `UD3_07_PCL6_2510a.zip`, previously invisible to `BuildCatalog` since it only ever
  looked for `.inf` files already sitting on disk). Extracts `Foo.zip` to a sibling `Foo/` folder
  once; leaves it alone (no re-extraction) if that folder already exists, however it got there.
  Zip-slip protected (rejects any entry that would extract outside the destination folder); a
  corrupt/unreadable zip is skipped (with its partial output cleaned up so a later run retries)
  rather than failing the whole catalog scan.

### Fixed
- Found while testing the above (the extra zip-scanning I/O made it consistently reproducible,
  though the underlying bug already existed): `App`'s catalog-dependent methods
  (`DefaultDriverFor`, `Models`, `DriverCandidates`, `GetCatalogStatus`, `Deploy`, `GetSettings`,
  `SaveSettings`, `OpenManufacturerURL`, `OpenConfiguration`, `SaveConfiguration`) could run before
  `startup()` finished populating `catalog`/`modelIndex`/`settings` - Wails does not block the
  frontend's own script from running until `OnStartup` returns, so a `BuildCatalog` scan slow enough
  to still be running when the frontend fires its first call (e.g. `DefaultDriverFor` on page load)
  would silently see nil/zero-value state and return an empty result with no error. Fixed with a
  `ready` channel every such method now blocks on (`<-a.ready`) before proceeding, closed once
  `startup()` completes - confirmed by reproducing the empty-Driver-field symptom, then confirming
  it's gone after the fix, both against the real Drivers folder.

### Verified
- `DefaultDriverNameFor` re-confirmed correct against the real catalog in isolation (ruling it out as
  the cause once the startup-race symptom appeared) before diagnosing and fixing the real bug above.
- New tests: `TestBuildCatalog_ExtractsAndScansZippedDriverPackage` and
  `TestBuildCatalog_DoesNotReExtractExistingFolder` (zip fixture built on the fly with `archive/zip`
  rather than committing a binary `.zip` to the repo).
- Screenshot-confirmed (`PrintWindow`) the Defaults panel's new button placement and that the
  Settings modal stays correctly hidden on load - proactively checked the new `.tab-panel` CSS for
  the same `display` vs. `[hidden]` specificity conflict already found and fixed on `.modal-backdrop`
  earlier, and guarded it the same way (`:not([hidden])`) before it could ship broken.

## 2026-09-05 - Real combobox for Model/Driver, Drivers/Windows/<version> layout support

### Added
- `setupCombobox()` (`main.js`): a small, self-contained dropdown component replacing the native
  `<input list=...><datalist>` attempt - shows every current candidate on focus, filters live as you
  type, click/Enter/arrow-keys to select. Wired up for the Defaults panel's Model/Driver fields and
  every grid row's Model/Driver fields.
- `internal/driver.BuildCatalog` now scans `driversRoot/Windows/<any version folder>/<Manufacturer>/...`
  (merging every version folder found) instead of assuming manufacturer folders sit directly under
  `driversRoot` - matching the real Drivers folder as it's being reorganized (Windows and macOS sides
  broken out, `Drivers/Windows/11/<Manufacturer>/...` for the Windows side). Falls back to the old
  flat layout when there's no `Windows` subfolder at all, so existing `internal/driver/testdata`
  fixtures and any pre-reorg `Drivers` folder keep working unmodified.
- `TestBuildCatalog_WindowsVersionNestedLayout`: new test against a dedicated nested fixture
  (`testdata_windows_layout/Windows/11/Canon/...`, a copy of the existing flat Canon fixture one
  level deeper) proving the new layout is actually found and scanned correctly.

### Changed
- Port name prefix's text field now shows "IP_" as a real placeholder (grayed hint, gone once you
  type) instead of a pre-filled default value - and, as before, stays disabled until the checkbox is
  checked.
- The Defaults panel's Driver field now expands to fill the remaining width of its row (was a fixed
  `size="40"`), via `flex: 1` on its wrapping label/combo rather than a fixed character count.

### Not done (explicitly out of scope for this round)
- The macOS side of the Drivers folder reorg (`Drivers/macOS/<Manufacturer>/<version>/...`, plus an
  `OpenPrinting` PPD fallback bucket) is not read by `BuildCatalog` at all yet, and there is still no
  macOS `Deployer`. Most of the real macOS packages on disk are `.dmg` images (frequently wrapping a
  nested `.dmg`, ultimately containing a `.pkg` installer) - extracting driver files/PPDs from those
  without a macOS host or a full GUI installer run is a real open question needing its own design
  pass, not something to guess at inside this change. See the README's "Drivers folder layout"
  section.

### Process note
- Mid-verification, an automated screenshot (`CopyFromScreen` at coordinates believed to be PDT's
  window) instead captured an unrelated terminal window's content that had ended up on top at that
  screen position - discarded immediately, not analyzed further. Switched all subsequent screenshots
  to `PrintWindow` (captures a specific window's own content directly, independent of focus/z-order),
  which avoids the failure mode entirely. Further interactive automation (simulated clicks/typing)
  was halted after a mouse click aimed at PDT's title bar landed on a different, unrelated window
  instead - the coordinate math proved unreliable in this multi-window desktop environment, and
  continuing risked interacting with unrelated windows rather than PDT.

## 2026-09-05 - Toolbar reorder, UseExistingPort fallback, DPI/resize robustness

### Changed
- **Open Configuration**/**Save Configuration** moved from the toolbar to the top bar, to the right
  of SalesChain ID. **Add Printer**/**Remove Selected** moved to the far left of the toolbar (ahead
  of New CSV/Import CSV).
- `UseExistingPort` checked with no matching port for the row's IP is no longer a fatal error - it
  now falls back to creating a new port instead (logged `[INFO]`), same as if the checkbox were
  unchecked. The row's real intent ("give me a working port for this IP") is still satisfiable, so
  failing the whole row over it was unnecessarily strict. Updated the matching tooltip, grid header,
  and README wording accordingly.
- Added `MinWidth`/`MinHeight` (700x520) to the main window - otherwise unconstrained and freely
  resizable, this floor just keeps it from being shrunk below a size the flex-based layout has
  actually been verified to hold up at with no clipping or overlapping controls.

### Verified
- Confirmed `build/windows/wails.exe.manifest` already declares `permonitorv2` DPI awareness, which
  WebView2 honors automatically - the window and its content scale correctly per-monitor with no
  changes needed there.
- Resized the running app down to 700x520 (well below the 1024x768 default) and confirmed via
  screenshot that the flex-wrap layout reflows cleanly (the Manufacturer/Model/Driver row wraps, the
  Port subsection's checkboxes wrap to a second line) with no clipped or overlapping controls -
  the practical stand-in for "things need more room" that DPI scaling or a smaller display would
  also produce.
- Re-verified `UseExistingPort`'s new fallback behavior end to end against the real spooler via
  `pdtdebug deployrow ... useexisting` with no matching port present: logs the fallback, creates the
  port, and the row deploys successfully instead of failing.

## 2026-09-05 - Defaults panel restructure, SalesChain ID validation, tooltips

### Changed
- Defaults panel restructured into one outer "Defaults (used by 'Add Printer')" box containing
  Manufacturer/Model/Driver, then three labeled subsections: **Port** (Subnet, Port name prefix +
  its text field, Use existing port, SNMP), **Print Defaults** (Monochrome, 1-sided), **Advanced**
  (Enable APF).
- SalesChain ID now restricts input to letters, digits, hyphen, and underscore as you type (covers
  every DOS/shell-reserved character - `\/:*?"<>|` on Windows, `/` and `:` on macOS - without needing
  to enumerate them), and separately rejects the Windows reserved device names (`CON`, `PRN`, `AUX`,
  `NUL`, `COM0`-`COM9`, `LPT0`-`LPT9`, case-insensitive, whole-string only) by refusing the keystroke
  that would complete one and reverting to whatever was there just before. A loaded configuration's
  SalesChain ID goes through the same check (resetting to empty if the saved value itself is a
  reserved name, since there's no "just before" to revert to for a freshly loaded value).
- Added a `title` tooltip to every interactive control in the app - toolbar buttons, Defaults panel
  fields, grid column headers, and every per-row input - explaining what it does or what it affects.

### Fixed
- Found during manual testing of the first tooltip pass: several tooltip strings contain a literal
  `"` (e.g. `"SalesChain: <id>"`), which - embedded directly into a double-quoted `title="..."`
  attribute - closed the attribute early and leaked the rest of the tooltip text onto the page as
  visible content. Fixed by routing every tooltip through a shared `tip()` helper that HTML-escapes
  it first; verified visually (rebuilt, screenshotted) that the leak is gone.

## 2026-09-05 - Phase 4: Wails frontend

### Added
- `app.go`: the full `App` API the frontend calls - `Manufacturers`/`Models`/`DriverCandidates` for
  the grid's dropdowns, `NewCsvTemplate`/`ImportCsv`/`OpenConfiguration`/`SaveConfiguration` (each
  wrapping a native OS file dialog), and `Deploy` (streams a `"deploy-progress"` event per row in
  addition to returning every result at the end). `confirm` implements `printer.Confirm` via a native
  `runtime.MessageDialog` Yes/No box - the Wails equivalent of the original tool's WinForms
  `MessageBox`. The driver catalog builds once at startup from a `Drivers/` folder resolved next to
  the running executable (falling back to `./Drivers` for `wails dev`).
- `frontend/src/main.js` (+ `app.css`): a plain HTML/CSS/vanilla-JS grid (no framework) replacing the
  Wails vanilla template - top bar, Defaults panel, toolbar, the row grid, and a live timestamped/
  level-tagged log panel, wired to the `App` API above.
- Every multi-signal or error-bearing `App` method returns a small DTO (`PathResult`, `ImportResult`,
  `OpenConfigResult`, `DeployRowResult`) rather than a second/third raw return value or a bare Go
  `error` - confirmed by reading Wails' own binding-dispatch code that it only supports `(T)` or
  `(T, error)` return shapes, and that a bare `error` has no exported fields to JSON-marshal a message
  from at all (it would cross the wire as `{}`).
- `internal/printer/batch.go`: `DeployAllWithProgress` (`DeployAll` now a thin wrapper over it) - runs
  the batch exactly as before but also invokes a callback after each row, for `Deploy` above to stream
  progress from.

### Design notes
- The grid never fully re-renders while a deploy is running: `onDeployProgress` correlates each
  incoming event to its row by submission order (`activeDeploy.nextIndex`) and toggles only that row's
  success/failure CSS class directly via a stable per-row `_id` - not the row's `Name` (never required
  to be unique) and not its position in `state.rows` (shifts as rows are added/removed). A full
  re-render on every progress event - a real bug caught before shipping - would otherwise destroy
  focus and in-progress edits in any other row while a multi-minute HP deploy is still running
  elsewhere in the grid.

### Verified
- `go build`/`go vet`/`go test` all pass; `npm run build` (Vite) and `wails build` both produce a
  working `PDT.exe`.
- Launched the built app against the real 3.1 GB `Drivers/` package tree (copied next to the exe):
  starts with no catalog-load warning, Manufacturer/Model/Driver dropdowns populate live, Add Printer
  adds a row, and the layout matches the design pass end to end (screenshot-verified via a real
  window capture + simulated clicks, not just "it compiles").

### Changed (layout, found during live manual testing of the first build)
- Added the missing **Subnet** default field (pre-fills a new row's IP when set, adding a trailing
  `.` if the user didn't type one - matches the original tool's own Subnet-default behavior), placed
  in the Defaults panel with **Port name prefix** immediately to its right (moved there from the top
  bar).
- Reordered the Defaults panel's checkboxes so **SNMP** sits immediately left of **Enable APF**.
- The Defaults panel's Manufacturer `<select>` now matches every text field's size/styling (it had no
  explicit sizing before and was noticeably narrower).
- **Monochrome** and **1-sided** are now checked by default in the Defaults panel.

## 2026-09-05 - Phase 3: Deploy orchestration

### Added
- `internal/printer/windows/deploy_windows.go`: `Deployer`, the real `printer.Deployer` implementation
  - the full per-row Deploy sequence (port -> driver -> printer object -> conditional NUL:-to-real-port
  rebind -> print config -> APF) wired on top of every Phase 2 binding.
- `internal/printer/batch.go`: `DeployAll` runs every row in order and always continues past a row
  that failed fatally - a bad printer object is fatal for that row alone, never for the run.
- `internal/printer/log.go`: `Logger` - every deploy log line gets a `2006-01-02 15:04:05` timestamp
  and an `[INFO]`/`[OK]`/`[WARN]`/`[ERR]` tag, per spec.
- Fatal/warning split, per spec: failing to create or update the printer object itself (port, driver
  install, `CreatePrinter`/`SetInfo2`, the NUL:-rebind) is fatal for the row; failing to apply
  duplex/color or advanced printing features afterward is only ever a warning, and both are always
  attempted regardless of whether the other succeeded.
- No `BindNulPort` checkbox (unlike the original tool): typing `NUL`/`NUL:` as a row's IP binds it to
  `NUL:` permanently; any row resolving to HP's Universal Print Driver family automatically gets the
  create-against-`NUL:`-then-rebind treatment with no user toggle (`printer.RequiresNulPortWorkaround`).
- Driver version handling ported from the original tool's fix for the same bug: a plain-name
  selection silently upgrades an older installed driver and never downgrades; an explicit version pin
  (a decorated `<name> (vVersion - date)` catalog label) is honored upgrade-or-downgrade but only
  after a confirmation dialog, since Windows shares one driver registration per name across every
  printer already using it.
- Existing-printer-object handling ported from the original tool: if a printer with the row's exact
  name already exists, its current driver/port/comment are diffed against what the row would set and
  a confirmation dialog lists exactly what would change (or no prompt at all if nothing would).
- `internal/printer/windows/driverinfo_windows.go`: `GetInstalledDriverVersion` - reads an installed
  driver's real date/version straight from
  `HKLM\SYSTEM\CurrentControlSet\Control\Print\Environments\<env>\Drivers\Version-3\<name>`
  (`DriverDate`/`DriverVersion` values). Verified against a real machine to hold the exact same
  version string a vendor `.inf`'s own `DriverVer=` line declares (Canon `3.50.0.0`, Kyocera
  `8.6.1022.0`, HP `61.360.1.26819`) - a cleaner and more reliable source than `Get-PrinterDriver`'s
  CIM-derived `Date`/`DriverVersion` properties, which the original tool found came back unusable
  (garbage version, blank date) no matter how the driver was installed. This sidesteps the original
  tool's own workaround entirely (reading the installed driver's staged `.inf` back off disk via its
  `InfPath` and reparsing `DriverVer=`) - not needed here since the registry values themselves proved
  reliable.
- `internal/printer/windows/portlookup_windows.go`: `FindTcpIpPortByHost` - finds an existing Standard
  TCP/IP port already targeting a host by reading every port's `HostName` value from
  `HKLM\SYSTEM\CurrentControlSet\Control\Print\Monitors\Standard TCP/IP Port\Ports\<name>` - the
  registry-level equivalent of the original tool's `Win32_TCPIPPrinterPort.HostAddress` WMI lookup.
  Used unconditionally (not just when `UseExistingPort` is checked) so a redeploy of the same row
  never creates a genuine duplicate port for a host that already has one - porting forward the
  original tool's own fix for that exact bug.
- `driver.CompareVersions`: exported wrapper around the existing internal numeric version comparison,
  for comparing an installed driver's registry-sourced version against a `ResolvedDriver.Version`
  from outside the `driver` package.
- `cmd/pdtdebug`: `driverversion`, `findport`, and `deployrow` commands to exercise the new pieces
  (and the whole Deploy sequence end to end) against this machine's real spooler and a real local
  driver catalog, auto-confirming every prompt and cleaning up after itself.

### Fixed
- Driver "already installed at the same version" comparison used exact string equality on the version
  field, while the "is it newer" comparison used numeric `driver.CompareVersions` - found during
  end-to-end testing against a real HP driver, where the registry's version string (`61.360.1.26819`)
  and the vendor `.inf`'s own (`61.360.01.26819`) differ only by a leading zero. Both comparisons now
  use `driver.CompareVersions`, so a numerically-identical version is never mistaken for "needs
  updating."

### Verified (against this machine's real print spooler and driver packages)
- Fresh printer creation (Canon) with an auto-created, unprefixed (bare-IP-named) Standard TCP/IP port.
- HP Universal Print Driver: automatic NUL:-first creation followed by a rebind to the real port,
  end to end.
- Plain-name driver auto-upgrade (Kyocera 8.6.1022.0 installed -> 8.7.422.0 resolved, upgraded with no
  prompt) and the known, non-fatal Kyocera color-switchback quirk logged as `[WARN]`.
- Idempotent redeploy of an unchanged row (existing port reused, no confirmation prompt, no changes
  applied) and a changed row (port change correctly diffed, confirmed, and applied).
- `UseExistingPort` checked with no matching port present: fails fatally for that row with a clear
  message, exactly as documented.
- An explicit older-version driver pin (Kyocera v8.7 installed, row pins v8.6): confirmation dialog
  shown, downgrade applied on confirmation - the original tool's own confirmed-and-fixed bug
  ("switching to a different driver doesn't prompt"), now proven on the Go rewrite from the start.

### Known limitations (carried forward from the original tool)
- Kyocera's driver has been observed not to reliably switch back from monochrome to color via
  DEVMODE (confirmed non-transient by retrying); logged as `[WARN]`, deployment still succeeds.
- Confirmed the HP Universal Print Driver's multi-minute creation delay against a live TCP/IP port
  is **not** an artifact of the original tool's PowerShell/PrintManagement cmdlet layer: a direct,
  isolated timing test (`cmd/pdtdebug timedcreate`) calling this rewrite's syscall-based
  `CreatePrinter` straight against a real TCP/IP port, with no NUL: workaround involved at all, still
  took 4m4.6s - essentially identical to the original tool's own ~4-minute observation. The
  automatic NUL:-first-then-rebind workaround (`RequiresNulPortWorkaround`) is therefore still
  necessary and is not a leftover-only-for-the-old-implementation workaround.

## Prior (Phases 1-2, undated - reconstructed from the existing codebase)

- **Phase 1** (`internal/config`, `internal/driver`, `internal/printer` types): CSV/JSON
  import-export (`internal/config`), the multi-version/multi-arch driver catalog and name/version
  resolution (`internal/driver`), and the platform-independent row/request/result types
  (`internal/printer`).
- **Phase 2** (`internal/printer/windows`): hand-written Win32 syscall bindings for printer
  create/read/update, the "Advanced printing features" toggle, `NUL:` port creation, duplex/color via
  DEVMODE, driver installation, and Standard TCP/IP port creation - see the README's "Windows
  bindings" table for the full list and the reasoning behind each one.
