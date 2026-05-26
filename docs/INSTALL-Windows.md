# Installing Spoolr Connect on Windows

**Spoolr Connect** installs as a background **Windows Service** (auto-start,
no tray icon or window). Once installed, it starts automatically whenever
Windows boots and keeps your 3D printers connected to Spoolr in the background.

The installer is **unsigned** — Spoolr does not hold an Authenticode (code-signing)
certificate, so Windows SmartScreen will show a warning dialog the first time you
run it. This is expected and safe to dismiss (see [SmartScreen warning](#smartscreen-warning) below).

---

## Requirements

- Windows 10 (version 1809 / build 17763) or later, **64-bit**
- Administrator rights (the installer creates a Windows Service)
- A Spoolr account and a **pairing code** from your dashboard

---

## Step 1 — Get a pairing code

1. Sign in at [spoolr.io](https://www.spoolr.io).
2. Open **Connectors** → **Add Connector**.
3. Copy the pairing code shown on screen — you will paste it into the installer wizard.

---

## Step 2 — Download the installer

Download **`SpoolrConnect-Setup.exe`** from the
[latest release](https://github.com/kurenn/printer-connector/releases/latest).

---

## Step 3 — Run the installer

Double-click `SpoolrConnect-Setup.exe`.

### SmartScreen warning

Because the installer is unsigned, Windows SmartScreen may show:

> **Windows protected your PC**

To proceed:

1. Click **More info** (below the warning text).
2. Click **Run anyway**.

SmartScreen shows this for any unsigned executable downloaded from the internet.
The binary is built directly from the open-source code in this repository via
GitHub Actions — you can inspect the workflow at
`.github/workflows/release.yml`.

### Wizard steps

1. **Welcome** — click **Next**.
2. **Select Destination Location** — the default (`C:\Program Files\SpoolrConnect`) is fine; click **Next**.
3. **Ready to Install** — click **Next**.
4. **Connect to Spoolr** — paste the pairing code you copied from the dashboard; click **Next**.
5. **Installing** — the wizard pairs the connector with Spoolr and registers the Windows Service. This takes 10–30 seconds depending on how many printers are on your network.
6. **Finish** — the **Spoolr Connect** service is now running.

---

## Verify the service is running

Open **Services** (`services.msc`) and look for **Spoolr Connect**. Its status should be **Running** with startup type **Automatic**.

Or in PowerShell:

```powershell
Get-Service -Name SpoolrConnect
```

---

## View logs

The service writes to the Windows **Event Log**. To view:

1. Open **Event Viewer** (`eventvwr.msc`).
2. Expand **Windows Logs** → **Application**.
3. Filter by source **SpoolrConnect**.

Or in PowerShell:

```powershell
Get-EventLog -LogName Application -Source SpoolrConnect -Newest 50
```

---

## Uninstall

Uninstall via **Settings → Apps** (search for "Spoolr Connect") or via
**Control Panel → Programs → Uninstall a program**.

The uninstaller stops and removes the Windows Service before deleting the files.

---

## Firewall note

The connector makes **outbound** HTTPS requests to `spoolr.io`. It does **not** open
any inbound ports. Windows Firewall should not require any changes.

---

## Configuration file

The connector config is stored at:

```
C:\ProgramData\SpoolrConnect\connector.json
```

This file contains your `connector_secret`. Do not share it.
