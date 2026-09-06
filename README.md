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
      Kyocera/...
      Ricoh/...
      Sharp/...
```

Every folder found directly under `Drivers/Windows/` is scanned and merged into one catalog - a
printer driver is rarely genuinely Windows-version-specific the way it can be for macOS (see below),
so there's no attempt to detect/match the running Windows version to a specific folder. Each
manufacturer folder's own internal structure (multi-version, multi-arch, `Archive` subfolders
excluded) is unchanged from before this layout existed.

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
extraction for a complete one.

**Default driver per manufacturer** (`internal/driver/default.go`, `DefaultDriverNameFor`): the
Defaults panel pre-selects a specific driver name when a manufacturer is chosen (Canon -> its UFR II
driver, HP/Ricoh -> PCL 6, Sharp -> PCL 6 UD3), matched by token presence (case- and
whitespace-insensitive, order-independent) against the real catalog rather than an exact string -
confirmed necessary since vendors aren't consistent about it even within this one Drivers folder
("PCL 6" vs "PCL6", and Sharp's own driver is literally named "SHARP UD3 PCL6", tokens reversed from
how "PCL 6 UD3" reads out loud).

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

A modal with three tabs: **General** (Save File Base Path - where Open/Save Configuration's dialogs
start from), **External Sites** (one URL per manufacturer, seeded from `defaultManufacturerURLs`
in `settings.go`, editable and persisted to `%AppData%\PDT\settings.json`), and **About** (version,
author, a clickable GitHub link, and **Check for Updates** - see below). The Defaults panel's own
**Check for Updates** button (a different one - printer driver updates, not app updates) opens the
currently-selected manufacturer's configured URL in the system browser (`OpenManufacturerURL` ->
`runtime.BrowserOpenURL`) - no vendor exposes an API to actually check the latest driver version, so
this only ever hands a human the page to look at themselves; true automated version-checking would
mean scraping each vendor's download portal individually; fragile, and high-maintenance per vendor, so
deliberately out of scope here.

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
