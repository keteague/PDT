# PDT (Printer Deployment Tool)

A Wails (Go + web frontend) rewrite of `Create-Printers.ps1`, the original PowerShell/WinForms
tool for bulk-deploying network printers (Standard TCP/IP ports, vendor drivers, print
configuration) from a CSV or saved JSON list of rows. This rewrite exists to get off the
PowerShell/WinForms DataGridView layer entirely while keeping every behavior the original tool's
real-world use already proved out - the Win32 mechanics below are ported (not reinvented) from
that tool's own hard-won findings, cross-checked against Microsoft's documentation and, wherever
that documentation didn't cover it, verified directly against this machine's real print spooler.

**[Download the latest installer](https://github.com/keteague/PDT/releases/latest)** - see
"Installing PDT" below for what the installer does and does not do.

`PDT.exe` requests elevation on launch (`build/windows/wails.exe.manifest`) - every real operation it
performs needs Administrator, so Windows prompts for UAC automatically rather than the app starting
unelevated and failing partway through a deploy. There is no unelevated fallback mode. This is
unrelated to (and unaffected by) the installer's own elevated-vs-unelevated install choice described
in "Installing PDT" below - wherever the installed `PDT.exe` ends up sitting, launching it still
UAC-prompts every time, per its own manifest.

## Status

- **Phase 1 - data layer** (`internal/config`, `internal/driver`, `internal/printer` types): CSV/JSON
  import-export, the driver catalog (multi-version/multi-arch scanning of a
  `Drivers/Windows/<any version folder>/<Manufacturer>/...` tree - see "Drivers folder layout" below -
  `Archive` subfolders excluded), driver-name/version resolution, and the platform-independent
  `PrinterRow`/`DeployRequest`/`DeployResult` shapes every phase builds on. Done.
- **Phase 2 - Win32 bindings** (`internal/printer/windows`): hand-written syscall bindings for every
  winspool.drv/setupapi.dll operation the tool needs - `golang.org/x/sys/windows` has no
  winspool.drv coverage at all. Done; see "Windows bindings" below.
- **Phase 3 - orchestration** (`internal/printer/windows/deploy_windows.go`, `internal/printer/batch.go`):
  the actual per-row Deploy sequence (resolve driver -> port -> driver install -> create/update
  printer -> conditional NUL:-to-real-port rebind -> print config -> APF) wired on top of phases 1-2,
  plus the confirmation-dialog flows for driver-version changes and printer updates. Done; see
  "Deploy sequence" below.
- **Phase 4 - Wails frontend** (`app.go`, `frontend/src/`): the actual GUI - a plain HTML/CSS/vanilla-JS
  grid (no framework), the `App` struct's bound methods the frontend calls, and native OS dialogs for
  file pickers and deploy confirmations. Done; see "Frontend" below.
- **macOS support** (`internal/printer/darwin`, `internal/driver`'s Mac* additions): a second real
  `printer.Deployer` implementation - installs a `.dmg`/`.pkg` driver package and creates/reuses a CUPS
  LPD print queue, verified against real vendor packages and this machine's own real queues. `package
  main` now compiles and runs as a real macOS `.app`, with a reduced frontend UI for the Windows-only
  concepts (Spooler, SNMP/port config, APF, DEVMODE capture, app self-update) that have no CUPS/macOS
  equivalent yet. Not yet installer-packaged. What's still genuinely open: the open question of
  whether a real signed `.app` avoids the ad-hoc-signing AMFI rejection a bare test binary hit -
  see "macOS support" below and the Changelog for the current list.
- **Not started**: Linux support.

## Windows bindings (`internal/printer/windows`)

`golang.org/x/sys/windows` has zero winspool.drv coverage, so every binding here is hand-written
against Microsoft's Win32 documentation, and - for the handful of things that documentation doesn't
cover at all - verified directly against this machine's real print spooler (see `cmd/pdtdebug`
below).

| File | What it does |
|---|---|
| `printer_windows.go` | `OpenPrinter`/`GetInfo2`/`SetInfo2`/`CreatePrinter`/`DeletePrinterByName`/`EnumLocalPrinterNames` - the PRINTER_INFO_2 read/write core everything else builds on. Always requests `PRINTER_ALL_ACCESS` explicitly (a `NULL` `PRINTER_DEFAULTS` grants a default access level too low for `SetPrinter` to succeed - the original tool's own early bug). |
| `apf_windows.go` | The printer Advanced tab's "Enable advanced printing features" toggle: `PRINTER_ATTRIBUTE_RAW_ONLY`, **inverted** (enabling APF clears the bit) - confirmed against Microsoft's docs after an earlier wrong guess (`PRINTER_ATTRIBUTE_ENABLE_DEVQ`) in the original tool. |
| `port_windows.go` | `NUL:` port creation via `XcvData "AddPort"` against the "Local Port" monitor. The obvious-looking `AddPortEx` API was tried first and rejected - it's documented as NT4-legacy and returns `ERROR_INVALID_PARAMETER` against the modern Local Port monitor in practice; `XcvData` is the real, current mechanism (and what Standard TCP/IP port creation below also needs). |
| `tcpport_windows.go` | Standard TCP/IP port creation via `XcvData "AddPort"`/`PORT_DATA_1` against the "Standard TCP/IP Port" monitor - the one piece with no PowerShell-cmdlet internals to port from at all (the original tool used the `Win32_TCPIPPrinterPort` WMI class, a completely different mechanism), built from Microsoft's TCPMON Xcv Commands documentation and verified end-to-end (see below). |
| `driverinstall_windows.go` | Driver installation via `SetupCopyOEMInfW` (stage into the driver store) + `InstallPrinterDriverFromPackageW` (register with the spooler). The latter's real export turned out to live in `winspool.drv` on this machine, not `spoolss.dll` as Microsoft's own docs list - found via the actual runtime error, not assumed. |
| `devmode_windows.go` | Duplex/color via `DocumentPropertiesW`/`DEVMODE`, replacing the original tool's unreliable `Set-PrintConfiguration` + PrintTicket-XML fallback entirely. **Known limitation, ported forward from the original tool**: Kyocera's driver won't reliably switch back from monochrome to color via DEVMODE (confirmed non-transient); logged as a `[WARN]`, never fatal. |
| `driverinfo_windows.go` | Reads an *installed* driver's real version straight from the registry (`...\Environments\<env>\Drivers\Version-3\<name>`, values `DriverDate`/`DriverVersion`) - confirmed on a real machine to hold the same version string a vendor `.inf`'s own `DriverVer=` line declares, unlike `Get-PrinterDriver`'s CIM-derived `Date`/`DriverVersion` properties, which the original tool found came back unusable regardless of how the driver was installed. |
| `portlookup_windows.go` | Finds an existing Standard TCP/IP port already targeting a given host, by reading every port's `HostName` value from the registry (`...\Monitors\Standard TCP/IP Port\Ports\<name>`) - the registry-level equivalent of the original tool's `Win32_TCPIPPrinterPort.HostAddress` WMI lookup. |
| `structs_windows.go` | Every Win32 struct used above (`DEVMODE`, `PRINTER_INFO_2`, `PORT_DATA_1`, `DELETE_PORT_DATA_1`, ...), laid out to match the real C structs byte-for-byte - see `structs_windows_test.go`'s `unsafe.Sizeof` assertions against the documented sizes (`DEVMODE`=220, `PRINTER_INFO_2`=136, `PORT_DATA_1`=964, `DELETE_PORT_DATA_1`=236). |

### `cmd/pdtdebug`

A throwaway CLI for verifying the bindings above against this machine's real print spooler -
mirroring how the driver-catalog logic was validated headlessly against real local files. It has no
role in the shipped app; delete it once the Wails UI exercises the same codepaths directly. Needs an
elevated terminal (every printer/port operation requires Administrator).

Notably: `deployrow <driversRoot> <manufacturer> <driverSelection> <ip> [nocleanup] [useexisting]`
runs the *entire* Phase 3 Deploy sequence end to end against a real local driver catalog and this
machine's real spooler for one throwaway printer, auto-confirming every prompt, then cleans up after
itself (unless `nocleanup` is given, to set up a same-row redeploy test). Run `pdtdebug` with no
arguments for the full command list.

## macOS support (`internal/printer/darwin`, `internal/driver`'s Mac* additions)

`printer.Deployer`'s own platform-independent design (from the original Windows-only build) meant a
second implementation could be added with no changes to the interface itself. The macOS side installs
a `.dmg`/`.pkg` driver package and creates a CUPS **LPD** print queue, instead of a Standard TCP/IP
port + `.inf` driver the Windows side uses - CUPS has no separate "port" object at all, so there's
nothing analogous to create ahead of the queue itself.

| File | What it does |
|---|---|
| `internal/driver/maccatalog.go`, `maczip.go` | Scans `Drivers/macOS/<Manufacturer>/<any version folder>/*.dmg`/`*.pkg` (version nested under manufacturer, the other way from the Windows side - see "Drivers folder layout" below for why), plus a flat `Drivers/macOS/OpenPrinting/<Manufacturer>/*.ppd` fallback bucket. `.zip` is auto-extracted first (`ensureMacZipsExtracted`, reusing the Windows side's own `extractZip`/`flattenRedundantWrapperDir` - no Windows-only dependency in either) - confirmed necessary against a real Canon download, which ships as a `.zip` directly wrapping one `.dmg` with no installer of its own inside. `__MACOSX/` and `._`-prefixed AppleDouble resource-fork stubs (macOS's own zip tooling litters these into any zip made on a Mac) are explicitly skipped - confirmed live one of these shares its real file's own `.dmg` extension, at a few hundred bytes instead of 80+ MB, so without this it would show up as a second, bogus catalog entry that fails the moment something tries to mount it. |
| `internal/driver/macmount.go` | `LocatePkg` resolves a `.dmg` to the real `.pkg` inside it (mounts via `hdiutil`, recurses into one level of nested `.dmg` - confirmed necessary against a real Kyocera package that wraps a nested image), with no bundled extraction tool needed - unlike Windows' bundled 7-Zip, macOS driver packages need no pre-extraction step at all. `PackageLabel` is a best-effort *display* label only (see below) - never used to decide which package is newest. `LocateLoosePPDs` is `LocatePkg`'s sibling for the opposite case - a `.dmg` with no `.pkg` inside at all, confirmed live to be Canon's own "PPD" bucket shape (a nested `.dmg` wrapping a plain folder-per-model tree of `*.PPD.gz` files, no installer anywhere) - same mount/one-nested-level convention, collecting every loose PPD found instead of a single `.pkg`. |
| `internal/driver/macresolve.go` | `ResolveMac` picks the newest package for a manufacturer **by file modification time**, not by any version parsed out of the package - confirmed against a real Kyocera distribution-style package that there's no reliable per-package version field on macOS at all (every component's own declared "version" was boilerplate `1.0`/`0`); the file's own mtime is the only honest signal available. `ResolveOpenPrintingPPD`/`OpenPrintingCandidates` fuzzy-match a technician-typed driver/model string against the OpenPrinting fallback bucket's own filenames (normalized from `Ricoh_MP_C3003.ppd`-style underscores to spaces first - confirmed necessary, `FuzzyMatchScore`'s subsequence matching is strict about order and does not treat `_` and ` ` as interchangeable). |
| `internal/driver/macppd.go` | `ReadPPDNickName` reads a PPD's own `*NickName` (falling back to `*ModelName`), transparently gzip-decompressing `.ppd.gz` - confirmed live necessary against real Canon PPDs, whose filenames (`CNPZUIRAC5840ZU.ppd.gz`) are cryptic vendor codes sharing no matchable substring, or even in-order character sequence, with how a technician would actually type the model (`iR-ADV C5840`); `FuzzyMatchScore` against the raw filename is a hard `-1`. `PackagePPDNickNames`/`PackageBestModelScore` do the same read-only `pkgutil --expand-full` inspection `PackageLabel` already does, but collect every PPD's `*NickName` and score them against a model string - lets a package be checked for whether it even supports a given model *before* installing it. |
| `internal/driver/macfamily.go` | `ResolveMacFamily` wraps `ResolveMac` for a manufacturer that ships more than one genuinely distinct driver as separate packages, not just version variants of one driver (`macFamilyPreference`: today just `"Canon": {"UFRII", "PS", "PPD"}`, Ken's own stated preference order) - tries each family newest-first, pre-checking its PPD payload against Model via `PackageBestModelScore` before committing to it, falling through to the next-preferred family on no match. Degrades silently to plain `ResolveMac` for any manufacturer with no family table. Today only the guess-based fallback path still calls this directly - see `macmodel.go`'s own row below for the build-once catalog that normally answers this ahead of time. |
| `internal/driver/macmodel.go` | `BuildMacModelIndex` builds, once per catalog build/refresh, a `MacModelIndex` (`manufacturer -> friendly model -> []MacPPDVariant`) for every `macFamilyPreference` manufacturer - inspecting only each family's own single newest package (never every version-folder's own copy) *and only when it's actually new or changed* (see `maccatalogdb.go` below - most launches skip inspection entirely now). `MacVariantForDeploy` then resolves a row's own `(Model, Driver)` straight to a `MacPPDVariant` - which package to install and which exact PPD filename it registers, or (for a family that ships loose PPDs with no installer at all - confirmed live against Canon's own "PPD" bucket) a permanently-cached local copy to hand `lpadmin` directly - with no per-deploy package re-inspection at all. |
| `internal/driver/maccatalogdb.go` | `MacManufacturerCatalog` - the persistent, per-manufacturer JSON record `BuildMacModelIndex` reads/writes (`Drivers/macOS/<Manufacturer>/catalog.<manufacturer>.json`) so a package it's already indexed never gets mounted/expanded again on a later launch. `IsCurrent` is the cheap (no mounting) staleness check - path/modtime/size against `BuildMacCatalog`'s own free directory-scan info; `DiffModels` is the "what changed" comparison surfaced to the frontend Log panel via `CatalogStatus.ModelChanges` when a package *has* changed. See "Model-driven PPD selection on macOS" above for the full story, including why the PPD cache itself (deploy-time bytes for a no-installer family) deliberately stays per-machine instead of living here too. |
| `internal/driver/macppd.go` | `packagePPDEntries` is the actual package inspection `indexFamilyPackage`/`PackagePPDNickNames` both use - `pkgutil --expand` (structure only) plus selective `cpio` extraction of just `*.ppd`/`*.ppd.gz` from each sub-package's own gzip-compressed `Payload`, not the original `pkgutil --expand-full` + full-tree walk (confirmed live: ~20x less I/O for the same result - see "Fast, selective PPD extraction" above). |
| `internal/printer/darwin/elevate_darwin.go` | Every privileged command (`installer`, `lpadmin`) runs through `osascript`'s `do shell script ... with administrator privileges` - the closest available equivalent to Windows' manifest-driven auto-UAC-elevation without a paid code-signing certificate. **Confirmed live that a bare, ad-hoc-signed CLI binary gets killed by AMFI** (`AppleMobileFileIntegrityError -423`) the moment the privileged command actually starts, even after the password prompt is accepted - whether a real signed `.app` bundle avoids this too is still an open question (see the file's own doc comment for the full story and what to try next). |
| `internal/printer/darwin/install_darwin.go`, `ppdinventory_darwin.go` | Installs a resolved package via `installer -pkg ... -target /`, and diffs `/Library/Printers/PPDs/Contents/Resources` before/after to discover which PPD(s) it actually registered - there's no Windows-registry-like "installed driver version" to read directly on macOS, so a before/after PPD-directory diff is the closest real signal (confirmed live against a real Kyocera install: hundreds of new PPDs appeared, including an exact match for a real deployed printer's own model). This is the **fallback** path now - see `canoninstall_darwin.go` below for the selective path a catalog-driven Canon UFR II row takes instead. |
| `internal/driver/maccanonselective.go`, `internal/printer/darwin/canoninstall_darwin.go` | The selective Canon UFR II install (see "Real deploy bugs found via live testing" below for the full story of why this exists) - installs the Core sub-package for real, then `cpio`-extracts just the *one* target model's own PPD + matching per-model "Recipe" bundle straight out of Device's own Payload, skipping the other ~548 pairs and Icons/Profiles/cnaccm entirely. `CanonCoreDevicePackages` locates the two sub-packages by name suffix; `ExtractCanonDeviceFiles` does the selective extraction (three cpio patterns - the PPD, the Recipe bundle, and a sibling `Recipe/<model>.rcp` symlink into it, found by diffing a real BOM). `installCanonSelective` re-packs the expanded Core sub-package via `pkgutil --flatten` before installing it (`installer -pkg` rejects an expanded component directory outright, confirmed live even without any privilege at all) and copies the staged files into place with `cp -RX` (`-X`: skip extended attributes - plain `cp -R` fails against `/Library` itself even as root). Falls back to the plain full-package install above whenever a package doesn't match this exact shape. |
| `internal/printer/darwin/canonbatch_darwin.go` | `Deployer.PrepareBatch` (implements the shared `printer.BatchPreparer` optional interface) - collapses every batchable row's own install+queue-create+defaults into **one** elevated call for the whole deploy run, since `do shell script ... with administrator privileges` never reuses a recent grant (confirmed live, repeatedly - every call shows its own fresh prompt). Scoped to catalog-driven Canon UFR II rows with no existing queue to reuse; everything else falls back to the old per-row path, own prompts included. Per-row success/failure comes back through a plain results file (`printf '%d:%d\n' <rowIndex> $? >> file`, one line per row's own `( ... )` subshell) rather than parsing the combined call's own stdout - `do shell script` mangles `\n` to `\r` and buffers everything until the whole script exits, both confirmed live, both sidestepped by reading a file off disk afterward instead. |
| `internal/printer/darwin/queue_darwin.go` | Creates/reuses a CUPS queue via `lpadmin`/`lpstat` - device URI built by `deploy_darwin.go`'s own `lpdDeviceURI` (see below), confirmed against this machine's own already-deployed real queues (`Jenks_Kyocera`, `Jackson_Streets`) that a bare `lpd://<ip>/` with no queue name is the working convention for most manufacturers. Reuses an existing queue already targeting the same device URI rather than ever creating a duplicate, the same rule `portlookup_windows.go` applies to Standard TCP/IP ports. |
| `internal/printer/darwin/printdefaults_darwin.go` | Best-effort duplex/color defaults, by reading a PPD's own declared option keywords/choices - either a live queue's `lpoptions -l` (`SetPrintDefaults`, for a reused queue) or the PPD file directly off disk (`PrintDefaultsForNewQueue`/`readPPDFileOptions`, for a brand-new queue, so the `-o` args can ride along on the same `lpadmin` call that creates it). `findOption` matches by an **exact** (case-insensitive) allowlist of known keywords - `Duplex`/`ColorModel` (standard) and `CNDuplex`/`CNColorMode` (Canon) - never a suffix or substring match: confirmed live that a real Canon PPD declares both `CNColorMode` (the real switch) and the unrelated `CNProcessColorMode` (a boolean toggle), both ending in "ColorMode", so an earlier suffix-based version picked whichever came first in that model's own option order and silently left color mode untouched. |
| `internal/printer/darwin/deploy_darwin.go` | The orchestrator (`Deployer.Deploy`) - resolve driver -> ensure it's installed -> resolve/create queue -> best-effort print defaults, or (when `PrepareBatch` already handled this row) just format the result it already computed. No NUL:-port workaround (nothing here is ever created against a placeholder port; CUPS queue creation doesn't have the multi-minute-against-a-live-port problem that motivated it on Windows) and no APF/"print spooled documents first" (both Windows spooler-specific concepts with no CUPS equivalent). `lpdDeviceURI` builds the device URI from the row's own optional `LPDQueueName` (grid column "LPD-Q", right after IP, on both platforms) - most manufacturers ignore the LPD queue-name segment entirely, but HP (`raw`) and Xerox (`lp`) are two confirmed exceptions, auto-filled by the frontend whenever a row's Manufacturer is set to either (`defaultLpdQueueFor` in `frontend/src/main.js`) and still freely overridable per row. |
| `pickfolder_darwin.go` (repo root) | Settings > General's Browse ("...") buttons show a native folder picker via AppleScript's `choose folder` (through `osascript`) instead of Wails' own `runtime.OpenDirectoryDialog` - confirmed live that every Wails-bound method (this one included) runs on its own freshly-spawned goroutine, never the process's real main thread, and AppKit's `NSOpenPanel` is only safe to drive from the main thread; calling it off-thread silently swallowed every click, with the sheet never appearing and no error surfaced either. `osascript` sidesteps the whole problem by running in its own process with its own main thread/run loop. Also captures/restores whichever app was frontmost around the call - `choose folder` run bare like this belongs to no particular app, and macOS attributes its window to Finder, activating it as a side effect that otherwise visibly buries PDT's own window. |

### Model-driven PPD selection on macOS

The grid's row-level **Model** field (`row.model` in `frontend/src/main.js`, `PrinterRow.Model`/
`SavedRow.Model` - always existed in the data layer/CSV/JSON, only got a grid UI once the Windows side
brought the field back) drives `App.Models` and `App.DriverCandidates` on macOS exactly the way it
drives Windows' own Kyocera model-narrowing: pick a Model first, then Driver's own candidate list
narrows to just that model's options. On macOS this matters for exactly the manufacturers
`macFamilyPreference` lists (Canon today) - a manufacturer that ships more than one genuinely distinct
driver as separate packages (UFR II, PostScript, and a plain-PPD-only package, each supporting a
different, overlapping-but-not-identical set of models) needs Model to know *which* package even
applies, not just which PPD inside one package to pick.

**Model is mandatory, and Driver is fully derived from it, for exactly these manufacturers - not
just narrowed.** `App.MacModelManufacturers()` (`drivercatalog_darwin.go`) lists them (Canon
today) without hardcoding a name in the frontend; `macModelDriven()` (`frontend/src/main.js`) is
the one predicate every call site below keys off:
- The grid row's own Model field is flagged (the same yellow `input-needs-value` styling
  Name/IP/Driver already use) when left blank for one of these manufacturers, since there's no
  single correct Driver to guess at without it.
- Committing a Model (typed or picked) auto-fills Driver to `DriverCandidates(manufacturer,
  model, "")`'s first result - already preference-ordered UFR II-first (see below), so this is
  exactly "the model's own UFR II variant" with no extra Go-side lookup - and clears Driver back
  to blank/flagged the moment the typed Model text stops resolving to a real one.
- The **Defaults panel's own Driver field is blank and not flagged** for these manufacturers
  (`App.DefaultDriverFor` returns `""` rather than guessing) - the Defaults panel has no Model
  field to narrow by at all, so there's no single package it could honestly default to among
  Canon's UFR II/PostScript/Generic PPD (a raw, unparsed package label like
  `"UFRII_v10.19.25_mac"` used to leak through here - a real, previously-shipped bug).
- A grid row's Driver also now auto-fills from the Defaults panel on a plain Manufacturer change
  (previously cleared to blank) on **both** platforms - the Defaults panel's own current Driver
  value when its Manufacturer already matches (preserving a manual override typed there),
  otherwise a fresh per-manufacturer default. On Windows this is how a Canon row picks up "Canon
  Generic Plus UFR II" automatically (`internal/driver/default.go`'s own `defaultDriverTokens`);
  Windows' Model field itself is unaffected by any of the above - it stays optional, and fuzzy
  search only ever has real data for Kyocera, since indexing Canon's own mac model data depends on
  `hdiutil`/`pkgutil` (macOS-only tools) - a technician can still type a Model on Windows anyway,
  for a config meant to be opened on a Mac later.

**The catalog is built once, not re-inspected per deploy.** `driver.BuildMacModelIndex`
(`internal/driver/macmodel.go`) runs at the same points `BuildMacCatalog` already does - app startup
and the toolbar's Refresh Drivers - inspecting only each family's own single newest package (never
every version-folder's own copy) and recording, for every friendly model name it finds, which package
registers it and under what exact PPD filename (or, for a family with no installer package at all - see
below - a permanent local copy of the PPD itself). The result (`driver.MacModelIndex`,
`App.macModelIndex`) is a plain in-memory map from then on: `App.Models`/`App.DriverCandidates` are pure
lookups, and `deploy_darwin.go`'s own `resolveDriver` resolves a row's `(Model, Driver)` straight to a
`driver.MacPPDVariant` (`driver.MacVariantForDeploy`) with **no package re-inspection at deploy time at
all** - a real change from how this worked before: `ResolveMacFamily`/`choosePPD` used to shell out to
`pkgutil --expand-full` fresh on every single deploy just to guess. That guess-based pair still exists
and still runs (see below), but only as the fallback for a manufacturer/model the catalog doesn't have
an entry for.

**Not re-inspected per *launch* either, once a package has been indexed once.** Mounting and
inspecting a real vendor package is itself expensive - confirmed live that `pkgutil --expand-full`
alone cost 6.4s and 255MB written per package (fixed - see "Fast, selective PPD extraction" below)
- and every family gets re-mounted on every single launch/Refresh otherwise. `driver.
MacManufacturerCatalog` (`internal/driver/maccatalogdb.go`) is a persistent, human-readable JSON
record - one file per manufacturer, `Drivers/macOS/<Manufacturer>/catalog.<manufacturer,
lowercased>.json` (e.g. `Drivers/macOS/Canon/catalog.canon.json`, not one combined file, so
rebuilding or deleting one manufacturer's own catalog never touches any other's) - of every
model/PPD indexed so far, plus exactly which package (and its own parent chain - outer `.dmg` ->
nested `.dmg` -> installer `.pkg` -> sub-package, each with path/modtime/size/version where one
exists) produced it. `BuildMacModelIndex` skips re-inspecting a family entirely once its recorded
package identity (path/modtime/size - free from `BuildMacCatalog`'s own directory scan already,
no mounting needed) still matches - confirmed live: a second build against the same real Canon
packages dropped from 17.7s to 0.147s (~120x), with byte-identical results, and the real app's own
launch time dropped from 35-45s to ~2s the same way. Lives inside the Drivers folder itself
(`Drivers/macOS/`, not `installedAppDataDir()`), deliberately, so it travels with a portable/flash-
drive copy between machines; a no-installer family's actual cached PPD bytes stay per-machine
(`~/Library/Application Support/PDT/PPDCache/` - a flash drive is normally write-protected in the
field, and re-inspection is cheap enough now that there's no real benefit to those bytes
traveling too) - `cachedVariantFilesExist` notices when a fresh machine's own cache doesn't have
what a borrowed catalog.json references yet and re-inspects rather than trusting a path that
doesn't resolve locally. Never written back to at all when this exact running copy is itself on a
removable drive (`app_darwin.go`'s `loadCatalog`, `flashdrive.IsRemovableDrive` - the same
reasoning the Windows side's own `BuildCatalogNoExtract` already applies) - a technician's laptop
is where catalog.json gets built/updated, on local NVMe; a flash drive plugged into a different
machine only ever reads whatever's already there. When a family's package *has* changed,
`driver.DiffModels` compares its freshly-indexed model names against what was recorded before, and
`CatalogStatus.ModelChanges` carries a human-readable summary back to the frontend Log panel after
a Refresh - "what changed between this package and the last one", not PDT trying to judge
staleness or regressions itself.

**Fast, selective PPD extraction** (`internal/driver/macppd.go`'s `packagePPDEntries`) is what a
*first-ever* (or genuinely changed) inspection now costs, replacing the original `pkgutil
--expand-full` + full-tree walk. `--expand-full` fully decompresses a package's *entire* payload -
driver binaries, a dozen languages of README/license text, icons, everything - just to find
`*.ppd(.gz)` files. Confirmed live against a real Canon UFR II package: 6.4s and 255MB written, for
a package whose actual PPDs total 25MB. `pkgutil --expand` (structure only, ~0.1s) leaves each
sub-package's own `Payload` as what it actually is on disk - plain gzip-compressed cpio data
(confirmed via `file`, no dependency on a more exotic format like `pbzx`) - which the system
`cpio` tool extracts selectively: `gunzip -c Payload | cpio -idm "*.ppd" "*.ppd.gz"` pulls out just
the matching entries in 0.2-0.4s. A ~20x cut on the actual bottleneck, using tools already in this
codebase's own style (already shells out to `hdiutil`/`pkgutil`/`installer`).

**How a model unifies across languages**: confirmed against all three of Canon's real macOS downloads
for the same physical printer (an iR-ADV C5840/5850) that each family's own PPD `*NickName` differs only
by a trailing language token - `"Canon iR-ADV C5840/5850"` (UFR II, no suffix), `"...C5840/5850 PS"`
(PostScript), `"...C5840/5850 PPD"` (the plain-PPD family) - so `stripLanguageSuffix` strips exactly that
known trailing token (`macFamilyPreference`'s own tokens) to get one canonical model name shared across
all three, which is what `App.Models` actually offers in the dropdown. Picking that model then offers
its own language variants in the Driver dropdown, labeled the way a technician would actually read them
(`"Canon iR-ADV C5840/5850 (UFR II)"` / `"(PostScript)"` / `"(Generic PPD)"`) rather than as a raw
NickName or filename.

**The third family needs no installer at all - a real, separate finding.** Inspecting Canon's actual
"PPD" bucket download live turned up something the original UFR II/PS-only design didn't account for:
`PPDv5.50_mac.dmg` wraps one nested `.dmg` that's just a plain folder-per-model tree of loose
`*.PPD.gz` files - no `.pkg` installer anywhere inside it at all. Confirmed by reading one of those
PPDs directly: no `*cupsFilter` line, `*LanguageLevel: "3"` - a real Generic PostScript Level 3 PPD, not
a proprietary Canon driver, so there's genuinely nothing to install (Canon's UFR II/PS PPDs, by
contrast, each declare `*cupsFilter` entries pointing at vendor filter binaries under
`/Library/Printers/Canon/...` that only `installer -pkg` deposits - confirmed by expanding a real UFR II
package and finding exactly those filter binary paths referenced). `driver.LocateLoosePPDs`
(`internal/driver/macmount.go`) is `LocatePkg`'s sibling for this shape - same mount/one-nested-level
convention, collecting every loose PPD instead of a single `.pkg` - and `BuildMacModelIndex` permanently
copies each one it finds into a local cache (`driver.CachePPDFile`, under
`~/Library/Application Support/PDT/PPDCache/`, since the source `.dmg` won't still be mounted at deploy
time) rather than trying to "install" a package that doesn't exist. At deploy time, a variant from this
family goes straight to `lpadmin -P <cached path>` with no `installer` call at all - the only family of
the three that skips installation entirely.

**The guess-based fallback** (`driver.ResolveMacFamily` + `choosePPD` in `deploy_darwin.go`) still exists
and still runs, for exactly the cases the catalog can't answer: a manufacturer `macFamilyPreference`
doesn't list, or Model left blank, or a typo that doesn't fold-match any known model.
`ResolveMacFamily` tries each family newest-first, pre-checking whether its own PPD payload even
supports Model (`driver.PackageBestModelScore`) before committing to it - logging a `[WARN]` on every
fallback tier, same as before. Once a package installs this way, `choosePPD` fuzzy-matches Model against
the newly-registered PPDs' own `*NickName` content (`driver.ReadPPDNickName`) rather than their
filenames - confirmed necessary against real Canon filenames (`CNPZUIRAC5840ZU.ppd.gz`), which share no
matchable substring, or even in-order character sequence, with how a technician would type the model
(`iR-ADV C5840`): `FuzzyMatchScore`'s subsequence fallback specifically fails on the `-`/` ` characters
the filename never contains, so filename-based matching there was a hard `-1`, not just weaker. This
path is also what a manufacturer with just one real driver package (Kyocera, Ricoh, Sharp - no
`macFamilyPreference` entry, so no pre-built model index at all) still uses for its own post-install PPD
pick - confirmed still correct against a real Kyocera fixture (`Kyocera TASKalfa MZ6001ci` vs.
`MZ6001i`) that a bare-substring Model query doesn't always land on a clean winner the way it looks like
it should, since `FuzzyMatchScore`'s own tie-break (shorter matched text wins) can outscore a match even
when the model text alone doesn't actually distinguish the two candidates - a `choosePPD` implementation
detail worth knowing about, not a bug.

- When there's no installer package at all (the OpenPrinting-bucket fallback, a manufacturer with
  neither a model index entry nor any local installer package), `row.Driver` is checked first for an
  *exact* `OpenPrintingCandidates` label match (`driver.OpenPrintingPPDByLabel` - the technician
  explicitly picked one from the dropdown, the most authoritative signal available) before falling back
  to fuzzy-matching Model via `driver.ResolveOpenPrintingPPD`.
- **Still not built**: an interactive disambiguation prompt for a genuinely ambiguous *fallback-path*
  match (two candidates tying for best score, or Model left blank with several candidates to choose
  from) - today this logs a `[WARN]` naming the best guess it made instead (`choosePPD`'s own
  `ambiguous` return, or `ResolveMacFamily`'s own `note` return), rather than pausing to ask. Not a
  concern at all for a catalog-indexed manufacturer/model, which is never ambiguous - `MacVariantForDeploy`
  always resolves to a real, known PPD once Model matches an index entry. A real prompt would need more
  than `printer.Confirm`'s yes/no shape (a candidate-list picker), which is a real, separable piece of
  future work if the `[WARN]`-and-guess behavior turns out not to be good enough in practice.

**Japan-market variants are filtered out of the index.** Confirmed against real Canon PPD `*NickName`
data that a Japan-only variant's nickname ends in a literal `" JP"` token, after the language token
`stripLanguageSuffix` already strips (e.g. `"...PS JP"`) - `isJapanMarketOnly` (`macmodel.go`) checks
the raw NickName for that suffix before any other processing, at both PPD-entry loops inside
`indexFamilyPackage` (installer-backed and loose-PPD-bucket paths), so a JP-only variant never reaches
`App.Models`/`App.DriverCandidates` at all. Only takes effect on a package that's actually (re)inspected -
a `catalog.<manufacturer>.json` already indexed before this filter existed keeps whatever it already
recorded until that package changes or the catalog file is deleted/rebuilt.

### Real deploy bugs found via live testing (v0.6.1)

Deploying real Canon UFR II rows live against this machine surfaced two bugs the design above didn't
anticipate - both are about what happens *after* a package/PPD has already been correctly resolved, not
about resolution itself:

- **CUPS queue names reject whitespace.** `lpadmin -p` fails outright (`Printer name can only contain
  printable characters`) for a queue name containing a space - confirmed live against a row literally
  named `"Copy Room"`. `man lpadmin` documents the real restriction: no SPACE, TAB, `/`, or `#`.
  `deploy_darwin.go`'s `sanitizeCUPSQueueName` maps each of those to `_` for the actual `-p` argument
  only (`strings.Map`) - the row's own `Description` (and everything else about the row) keeps the
  original, unsanitized name; a `[WARN]` logs when the two actually differ.
- **A package used to get reinstalled from scratch for every row that needed it**, even multiple rows
  in the same deploy run needing the identical package - `install_darwin.go`'s `EnsureDriverInstalled`
  has no existing-install check of its own, it always shells out to `installer -pkg` unconditionally.
  Confirmed live against a real Canon UFR II package that one install costs **4m38s**, so 3 identical
  test rows cost ~14 minutes of pure redundant work - and since each install routinely outlasts macOS's
  own few-minutes Authorization Services cache, this was also why deploying a handful of same-manufacturer
  rows kept prompting for the admin password over and over instead of once. `Deployer.installedThisRun`
  (keyed by the resolved package's own path) plus `ensureInstalledOnce` - the new call site both
  `resolveDriver`'s guess-based fallback and `installVariant` use instead of calling
  `EnsureDriverInstalled` directly - makes a real install happen at most once per deploy run regardless
  of how many rows need that same package, logging an `[INFO]` skip line on every row after the first.

Two smaller, frontend-only fixes landed alongside these:

- **The grid's Driver field used to flash a raw, unparsed package label** (`"UFRII_v10.19.25_mac"`) for
  a `macModelDriven` manufacturer while a technician was still typing into the Model field, before
  settling once a real Model was actually selected - `setupCombobox`'s Driver auto-fill logic was wired
  to fire on every keystroke (`onChange`), which fed partial/non-matching text through
  `DriverCandidates`' own guess-based fallback. Moved to a new `onCommit` parameter (fired only when a
  value is actually committed - Enter or a dropdown pick, not every keystroke) that `setupCombobox` now
  accepts alongside `onChange`.
- **The Defaults panel's Subnet field had no input validation at all.** `isValidSubnetPrefix`
  (`frontend/src/main.js`) accepts blank (stays white, not flagged) or exactly 3 dot-delimited octets
  (each `0`-`255`, no leading zeros, an optional trailing dot) and flags anything else with the same
  yellow `input-needs-value` styling used elsewhere in the grid - wired on the field's own `input` event,
  same on both platforms.

### Selective Canon UFR II install and one-prompt batching (v0.6.5-v0.6.9)

A real Canon UFR II install (the full `installer -pkg <Distribution>.pkg -target /`) took a
confirmed-live **5m02s** and, worse, prompted for the admin password once per row even when
several rows shared the identical package. Two changes fixed this, in order:

**1. Install only what's load-bearing for the one model being deployed.** Inspecting a real UFR II
package (`pkgutil --expand`, read-only, no install) found a `Distribution` wrapping 5 sub-packages
totaling **22,900 files / ~224MB**: `Core` (the real driver framework/backend/PDE filter binaries -
9,548 files, genuinely needed), `Device` (7,146 files: **one PPD + one per-model "Recipe" bundle
for each of 549 models the family supports** - only one pair ever needed per row), and
`Icons`/`Profiles`/`cnaccm` (cosmetic icons, ICC color profiles, the Canon Accounting Manager
Client utility - 6,206 files total, none required for functional duplex/color/network/finishing-
feature printing, all of which live directly in the PPD's own `*OpenUI` options: confirmed live
that a real Canon PPD declares `*CNFinisher`/`*CNPuncher`/`*CNFolder`/`*CNSaddleStitch`/
`*CNVfolding`/`*CNCopyTray` directly). `canoninstall_darwin.go`'s `installCanonSelective` now
installs Core for real and `cpio`-extracts just the one target PPD + matching Recipe bundle out of
Device's own Payload (`driver.ExtractCanonDeviceFiles` - three patterns: the PPD, the whole bundle
tree, and a sibling `Recipe/<model>.rcp` symlink into it, found by diffing a real BOM, not
guessed), skipping Device's installer run and Icons/Profiles/cnaccm entirely. Two real bugs
surfaced getting this working, both confirmed live before being fixed: `installer -pkg` rejects a
`pkgutil --expand`-produced sub-package directory outright (reproduced the identical error with no
privilege at all, proving it's a format issue, not permissions) - fixed by re-flattening it with
`pkgutil --flatten` first; and a plain `cp -R src/. /Library/` fails even as root ("unable to copy
extended attributes to /Library/.: Operation not permitted", since `cp -R` also tries to copy the
*source directory's own* attributes onto the destination directory entry itself) - fixed with `-X`
(don't copy extended attributes; none of these freshly-extracted files carry any worth preserving
anyway).

**2. Batch every row's privileged work into one elevated call per deploy run.** Even after (1),
each row still cost up to 2 separate native password prompts (one for its own install, one for its
own queue-create) - confirmed live, repeatedly, that `do shell script ... with administrator
privileges` never reuses a recent grant, no matter how little time passed between two separate
calls. `printer.BatchPreparer` is an optional `Deployer` interface extension
(`PrepareBatch(ctx, reqs, confirm)`, checked via a type assertion in `DeployAllWithProgress` -
Windows' own Deployer doesn't implement it, a no-op there) that macOS's own
`canonbatch_darwin.go` uses to do a first, entirely unprivileged pass over every row - resolving
each one's package, extracting its staged files, computing its queue name/device URI/print-
defaults args - then combining every *batchable* row's own commands into one script and running
it through **one** elevated call for the whole run, still deduplicating each unique package's own
Core install. Deliberately narrow scope: only a row that resolves to a catalog-driven Canon UFR II
package with no existing queue to reuse gets batched; everything else (a different manufacturer,
the guess-based fallback, a loose-PPD family, or an existing queue) falls back to the old per-row
path, own separate prompts included - the one path proven correct end to end across several
live-tested rounds, covering every row actually tested so far. Per-row success/failure comes back
through a plain results file, not by parsing the combined call's own stdout - confirmed live
(twice) that `do shell script` silently mangles every `\n` in captured output to `\r`, and buffers
a command's entire output until it fully exits regardless of how many pipe stages run inside the
script, both real problems for structured multi-row output that reading a file off disk afterward
sidesteps entirely. **Confirmed live: 1 auth prompt for a 2-row same-package Canon deploy**, both
rows created successfully, print defaults correct - the accepted trade-off (every batched row's
result becomes known only once the one combined call returns, not streamed in per-row as it
otherwise would be) showed up in the log as a real but expected pause before either row's own
result appeared, not a bug.

An earlier attempt at a *different* improvement - real-time phase-by-phase install progress/timing
via `installer -verboseR` piped through the same elevated call - was built, broke three separate
ways across three live tests, and was ultimately abandoned as an architectural dead end: confirmed
live (via fast, harmless synthetic tests rather than more real 5-minute installs) that `do shell
script`'s own privileged-execution mechanism buffers a command's *entire* output until it fully
exits no matter how many pipe stages run inside the script - there is no way to get genuine
real-time progress or timing out of it at all. Real-time progress during a privileged operation
would need a fundamentally different mechanism (e.g. an elevated script writing to a file an
unprivileged goroutine tails independently, entirely bypassing `do shell script`'s return value) -
not attempted, since the actual speed fix in (1) above didn't end up needing precise timing data to
justify itself.

### `cmd/pdtdebugmac`

The macOS analog of `cmd/pdtdebug` - `catalog`/`models`/`installpkg`/`deployqueue` commands for
exercising the catalog/model-index/install/queue-creation codepaths by hand against real state,
validated before the Wails UI could drive them directly. `installpkg`/`deployqueue` run real
privileged commands and prompt for the admin password the same way a real Deploy does. `models
<driversRoot>` builds the real `BuildMacModelIndex` and prints every manufacturer/model/variant it
finds, plus what `App.Models`/`App.DriverCandidates` would actually return for a blank Model and
filter - what surfaced a real, previously-shipped bug precisely: the model index itself was
indexing real data correctly the whole time (641 real Canon models, confirmed live), while
`MacModelCandidates` alone returned nothing for a blank Model (see "Model-driven PPD selection on
macOS" above).

### `package main` on darwin

`app.go`'s Windows-only pieces are split into `app_windows.go`/`drivercatalog_windows.go`/
`update_windows.go`/`openfolder_windows.go`, each with a `_darwin.go` counterpart where one makes
sense; `spooler.go`/`devmode.go`/`sevenzip.go` are renamed outright to `_windows.go` (Print Spooler
control, DEVMODE/Device Settings capture, and the bundled-7-Zip tooling for self-extracting Windows
archives all remain Windows-only - no CUPS/macOS equivalent built yet). `App.Platform()`
(`runtime.GOOS`) is the frontend's one feature-detection signal, gating the reduced macOS UI described
above (see `frontend/src/main.js`'s own `isMac()`/`state.platform`).

Flash Drive/Sync are fully ported too (`internal/flashdrive/flashdrive_darwin.go`) - removable-drive
enumeration and exFAT formatting via `diskutil` (its own `RemovableMediaOrExternalDevice` field,
converted from plist to JSON via `plutil` for reliable parsing) and `syscall.Statfs` for free/total
space, in place of Windows' `GetDriveType`/`GetDiskFreeSpaceEx` family.

## Drivers folder layout

`driver.BuildCatalog(driversRoot)` (`internal/driver/catalog.go`) expects:

```
Drivers/
  Windows/
    11/                          <- any name; every folder under Windows/ is scanned and merged
      Canon/...
      HP/...
      Konica Minolta/...         <- or "KonicaMinolta" - see naming below
      Kyocera/...
      Ricoh/...
      Sharp/...
      Toshiba/...
      Xerox/...
```

Every folder found directly under `Drivers/Windows/` is scanned and merged into one catalog - a
printer driver is rarely genuinely Windows-version-specific the way it can be for macOS (see below),
so there's no attempt to detect/match the running Windows version to a specific folder. Each
manufacturer folder's own internal structure (multi-version, multi-arch, `Archive` subfolders
excluded) has no required layout beyond that - `BuildCatalog` recursively walks every subfolder
looking for `.inf` files, wherever they end up nested.

**Manufacturer folder naming**: matched against `driver.Manufacturers` case-insensitively *and*
space-insensitively (`foldMatchIgnoringSpaces`) - confirmed necessary against the real Drivers
folder, where "Konica Minolta" (this app's own display name, spaced for readability everywhere it
shows up in the UI) sits on disk as `KonicaMinolta`, no space. Either spelling works; a folder name
that doesn't fold-match any entry in `driver.Manufacturers` at all is silently skipped, not an error -
useful if you keep other, unsupported manufacturers' packages in the same Drivers tree.

**Every manufacturer is offered regardless of whether its drivers are present locally.** The Defaults
panel's Manufacturer dropdown (and each grid row's) lists all of `driver.Manufacturers`
(`App.Manufacturers()`), same set Settings > External Sites uses (`App.AllManufacturers()`, just
alphabetical instead of the user's own drag/drop order there) - deliberately not filtered down to
`driver.ManufacturersWithDrivers`, so a brand-new install with an empty Drivers folder can still pick
a manufacturer and use **Check for Updates** to reach its download page (see the banner PDT shows on
launch when no manufacturer has any driver present yet). Picking a manufacturer with nothing in its
Drivers folder just leaves the Driver field with no candidates to offer yet - not an error, just
nothing to deploy from until a driver package is downloaded there.

**Back-compat**: if `driversRoot` has no `Windows` subfolder at all, it's treated as the older flat
layout (`Drivers/<Manufacturer>/...` directly) - this is what the unit tests under
`internal/driver/testdata/` still use, and what an old, pre-reorg `Drivers` folder would still work
against unmodified.

**`.zip` packages are extracted automatically** (`internal/driver/zip.go`, `ensureZipsExtracted`,
called before each manufacturer folder is scanned): confirmed necessary against a real package
(Sharp's UD3 driver ships as `UD3_07_PCL6_2510a.zip`) - `BuildCatalog` only ever looks for `.inf`
files already sitting on disk, so a driver that's never been extracted is otherwise completely
invisible to it. `Foo.zip` extracts to a sibling `Foo/` folder the first time it's seen; if that
folder already exists (however it got there - this, or a manual extraction), it's left alone and not
re-extracted. A zip that fails to extract (corrupt, or an entry that would land outside the
destination folder) is skipped rather than failing the whole catalog scan, and any partial output is
cleaned up so a later run - once whatever's wrong is fixed - retries instead of mistaking a partial
extraction for a complete one. **Kyocera no longer ships `.zip` packages at all (as of roughly
2026)** - see "Kyocera: self-extracting .exe packages" below for how to get an equivalent already-
extracted folder onto disk by hand, since there's nothing here that can extract a self-extracting
`.exe` automatically.

**Two drivers, one entry - preferring the descriptive/versioned name over a generic alias**: several
vendors' INFs register the exact same underlying driver under more than one friendly name - a vague,
manufacturer-less alias alongside a properly branded one (Ricoh: `"PCL6 Driver for Universal Print"`
next to `"RICOH PCL6 UniversalDriver V4.45"`), or a generic branded name alongside a version-numbered
one (Xerox: `"Xerox Global Print Driver PCL6"` next to `"Xerox GPD PCL6 V5.1076.4.0"`; Konica
Minolta: `"KONICA MINOLTA Universal PCL"` next to `"...Universal PCL v3.9.13"`). PDT always prefers
whichever name is more descriptive - the one carrying the manufacturer's own brand and/or an actual
version number - the same way you'd pick it by hand from the list Windows' own driver-install dialog
shows for that INF:
- A name with **no manufacturer identification at all** (Ricoh's vague alias) is filtered out of the
  catalog entirely - `isUsableDriverName`/`vagueNameFilterBrand` in `catalog.go` - it never appears as
  a selectable option at all, since nothing about it identifies which vendor's driver it even is.
- Between two **branded** names that are otherwise the same driver, the Defaults panel's own
  pre-selected default (`DefaultDriverNameFor` in `default.go`) prefers the one carrying a version
  number, and - when there genuinely are multiple different versions on disk at once - the newest one.
  Both names stay selectable in the Driver dropdown either way; this only decides which one is
  pre-filled.
- **This preference is not a one-time snapshot.** `DefaultDriverTokens` matches by token, not exact
  string, deliberately excluding the version number itself - so "Xerox GPD PCL6 V5.1076.4.0" today
  keeps resolving correctly once a newer `V5.1078.x.x` (or whatever the next one is called) replaces
  it on disk, with no code change needed. The same holds for Ricoh and any other manufacturer whose
  preferred driver's own name embeds a version number.
- **A newer date doesn't always mean "the newer version of the same driver."** Confirmed against the
  real Lexmark package: alongside "Lexmark Universal v2" there's a genuinely different, more
  specialized "Lexmark Universal v2 XL" (an extra-large-format variant, and the one actually
  preferred here) built two days later - a different product, not a newer build of the other one.
  Lexmark's own token rule requires "XL" specifically to settle which one is meant, but for a
  *future* manufacturer with a similar surprise before its tokens get tightened the same way,
  `DefaultDriverNameFor`'s tie-break still matters: when neither name carries a version number of its
  own, it prefers the *shorter* matching name (a name that's a superset of another, with an extra
  qualifier tacked on, is presumed to be the more specialized variant) before ever considering date -
  see its doc comment in `default.go` for the complete tie-break order.

**Default driver per manufacturer** (`internal/driver/default.go`, `DefaultDriverNameFor`): the
Defaults panel pre-selects a specific driver name when a manufacturer is chosen, matched by token
presence (case- and whitespace-insensitive, order-independent) against the real catalog rather than
an exact string - confirmed necessary since vendors aren't consistent about it even within this one
Drivers folder ("PCL 6" vs "PCL6", and Sharp's own driver is literally named "SHARP UD3 PCL6", tokens
reversed from how "PCL 6 UD3" reads out loud). Today's rules:

| Manufacturer | Preferred driver | Token match |
|---|---|---|
| Canon | Canon Generic Plus UFR II | UFR, II |
| HP | HP Universal Printing PCL 6 | PCL, 6 |
| Ricoh | RICOH PCL6 UniversalDriver V*x.xx* | PCL, 6 |
| Sharp | SHARP UD3 PCL6 | PCL, 6, UD3 |
| Toshiba | TOSHIBA Universal Printer 2 | Universal, Printer, 2 |
| Xerox | Xerox GPD PCL6 V*x.xxxx.x.x* | GPD, PCL, 6 |
| Konica Minolta | KONICA MINOLTA Universal PCL v*x.x.xx* | Universal, PCL |
| Lexmark | Lexmark Universal v2 XL | Universal, v2, XL |
| Kyocera | *(no rule - pick per model instead; see below)* | - |

Kyocera has no manufacturer-wide default: its driver *names* are per-model (`"Kyocera <model> KX"`),
so the Defaults panel's Model field narrows the Driver dropdown instead of pre-filling one fixed name.

**Am I missing any major brands?** These eight cover the large majority of enterprise MFP fleets.
**Brother** was considered and deliberately left out: it lacks a universal print driver compatible
with most of its larger models, and its lineup skews home/small-office rather than the fleet-deployment
scale this tool is for. Beyond that, there isn't an obvious major brand still missing - if one comes
up, add a `Drivers/Windows/<version>/<Manufacturer>/...` folder with a real package and ask for it to
be wired up (a `driver.Manufacturers` entry, a `defaultDriverTokens` rule once you know the real
driver name, and a default URL in `settings.go`) the same way Toshiba/Xerox/Konica Minolta/Lexmark
were.

### Lexmark: self-extracting RAR + `.msi`-packaged drivers

Lexmark's package is a **self-extracting RAR archive** (confirmed by its `Rar!` signature, not a ZIP
or 7z) with its actual driver files packaged inside `.msi` installers one level in.

**The RAR layer is auto-extracted** (`internal/driver/sfx.go`, `ensureSfxArchivesExtracted`) - unlike
`.zip` extraction, this can't use Go's standard library (it has no RAR reader at all), and the one
pure-Go RAR library evaluated (`nwaples/rardecode`) was found to silently corrupt exactly the `.msi`
files this needs (confirmed by feeding its output to `msiexec`, which rejected it as an invalid
package - `ERROR_INSTALL_PACKAGE_INVALID`). 7-Zip's own easily-redistributable "Extra" console-only
package doesn't include RAR support either (confirmed directly - it errors "Cannot open the file as
archive"; only the full `7z.dll` does). What actually works, and is what PDT bundles: `7z.exe` +
`7z.dll` (~2.5MB total) copied out of a full 7-Zip install with no installer needed - confirmed these
two files run completely standalone. They're embedded directly into `PDT.exe` (`sevenzip.go`,
`go:embed third_party/7zip/...`) and extracted once to `%LocalAppData%\PDT\tools\7zip\` at startup;
`BuildCatalog` then auto-detects any `.exe` containing a RAR, 7z, or Zip signature
(`isSelfExtractingArchive` - a byte-signature scan, not a naming convention, so it works for a future
self-extracting package from any manufacturer) and extracts it via the bundled `7z.exe` - which
auto-detects the exact format itself, so nothing downstream needs to know which one matched - the same
skip-if-already-extracted convention as `.zip` auto-extraction. Confirmed live against a real
self-extracting **7z** package too: Konica Minolta's own driver ships as a `7z.sfx.exe` stub with the
real archive simply appended after it (same layout as Lexmark's RAR, different signature), extracted
correctly with no Kyocera-style special casing needed (see below). Redistributing `7z.exe`/`7z.dll` is
permitted under 7-Zip's own license (LGPL + an "unRAR restriction" that only bars using the code to
build a RAR *compressor*, not redistributing the decoder) - `third_party/7zip/License.txt` travels
with the binaries per that license's own terms.

**The `.msi` layer past that point is also auto-extracted** (`internal/driver/msi.go`,
`ensureMsiExtracted`) - unlike the RAR layer, this needs no bundled tool at all, since `msiexec.exe`
and `expand.exe` are both already part of Windows itself. For every `.msi` `BuildCatalog` finds (e.g.
`print64PCL.msi` under `InstallationPackage\Drivers\x64\`, reachable only after the RAR layer above
has already been unpacked), it:

1. Runs an MSI **administrative install** to unpack it with real filenames/paths intact - **this does
   not install anything**, it only extracts:
   ```
   msiexec /a "print64PCL.msi" /qn TARGETDIR="<sibling folder>"
   ```
   This alone gets the main `.inf` out correctly (`LMUD1o40.inf`, not the mangled `LMUD1o40inf` a
   plain archive-tool extraction of the `.msi` produces) - the `.msi`'s own file table maps its
   internal mangled CAB entry names back to real ones, which only an administrative install (not a
   generic un-zip/un-cab tool) actually reads. The result lands under
   `<sibling folder>\Lexmark\Lexmark Universal v2\Drivers\Print\GDI\` - the real `.inf` alongside its
   `.dl_`/`.gd_`/`.gp_`/`.tx_`/`.in_`/`.xm_`/`.pn_`/`.ex_` compressed siblings (Microsoft's legacy
   single-file-compressed form) and `amd64`/`i386` subfolders of compiled binaries the INF references.
2. Decompresses every compressed sibling with Windows' own `expand.exe`, **using `-R` (restore
   original name) rather than guessing the real extension from the compressed one**. This matters more
   than it looks: the extension-to-extension mapping isn't the simple 1:1 scheme it looks like at a
   glance - `.gd_` decompresses to `.gdl` and `.in_` to `.ini` here, *not* to `.gpd`/`.inf` as their
   names suggest (there's a genuinely separate `.gp_` -> `.gpd` pair too). An early pass at this
   guessed the mapping instead of asking `expand.exe`, and paid for it: the wrong guess silently
   overwrote the `.msi`'s own correctly-extracted `.inf` with unrelated `.ini` content sharing the
   same assumed filename, and left a genuinely-required `.gdl` file missing entirely - both errors
   `SetupCopyOEMInf`/`BuildCatalog` tolerated quietly enough at staging time to look like success,
   while `AddPrinter` failed outright the moment something tried to actually use the driver
   (`ERROR_CAN_NOT_COMPLETE`) - a good example of why "it staged with no error" isn't the same as "it
   actually works." `-R` sidesteps the whole problem by asking `expand.exe` itself, which reads the
   real name straight out of the compressed file's own header instead of guessing from its extension.

Putting it together: drop the downloaded `Lexmark_..._Installation_Package_*.exe` directly into
`Drivers\Windows\<version>\Lexmark\` and run PDT once - the RAR layer, then every `.msi` inside it,
extract automatically with no manual steps at all. Confirmed end to end against the real package: the
resulting `.inf` is found and cataloged correctly, and driver install, printer creation, duplex/color,
and APF all work through PDT's existing, unmodified `SetupCopyOEMInf`-based install path
(`driverinstall_windows.go` - the exact same mechanism used for every other manufacturer here, no
Lexmark-specific code needed).

**One real surprise worth knowing about**: Lexmark's package contains more than one product variant
sharing a base name - alongside `print64PCL.msi`'s "Lexmark Universal v2" there's also
`print64XL.msi`'s "Lexmark Universal v2 XL" (an extra-large-format variant), built two days later.
Auto-extracting *all* the `.msi` files in the tree means both show up in the catalog, and **XL is the
one actually preferred** here, so `defaultDriverTokens["Lexmark"]` requires "XL" specifically (not
just "Universal"/"v2", which the base driver also matches) to select it. The base driver stays fully
selectable in the Driver dropdown either way - this only decides which one is pre-filled. Finding this
also exercised a real, more general tie-break question worth knowing about for any *future*
manufacturer with a similar surprise: `DefaultDriverNameFor` prefers the shorter of two
same-token-matching names over a merely-newer one that carries no version number of its own, rather
than trusting "newest date" blindly (see its doc comment in `default.go` for the full tie-break order,
and the README's own note in "Default driver per manufacturer" above).

### Kyocera: self-extracting `.exe` packages

Kyocera stopped shipping `.zip`-packaged drivers roughly 8 months before this was written; current
downloads are a self-extracting `.exe` that launches Kyocera's own installer UI instead of just
unpacking to a folder. Three ways to get an already-extracted folder onto disk under
`Drivers\Windows\<version>\Kyocera\`, all ending at the same result - a folder full of `.inf` files
and friends, exactly like every other manufacturer's already-extracted package:

**Method 1 - automatic (`internal/driver/kyoceraexe.go`).** `BuildCatalog` now does this for you: on
every PDT startup, `ensureKyoceraExesExtracted` looks directly inside each `Drivers\Windows\<version>\
Kyocera\` folder for a `.exe` matching Kyocera's current naming (`KXDRIVER 8.6A.1412.exe`,
`KXDriver_8.6.1022.exe`, etc. - `kyoceraExeNameRe`, case-insensitive), pulls the version token out of
the filename, and - unless a sibling folder's name already contains that same version token - runs the
identical two-stage 7-Zip extraction Method 2 describes by hand, landing the result in a new
`KXDriver_<version>` folder right next to the `.exe`. So the real manual step, in practice, is just:
drop the freshly downloaded `.exe` directly into the right `Drivers\Windows\<version>\Kyocera\` folder
and start PDT once - no scratch folders, no running the installer. Uses the same bundled `7z.exe` as
the Lexmark/Konica Minolta self-extracting-archive auto-extraction (`ensureSfxArchivesExtracted`) and
is equally a no-op if
`driver.SevenZipPath` isn't set (`go test`, `pdtdebug`, or extraction failing for that one package
never blocks the rest of the catalog scan - the same "best-effort, clean up and retry next time"
convention every `ensure*Extracted` helper in this package already follows). Methods 2 and 3 below
remain useful as a manual fallback (a driver naming variant the regex doesn't recognize, or wanting to
inspect the raw extraction yourself).

**Method 2 - extract with 7-Zip by hand, no installer run at all.** A Kyocera "self-extracting" `.exe`
is actually a normal PE executable with a large embedded archive resource; 7-Zip can pull that
resource out directly without ever launching the installer:

1. Make a scratch folder (e.g. `Downloads\temp`) and copy the downloaded `.exe` into it (e.g.
   `KXDRIVER 8.6A.1412.exe`).
2. Right-click it -> 7-Zip -> Extract to "*foldername*\". This produces a handful of files, one of
   which - always named `.text` - is many times larger than the rest (hundreds of MB): that's the
   embedded archive itself, and everything else in that first extraction is installer scaffolding to
   discard.
3. Move just the `.text` file into a second, empty scratch folder (keeps its contents from mixing
   with the first extraction's leftovers) and 7-Zip-extract it too. This second extraction is the
   real driver data - `Setup.exe`, `KmInstall.exe`, a `32bit`/`64bit`/`arm64` split, `Document`,
   `MetaData`, etc.
4. Rename that folder to something version-identifying (e.g. `KXDRIVER_8.6A.1412`) and move it into
   `Drivers\Windows\<version>\Kyocera\`.

**Method 3 - let the installer extract, then take its temp copy before it does anything else.**

1. Run the downloaded `.exe`. When Kyocera's "Product Library" installer window appears, **stop -
   don't proceed with the install.**
2. Open File Explorer and go to `%LocalAppData%`. Find a `KX Driver` folder, and inside it an
   `originalfiles` folder (there's also an `originalfiles.zip` alongside it - the folder, already
   extracted, is the one you want).
3. Copy `originalfiles` into `Drivers\Windows\<version>\Kyocera\` and rename it to something
   version-identifying (e.g. `KXDRIVER_8.6A.1412`).
4. Exit the Product Library installer without installing anything.

A few things worth knowing before relying on either manual method:
- The installer's own temp extraction (Method 3) has been observed to survive under `%LocalAppData%`
  even after exiting the installer without installing - useful, since it means you can grab a copy
  after the fact if you forgot to before closing it, but not guaranteed to hold true for every
  Kyocera installer version; if `KX Driver` isn't there, you'll need Method 2 instead, or to retry
  Method 3 and copy the folder out *before* exiting the installer.
- Installers generally extract to `%LocalAppData%\Temp`, not `%LocalAppData%` itself, so if a future
  Kyocera installer version relocates this, checking under `Temp` first (or using Sysinternals'
  Process Monitor to watch what the installer actually writes and where) is the way to re-find it.

**macOS is not read by this function at all** - `BuildCatalog` only ever reads the Windows side; the
macOS side of the Drivers tree has its own scanner now (`driver.BuildMacCatalog`, see "macOS support"
above), and nests the *other* way - `Drivers/macOS/<Manufacturer>/<macOS version>/...` (version under
manufacturer, not manufacturer under version like Windows) - reflecting that macOS driver packages
genuinely do vary by OS release in a way Windows ones generally don't. There is also a
`Drivers/macOS/OpenPrinting/<Manufacturer>/*.ppd` bucket (flat, no version breakdown) as a fallback
source for manufacturers/models with nothing better - both are described in full in "macOS support"
above, including how `.dmg`/`.pkg` extraction actually ended up working out (mounting via `hdiutil`,
no bundled tool needed - simpler than the Windows side's own 7-Zip/msiexec/expand.exe pipeline, since
macOS packages need no pre-extraction at install time at all).

**`ensureMacDriversScaffold` (`driversfolder.go`), the darwin analog of `ensureDriversScaffold`
above**, runs at startup too, but isn't a straight port - it only ever ensures a bare
`Drivers/macOS/<Manufacturer>` folder exists for every entry in `driver.Manufacturers` (there's no
one macOS version it could hardcode the way `Windows/11` is hardcoded above, since macOS driver
packages genuinely do vary by release) and retroactively backfills `Archive/README.txt` into
whatever version folders already exist under each manufacturer - it never invents a version folder
itself.

## Deploy sequence (`internal/printer/windows/deploy_windows.go`)

`Deployer.Deploy` ports `Create-Printers.ps1`'s `Deploy-PrinterRow`, in the same order (port ->
driver -> printer object -> conditional rebind -> print config -> APF), with one deliberate
simplification versus the original: there is no `BindNulPort` checkbox. Instead:

- Typing `NUL` (or `NUL:`) as a row's IP permanently binds it to the local `NUL:` port (no network
  I/O at all) - for a placeholder/test row.
- Any row whose resolved driver is HP's Universal Print Driver family, or whose manufacturer is
  Kyocera (any driver), automatically gets the create-against-`NUL:`-then-rebind-to-the-real-port
  treatment with **no user toggle** - `printer.RequiresNulPortWorkaround` - since both have been
  directly observed to take noticeably longer to create against a live TCP/IP port than against
  `NUL:` (HP's Universal family: several minutes vs. a few seconds).

**Fatal vs. warning, per spec**: creating or updating the printer object itself (port resolution,
driver install, `CreatePrinter`/`SetInfo2`, the NUL:-to-real-port rebind) is the one thing that
matters most - failing any of those steps is fatal *for that row only* (`DeployResult.Err`, logged
`[ERR]`) and `printer.DeployAll` always continues to the next row regardless. Everything after the
object exists - duplex/color, advanced printing features, "Print spooled documents first" - is
best-effort: a failure there is a `[WARN]`, never stops the row, and all of them are always attempted
even if an earlier one already failed.

**Duplex/color is set two ways, deliberately** - `SetDuplexAndColor` (raw DEVMODE) *and*
`SetPrintConfigurationViaShell` (shells out to `Set-PrintConfiguration`). Confirmed necessary against
a real Canon UFR II printer: DEVMODE alone was already correct (verified via .NET's own
`PrinterSettings`, independent of this tool), yet the printer's own Properties dialog and
`Get-PrintConfiguration` still showed the opposite settings - they read from a separate
PrintTicket-based store DEVMODE never touches, apparently left at the driver's install-time default
until something updates it explicitly. Both calls target the same end state, so this is redundant on
drivers where DEVMODE alone would have been enough - the cost is one extra `powershell.exe` launch
per row, worth it since there's no way to tell in advance whether a given driver needs it.

**"Print spooled documents first"** (`SetPrintSpooledDocumentsFirst`, `PRINTER_ATTRIBUTE_DO_COMPLETE_FIRST`)
is enabled unconditionally for every row - unlike APF, there's no per-row checkbox for it; it's just
a fixed default every deployment gets.

Every log line is timestamped and level-tagged (`internal/printer/log.go`'s `Logger`:
`[INFO]`/`[OK]`/`[WARN]`/`[ERR]`) so the UI can show a row's progress directly with no further
formatting.

### Driver version handling

- **Plain name** selection (the common case): silently upgrades an older installed driver to the
  newer local package; never silently downgrades.
- **An explicit version pin** (one of the driver catalog's `<name> (vVersion - date)` labels):
  honored upgrade *or* downgrade, but only after a confirmation dialog, since Windows shares one
  driver registration per name across every printer already using it. Declining leaves the
  currently-installed version in place (`[WARN]`, not fatal).

### Existing printer objects

If a printer with the row's exact name already exists, its current driver/port/comment are diffed
against what this row would set; a confirmation dialog lists exactly what would change (any
combination of the three, or none - no prompt at all if nothing would actually change). Declining
leaves it completely untouched; print configuration and APF still apply independently either way.

### Port resolution / `UseExistingPort`

Every row first checks whether a Standard TCP/IP port already targets its IP (registry-based,
`portlookup_windows.go`) and reuses it if so - this also doubles as the fix for ever creating a
genuine duplicate port for a host that already has one, e.g. on redeploy. Otherwise a new port is
created, named `<prefix><ip>` (or just `<ip>` with no prefix set), with a numeric suffix appended
only if that exact name is already in use by a *different* host - including when `UseExistingPort`
is checked but no existing port targets the IP: that's a fallback to creating one, not a failure.

## Frontend (`app.go`, `frontend/src/`)

Plain HTML/CSS/vanilla JS (no framework) - a single-page grid mirroring the original tool's layout:
top bar (SalesChain ID, Open/Save Configuration), a Defaults panel (Manufacturer/Model/Driver, then
the **Port** / **Print Defaults** / **Advanced** subsections, all used by "Add Printer"), a toolbar
(Add Printer/Remove Selected on the left, New CSV/Import CSV, then Deploy on the right), the row grid
itself, and a live log panel.

**Model and Driver are a small custom combobox** (`setupCombobox()` in `main.js`), not a native
`<input list=...><datalist>` - datalist only offers suggestions once the user starts typing (no
"click to see everything available" the way the original WinForms ComboBox did) and its filtering is
inconsistent across browsers, so it didn't actually deliver the original "type to filter" feel.
Every instance (Defaults panel and each grid row) owns its own DOM elements and closure state, so -
unlike the WinForms `DataGridView` bug that forced a full rewrite of the original tool's grid, rooted
in cells sharing one live editing control - there's no shared state for one row's combobox to leak
into another's. Model candidates are filtered client-side (substring match) against `Models()`'s
full per-manufacturer list; Driver candidates are forwarded straight to `DriverCandidates()`, which
already fuzzy-filters/ranks server-side.

`app.go`'s `App` struct is the only thing the frontend talks to (Wails auto-generates
`frontend/wailsjs/go/main/App.d.ts`/`.js` from its exported methods on every `wails build`/`wails dev`
- regenerate with `wails generate module` after changing that struct's method set). Two things shaped
its design:

- **Wails only supports a bound method returning `(T)` or `(T, error)`** - never more outputs, and
  anything else is silently dropped - so every method that can both fail *and* needs a second signal
  (a canceled file dialog, a row's error message) wraps its result in a small DTO (`PathResult`,
  `ImportResult`, `DeployRowResult`, ...) instead.
- **A bare Go `error` value doesn't JSON-marshal its message at all** (most concrete error types have
  no exported fields - it would cross the wire as `{}`), so `DeployRowResult.Error` is a plain
  `string` (`""` on success), never a Go `error`.

Deploying streams live progress: `Deploy` emits a `"deploy-progress"` event (one `DeployRowResult`)
after each row finishes - necessary since a single HP row alone can run for minutes even with the
NUL: workaround declined - in addition to returning every result once the whole run completes.
Confirmation dialogs (driver-version change, printer update) are native OS Yes/No message boxes
(`runtime.MessageDialog`), the same kind of blocking modal the original tool used (a WinForms
`MessageBox`), just through Wails' cross-platform equivalent. The driver catalog is built once at
startup from a `Drivers/` folder next to the running executable (falling back to `./Drivers` under
the working directory for `wails dev`).

The grid never does a full-table re-render while a deploy is running or while progress events are
arriving - only the specific row a progress event is about gets its success/failure class toggled,
by a stable per-row `_id` rather than by array position or by name (which the tool has never required
to be unique). A full re-render there would destroy focus and in-progress edits in any other row the
user might be editing while a multi-minute deploy is still running elsewhere in the grid.

**The log panel has its own right-click context menu** (Select All / Copy / Clear Log,
`wireLogContextMenu()` in `main.js`) rather than the browser/webview's native one - positioned at the
cursor via the `contextmenu` event, dismissed on an outside click or Escape, the same transient-popup
shape the Spooler dropdown and Model/Driver combobox already use elsewhere in this file. Select
All/Copy always act on the log's own full text (`state.logLines` joined the same way `renderLog`
itself joins them), not whatever happened to be selected when the menu was opened - closer to what a
technician pasting a log into a support ticket actually wants. Copy tries the async Clipboard API
first, falling back to a hidden-textarea `execCommand('copy')` for a webview context where that API
might be restricted.

### Settings (gear icon, top-right)

A modal with three tabs: **General** (Save File Base Path, plus a drag-and-drop **Manufacturer sort
order** list - see below), **External Sites** (one editable URL field per manufacturer - every
manufacturer PDT knows about, not just ones with drivers currently on disk; see "Drivers folder
layout" above - seeded with `defaultManufacturerURLs` in `settings.go`, saved together with the rest
of Settings), and **About** (version, author, a clickable GitHub link, and **Check for Updates** - see
below). The modal is a fixed size regardless of which tab is showing or how many manufacturers there
are - both External Sites and the sort-order list scroll internally rather than growing the window
once they're taller than that fixed size. The Defaults panel's own
**Check for Updates** button (a different one - printer driver updates, not app updates) opens the
currently-selected manufacturer's configured URL in the system browser (`OpenManufacturerURL` ->
`runtime.BrowserOpenURL`) - no vendor exposes an API to actually check the latest driver version, so
this only ever hands a human the page to look at themselves; true automated version-checking would
mean scraping each vendor's download portal individually; fragile, and high-maintenance per vendor, so
deliberately out of scope here.

### Manufacturer sort order (Settings > General)

A plain HTML5 drag-and-drop list (no external library) of every manufacturer PDT knows about,
persisted as `Settings.ManufacturerOrder` (`settings.go`) and applied by `App.Manufacturers()`
(`applyManufacturerOrder` in `app.go`) - this is what actually controls the order of the Manufacturer
dropdown in both the Defaults panel and every grid row (both read from the same `state.manufacturers`
in the frontend). Saving triggers `refreshManufacturerDropdowns()`, which re-fetches the reordered list
and rebuilds every already-rendered Manufacturer `<select>`'s options in place - the Defaults panel's
and each existing grid row's - preserving each one's current selection rather than resetting it.

**Settings > External Sites is deliberately exempt** - it's always alphabetical
(`App.AllManufacturers()` sorts it every time), regardless of this custom order, since its job is
finding a specific manufacturer to edit a URL for, not deployment convenience.

`reconcileManufacturerOrder` (`settings.go`) keeps the saved order valid across changes to
`driver.Manufacturers` itself: on load and on every save, it keeps the user's own ordering for
manufacturers still present, drops any name no longer recognized, and appends any manufacturer not yet
in the saved order (freshly added to `driver.Manufacturers`, or never dragged by this user) at the end
- so adding a ninth/tenth manufacturer later never causes it to silently disappear from either the
reorder list or the dropdowns.

### Checking for and applying app updates (`internal/update`, About tab)

Unlike driver updates above, PDT's *own* updates genuinely can be checked and applied automatically,
since this project's GitHub Releases are under our own control: About's **Check for Updates** queries
`GET /repos/keteague/PDT/releases/latest` and compares the release tag against `AppVersion`
(numerically, via `driver.CompareVersions` - a generic comparator despite living in the driver
package). If newer, **Update Now** downloads that release's `PDT.exe` asset and installs it in place
of the running executable, then relaunches it - see `internal/update`'s doc comment for how that works
with **no separate installer**: Windows lets a running executable's file be renamed out of the way
while it keeps running from the renamed file, which is enough to drop the new exe in at the original
name; the process finishes on its own a moment later and its renamed-away `.old` file is cleaned up
the next time the app starts.

**For this to find anything**, a release actually has to exist: bump `AppVersion` (`version.go`) and
`wails.json`'s `info.productVersion` together, `wails build`, then create a GitHub Release tagged
`v<AppVersion>` with `build/bin/PDT.exe` uploaded as a release asset named exactly `PDT.exe` (the exact
name `CheckForUpdate` looks for).

### Keeping the bundled 7-Zip up to date (`sevenzip.go`, About tab)

About also credits 7-Zip (by Igor Pavlov) - the tool bundled to auto-extract self-extracting RAR/7z/Zip
driver packages, see "Lexmark" above - and shows the version currently cached
(`GetSevenZipVersion`, parsed from `7z.exe i`'s own startup banner). **Check for 7-Zip Updates**
queries 7-Zip's own GitHub Releases (development now lives at `ip7z/7zip`, not `7-zip.org` directly)
via the exact same `internal/update.FetchLatest` PDT's own update check uses - the package was
already generic enough that checking a *different* project's releases needed no changes to it beyond
adding `Release.AssetMatching(re)`, since 7-Zip's own release asset names embed a version number
("7z2603-x64.exe") that can't be looked up by exact name the way PDT's own "PDT.exe" can.

**Update 7-Zip Now** does exactly what was tested by hand first: downloads that release's x64 GUI
installer, then uses the *currently cached* `7z.exe` to pull `7z.exe`/`7z.dll`/`License.txt` back out
of it directly - confirmed the installer is itself an extractable 7-Zip archive, no need to actually
run it as an installer - and overwrites the cached copies with the result. Extracts to a scratch
folder first and only overwrites the real cached files once that fully succeeds, so a bad download or
a failed extraction never touches the existing, working files. Verified end to end against the real,
live release.

### App identity (titlebar, icon, version)

`version.go` holds the single source of truth for the app's display name, version, author, and repo
URL (`AppVersion` must be kept in sync by hand with `wails.json`'s `info.productVersion`, since Go
can't read that value back out of the compiled resource at runtime). The titlebar (`main.go`) reads
"Printer Deployment Tool v0.1.0" from these same constants, and Settings > About displays them
directly. `build/appicon.png` and `build/windows/icon.ico` (the titlebar/File-Explorer icon) are
generated by `cmd/geniconassets`, a small standalone tool that draws the printer-glyph icon
procedurally - re-run `go run ./cmd/geniconassets` if the icon design itself ever needs to change.

### Startup readiness (`App.ready`)

Every method reading `catalog`/`modelIndex`/`settings` blocks on `<-a.ready` (closed once `startup`
finishes populating them) before proceeding. This turned out to be a real bug, not just defensive
coding: Wails does not block the frontend's own script from running until `OnStartup` returns, and
`BuildCatalog` scanning a real `Drivers` folder - particularly with zip extraction added - easily
takes longer than the frontend needs to fire its first catalog-dependent call (`DefaultDriverFor` on
page load), which was silently seeing `catalog`/`modelIndex` still at their nil zero value and
returning empty/wrong results with no error at all.

### On-launch scan skips extraction on removable media (Windows)

`loadCatalog` (`app_windows.go`) calls `driver.BuildCatalogNoExtract` instead of `driver.BuildCatalog`
whenever this exact running `PDT.exe` sits on a removable drive (`flashdrive.IsRemovableDrive`) -
`BuildCatalogNoExtract` does the same `.inf` scan but never runs any of the `ensure*Extracted` helpers
(`internal/driver/catalog.go`). Confirmed live: the extracting scan popped up a visible `expand.exe`
console window per Lexmark `.msi` and could take a long time over USB 2.0, even when every archive on
the drive was already extracted. This assumes the field workflow described under "Write to Flash
Drive"/Sync below - flash drives get a physical write-protect switch and are only ever written to from
a technician's local install, which always runs the full extracting `BuildCatalog`
(`postSyncDriversHook`) - so a USB-run copy can safely skip straight to reading already-extracted
`.inf`s. A local/fixed-drive install is unaffected either way.

## Building / testing

```
go build ./...
go vet ./...
go test ./...
```

`wails dev` / `wails build` build the actual application (see `wails.json`); the Go backend above has
no dependency on the frontend and is fully testable on its own.

### Building the installer (`installer/pdt.iss`)

```
wails build
iscc installer\pdt.iss
```

Requires [Inno Setup 6](https://jrsoftware.org/isinfo.php) (`iscc.exe` on `PATH`, or invoke it by full
path - `winget install JRSoftware.InnoSetup` is the fastest way to get it). Produces
`build\bin\PDT-Setup-<version>.exe`. Deliberately packages only `PDT.exe` itself - no `Drivers` folder,
which would bloat the installer for no benefit (driver packages are hundreds of MB each; see "Drivers
folder layout" above) since PDT already scaffolds an empty `Drivers\Windows\11\<Manufacturer>\`
structure on first launch regardless (`ensureDriversScaffold`, `driversfolder.go`) ready for a
technician to drop real packages into. Version numbering has one canonical source, the repo-root
`VERSION` file: `version.go` embeds it directly (`go:embed`), and `installer/pdt.iss` reads it at
compile time via its own preprocessor (`FileOpen`/`FileRead`) - a version bump only needs to edit
`VERSION` itself. The one holdout is `wails.json`'s `info.productVersion` (used for the compiled exe's
own Win32 version resource) - Wails has no mechanism to read it from elsewhere, so it still needs
updating by hand to match `VERSION` on every bump.

## Installing PDT

The installer is **elevation-optional by design** (Inno Setup's `PrivilegesRequired=lowest` +
`PrivilegesRequiredOverridesAllowed`, plus `DefaultDirName={autopf}\PDT` - `{autopf}` is Inno Setup's
own elevation-aware constant, resolving differently depending on how the installer itself ends up
running): double-clicking it normally runs **unelevated, no UAC prompt**, installing to
`%LocalAppData%\Programs\PDT`. Explicitly choosing "Run as administrator" instead installs to
`%ProgramFiles%\PDT`. Neither choice affects whether *PDT itself* elevates once installed - see this
README's very first section: `PDT.exe`'s own manifest always requests Administrator on launch,
regardless of which folder it's running from.

Either way, PDT's Drivers and Configs folders (`Settings.DriversBasePath`/`SaveFileBasePath`,
`settings.go`) default to `%LocalAppData%\PDT\Drivers` / `%LocalAppData%\PDT\Configs` - **not** wherever
the executable itself landed - since `%ProgramFiles%` isn't writable by an ordinary user, and using the
same location either way means both install modes behave identically once PDT is actually running (a
technician can drop a new driver package into `%LocalAppData%\PDT\Drivers` from an ordinary,
non-elevated Explorer window regardless of which mode PDT itself was installed in). Both paths are
editable in Settings > General if a different location is ever needed - Drivers Base Path takes effect
after restarting PDT (`BuildCatalog` only scans once, at startup); Configuration Files Base Path takes
effect immediately.

This default-path detection is shared with the portable/flash-drive case (see "Write to Flash Drive"):
`defaultDriversBasePath`/`defaultSaveFileBasePath` (`settings.go`) both check for a real, already-
populated `Drivers` folder sitting next to the running executable first - true for a flash drive with
`Drivers`/`Configs` already on it, false for a freshly-installed copy - before falling back to
`%LocalAppData%\PDT`.

The installer is unsigned (no code-signing certificate) - Windows SmartScreen will likely show an
"unrecognized app" warning on first run ("More info" -> "Run anyway"). This is expected for a small
internal tool without a paid signing certificate, not a build error.
