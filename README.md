# PDT (Printer Deployment Tool)

A Wails (Go + web frontend) rewrite of `Create-Printers.ps1`, the original PowerShell/WinForms
tool for bulk-deploying network printers (Standard TCP/IP ports, vendor drivers, print
configuration) from a CSV or saved JSON list of rows. This rewrite exists to get off the
PowerShell/WinForms DataGridView layer entirely while keeping every behavior the original tool's
real-world use already proved out - the Win32 mechanics below are ported (not reinvented) from
that tool's own hard-won findings, cross-checked against Microsoft's documentation and, wherever
that documentation didn't cover it, verified directly against this machine's real print spooler.

`PDT.exe` requests elevation on launch (`build/windows/wails.exe.manifest`) - every real operation it
performs needs Administrator, so Windows prompts for UAC automatically rather than the app starting
unelevated and failing partway through a deploy. There is no unelevated fallback mode.

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
- **Not started**: macOS/Linux support (`printer.Deployer` is implemented for Windows only; there is no
  `darwin`/other-OS stub yet).

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

**A manufacturer with no drivers present locally is not offered as a deployment option.** The
Defaults panel's Manufacturer dropdown (and each grid row's) only ever lists a manufacturer once its
folder actually has at least one usable `.inf`-declared driver in it; not every PDT install needs
every manufacturer's drivers, and there's no reason to offer one as a choice before its files are
actually there. This is `App.Manufacturers()` / `driver.ManufacturersWithDrivers` - **Settings >
External Sites is deliberately the one exception**: it lists every manufacturer in
`driver.Manufacturers` regardless (`App.AllManufacturers()`), so a manufacturer's update-check URL
can be configured before its drivers are ever added.

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
| Lexmark | Lexmark Universal v2 | Universal, v2 |
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
or 7z), so it can't be auto-extracted the way `.zip` packages are - unlike Kyocera's self-extracting
`.exe`, there's no zero-dependency way to unpack this one in Go (Go's standard library has no RAR
reader, and RAR5 support among third-party Go libraries is thin), so extraction is a manual,
one-time-per-version step, same in spirit as Kyocera's.

**Extraction procedure** (confirmed end to end against the real
`Lexmark_Universal_v2_UD1_Installation_Package_*.exe` package - driver install, printer creation,
duplex/color, and APF all verified working through PDT's own existing, unmodified code):

1. Extract the `.exe` with 7-Zip (it recognizes the embedded RAR archive directly, despite the file
   having a `.exe` extension and no visible RAR structure at a glance).
2. Find the manufacturer/model's `.msi` under `InstallationPackage\Drivers\<x64 or x86>\` (e.g.
   `print64PCL.msi` for the x64 PCL driver).
3. Run an MSI **administrative install** to unpack it with real filenames/paths intact - **this does
   not install anything**, it only extracts:
   ```
   msiexec /a "print64PCL.msi" /qn TARGETDIR="C:\some\empty\folder"
   ```
   This alone gets the main `.inf` out correctly (`LMUD1o40.inf`, not the mangled `LMUD1o40inf` a
   plain archive-tool extraction of the `.msi` produces) - the `.msi`'s own file table maps its
   internal mangled CAB entry names back to real ones, which only an administrative install (not a
   generic un-zip/un-cab tool) actually reads.
