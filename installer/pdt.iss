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

#define AppVersion "0.3.0"

[Setup]
AppId={{40FB3E79-C3DC-4C78-A969-35012251BD36}
AppName=Printer Deployment Tool
AppVersion={#AppVersion}
AppPublisher=Ken Teague
AppPublisherURL=https://github.com/keteague/PDT
VersionInfoVersion={#AppVersion}
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
Filename: "{app}\PDT.exe"; Description: "Launch Printer Deployment Tool"; Flags: nowait postinstall skipifsilent
