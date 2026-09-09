# Changelog

All notable changes to this project are documented here. This is a from-scratch Go/Wails rewrite of
`Create-Printers.ps1`; entries reference that original tool's own history where a decision or
limitation carries forward from it.

## 2026-09-09 - Field-observed issues (Windows), not yet investigated/fixed

Two issues Ken observed using PDT at a real client site (Windows, on-site deployment) - reported here,
not yet reproduced or root-caused (this entry was written from a macOS session with no Windows/USB
access to verify against), so the Windows-side session picking these up next has the full report
rather than a secondhand summary.

- **On-launch Drivers scan is slow from a USB flash drive, especially over USB 2.0** - observed as a
  series of `C:\Windows\System32\expand.exe` console windows flashing up during startup. `expand.exe`
  is only ever invoked from `ensureMsiExtracted` (`internal/driver/msi.go`, part of the Lexmark
  `.msi`-extraction pipeline - see the README's own "Lexmark" section), which already has a skip-if-
  already-extracted check (`os.Stat(destDir)` before extracting) - so either that check is somehow not
  holding across launches for this case, or (more likely, and worth checking first) `BuildCatalog`'s
  full `filepath.WalkDir` over the whole Drivers tree - which every one of its `ensure*Extracted`
  helpers does, unconditionally, on **every app startup and every Refresh Drivers click**, not just
  once - is itself the slow part on a real, large Drivers folder (tens of thousands of files, per the
  flash-drive-copy-speed work earlier in this changelog) over a slow USB 2.0 link, independent of
  whether any actual extraction ends up happening. On Ken's own nVME-backed dev machine the same scan
  takes ~10 seconds, which he considers acceptable - the concern is specific to slow removable media.
  - **Ken's proposed fix**: only run the on-launch Drivers scan/extraction when running from a local
    install (a technician's laptop), and/or when actually writing to a USB drive (Write to Flash
    Drive/Sync) - never when running *from* the USB drive itself, since by the time a flash drive is
    handed off for field use, everything on it should already be extracted. This fits the intended
    real-world deployment: flash drives get a physical write-protect switch, and the only time one
    should ever be written to is from the tech's own laptop's local install - a USB-run copy has no
    business re-scanning/re-extracting at all under that model.
- **Editing and re-saving a JSON configuration didn't propagate a fix to other endpoints** - Ken loaded
  a saved configuration on one endpoint, noticed a typo in a printer object's name, fixed it, used Save
  Configuration to overwrite the same file, then loaded what he expected to be the corrected file onto
  other endpoints - the typo was still there. `config.SaveConfig`/`LoadConfig`
  (`internal/config/json.go`) are a plain `os.WriteFile`/`os.ReadFile` round-trip with nothing
  suspicious on inspection, so the likely cause is upstream of the actual file write - most likely
  `SaveConfiguration`'s save dialog (`app.go`) defaulting to `configsRoot()` +
  `<SalesChainID>.json` rather than reopening at the exact path the file was originally loaded from,
  which would silently write the fix to a *different* file than the one being distributed to the other
  endpoints if the two ever diverge. Needs reproducing interactively (what path did Open Configuration
  load from vs. what path did Save Configuration's dialog default to/actually write to) before
  concluding that's really it.

## 2026-09-09 (v0.4.0) - macOS support (data layer, darwin Deployer, Wails app, frontend)

The first real macOS build - PDT is no longer Windows-only. Built and verified on a real Mac (macOS
26 "Tahoe", Apple Silicon) against real vendor driver packages (Kyocera, Ricoh, Sharp) and this
machine's own already-deployed CUPS queues, not just unit tests. `internal/printer.Deployer` - already
a clean, platform-independent interface from the original Windows-only design - now has a second real
implementation alongside `internal/printer/windows`.

### Added
- **macOS driver catalog** (`internal/driver/maccatalog.go`, `macmount.go`, `macresolve.go`): scans
  `Drivers/macOS/<Manufacturer>/<any version folder>/*.dmg`/`*.pkg` (mirroring the Windows side's own
  `Drivers/Windows/<version>/<Manufacturer>/` convention, version nested the other way per the
  README's own longstanding note that macOS packages genuinely vary by OS release), plus a flat
  `Drivers/macOS/OpenPrinting/<Manufacturer>/*.ppd` fallback bucket for a manufacturer/model with no
  vendor installer at all. `driver.LocatePkg` resolves a `.dmg` to the real `.pkg` inside it
  (mounting via `hdiutil`, recursing into one level of nested `.dmg` - confirmed necessary against a
  real Kyocera package) with no bundled extraction tool needed (unlike Windows' bundled 7-Zip - macOS
  driver packages need no pre-extraction at all).
  - **No reliable per-package version field exists on macOS**, unlike a Windows `.inf`'s `DriverVer=`
    line - confirmed against a real Kyocera distribution-style package, where every component's own
    declared "version" was boilerplate `1.0`/`0`. `ResolveMac` picks the newest package by file
    modification time instead of trying to compare a version string; `PackageLabel` is a display-only
    best-effort label (a flat `.pkg`'s real `PackageInfo` version when there is one, else the
    filename), never used for ordering.
- **darwin `Deployer`** (`internal/printer/darwin`): installs a resolved `.dmg`/`.pkg` via macOS's own
  `installer` tool (elevated - see below), diffs `/Library/Printers/PPDs/Contents/Resources` before
  and after to discover which PPD(s) the install actually registered (there's no Windows-registry-like
  "installed driver version" to read directly), and creates/reuses a CUPS **LPD** print queue
  (`lpadmin`/`lpstat`) - `lpd://<ip>/`, no queue name, confirmed against this machine's own
  already-deployed real queues to be exactly the working convention already in use here. Best-effort
  duplex/color defaults are set by reading each queue's actual PPD-declared option keywords/choices
  (`lpoptions -l`) rather than hardcoding one vendor's naming - confirmed against a real installed
  Kyocera PPD (`Duplex`/`None`/`DuplexTumble`/`DuplexNoTumble`, `ColorModel`/`CMYK`/`Gray`) that PPD
  option naming is inconsistent enough across vendors that this has to be dynamic, the same lesson
  `devmode_windows.go` already learned the hard way for DEVMODE on Windows. No NUL:-port workaround and
  no APF/"print spooled documents first" - both Windows spooler-specific concepts with nothing
  analogous in CUPS (see `deploy_darwin.go`'s own doc comment for why).
- **Elevation**: every privileged command (`installer`, `lpadmin`) runs through `osascript`'s
  `do shell script ... with administrator privileges` - the closest available equivalent to Windows'
  manifest-driven auto-UAC-elevation without needing a paid code-signing certificate. **Confirmed live
  that a bare, ad-hoc-signed CLI binary gets killed by AMFI** (`AppleMobileFileIntegrityError -423`,
  "adhoc signed or signed by an unknown certificate chain") the moment the privileged command actually
  starts, even after the password prompt is accepted - a real `wails build`-produced `.app` bundle
  (also only ad-hoc signed today, no paid Developer ID) has not yet been confirmed to avoid the same
  rejection; see `elevate_darwin.go`'s own doc comment. If it turns out a real `.app` hits this too,
  the fix is a paid Apple Developer ID signature (and likely notarization) - a real cost/process change
  from this project's current unsigned-installer precedent on the Windows side.
- **`cmd/pdtdebugmac`**: a throwaway CLI mirroring `cmd/pdtdebug`'s own role, for exercising the darwin
  catalog/install/queue-creation codepaths by hand against real state before the Wails UI could.
- **`package main` now actually compiles and runs on darwin** - `app.go`'s Windows-only pieces
  (driver-catalog methods, the self-update mechanism, the Explorer-opening toolbar button) split into
  `app_windows.go`/`drivercatalog_windows.go`/`update_windows.go`/`openfolder_windows.go`, each with a
  matching `_darwin.go` counterpart where one makes sense; `spooler.go`/`devmode.go`/`sevenzip.go`
  renamed outright to `_windows.go` (Print Spooler control, DEVMODE/Device Settings capture, and the
  bundled-7-Zip-for-self-extracting-archives tooling all remain Windows-only for now - no CUPS/macOS
  equivalent built yet). New `App.Platform()` bound method (`runtime.GOOS`) is the frontend's only
  feature-detection signal.
- **Flash Drive / Sync ported to macOS** (`internal/flashdrive/flashdrive_darwin.go`): removable-drive
  enumeration and exFAT formatting via `diskutil` (`RemovableMediaOrExternalDevice` from
  `diskutil info -plist`, converted to JSON via `plutil` for reliable parsing) and `syscall.Statfs` for
  free/total space, in place of Windows' `GetDriveType`/`GetDiskFreeSpaceEx`. Same `flashdrive.Drive`
  shape on both platforms - `Letter` holds a mount point path (e.g. `/Volumes/MYDRIVE`) on macOS
  instead of an actual drive letter, since every caller already treats it as an opaque identifier
  string rather than parsing it.
- **Frontend** (`frontend/src/main.js`): a `state.platform`-gated reduced UI on macOS - Spooler,
  SNMP/port-prefix/Use-existing-port, APF, DEVMODE capture, Import Printers, app self-update, and the
  7-Zip credit/update section are all hidden (Windows-only concepts, per above); the Driver combobox
  stays (now backed by `App.DriverCandidates`/`DefaultDriverFor`'s own darwin implementation - the
  resolved package's own label when one exists locally, or fuzzy-ranked OpenPrinting PPD labels
  otherwise). Switched from named imports of the generated Wails bindings to a namespace import
  (`import * as App from ...`) - confirmed live that Vite/Rollup hard-fails the production build on a
  named import of a bound method that doesn't exist in that platform's generated `App.js` (a fair
  number of `App`'s own methods only exist on one platform now), where a namespace import's member
  access degrades to `undefined` at runtime instead, caught by the same `isMac()` guards already
  needed at every one of those call sites.
- User-visible "SalesChain ID" wording changed to "Save ID" throughout (tooltips, log/status messages,
  confirmation dialogs) - internal identifiers and the saved-JSON-config field name are unchanged, so
  this doesn't affect config file compatibility.

### Fixed
- Two pre-existing test gaps, invisible until `package main` could actually compile/run on darwin for
  the first time this pass: `resolveexerelative_test.go` hardcoded Windows-only path syntax
  (`C:\Users\...`, `.\Drivers`) for a function that's genuinely cross-platform; and
  `TestResolve_StaleVersionFallsBackToNewest` (`internal/driver`) was missing the same amd64-only
  `t.Skip` its two sibling tests already had, failing on Apple Silicon since the real-vendor-INF test
  fixtures it uses have no arm64 build for that particular old Kyocera model.

### Known issues / open for the next pass
- **Settings > General's Browse (`...`) buttons don't open a folder picker on macOS** - not yet
  root-caused; `PickFolder`/`runtime.OpenDirectoryDialog` is nominally cross-platform Wails runtime
  code, unmodified here.
- **Settings > General's base path fields display Windows-style `\`-separated paths on macOS** -
  `defaultDriversBasePath`/`defaultSaveFileBasePath`/`defaultPreinstallBasePath` (`settings.go`) still
  need a macOS-appropriate default location decided and implemented (the Windows side defaults to
  `%LocalAppData%\PDT\Drivers` etc.); until then, paths display with the wrong separator and may not
  point anywhere sensible on macOS.
- No macOS equivalent of `ensureDriversScaffold` yet (auto-creating the `Drivers/macOS/<Manufacturer>/`
  tree on first launch) - a freshly-installed macOS copy's Drivers folder isn't scaffolded the way an
  installed Windows copy's is.
- A minor Defaults-panel spacing/padding issue above the Manufacturer row, reported after the fixes
  above landed - not yet reproduced/fixed.
- `PrinterRow` has no explicit LPD-queue-name field - every macOS deploy uses `lpd://<ip>/` (no queue
  name), confirmed to match this machine's own real, already-deployed queues, but not yet exposed as a
  per-row override if a future printer's LPD server needs a specific queue name.

## 2026-09-07 (v0.3.9) - Toolbar Sync button; fix and speed up flash drive copying

### Added
- **Toolbar Sync button** (the two opposing horizontal arrows, between Refresh and the Drivers-folder
  button) copies this computer's Drivers folder onto a flash drive that already has a portable PDT
  copy on it, then extracts anything newly-copied right there - for topping up a flash drive with
  driver packages downloaded since it was last written, without rewriting the exe/Configs/tools or
  waiting to plug it into another computer first just to trigger extraction. Available whether PDT
  itself is running from a local install or from a flash drive already (unlike Write to Flash Drive,
  which is disabled in the latter case) - syncing one portable copy's Drivers onto a *different* flash
  drive is a legitimate workflow, guarded against picking the exact same drive PDT is currently running
  from (`syncDriversTo`'s own `samePath` check - copying a tree onto itself would truncate a source
  file while still reading it).
- **Write to Flash Drive now also copies this computer's 7-Zip tools folder** onto the destination, so
  a portable copy is fully self-contained and needs nothing else to work on another computer.
- **Copy-progress dialog** for both Write to Flash Drive and Sync - a real Drivers folder can be tens
  of thousands of files and take several minutes over a real USB port with otherwise zero indication
  it hadn't just hung (confirmed live). Shows which step (Drivers/Configs/7-Zip tools) is currently
  copying and a live "N / total files" progress bar per drive, via a new `flashcopy-progress` Wails
  event emitted throughout the copy (throttled to at most once per 150ms so tens of thousands of files
  don't flood the frontend with events).

### Fixed
- **Write to Flash Drive silently copied none of the real driver files onto a flash drive that already
  had anything under `Drivers\`** (a prior Write to Flash Drive, or its own scaffolded
  `Archive\README.txt` files) - confirmed live, and root-caused to `os.CopyFS`'s own documented
  behavior: "will not overwrite existing files ... stops at and returns the first error encountered."
  Replaced with `copyTreeMerge`, which merges into an already-populated destination instead of failing
  outright on the first pre-existing path it finds.
- **A second, related bug found while fixing the first one**: `copyTreeMerge`'s own first version still
  aborted the *entire* copy the moment any single file failed for any other reason (`filepath.WalkDir`'s
  default behavior) - confirmed live against a real flash drive, where a file deep in Lexmark's own
  driver package (thousands of files, deeply nested - a plausible Windows `MAX_PATH` issue) failed, and
  every manufacturer sorting after Lexmark (Ricoh, Sharp, Toshiba, Xerox) never got copied at all as a
  result, with nothing to explain why. `copyTreeMerge` is now best-effort per file (one bad file no
  longer stops the rest of the tree), and `writePortablePDTTo`/`syncDriversTo` now attempt every step
  (Drivers, Configs, tools) regardless of whether an earlier one hit a partial failure, instead of
  bailing out of the whole operation on the first error.
- **Repeat Write to Flash Drive/Sync against an already-populated drive was extremely slow** - tens of
  thousands of individual `os.Stat` round-trips to a real USB-attached filesystem, one per source file,
  turned out to be the dominant cost once the two bugs above were fixed and files actually started
  landing. Replaced with a single bulk directory listing of the destination up front
  (`listFileSizes`) - a repeat sync of an already-fully-synced ~23,000-file, ~14GB real Drivers folder
  went from several minutes to about 4 seconds. CRC32/MD5 content hashing was considered for detecting
  changed files and rejected: computing a hash means reading every byte of every file on both sides,
  which costs far more I/O than the size-comparison it would replace, for a correctness guarantee this
  case doesn't need - driver packages are downloaded once and never silently modified in place
  afterward, so a size match is already as good as a hash match here.
- Fixed the Sync modal's "Format as exFAT" row staying visible despite being correctly hidden under the
  hood - another instance of this codebase's recurring `[hidden]`-vs-bare-`display` CSS specificity bug
  (see `.modal-backdrop`'s own comment), this time on `.modal-field-inline`; also switched that one
  element to an inline `style.display` toggle instead, since the row still rendered even with the
  compiled bundle's own logic and the CSS fix both verified correct byte-for-byte - moot with the
  belt-and-suspenders fix in place either way.

## 2026-09-07 (v0.3.8) - Startup overlay; portable flash drives now fully self-contained

### Added
- **Startup overlay** ("Initializing...", with a spinner) covers the whole app from the moment it
  renders until `init()` finishes wiring up every event listener - previously that whole span (easily
  noticeable when `BuildCatalog` has real extraction work to do) had zero event listeners attached at
  all, so clicking anything, Settings included, did nothing with no indication why. Confirmed live with
  an artificial delay that the overlay blocks input the entire time and disappears the instant the app
  is actually ready.
- **Write to Flash Drive's toolbar button is now disabled when PDT itself is running from a removable
  drive** (`IsRunningFromRemovableDrive`, `flashdrive.IsRemovableDrive`) - confirmed live this used to
  fail with "The process cannot access the file because it is being used by another process" trying to
  overwrite its own running exe; only an installed copy on a technician's laptop is a sensible source
  for stamping out more portable copies anyway.
- **A portable copy's Settings now shows and uses relative base paths** (`.\Drivers`, `.\Configs`)
  instead of an absolute path baked to whatever drive letter the flash drive happened to have at the
  time - a real gap, since a flash drive doesn't keep the same letter across computers, or even across
  relaunches on the same one. `resolveExeRelative` resolves a relative Settings path against wherever
  the exe is *currently* running from, at every point of use (`driversRoot`/`configsRoot`, `PickFolder`,
  Open/Save Configuration's own starting directory, the toolbar's Drivers-folder button) - an absolute
  path (an installed copy's `%LocalAppData%\PDT\...`, or anything explicitly chosen via Browse) is
  unaffected.

### Fixed
- **Write to Flash Drive left the destination missing its Configs folder entirely**, and missing
  Drivers subfolders for any manufacturer not already downloaded on the technician's own laptop -
  confirmed live. A technician's local Configs folder very often doesn't exist yet (nothing saved or
  captured there so far) and their local Drivers folder is rarely fully populated for every
  manufacturer PDT knows about; `writePortablePDTTo` now guarantees both a Configs folder and a full
  manufacturer scaffold exist on the flash drive regardless of what the source had.
- **Settings > General's "Manufacturer sort order (Alphabetize)" link moved next to its label**
  (previously sat on its own line below it, from when it was first added).
- **Settings > External Sites' text fields butted straight up against the tab's own scrollbar** with
  no visual gap at all once there were enough manufacturers to make it scroll - added 8px of padding.

## 2026-09-07 (v0.3.7) - Fix disabled format-warning buttons; USB icon for Flash Drive

### Fixed
- **Write to Flash Drive's "Erase and Format" warning had both its buttons disabled whenever Save ID
  was empty.** Write to Flash Drive itself is deliberately job-independent (exempt from the Save ID
  gate - see `applySalesChainGate`'s own doc comment), but the shared confirm dialog it pops up through
  (`showConfirm()`/`#confirmBackdrop`) lived outside that exemption, so its own OK/Cancel buttons still
  got swept and disabled - confirmed live, with a real removable drive, that both buttons were
  unusable with an empty Save ID before this fix. `#confirmBackdrop` is now exempt too; the two other
  callers of `showConfirm()` (Reset Configuration, Export Configs) are themselves gated by the same
  sweep, so their own confirm popups were already unreachable while locked either way - this changes
  nothing for them.

### Changed
- **Flash Drive toolbar icon changed again** - the 🖴 swapped in last release still didn't read as a
  flash drive to actual use. Replaced with a hand-drawn inline SVG of the classic USB trident symbol
  (circle/square/triangle branches over a plug shape) instead of gambling on a third emoji glyph -
  guarantees the exact appearance regardless of font/emoji rendering differences across machines.

## 2026-09-07 (v0.3.6) - Fix appwiz.cpl self-update sync, retroactive Kyocera repair, UI polish

### Added
- **Settings > General's Manufacturer sort order gains an "Alphabetize" link** next to the label,
  sorting the reorderable list A-Z in one click rather than dragging every entry by hand. Operates on
  whatever's currently shown (including an unsaved drag reorder already in progress), the same way the
  list's own drag-and-drop only takes effect once Save is clicked.
- **Self-updating through Settings > About now keeps Programs and Features (appwiz.cpl) in sync.**
  `ApplyUpdate` previously only replaced the running `PDT.exe` in place - the Inno Setup uninstall
  entry's `DisplayVersion` (all appwiz.cpl's own Version column ever reflects) was untouched, so it
  kept showing whichever version the installer itself last ran, even after a self-update moved the
  actual exe ahead of it. `ApplyUpdate` now also best-effort updates that registry value (tries both
  `CURRENT_USER` and `LOCAL_MACHINE`, since exactly one holds it depending on how PDT was installed;
  a portable/flash-drive copy has neither, silently a no-op) - confirmed live against this project's
  own installed copy, `appwiz.cpl`'s Version column moved from 0.3.4 to 0.3.5 with no reinstall.

### Fixed
- **v0.3.5's fix only prevented the Kyocera raw-PE-dump bug from recurring - it didn't repair an
  install that already had one sitting around from before that fix existed.** A leftover wrong folder
  (created by the bug this same release already documents) still satisfied
  `kyoceraVersionAlreadyExtracted`'s substring check, so the correct extraction kept getting skipped
  even on an install upgraded to carry the v0.3.5 fix - confirmed live against this project's own
  actual installed copy (`%LocalAppData%\Programs\PDT`), not just its dev build. `ensureKyoceraExesExtracted`
  now recognizes this exact leftover (a top-level `.text` *file*, not a folder - `looksLikeRawPEDump`)
  and removes it before deciding whether a version still needs extracting, so upgrading to this fix
  actually self-repairs an already-poisoned install rather than requiring a manual folder deletion.

### Changed
- **Settings dialog +80px wider, +50px taller** (`.settings-modal` 550px -> 630px; `.tab-panel` height
  385px -> 435px) - on top of v0.3.3/v0.3.4's own bumps, for the new Alphabetize link and more general
  breathing room.
- **Flash Drive toolbar button no longer looks like a floppy disk.** Swapped the icon from 💾 (floppy
  disk) to 🖴 (a flat rectangular drive shape) - the feature itself (Write to Flash Drive) was never
  about floppy disks, and the old icon was confusing at a glance.

## 2026-09-07 (v0.3.5) - Fix Kyocera extraction regression from generalized SFX detection

### Fixed
- **Generalizing self-extracting-archive detection to 7z/Zip (v0.3.3) broke Kyocera's own two-stage
  extraction** - a Kyocera driver package's raw bytes do contain a real archive signature within
  `ensureSfxArchivesExtracted`'s scan window (its `.text` PE section IS the embedded archive, just not
  appended cleanly after the PE stub the way RAR/7z/Zip SFX packages are), so it was extracting
  Kyocera's `.exe` too - wrongly, into a same-named sibling folder containing nothing but raw PE
  sections (`.text`/`.rsrc`/`.reloc`/`CERTIFICATE`, not a single real driver file). Worse, that wrong
  folder's name then satisfied `kyoceraVersionAlreadyExtracted`'s own substring check, permanently
  blocking `kyoceraexe.go`'s correct two-stage extraction from ever running for that version again.
  Fixed by having `ensureSfxArchivesExtracted` skip any Kyocera-named `.exe` outright (`kyoceraExeNameRe`)
  rather than relying on call order alone, and reordered `scanManufacturerFolders` to run the
  Kyocera-specific extraction first regardless. The bad leftover folder in this project's own Drivers
  folder was removed; a fresh extraction was confirmed byte-identical to the correct one already
  produced by hand earlier.

## 2026-09-07 (v0.3.4) - Settings dialog widened further

### Changed
- **Settings dialog +50px wider, +20px taller** (`.settings-modal` 500px -> 550px; `.tab-panel` height
  365px -> 385px) - on top of v0.3.3's own +40px/+15px bump, for more breathing room around its
  base-path fields and manufacturer URL list.

## 2026-09-07 (v0.3.3) - Live driver catalog refresh; wider toolbar/Settings; retrofit Archive folders

### Added
- **Toolbar Refresh button (🔄)** rescans the Drivers folder in place (`RefreshDriverCatalog`) - no
  more restarting PDT just to pick up a newly downloaded or extracted driver package. The driver
  catalog (`App.catalog`/`modelIndex`) is now guarded by a real lock (`catalogMu`) rather than being
  build-once-at-startup-only, since a `DriverCandidates`/`Deploy` call can now land at the same moment
  as a refresh. The no-drivers banner's wording points at this button instead of suggesting a restart:
  "...once a driver package is downloaded into the Drivers folder, press the Refresh button (🔄) to
  make it available."
- **Self-extracting archive auto-extraction now also handles 7z and Zip, not just RAR** - what used to
  be `internal/driver/rarsfx.go` (`ensureRarSfxExtracted`, RAR-signature-only) is now
  `internal/driver/sfx.go` (`ensureSfxArchivesExtracted`), matching any of the three signatures within a
  candidate `.exe` and letting the bundled `7z.exe` auto-detect the exact format itself. Confirmed live
  against a real Konica Minolta driver package (`KM_UPD_pcl6_win64_...inst.exe`) that ships as a 7z SFX
  stub with the archive simply appended after it - same layout as Lexmark's RAR, different signature -
  extracted correctly with no manufacturer-specific handling needed, unlike Kyocera's own packaging
  (its embedded archive sits inside the `.text` PE section instead, so it keeps its bespoke two-stage
  extraction in `kyoceraexe.go`).
- **Installer's Programs and Features (appwiz.cpl) entry now reads just "Printer Deployment Tool"**,
  not "Printer Deployment Tool 0.3.1" - added an explicit `UninstallDisplayName` (Inno Setup otherwise
  defaults that to AppName + AppVersion). The version is still visible in that same dialog's own
  "Version" column, and the installer wizard's own title bar is unaffected.
- **Toolbar Drivers-folder button (📂)** opens the current Drivers Base Path in File Explorer directly
  from the toolbar (`OpenDriversBasePathInExplorer`, scaffolding it first if it's empty) - Settings >
  General's own equivalent right-arrow button was removed as redundant now that this exists.

### Changed
- **Main window widened 1054px -> 1204px** - the top toolbar had grown (Spooler dropdown, then Refresh
  and Drivers-folder buttons added this same release) to where it was only ~18px away from wrapping
  onto a second line at the old width; confirmed via direct measurement of each button's own on-screen
  position that this now leaves roughly 150px of slack instead, enough headroom to absorb a
  wider-than-usual system font/DPI rendering the button row's own gap can't otherwise account for.
- **Settings dialog +40px wider, +15px taller** (`.settings-modal` override of the shared `.modal`
  width; `.tab-panel` height 350px -> 365px) - its General/External Sites/About tabs carry more
  per-row content than the simple confirm-style dialogs that share the base `.modal` class, which stay
  at their original size.

### Fixed
- **Extracting a self-extracting `.exe` or `.zip` whose own internal content was already a single
  top-level folder produced a redundant `Foo/Foo/...` nesting** instead of the intended `Foo/...` -
  confirmed against a real Konica Minolta package (`KM_UPD_pcl6_win64_...inst.exe`) that packages
  itself this way. `flattenRedundantWrapperDir` (`internal/driver/flatten.go`) now collapses that
  extra level after `ensureZipsExtracted`/`ensureSfxArchivesExtracted` run - but only when the single
  top-level entry's name actually matches the destination folder's own name, so a package whose
  genuine, intentional layout happens to be a single folder (a plain "Driver" subfolder, say) is left
  exactly as it was; a real regression caught in this package's own test suite while implementing this
  is what led to that narrower, name-matched condition instead of a blanket "always hoist a lone
  subfolder" rule. The already-broken KM extraction left over from testing this before the fix
  existed was removed so the next scan re-extracts it correctly.
- Confirmed the toolbar's Refresh button already re-runs every archive auto-extraction step
  (zip/self-extracting-archive/msi/Kyocera), not just re-scanning already-extracted `.inf` files -
  `RefreshDriverCatalog` calls the exact same `driver.BuildCatalog` startup does, so dropping in a
  raw, never-extracted driver package and clicking Refresh is enough on its own. Documented explicitly
  on `RefreshDriverCatalog` itself, since it wasn't obvious this fell out for free.
- **`ensureDriversScaffold` never actually added `Archive` folders to a manufacturer that already had
  real driver packages in it** - the "only scaffold if root is completely empty" check operated on the
  whole Drivers root, so it silently no-opped for every already-populated install, including this
  project's own real Drivers folder. It's now unconditional and idempotent (safe on every startup):
  every manufacturer in `driver.Manufacturers` gets its `Archive` folder and README ensured, whether
  its own top-level folder is brand new or has been populated by hand for months - the one case still
  left alone entirely is an older flat-layout Drivers folder (no `Windows` subfolder at all), so this
  can't accidentally trip `BuildCatalog`'s own flat-vs-nested back-compat detection.
- Archive folder's placeholder file is now `README.txt` (was `README.md`) - a stale `README.md` from
  an earlier PDT version is removed the next time this scaffold step runs. Wording also changed from
  "The script never scans this folder..." to "The program never scans this folder..." (PDT is a
  compiled program, not the PowerShell script - `Create-Printers.ps1` - it replaced).

## 2026-09-07 (v0.3.1) - Zero-driver first run now actually usable; installer launch-after-install fix

### Added
- **PDT is now fully usable on a brand-new install with zero drivers present** - previously, a fresh
  install's empty Drivers folder crashed startup entirely (see Fixed, below); now that the crash is
  fixed, the Manufacturer dropdowns (Defaults panel and each grid row) offer every manufacturer PDT
  knows about regardless of whether any driver is present locally yet, and a new banner above the
  Defaults panel explains the bootstrap workflow: pick a Manufacturer, click **Check for Updates** to
  open its download page, then drop the downloaded package into the Drivers folder. `GetCatalogStatus`
  gained a `HasDrivers` field (distinct from its existing load-error `OK`/`Error`) so the frontend can
  tell "loaded fine, just genuinely nothing here yet" apart from a real catalog failure. The
  Manufacturer dropdown and Check for Updates button are also now exempt from the Save ID gate (see
  `applySalesChainGate`) that otherwise disables nearly everything in PDT until a Save ID is entered -
  picking a manufacturer and opening its download page is job-independent, the same reasoning that
  already exempted Write to Flash Drive and Spooler, and this bootstrap workflow needs to work before
  there's any job to name yet.
- Every manufacturer folder `ensureDriversScaffold` creates now also gets its own `Archive\README.md`,
  matching the real Drivers folders already documented in the "Drivers folder layout" section of this
  README - not just the bare manufacturer folder it created before.

### Changed
- `App.Manufacturers()` (the Defaults panel and grid rows' dropdown source) no longer filters to only
  manufacturers with a driver already present locally - see Added, above, for why. Settings > External
  Sites' `AllManufacturers()` is unaffected (same manufacturer set either way, just alphabetical
  instead of the user's custom order).

### Fixed
- **Installer's "Launch Printer Deployment Tool" checkbox failed with "CreateProcess failed; code
  740. The requested operation requires elevation."** Inno Setup's `[Run]` step defaults to
  `CreateProcess`, which cannot trigger UAC for an exe whose manifest demands elevation (double-
  clicking the installed exe or its Start Menu shortcut already worked fine, since those go through
  `ShellExecute`). Fixed by adding the `shellexec` flag to that `[Run]` entry.
- **A fresh install with an empty (but successfully scaffolded) Drivers folder was completely
  unresponsive** - the SalesChain ID field, every button, all of it, ignored every click and
  keystroke, with no visible error. Root cause: `driver.ManufacturersWithDrivers` returned a nil Go
  slice for "no manufacturers have drivers yet," which marshals to JSON `null` (not `[]`) across the
  Wails bridge; `state.manufacturers.map(...)`, building the Manufacturer `<select>`'s options, threw
  on that `null` as the very next line in `init()`, aborting the rest of startup before
  `wireEvents()` - which attaches literally every event listener in the app - ever ran. Fixed at the
  source (`ManufacturersWithDrivers` now returns `[]string{}`), then swept for and fixed the same
  nil-slice-across-the-bridge pattern in six more spots reachable via ordinary "nothing found"
  outcomes: `driver.Candidates` (unknown manufacturer), `config.ImportCsv` (nothing to import),
  `flashdrive.EnumRemovableDrives` (no removable drives mounted), `exportconfigs.go`'s
  `matchingPreinstallFolders`/`matchingConfigFiles`/`CheckExportCollisions`, and
  `devmode.go`'s `EnumerateLocalPrinters` (enumeration failure).

## 2026-09-07 (v0.3.0) - Windows installer; Drivers/Configs now live in %LocalAppData%\PDT

### Added
- **Windows installer** (`installer/pdt.iss`, built with Inno Setup 6 - `iscc installer\pdt.iss`),
  published as a GitHub Release asset (`PDT-Setup-<version>.exe`). Deliberately packages only
  `PDT.exe` - no `Drivers` folder, which would bloat it for no benefit (driver packages are hundreds
  of MB each) since PDT already scaffolds an empty `Drivers\Windows\11\<Manufacturer>\` structure on
  first launch regardless. **Elevation-optional by design**
  (`PrivilegesRequired=lowest`/`PrivilegesRequiredOverridesAllowed` + `DefaultDirName={autopf}\PDT`):
  double-clicking it normally installs unelevated, no UAC prompt, to
  `%LocalAppData%\Programs\PDT`; explicitly running it as administrator installs to
  `%ProgramFiles%\PDT` instead. Live-verified end to end for the elevated path (silent install landed
  in `C:\Program Files\PDT`, Start Menu/Desktop shortcuts and an HKLM uninstall entry created
  correctly, launching the installed copy correctly scaffolded
  `%LocalAppData%\PDT\Drivers\Windows\11\<Manufacturer>\` for every manufacturer, and the generated
  uninstaller removed everything cleanly); the unelevated path relies on Inno Setup's own
  well-documented `{autopf}` mechanism and could not be directly exercised in this sandboxed session
  (repeated attempts to force it via `/CURRENTUSER` from an already-elevated automated shell hung,
  most likely an artifact of that shell having no interactive desktop session to de-elevate into,
  not a script defect) - worth a real-world confirmation on an ordinary desktop session.
- **Settings > General gains "Drivers Base Path"** (label, text field, Browse button, and a
  right-arrow button that scaffolds-if-empty then opens the folder in File Explorer), positioned
  between Configuration Files Base Path and Preinstall Base Path. Unlike Preinstall Base Path (a
  convenience pointer only), this is a **live** setting: `BuildCatalog` now reads
  `Settings.DriversBasePath` directly, so it actually controls where PDT loads its driver catalog
  from (takes effect after restarting PDT, since the catalog only scans once at startup).

### Changed
- **Drivers and Configs for an installed (non-portable) copy of PDT now default to
  `%LocalAppData%\PDT\Drivers` and `%LocalAppData%\PDT\Configs`**, regardless of whether PDT itself
  was installed under `%ProgramFiles%` or `%LocalAppData%\Programs` - neither is guaranteed writable
  by an ordinary user (`%ProgramFiles%` never is), so a single, always-writable, always-the-same
  location was chosen over forking behavior by install mode. `defaultDriversBasePath`/
  `defaultSaveFileBasePath` (`settings.go`) both still prefer a real, already-populated `Drivers`
  folder sitting next to the running executable first (the portable/flash-drive case), matching
  existing behavior exactly for that case - only a freshly-installed copy with no such folder falls
  back to the new `%LocalAppData%\PDT` default. **Configuration Files Base Path is now also a live
  setting** the same way (`configsRoot()`/`driversRoot()` now read `Settings.SaveFileBasePath`/
  `DriversBasePath` instead of a hardcoded exe-relative-only computation), and takes effect
  immediately within the same session (DEVMODE capture, Export Configs, Write to Flash Drive) rather
  than needing a restart.
- Write to Flash Drive already wrote `Drivers`/`Configs` as siblings of the copied executable on the
  destination drive regardless of the source machine's own `driversRoot()`/`configsRoot()`
  resolution (confirmed unchanged, no code needed) - so a portable copy stamped out from an installed
  PDT (now reading from `%LocalAppData%\PDT`) still gets a normal `<drive>:\Drivers`,
  `<drive>:\Configs` layout, unaffected by where the source machine keeps its own copies.
  Preinstall Base Path is untouched by this entirely, as it should be - it's a technician's own
  laptop-local site-survey folder, never something that belongs on the flash drive itself.
- Settings dialog is 30px taller (each tab's fixed content height: 320px -> 350px), to fit the new
  Drivers Base Path field without scrolling.

### Verified
- New tests: `TestInstalledAppDataDir`, `TestEnsureDriversScaffold_CreatesManufacturerFolders`,
  `TestEnsureDriversScaffold_LeavesNonEmptyRootAlone`. Full suite (`go build`/`vet`/`test`,
  `wails build`, `iscc installer\pdt.iss`) clean.
- Live end to end as described above under "Added" - real silent install/uninstall cycle via the
  compiled installer, not just a review of the `.iss` script.

## 2026-09-07 (v0.2.1) - Kyocera driver packages now auto-extract on startup

### Added
- **Kyocera self-extracting `.exe` driver packages now auto-extract on PDT startup**
  (`internal/driver/kyoceraexe.go`, `ensureKyoceraExesExtracted`) - manually verified live against a
  real ~250MB package before automating it: 7-Zip can pull the embedded driver archive straight out of
  the `.exe`'s own `.text` PE section without ever launching Kyocera's installer, then that extracted
  `.text` file is itself a normal archive, extracted the same way a second time. `BuildCatalog` now
  runs this for every `Drivers\Windows\<version>\Kyocera\` folder it scans, alongside the existing
  `ensureZipsExtracted`/`ensureRarSfxExtracted`/`ensureMsiExtracted` steps: finds every `.exe` matching
  Kyocera's current naming, pulls its version token out of the filename, and skips it if a sibling
  folder's name already contains that version (so it's a one-time cost per driver version, not a
  redo-every-launch one) - same bundled-`7z.exe`/no-op-if-`SevenZipPath`-unset/best-effort-cleanup-on-
  failure conventions as `ensureRarSfxExtracted`. README's "Kyocera" section now documents this as
  Method 1 (drop the `.exe` in and start PDT once - no scratch folders, no running the installer), with
  the two pre-existing manual approaches renumbered to Methods 2 and 3 as fallbacks.

### Verified
- New tests: `TestKyoceraExeNameRe`, `TestKyoceraVersionAlreadyExtracted`,
  `TestEnsureKyoceraExesExtracted_NoOpWithoutSevenZipConfigured`,
  `TestEnsureKyoceraExesExtracted_SkipsAlreadyExtractedVersion`. Full suite (`go build`/`vet`/`test`,
  `wails build`) clean. Confirmed live: the manual two-stage 7-Zip extraction this automates was run by
  hand first, against a real Kyocera KX Driver v8.6A.1412 package, producing the expected
  `32bit`/`64bit`/`arm64`/`Setup.exe`/`KmInstall.exe` layout; PDT.exe then confirmed to start cleanly
  with the new startup step wired in.

## 2026-09-07 (v0.2.0) - Spooler control, per-row SNMP community string, window/label polish

### Added
- **Spooler button** (top bar, between Export Configs and Write to Flash Drive) with a Restart/Start/
  Stop dropdown for the Windows Print Spooler service - `internal/spooler` (`golang.org/x/sys/windows/
  svc/mgr`), a manual escape hatch for a stuck print object or jammed queue, the same fix a technician
  would reach for via services.msc or `net stop/start spooler`. The button itself colors live to match
  the service's actual state: green running, red stopped, yellow while settling (including for the
  duration of Restart's own stop-then-start sequence) - checked once at startup and refreshed after
  every action. Job-independent, so it's exempt from the SalesChain ID gate like Settings/Write to Flash
  Drive.
- **Per-row SNMP community string**: the grid's SNMP column is now a text field, not a checkbox - blank
  disables SNMP monitoring on that row's port, any text enables it and is the community string used
  (`PrinterRow`/`SavedRow` gain `SNMPCommunity` alongside the existing `SNMP` bool). The Defaults panel
  keeps its SNMP checkbox, now paired with its own community field (defaults to "public", disabled until
  checked) used as the template for "Add Printer." A config saved before this field existed (bare
  `Snmp: true`, no community) still infers "public" on load, matching `AddStandardTcpIpPort`'s own
  existing empty-community default instead of silently disabling SNMP for it.

### Changed
- Main window is 30px wider (1024 -> 1054).
- "SalesChain ID" field label reads "Save ID" (the field's id, tooltips, log messages, and the
  `<SalesChainID>-...` filename convention are all unchanged - display label only).

### Fixed
- The Spooler dropdown menu had the same specificity bug this codebase already fixed once for
  `.modal-backdrop`: a plain `.dropdown-menu { display: flex; }` has equal CSS specificity to the
  browser's own `[hidden] { display: none }` and, coming later in the cascade, always won - the menu
  showed permanently open regardless of its `hidden` attribute, making the Spooler button appear stuck.
  Fixed the same way: `.dropdown-menu:not([hidden])`.

### Verified
- `go build`/`vet`/`test` and `wails build` clean. Version bumped to 0.2.0 (`version.go`,
  `wails.json`'s `info.productVersion`).

## 2026-09-07 - Captured DEVMODE/Device Settings, Export Configs, portable flash drives, deploy safeguards

### Added
- **Captured-DEVMODE workflow**, replacing programmatic duplex/color guessing with real driver-produced
  settings replayed verbatim: a per-row "Get DEVMODE" button (green "DEVMODE SET" once captured) tries a
  live capture from a local printer of the same name first, falling back to browsing for an existing
  `.bin`; a toolbar "Get DEVMODE" bulk-captures every checked row; an "Import Printers" dialog lists
  already-configured local printers (physical ones checked by default, software/virtual ones like PDF
  printers unchecked) with an optional immediate capture on import. Captured DEVMODE is stored in a
  `Configs` folder as `<SalesChainID>-<PrinterName>.bin` - the saved JSON config only ever holds a
  filename pointer (`PrinterRow.DevModeFile`/`SavedRow.DevModeFile`), never the raw bytes - and resolves
  at deploy time via `printer.ResolveDevModePath` (explicit pointer first, falling back to the
  conventional filename so forgetting to re-save the JSON after capturing still works). Applied as the
  very last deploy step, after everything else, so nothing else can override it.
- **Device Settings capture**, alongside DEVMODE: most print drivers keep tray assignments and
  installable options (a duplexer, extra trays, a finisher) entirely outside DEVMODE, in a
  `PrinterDriverData` registry key instead (confirmed live: a captured printer's `DeviceOption01`/
  `DeviceOptionSize` values differ from an unconfigured one). `windows.EnumPrinterDataEx`/
  `SetPrinterDataEx`/`CaptureDriverData`/`ApplyDriverData` capture/replay this key verbatim as an opaque
  `<SalesChainID>-<PrinterName>.driverdata.json` sidecar alongside the `.bin`, applied at the same final
  deploy step.
- **Both a per-user AND a global DEVMODE write** (`OpenedPrinter.SetPerUserDevMode` via `SetPrinter`
  Level 2, `SetGlobalDevMode` via the simpler, single-field `PRINTER_INFO_8`/Level 8) - per Microsoft's
  own "Per-User DEVMODE" documentation these are two independent stores Windows never keeps in sync:
  Level 2 is what the General tab's "Preferences" button reads for the calling account, Level 8 is the
  actual admin-set default (Advanced tab's "Printing Defaults", and what Device Settings reads too, for
  most drivers) inherited by any user without their own override. `SetDuplexAndColor`/
  `ApplyCapturedDevMode` now write both, having previously only written Level 2 (nothing else ever
  showed the change) and, briefly, only Level 8 (Preferences stopped reflecting it).
- **Export Configs**: copies every `Configs/<SalesChainID>*` file (saved JSON, captured DEVMODE/driver
  data) to a technician's site-survey folder on their own laptop - locates the
  `"<SalesChainID> - <Client> - <Address>"` subfolder under a new **Preinstall Base Path** setting
  (prompting to pick one if more than one matches), creates a `PDT` subfolder inside it, and copies
  everything over. Warns first that this only makes sense run on the technician's own laptop, and offers
  Overwrite vs. a freshly timestamped subfolder (`PDT\2026-09-06_1622`) if any destination file already
  exists.
- **Write to Flash Drive**: lists currently-mounted USB flash drives (`GetLogicalDrives`/
  `GetDriveType`'s `DRIVE_REMOVABLE`, excluding fixed/network/optical drives), with an opt-in "Format as
  exFAT first" (via PowerShell's `Format-Volume`, with an explicit erase-everything warning naming the
  exact drives) before copying the running executable plus this computer's own `Drivers`/`Configs`
  folders onto each selected drive - the other half of "install PDT once on a laptop, then stamp out
  portable copies."
- **SalesChain ID interaction gate**: every control except the field itself, Open Configuration,
  Settings, and Write to Flash Drive is disabled until SalesChain ID has a value, ruling out ever
  configuring/deploying under the wrong job's ID by mistake. Applied synchronously at first paint (not
  only once `init()`'s async catalog/settings loading finishes) so nothing is briefly clickable before
  the gate takes effect.
- **Required-field yellow highlighting**: SalesChain ID, Manufacturer, and Driver (Defaults panel and
  per row), plus per-row Name (whitespace-only counts as empty) and IP (a real IPv4 address or the
  literal "NUL" - anything else, including a bare subnet prefix missing its last octet).
- **Deploy Checked Printers stays disabled** until every checked row's IP is one of those valid port
  values, decided independently of the SalesChain ID gate (`updateDeployButtonEnabled`) so IP validity
  and SalesChain ID can't fight each other over the button's state.
- **Red STOP button**, enabled only while a deploy is running. Its warning dialog offers a graceful
  **Stop** (cancels between rows - the row already in progress always finishes, since aborting a
  printer/port/driver change half-applied risks leaving it broken) and a **Force Stop** for when PDT is
  genuinely locked up: `App.ForceQuit` immediately terminates the process (`os.Exit`), bypassing every
  graceful-shutdown path on purpose, since a single hung Win32 call has no safe way to be canceled from
  Go once started.
- **Reset Configuration** button: takes the whole app - every row, SalesChain ID, and the Defaults panel
  - back to its fresh-launch state, confirming first whenever there's anything to lose.
- **Double-click (not a confirm dialog) to remove a row** - a stray single click can no longer delete a
  row by accident, without needing a popup for something this frequent.
- An in-app **Warning/Confirm modal** replacing every `window.confirm()` - a native confirm's title bar
  ("wails.localhost says") is fixed browser/WebView2 chrome that can't be reworded or removed, and read
  as an unbranded, out-of-place popup inside an otherwise normal desktop app.
- **Kyocera now gets the create-against-`NUL:`-then-rebind treatment** `RequiresNulPortWorkaround`
  already gave HP's Universal Print Driver family - applied manufacturer-wide (Kyocera has no single
  "universal" driver name to key off the way HP does) after live testing showed Kyocera deploying
  noticeably slower against a live TCP/IP port than Canon or Ricoh; confirmed much faster afterward.

### Changed
- **Model field removed** from the Defaults panel and the grid - the Driver combobox's own text filter
  already does the same narrowing (typing part of a model name finds a model-specific driver name
  directly, which is the only case Model ever mattered for - Kyocera, mainly). The underlying
  `PrinterRow.Model`/`SavedRow.Model` fields still round-trip silently for backward compatibility with
  configs saved before this change; nothing reads them for driver filtering anymore.
- **No more `SalesChain: <id>` printer Comment** - PDT no longer writes this field at all; an existing
  printer's own comment (from before this change, or set by hand) is left alone.
- **"Save File Base Path" renamed to "Configuration Files Base Path"** and now defaults to `Configs\`
  next to the running executable (matching where DEVMODE captures and saved JSON configs already live)
  instead of `Documents\Preinstall`.
- Every frontend-originated log line now timestamps with the same `YYYY-MM-DD HH:MM:SS` format the Go
  side's own log lines use, instead of `toLocaleString()`'s locale-dependent (en-US: `M/D/YYYY, H:MM:SS
  AM/PM`) format, which made the two visibly inconsistent in the same log panel.
- Every one-off status message (a save/load result, a validation warning) now goes to the log panel
  instead of a separate status bar - a long bulk-operation summary ("Captured DEVMODE for N of M checked
  row(s)") could push Deploy Checked Printers onto its own line in the toolbar.

### Fixed
- The SalesChain ID gate's "only re-enable what I disabled" bookkeeping (a `dataset.gateLocked` marker)
  could end up never re-enabling controls at all, depending on call order, when composed with a control
  that had its own independent disable logic elsewhere (`portPrefixText`, disabled both by its own
  checkbox handler and, redundantly, on every `resetDefaultsPanel()`/`wireEvents()` call) - replaced with
  an unconditional, history-independent sweep (every non-exempt control's `disabled` is recomputed fresh
  on every call) plus one explicit re-assertion pass for `portPrefixText`'s own extra condition, rather
  than trying to track "did the gate itself do this."

### Verified
- New tests: `TestDevModeFileName`/`ResolveDevModePath` (+ driver-data equivalents), `TestFileExists`,
  `TestMatchingConfigFiles`/`MatchingPreinstallFolders`, `TestIsPhysicalPrinterGuess`,
  `TestFindManufacturerForDriver`, and updated `TestRequiresNulPortWorkaround` for Kyocera (plus a
  regression case confirming Ricoh's own "UniversalDriver" naming doesn't accidentally match HP's
  manufacturer-specific check). Full suite (`go build`/`vet`/`test`, `wails build`) clean throughout.
- Live end to end on real hardware across this session: Canon, Kyocera, and Ricoh MFDs deployed
  successfully, including the full capture-on-a-reference-printer -> deploy-to-a-fresh-printer DEVMODE/
  Device Settings round trip (confirmed via Printer Properties' Preferences, Printing Defaults, and
  Device Settings tabs all reflecting the captured configuration) and the Configs-folder fallback path.
  Flash drive enumeration confirmed live (correctly reports zero drives with none plugged in); the
  format and copy-to-drive paths are implemented but not yet exercised against real removable media.

## 2026-09-06 - Lexmark default is now the XL driver; 7-Zip credit + self-update in About

### Changed
- **Lexmark's Defaults-panel default is now "Lexmark Universal v2 XL"**, not the base driver -
  `defaultDriverTokens["Lexmark"]` now requires "XL" specifically (alongside "Universal"/"v2", which
  the base driver also matches) so the token match itself picks XL directly, rather than relying on
  the shorter-name tie-break to settle it. The base driver stays fully selectable in the Driver
  dropdown; this only changes which one is pre-filled. Updated `TestDefaultDriverNameFor`'s Lexmark
  case and replaced the Lexmark-specific tie-break test with
  `TestDefaultDriverNameFor_PrefersShorterNameOverNewerUnversionedVariant`, a synthetic,
  manufacturer-agnostic test of the tie-break tier itself (added directly to `defaultDriverTokens` and
  removed after, not real testdata) - it should keep covering that logic regardless of what any real
  manufacturer's own tokens require going forward.

### Added
- **Settings > About now credits 7-Zip** (by Igor Pavlov) - the tool bundled to auto-extract
  self-extracting RAR driver packages - shows the version currently cached, and links to 7-zip.org.
- **Check for 7-Zip Updates / Update 7-Zip Now**, mirroring PDT's own self-update UI exactly: queries
  7-Zip's own GitHub Releases (development now lives at `ip7z/7zip`) via `internal/update.FetchLatest`
  - already generic enough to reuse as-is for a different project's releases, needing only one small
    addition, `Release.AssetMatching(re)`, since 7-Zip's own asset names embed a version number
    ("7z2603-x64.exe") `Asset`'s exact-name lookup can't match. Downloads the latest x64 GUI installer
    and uses the *currently cached* `7z.exe` to pull `7z.exe`/`7z.dll`/`License.txt` back out of it
    directly - confirmed the installer is itself an extractable 7-Zip archive (it can list and extract
    from itself without ever being run as an installer) - then overwrites the cached copies. Extracts
    to a scratch folder first and only overwrites the real cached files once that fully succeeds, so a
    bad download or failed extraction never touches the existing, working files.

### Verified
- New tests: `TestRelease_AssetMatching`, `TestDefaultDriverNameFor_LexmarkXLIsSelectedOverBase`,
  `TestDefaultDriverNameFor_PrefersShorterNameOverNewerUnversionedVariant`. Full suite
  (`go build`/`vet`/`test`, `wails build`) clean.
- Live end to end against the real, current `ip7z/7zip` release: screenshot-confirmed the About tab
  shows "7-Zip 26.03" and "Check for 7-Zip Updates" correctly reports "You have the latest version of
  7-Zip" (accurate - the bundled copy already is 26.03). Since there was nothing newer to test the
  actual download/apply path against live through the UI, called `App.UpdateSevenZip` directly in a
  throwaway test (removed after) with the real, current release's asset URL: it downloaded, extracted,
  and overwrote the cached `7z.exe` successfully (confirmed via the file's updated modtime), then
  confirmed no leftover temp/extraction files remained in the cache folder afterward.
- Screenshot-confirmed the About tab's added content doesn't disturb the Settings modal's fixed size -
  still lands at the same height as before, no scrolling needed.

## 2026-09-06 - Auto-extract .msi drivers too; fix a real default-driver bug it surfaced

### Added
- **`.msi`-packaged drivers are now auto-extracted** (`internal/driver/msi.go`, `ensureMsiExtracted`),
  completing the Lexmark pipeline the previous entry's RAR auto-extraction started: for every `.msi`
  found, runs an MSI *administrative install* (`msiexec /a ... TARGETDIR=...` - unpacks with real
  filenames/paths, installs nothing) into a sibling folder, then decompresses every Microsoft
  legacy-compressed sibling file the install produces via `expand.exe -R` (restore original name,
  read out of the compressed file's own header - not a guessed extension mapping; see the previous
  correction entry for why guessing broke this before). Uses `msiexec.exe`/`expand.exe` directly, no
  bundled tool needed (both are already part of Windows). End to end, dropping the raw, unmodified
  `Lexmark_Universal_v2_UD1_Installation_Package_*.exe` into `Drivers\Windows\<version>\Lexmark\` and
  launching PDT now needs zero manual steps to reach a scannable, installable driver.

### Fixed
- **Real bug, found live once the full pipeline ran against everything in the tree at once**:
  auto-extracting *every* `.msi` surfaced a second real driver - `print64XL.msi`'s
  "Lexmark Universal v2 XL" (an extra-large-format variant), built two days *after* the base
  "Lexmark Universal v2" - and `DefaultDriverNameFor`'s existing "newest date wins" tie-break picked
  the XL variant as the default, which is wrong: it's a different, more specialized product, not a
  newer version of the base driver. Neither name carries a version number of its own (Xerox's/Konica
  Minolta's own tie-break signal doesn't apply here), so the tie-break was reordered to prefer the
  *shorter* matching name before ever considering date - a name that's a superset of another, with an
  extra qualifier tacked on, is presumed to be the more specialized variant. Confirmed the reordering
  doesn't disturb the Xerox/Konica Minolta cases (their own version-number signal is checked first and
  fully resolves both before the shorter-name tier is ever reached).

### Verified
- New tests: `TestCompressedSiblingRe`, `TestEnsureMsiExtracted_SkipsAlreadyExtracted`,
  `TestDefaultDriverNameFor_PrefersBaseNameOverNewerSpecializedVariant` (a new `LexmarkXLPkg` testdata
  fixture reproducing the real two-days-newer-but-wrong-default scenario exactly). Full suite
  (`go build`/`vet`/`test`, `wails build`) clean.
- Live end to end against the real, unmodified Lexmark package: launched the built `PDT.exe` fresh,
  confirmed the full RAR-then-MSI-then-expand chain produced a byte-identical result to the earlier
  hand-verified-correct extraction (same file sizes for `.inf`/`.gdl`/`.gpd`/`.ini`/`.dll`), and
  confirmed via screenshot that the Defaults panel now correctly pre-selects "Lexmark Universal v2"
  (not "...XL") once the tie-break fix was in.

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
