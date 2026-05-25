# Bambu Lab integration — research, decision, and design

> Status: **implemented in the connector** (this branch). Telemetry + control are
> unit-tested; FTPS file transfer and the live MQTT session need validation
> against physical hardware. See "What still needs a real printer" at the end.

This document is the autonomous research + design pass requested for integrating
Bambu Lab printers (the ecosystem the open-source **Bambuddy** project targets)
into Spoolr. It records *why* the integration lives where it does, the protocol
facts it relies on, the implementation, and the follow-ups.

---

## 1. What "integrate Bambuddy" actually means

[Bambuddy](https://github.com/maziggy/bambuddy) is a **self-hosted, full
application** — a Python/FastAPI backend + React/TypeScript frontend + optional
slicer sidecar, AGPL-3.0 licensed — that replaces Bambu Cloud for local control
of Bambu Lab printers. It is *not* a library you embed.

So there are two very different readings of "integrate it":

1. **Vendor/embed Bambuddy.** Rejected. It is an entire competing application
   (its own DB, web UI, auth, queue). Embedding it would duplicate Spoolr's
   stack, and its **AGPL-3.0** license is incompatible with linking it into our
   MIT connector / proprietary cloud without relicensing obligations.

2. **Support Bambu Lab printers in Spoolr the same way Bambuddy does — by
   speaking the printer's native LAN protocol.** Accepted. The *protocol* (MQTT
   over TLS + FTPS) is reverse-engineered, openly documented, and used
   identically by Bambuddy, `pybambu`/ha-bambulab, `bambulabs_api`, and
   OpenBambuAPI. Protocol knowledge isn't copyrightable; we implement a clean
   native Go driver against it and copy **no** Bambuddy code.

**Decision: implement a native Bambu Lab driver in the printer-connector.** We
treat Bambuddy as a reference for the protocol, not as a dependency.

---

## 2. Why the connector, not the Rails app

| | Rails (Spoolr cloud) | Connector (this repo) |
|---|---|---|
| Where it runs | Render (public cloud) | On the user's LAN, next to the printer |
| Can it reach a printer on `192.168.x.y:8883`? | **No** — printers aren't internet-exposed | **Yes** — same network |
| Already has a driver seam for Bambu? | n/a | **Yes** (`internal/driver`, with a Bambu stub) |

Bambu LAN mode is **local-network only**: MQTT on `:8883` and FTPS on `:990`,
both TLS, authenticated with the printer's access code. The Rails app is hosted
in the cloud and fundamentally cannot open those connections. The connector
exists precisely to bridge LAN printers to the cloud, and it *already had* a
protocol-agnostic `driver.Driver` seam plus a `bambu` stub waiting for a real
implementation. This is the natural and only sensible home.

The Rails side is **already vendor-agnostic** and needed almost nothing:
- `Printer#printer_type` enum already includes `"bambu"`.
- The canonical telemetry contract (`Telemetry::Normalized`) already accepts
  `"driver": "bambu"` and drives status, job derivation, and filament deduction
  off the normalized payload.
- The connector API (`register`, `snapshots/batch`, `commands`, `webcam`) is
  protocol-neutral.

The one real Rails gap is covered in §6.

---

## 3. Protocol facts (the contract we code against)

