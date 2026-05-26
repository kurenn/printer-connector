; Spoolr Connect — Windows Installer
; Built with Inno Setup 6 (https://jrsoftware.org/isinfo.php)
;
; To compile from the command line (after building printer-connector.exe):
;   ISCC.exe /DAppVersion=1.2.3 windows\installer.iss
;
; The AppVersion can be overridden at compile time with /DAppVersion=<version>.
; Without the flag it defaults to "0.0.0-dev" for local testing.
;
; IMPORTANT: the installer is UNSIGNED (no Authenticode certificate).
; Windows SmartScreen will show a "Windows protected your PC" warning.
; Users must click "More info" → "Run anyway" to proceed. See docs/INSTALL-Windows.md.

#ifndef AppVersion
  #define AppVersion "0.0.0-dev"
#endif

#define AppName      "Spoolr Connect"
#define AppPublisher "Spoolr"
#define AppURL       "https://www.spoolr.io"

[Setup]
AppId={{B8A3C2F4-1D6E-4F89-A0C7-9E5D2B3F7A1C}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
AppSupportURL={#AppURL}
AppUpdatesURL={#AppURL}/releases
; Install to Program Files\SpoolrConnect on all Windows editions.
DefaultDirName={autopf}\SpoolrConnect
; No Start Menu group needed — the connector is a background service.
DisableProgramGroupPage=yes
; Allow upgrading over an existing installation without user prompt.
CloseApplications=yes
; Output location (relative to the .iss file during ISCC compilation).
OutputDir=Output
OutputBaseFilename=SpoolrConnect-Setup
; Require Windows 10 (build 17763) or later.
MinVersion=10.0.17763
; 64-bit only installer — the Go binary targets windows/amd64.
ArchitecturesAllowed=x64
ArchitecturesInstallIn64BitMode=x64
; Elevation is required to install a Windows Service and write to Program Files.
PrivilegesRequired=admin
; Use modern wizard style.
WizardStyle=modern
Compression=lzma2
SolidCompression=yes

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Files]
; The Go binary must be present adjacent to installer.iss at ISCC compile time.
; The release workflow builds it and copies it into place — see release.yml.
Source: "..\printer-connector.exe"; DestDir: "{app}"; Flags: ignoreversion

[Dirs]
; Ensure ProgramData\SpoolrConnect exists so the register step can write config.
; "everyone-readexec" lets LocalSystem (the service account) read the file.
Name: "{commonappdata}\SpoolrConnect"; Permissions: everyone-readexec

[Code]
// ---------------------------------------------------------------------------
// Wizard page: collect the pairing code shown on the Spoolr dashboard.
// ---------------------------------------------------------------------------

var
  PairingPage: TInputQueryWizardPage;

// InitializeWizard adds the pairing-code input page just before installation.
procedure InitializeWizard();
begin
  PairingPage := CreateInputQueryPage(
    wpReady,
    'Connect to Spoolr',
    'Enter the pairing code shown on your Spoolr dashboard (Connectors → Add Connector).',
    ''
  );
  PairingPage.Add(
    'Pairing code:',
    False  // not a password field so users can verify the value
  );
  PairingPage.Values[0] := '';
end;

// NextButtonClick validates the pairing code before allowing the install to proceed.
function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;
  if CurPageID = PairingPage.ID then
  begin
    if Trim(PairingPage.Values[0]) = '' then
    begin
      MsgBox(
        'A pairing code is required.' + #13#10 +
        'Please enter the code shown on your Spoolr dashboard.',
        mbError,
        MB_OK
      );
      Result := False;
    end;
  end;
end;

// GetPairingCode is referenced via {code:GetPairingCode} in [Run] Parameters.
function GetPairingCode(Param: String): String;
begin
  Result := Trim(PairingPage.Values[0]);
end;

// GetConfigPath is referenced via {code:GetConfigPath} in [Run] Parameters.
// Returns the canonical ProgramData path for the connector config file.
function GetConfigPath(Param: String): String;
begin
  Result := ExpandConstant('{commonappdata}') + '\SpoolrConnect\connector.json';
end;

[Run]
; Step 1: pair the connector with Spoolr using the entered code.
; Writes connector_id + connector_secret into ProgramData\SpoolrConnect\connector.json.
; {code:GetPairingCode} calls the Pascal function above at run time.
Filename: "{app}\printer-connector.exe"; \
  Parameters: "register --token ""{code:GetPairingCode}"" --config ""{code:GetConfigPath}"""; \
  StatusMsg: "Pairing with Spoolr..."; \
  Flags: runhidden waituntilterminated

; Step 2: register and start the Windows Service (auto-start, LocalSystem).
; Idempotent — safe to run on upgrade.
Filename: "{app}\printer-connector.exe"; \
  Parameters: "install-service --config ""{code:GetConfigPath}"""; \
  StatusMsg: "Installing and starting Spoolr Connect service..."; \
  Flags: runhidden waituntilterminated

[UninstallRun]
; Stop and delete the Windows Service before the installer removes the files.
Filename: "{app}\printer-connector.exe"; \
  Parameters: "uninstall-service"; \
  StatusMsg: "Stopping Spoolr Connect service..."; \
  Flags: runhidden waituntilterminated; \
  RunOnceId: "UninstallService"
