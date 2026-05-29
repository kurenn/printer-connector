# Installing the Spoolr Connect app on macOS

**Spoolr Connect** is the optional macOS menu-bar app that scans your local
network, pairs your printers with one click, and runs the connector agent in the
background. (On a Raspberry Pi / Creality K1 you'd use the shell installers
instead — see the main README.)

The app is **universal** (Apple Silicon + Intel) and **ad-hoc signed**. Spoolr
does not pay for an Apple Developer account, so it is **not Developer-ID signed
or notarized**. That's fine — the app runs perfectly — but how you obtain it
decides whether macOS shows a one-time security prompt.

---

## Recommended: one-line install (no security prompt)

Paste this into **Terminal**:

```bash
curl -fsSL https://raw.githubusercontent.com/kurenn/printer-connector/main/install-macos.sh | bash
```

It downloads the latest release, installs **Spoolr Connect.app** to
`/Applications`, and launches it. Because the download happens in Terminal (not a
browser), macOS does **not** quarantine it, so there is **no "unverified
developer" warning**. Look for the icon in your menu bar.

Install a specific version with `VERSION=v0.2.0 ... | bash`.

---

## Alternative: download from the Releases page

1. Download **`SpoolrConnect-macos.zip`** from the
   [latest release](https://github.com/kurenn/printer-connector/releases/latest)
   and unzip it.
2. Move **Spoolr Connect.app** to `/Applications`.
3. Because a browser download is quarantined, the first launch is blocked. Clear
   it once, one of two ways:

   **macOS Sequoia (15) and later** — double-click the app, dismiss the warning,
   then go to **System Settings → Privacy & Security**, scroll to the message
   about "Spoolr Connect", and click **Open Anyway**. (Right-click → Open no
   longer works on Sequoia.)

   **macOS Sonoma (14) and earlier** — **right-click** (or Control-click) the
   app → **Open** → **Open** in the dialog.

   Or, from Terminal, clear the flag directly:

   ```bash
   xattr -dr com.apple.quarantine "/Applications/Spoolr Connect.app"
   ```

> If you ever see **"Spoolr Connect is damaged and can't be opened"**, that's the
> same quarantine issue — run `xattr -cr "/Applications/Spoolr Connect.app"`.

---

## First launch: allow local network access

On first run, macOS asks whether **Spoolr Connect** may access devices on your
**local network**. Click **Allow** — the app needs it to discover your printers.
If you deny it, discovery returns nothing; re-enable it under **System Settings →
Privacy & Security → Local Network**.

---

## Build it yourself (zero warnings, if you'd rather not trust a binary)

The app is open source. Building locally produces an unquarantined app:

```bash
git clone https://github.com/kurenn/printer-connector.git
cd printer-connector/macos/SpoolrConnect
./scripts/build_app.sh --install
```

(Requires Xcode/Swift and Go 1.23+.)

---

## Updating

Re-run the one-line installer — it backs up the previous app and installs the
latest. (There is no in-app auto-updater.)

## Uninstalling

Quit the app (menu-bar icon → Quit), then drag **Spoolr Connect.app** from
`/Applications` to the Trash. To also remove the saved pairing:

```bash
rm -rf "$HOME/Library/Application Support/Spoolr"
```

---

## Why the security prompt?

Apple's "verified developer" check (Gatekeeper) is satisfied only by apps signed
with a paid **Developer ID** certificate and **notarized** by Apple. This app is
ad-hoc signed instead, so macOS can't attribute it to a registered developer —
hence the one-time prompt. The app is the same open-source binary built by the
release workflow; you can verify the checksum on the release, or build it
yourself with the steps above.
