# Changelog

All notable changes to this project are documented here. This is a from-scratch Go/Wails rewrite of
`Create-Printers.ps1`; entries reference that original tool's own history where a decision or
limitation carries forward from it.

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