Sources: [OpenBambuAPI](https://github.com/Doridian/OpenBambuAPI),
ha-bambulab/`pybambu`, `bambulabs_api`. All agree on the following for **LAN /
Developer Mode**:

### MQTT (telemetry + control) — `ssl://<ip>:8883`
- TLS required; printer presents a **self-signed** cert → skip verification. The
  **access code is the authentication**, not the cert.
- Username `bblp`, password = the printer's **access code**.
- Subscribe: `device/<serial>/report` (printer → us).
- Publish: `device/<serial>/request` (us → printer).
- On connect, request a full state dump:
  ```json
  {"pushing":{"sequence_id":"0","command":"pushall","version":1,"push_target":1}}
  ```
- Reports arrive under the top-level **`print`** key as a stream of **partial
  diffs** (only changed fields) between full pushes — so the driver
  **deep-merges** each message into one accumulated state.

Key telemetry fields (under `print`):

| Field | Meaning | Canonical mapping |
|---|---|---|
| `gcode_state` | `IDLE`/`PREPARE`/`SLICING`/`RUNNING`/`PAUSE`/`FINISH`/`FAILED` | → `state` |
| `mc_percent` | progress, **0–100** | → `job.progress` (÷100, fraction 0–1) |
| `mc_remaining_time` | remaining, **minutes** | → `job.remaining_s` (×60) |
| `layer_num` / `total_layer_num` | layers | → `job.current_layer` / `total_layers` |
| `subtask_name` / `gcode_file` | job name / file | → `job.filename` |
| `nozzle_temper` / `nozzle_target_temper` | hotend °C | → `temps.nozzle` |
| `bed_temper` / `bed_target_temper` | bed °C | → `temps.bed` |
| `chamber_temper` | chamber °C | → `temps.chamber` |
| `print_error` | status word (HMS) | surfaced as `error` only when `gcode_state == FAILED` |

Control commands (QoS 1) to `device/<serial>/request`:
```json
{"print":{"sequence_id":"<n>","command":"pause","param":""}}   // also resume, stop
```

### FTPS (file transfer) — implicit TLS `:990`
- Username `bblp`, password = access code, self-signed cert.
- 3MF/gcode is uploaded to the FTP **root**, then a print is started over MQTT:
  ```json
  {"print":{"command":"project_file","url":"ftp:///<file>.3mf",
            "param":"Metadata/plate_1.gcode","subtask_name":"<name>",
            "use_ams":false,"bed_type":"auto", ...}}
  ```

---

## 4. How it maps onto the existing connector

The connector already had the seam — this branch fills in the stub:

```
internal/driver/driver.go     Driver interface + canonical Telemetry (unchanged)
internal/bambu/
  client.go      Driver impl: lazy MQTT connect, state gating, command dispatch
  mqtt.go        paho TLS session, subscribe + pushall on connect, deep-merge reports
  normalize.go   pure: merged Bambu report -> canonical driver.Telemetry
  commands.go    pure: pause/resume/stop/pushall/project_file payload builders
  ftp.go         implicit-FTPS upload / delete / list (jlaffaye/ftp)
  helpers.go     defensive map accessors
```

Design choices worth noting for review:

- **Lazy, persistent MQTT session.** `New()` opens nothing; the session is
  dialed on first telemetry query/command and kept alive with paho
  auto-reconnect. On every (re)connect we re-subscribe and re-`pushall`. A failed
  initial dial leaves the session nil so the next 30 s snapshot tick retries.

- **`QueryObjects` returns the raw report; `NormalizeRaw` produces the canonical
  view.** The agent already attaches `payload["normalized"] = d.NormalizeRaw(raw)`
  to every snapshot (the `emit-normalized-telemetry` work, now on `main`).
  `QueryObjects` returns the merged Bambu report (top-level `print`/`info`), and
  `NormalizeRaw` maps it to canonical telemetry — symmetric with the Moonraker
  driver. This matters because, unlike Klipper, Bambu has no `result.status`
  shape Rails can read, so Bambu is consumed entirely through the `normalized`
  payload (Rails already prefers it).

- **Offline semantics.** `QueryObjects` **errors** when the printer is
  unreachable, so the snapshot loop *skips* it rather than pushing a snapshot
  (the cloud treats any received snapshot as a liveness signal and would
  otherwise mark an offline printer online). `Telemetry` instead returns an
  `offline` value, since it backs UI/status reads.

- **Capabilities are honest.** We advertise only what works:
  `pause, resume, cancel, start_print, upload_file, delete_file, sync_files`.
  `homing`, history, and webcam return `ErrNotSupported` and are **not**
  advertised, so the cloud won't offer broken controls.

- **Credentials stay local.** The access code and serial live only in the
  connector config and never leave the LAN. Registration sends the printer's
  `type` and `host` to the cloud — never its secrets.

---

## 5. Tests

`go test ./internal/bambu/` covers the logic that doesn't need hardware:

- **Normalization** — RUNNING report → correct state/progress(fraction)/remaining
  (min→s)/layers/temps; every `gcode_state` → canonical state; FAILED surfaces an
  error while a non-zero `print_error` during RUNNING does **not**; empty report
  → idle; progress clamped.
- **Command payloads** — exact wire shape of pause/resume/stop, pushall, and
  `project_file` (basename-only URL, extension-stripped subtask, plate_1, AMS off).
- **Deep-merge** — a partial push updates one field and preserves siblings;
  `cloneMap` isolates readers from the writer.
- **Client behavior (fake transport)** — control commands publish to the right
  topic at QoS 1 with incrementing sequence ids; `StartPrint` emits
  `project_file`; `UploadFile` delegates to FTPS; `Telemetry` reports offline when
  unreachable and normalizes when live; `QueryObjects` errors when offline and
  embeds raw+normalized when live; unsupported actions return `ErrNotSupported`.

Whole connector suite stays green; `go vet ./...` clean.

---

## 6. The Rails side (separate PR)

The only Rails change needed is at the **registration trust boundary**: the
`register` action reads `host`/`moonraker_port`/`ui_port` per printer but not
`type`, so Bambu printers would be created with the default `printer_type`
("moonraker"). The fix is to read a sanitized `type` (validated against the
`Printer#printer_type` enum, defaulting to moonraker) and assign it on create.

This connector branch already **sends** `type` (and `host`) in the registration
payload — harmless until Rails reads it. Per the Rails repo's CLAUDE.md, changes
to connector registration go through `/rails-feature`; that PR is tracked
separately. Everything else on the Rails side already works unchanged.

---

## 7. What still needs a real printer

The pure logic is unit-tested, but two paths talk to hardware and must be
validated once a Bambu printer is available:

1. **Live MQTT session** — confirm the `bblp` + access-code TLS handshake, that
   `pushall` seeds state, and that partial diffs merge into a coherent report.
   (Logic is tested via a fake transport; the paho wiring is not.)
2. **FTPS upload** — Bambu firmware is strict about reusing the control-channel
   TLS session on the data channel; `jlaffaye/ftp` negotiates a fresh data
   session, which most firmware accepts but some rejects. `ftp.go` is isolated
   and injected into `Client`, so the implementation can be swapped without
   touching driver logic if a quirk surfaces.

### Deferred follow-ups (intentionally out of scope here)
- **Camera.** Model-specific: P1/A1 use a chamber-image protocol on `:6000`; X1
  uses RTSP. Not an HTTP snapshot, so `GetWebcamSnapshot` returns `ErrNotSupported`
  for now and "webcam" is not advertised.
- **AMS.** Slot/filament mapping for multi-material prints (telemetry + start-print
  `use_ams`/`ams_mapping`). Currently `use_ams:false`, single plate.
- **Multi-plate 3MF** — `project_file` hardcodes `Metadata/plate_1.gcode`.
- **HMS error decoding** — we surface the numeric `print_error`; a human-readable
  table can be added in `bambuErrorMessage` without touching `Normalize`.

---

## 8. How to test with hardware (for the morning / later)

1. On the printer: Settings → enable **LAN Mode / Developer Mode**; note the
   **access code** and **serial number**.
2. Copy `config/config.bambu.example.json`, fill in `host`, `serial`,
   `access_code`, and a `pairing_token` minted from the web app.
3. Run the connector against it; watch for `snapshots pushed` logs and confirm
   the printer appears with live temps/progress in Spoolr.
4. Quick protocol sanity check without the connector:
   ```bash
   mosquitto_sub --cafile /dev/null --insecure -h <ip> -p 8883 \
     -u bblp -P <access_code> -t "device/<serial>/report" -v
   ```
