; Inno Setup script for PDT (Printer Deployment Tool).
;
; Deliberately does NOT bundle the Drivers folder - printer driver packages
; are hundreds of MB each (see the README's "Drivers folder layout") and
; would bloat this installer for no benefit, since PDT already scaffolds an
; empty Drivers\Windows\11\<Manufacturer>\ structure on first launch
; (ensureDriversScaffold, driversfolder.go) ready for a technician to drop
; packages into.
;
; Elevation-optional by design (PrivilegesRequired=lowest +
; PrivilegesRequiredOverridesAllowed): double-clicking this installer
; normally runs unelevated, no UAC prompt, installing to
; %LocalAppData%\Programs\PDT. Choosing "Run as administrator" instead
; installs to %ProgramFiles%\PDT. Either way, PDT's own Drivers/Configs
; folders always live under %LocalAppData%\PDT (see
; defaultDriversBasePath/defaultSaveFileBasePath in settings.go) - not
; wherever the executable itself ended up - since %ProgramFiles% isn't
; writable by an ordinary user, and this way both install modes behave
; identically once PDT is actually running.
;
; Build with: iscc installer\pdt.iss
; (requires a `wails build` first, so build\bin\PDT.exe exists to package)

; Read from the repo-root VERSION file rather than a hardcoded literal here -
; see version.go's own doc comment for why (a stale hand-typed copy here is
; exactly the bug that motivated this).
#define VersionFile FileOpen("..\VERSION")
#define AppVersion Trim(FileRead(VersionFile))
#expr FileClose(VersionFile)

[Setup]
AppId={{40FB3E79-C3DC-4C78-A969-35012251BD36}
AppName=Printer Deployment Tool
AppVersion={#AppVersion}
AppPublisher=Ken Teague
AppPublisherURL=https://github.com/keteague/PDT
VersionInfoVersion={#AppVersion}
; Without this, Inno Setup's default Programs and Features (appwiz.cpl)
; listing falls back to AppName + AppVersion (e.g. "Printer Deployment Tool
; 0.3.1") - just the bare app name there instead, matching every other
; normally-packaged Windows app; the version is still visible in that same
; dialog's own "Version" column, and the installer's wizard title bar still
; shows AppVerName (AppName + AppVersion) as before.
UninstallDisplayName=Printer Deployment Tool
DefaultDirName={autopf}\PDT
DefaultGroupName=Printer Deployment Tool
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=commandline dialog
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir=..\build\bin
OutputBaseFilename=PDT-Setup-{#AppVersion}
Compression=lzma2
SolidCompression=yes
SetupIconFile=..\build\windows\icon.ico
UninstallDisplayIcon={app}\PDT.exe
WizardStyle=modern

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Create a &desktop shortcut"; GroupDescription: "Additional shortcuts:"

[Files]
; PDT.exe only - no Drivers, no Configs. See this file's own header comment.
Source: "..\build\bin\PDT.exe"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Printer Deployment Tool"; Filename: "{app}\PDT.exe"
Name: "{autodesktop}\Printer Deployment Tool"; Filename: "{app}\PDT.exe"; Tasks: desktopicon

[Run]
; shellexec (not the default CreateProcess-based execution) is required here:
; PDT.exe's own manifest demands elevation (requireAdministrator - see this
; project's README), and CreateProcess cannot trigger a UAC prompt for that on
; its own - confirmed live: an unelevated install with "Launch Printer
; Deployment Tool" left checked failed with "CreateProcess failed; code 740.
; The requested operation requires elevation." ShellExecute (what double-
; clicking the exe, or its Start Menu/Desktop shortcut, already does) knows
; how to prompt for UAC consent instead of just failing.
Filename: "{app}\PDT.exe"; Description: "Launch Printer Deployment Tool"; Flags: nowait postinstall skipifsilent shellexec
