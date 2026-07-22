; Inno Setup script for Multi-Claude Switcher (Windows installer).
;
; Build:
;   ISCC.exe /DMyAppVersion=x.y.z packaging\windows-setup.iss
; Expects dist\mcs-tray.exe to exist (built with -H=windowsgui) and produces
;   dist\Multi-Claude-Switcher_<version>_windows_setup.exe
;
; Per-user install (PrivilegesRequired=lowest): installs under the user's
; %LOCALAPPDATA%\Programs, with no UAC / administrator prompt. Upgrades are done
; by running a newer installer, which replaces the exe in place (same AppId).

#define MyAppName "Multi-Claude Switcher"
#define MyAppExeName "mcs-tray.exe"
#define MyAppPublisher "miou1107"
#define MyAppURL "https://github.com/miou1107/multi-claude-switcher"
#ifndef MyAppVersion
  #define MyAppVersion "0.0.0-dev"
#endif

[Setup]
AppId={{8F3B1E52-6C4A-4D7E-9A2B-1F5C9E0D34A7}}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
; The .iss lives in packaging\; resolve relative paths from the repo root.
SourceDir=..
DefaultDirName={autopf}\{#MyAppName}
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
OutputDir=dist
OutputBaseFilename=Multi-Claude-Switcher_{#MyAppVersion}_windows_setup
SetupIconFile=cmd\mcs-tray\assets\icon.ico
UninstallDisplayIcon={app}\{#MyAppExeName}
Compression=lzma
SolidCompression=yes
WizardStyle=modern

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; GroupDescription: "Additional shortcuts:"; Flags: unchecked

[Files]
Source: "dist\mcs-tray.exe"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "Launch Multi-Claude Switcher"; Flags: nowait postinstall skipifsilent