4. The result lands under `<TARGETDIR>\Lexmark\Lexmark Universal v2\Drivers\Print\GDI\` - the real
   `.inf` alongside its `.dl_`/`.gd_`/`.gp_`/`.tx_`/`.in_`/`.xm_`/`.pn_`/`.ex_` compressed siblings
   (Microsoft's legacy single-file-compressed form) and `amd64`/`i386` subfolders of compiled binaries
   the INF references.
5. Decompress every compressed sibling with Windows' own `expand.exe`, **using `-R` (restore original
   name) rather than guessing the real extension from the compressed one**:
   ```
   expand -R LMUD1o40.gd_
   ```
   This matters more than it looks: the extension-to-extension mapping isn't the simple 1:1 scheme it
   looks like at a glance - `.gd_` decompresses to `.gdl` and `.in_` to `.ini` here, *not* to `.gpd`/
   `.inf` as their names suggest (there's a genuinely separate `.gp_` -> `.gpd` pair too). Guessing
   this mapping (as an early pass at this did) silently produces a wrong-but-plausible-looking result:
   it can overwrite a file the `.msi` already extracted correctly (the real `.inf`, clobbered by
   decompressing `.in_` on top of it under the same assumed name) and leave genuinely-required files
   named `.gdl` missing entirely (assumed to be redundant with `.gpd` and silently never produced) -
   both errors that `SetupCopyOEMInf`/`BuildCatalog` tolerate quietly enough at staging time to look
   like success, while `AddPrinter` fails outright the moment something tries to actually use the
   driver (`ERROR_CAN_NOT_COMPLETE`) - a good example of why "it staged with no error" isn't the same
   as "it actually works," and worth re-verifying end to end rather than stopping at the first
   success signal. `-R` sidesteps the whole problem by asking `expand.exe` itself, which reads the
   real name straight out of the compressed file's own header instead of guessing from its extension.
6. Move the resulting folder into `Drivers\Windows\<version>\Lexmark\`, renamed to something
   version-identifying.

This produces a folder that both `driver.BuildCatalog` (finds and catalogs it as
`"Lexmark Universal v2"`) and PDT's existing `SetupCopyOEMInf`-based install path
(`driverinstall_windows.go` - the exact same mechanism used for every other manufacturer here, no
Lexmark-specific code needed) handle correctly with no changes.

### Kyocera: self-extracting `.exe` packages

Kyocera stopped shipping `.zip`-packaged drivers roughly 8 months before this was written; current
downloads are a self-extracting `.exe` that launches Kyocera's own installer UI instead of just
unpacking to a folder - `BuildCatalog` has no way to run that `.exe` for you, so getting an
already-extracted folder onto disk under `Drivers\Windows\<version>\Kyocera\` is a manual, one-time
step per driver version. Two ways to get there, both ending at the same result - a folder full of
`.inf` files and friends, exactly like every other manufacturer's already-extracted package:

**Method 1 - extract with 7-Zip, no installer run at all.** A Kyocera "self-extracting" `.exe` is
actually a normal PE executable with a large embedded archive resource; 7-Zip can pull that resource
out directly without ever launching the installer:

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

**Method 2 - let the installer extract, then take its temp copy before it does anything else.**

1. Run the downloaded `.exe`. When Kyocera's "Product Library" installer window appears, **stop -
   don't proceed with the install.**
2. Open File Explorer and go to `%LocalAppData%`. Find a `KX Driver` folder, and inside it an
   `originalfiles` folder (there's also an `originalfiles.zip` alongside it - the folder, already
   extracted, is the one you want).
3. Copy `originalfiles` into `Drivers\Windows\<version>\Kyocera\` and rename it to something
   version-identifying (e.g. `KXDRIVER_8.6A.1412`).
4. Exit the Product Library installer without installing anything.

A few things worth knowing before relying on either method:
- The installer's own temp extraction (Method 2) has been observed to survive under `%LocalAppData%`
  even after exiting the installer without installing - useful, since it means you can grab a copy
  after the fact if you forgot to before closing it, but not guaranteed to hold true for every
  Kyocera installer version; if `KX Driver` isn't there, you'll need Method 1 instead, or to retry
  Method 2 and copy the folder out *before* exiting the installer.
- Installers generally extract to `%LocalAppData%\Temp`, not `%LocalAppData%` itself, so if a future
  Kyocera installer version relocates this, checking under `Temp` first (or using Sysinternals'
  Process Monitor to watch what the installer actually writes and where) is the way to re-find it.

**macOS is not read by this function at all yet.** The real macOS side of the Drivers tree (being
built out alongside the Windows side) nests the *other* way - `Drivers/macOS/<Manufacturer>/<macOS
version>/...` (version under manufacturer, not manufacturer under version like Windows) - reflecting
that macOS driver packages genuinely do vary by OS release in a way Windows ones generally don't.
There is also a `Drivers/macOS/OpenPrinting/<Manufacturer>/*.ppd` bucket (flat, no version
breakdown) as a fallback source for manufacturers/models with nothing better. None of this is
scanned yet - `internal/driver` has no macOS-specific code, and there is no macOS `Deployer`
implementation at all (this tool remains Windows-only for now). The bigger open question for that
work: most of the macOS packages on disk are `.dmg` images (some wrapping a nested `.dmg`, most
ultimately containing a `.pkg` installer) - extracting the driver files (or PPDs) from those without
either running a full installer or requiring a macOS host to mount them is unsolved and needs its
own design pass before any macOS catalog-scanning code gets written.

## Deploy sequence (`internal/printer/windows/deploy_windows.go`)

`Deployer.Deploy` ports `Create-Printers.ps1`'s `Deploy-PrinterRow`, in the same order (port ->
driver -> printer object -> conditional rebind -> print config -> APF), with one deliberate
simplification versus the original: there is no `BindNulPort` checkbox. Instead:

- Typing `NUL` (or `NUL:`) as a row's IP permanently binds it to the local `NUL:` port (no network
  I/O at all) - for a placeholder/test row.
- Any row whose resolved driver is HP's Universal Print Driver family automatically gets the
  create-against-`NUL:`-then-rebind-to-the-real-port treatment with **no user toggle** -
  `printer.RequiresNulPortWorkaround` - since that HP driver family has been directly observed to
  take several minutes to create against a live TCP/IP port versus a few seconds against `NUL:`.

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

## Building / testing

```
go build ./...
go vet ./...
go test ./...
```

`wails dev` / `wails build` build the actual application (see `wails.json`); the Go backend above has
no dependency on the frontend and is fully testable on its own.
