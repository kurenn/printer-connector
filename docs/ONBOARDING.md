# Smooth printer onboarding — design & status

The goal: adding a printer to Spoolr should be friction-free for non-technical
users — no SSH, no per-printer config, ideally no terminal.

## The key fact

**The connector does not have to run on the printer.** It only needs to be on
the same LAN and able to reach the printer's Moonraker API + the cloud. So you
install **one agent per network** (a laptop, a Pi, a NAS — anywhere), and it can
discover and adopt *every* printer on that network at once.

## Architecture (unchanged)

- **Cloud app (Rails)** — management UI (from anywhere) + API + auth. Source of truth.
- **Connector (this repo, Go)** — the LAN agent: discovers printers, bridges
  telemetry + commands. One agent → many printers.
- **(Future) menubar app** — a thin Tailscale-style GUI wrapper around this same
  connector for one-click onboarding + status. See "Next" below.

Decisions: **stay Go** (single static binary, trivial cross-compile to
mac/win/linux/arm/mips, stdlib-only); **one repo** (the GUI becomes another
`cmd/`, reusing `internal/` — no duplicate logic, one version).

## Onboarding tiers

1. **Default (everyone, zero printer-side steps):** install the agent on any
   computer on the network → it scans → you pick printers → adopt. (Today: the
   `setup` CLI below; next: the menubar app makes this a double-click.)
2. **Always-on (advanced):** run the same agent as a service on a Pi/NAS/the
   printer so monitoring continues 24/7 even when a laptop sleeps. This is where
   the SSH install script belongs — an upgrade, not the first step.
3. **Cloud printers (Bambu, etc.):** OAuth/cloud connect, no local agent.

## What shipped (this PR)

`internal/discovery` (stdlib: parallel subnet sweep of the local /24 for the
Moonraker port, confirmed by a `GET /printer/info` probe) plus two subcommands:

```
# See every Moonraker printer on the network:
printer-connector discover            # table   (add --json for machine output)

# One command: discover → pair → adopt ALL found printers:
printer-connector setup --token <PAIRING_TOKEN> [--cloud-url URL] [--config PATH]
```

Verified on a real network: `discover` found 3 printers (K1 Max + two others);
`setup` adopted all 3 in one shot, each with its true LAN host, streaming live.

### Companion Rails change (print_dock)

The register contract now carries each printer's real `host`/`moonraker_port`,
so multiple printers on one connector get **distinct identities** (previously
they all inherited the connector's IP and collided on the host:port uniqueness
check — which silently capped a connector at one printer).

## Next

- **Menubar app (`cmd/spoolr-menubar`, Tailscale-style):** a `fyne.io/systray`
  GUI that runs the agent in-process and shows "connector running" + per-printer
  connected status, with sign-in (device-auth) + the discover/adopt screen. GUI
  deps stay isolated to that binary; the headless connector stays zero-dep.
- **Device-auth** in Rails so the helper signs in without token copy-paste.
- **mDNS** as a discovery fallback (the sweep is primary — in the field many
  Klipper rigs don't advertise `_moonraker._tcp`).
- **Idempotent adopt:** re-running `setup` should re-adopt existing printers
  (find-or-create by host:port) rather than error.
