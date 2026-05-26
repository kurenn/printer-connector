import SwiftUI
import Combine

// MARK: - Domain types
//
// These mirror the per-printer fields the agent exposes (see handoff README →
// "Per-printer fields"). The agent itself is a separate process; this app is the
// UI. Wire a real status stream into `FleetModel` where noted — the sample data
// below renders every state so the views can be built and reviewed standalone.

enum PrinterKind: String, Codable {
    case bambu, klipper, printer // driver type → icon glyph
}

enum PrinterState: String, Codable {
    case printing, idle, error, offline
}

struct Printer: Identifiable, Equatable {
    let id: String
    var name: String
    var kind: PrinterKind
    var state: PrinterState
    var progress: Double?  // 0...1, printing only
    var job: String?       // gcode filename, printing only
    var eta: String?       // printing only
    var layer: String?     // "284 / 612", printing only
    var temp: String?      // "24°C", idle only
    var error: String?     // state == .error only
    var lastSeen: String?  // offline only
    var location: String?
}

enum DiscoveryStatus { case discovered, checking }

struct DiscoveredPrinter: Identifiable, Equatable {
    let id: String
    var name: String
    var kind: PrinterKind
    var detail: String          // "Klipper · Moonraker · 10.0.1.54"
    var status: DiscoveryStatus
    var host: String
}

/// The popover views. Maps to agent state per the handoff's state machine.
enum PopoverState: Equatable {
    case attention   // home (Attention Mode)
    case empty       // linked, 0 printers
    case tokenEntry  // paste a pairing code (token → register all)
    case scanning    // per-printer discovery in progress
    case linking         // discovering + registering all under a token
    case bambuCredentials // enter access codes for discovered Bambu printers
    case justPaired      // success, auto-times-out back to attention
}

/// A Bambu printer found via SSDP. The access code can't be discovered — the
/// user reads it off the printer's screen — so it's collected separately.
struct BambuDevice: Identifiable, Equatable {
    var id: String { serial }
    var host: String
    var serial: String
    var model: String
    var name: String
    var accessCode: String = ""
}

// MARK: - Fleet model

final class FleetModel: ObservableObject {
    @Published var state: PopoverState = .attention
    @Published var printers: [Printer] = []

    // Connection metadata shown in the header / foot strip.
    @Published var workspace: String = "northshore"
    @Published var version: String = "0.18.2"

    // Transient-state working data.
    @Published var discovered: [DiscoveredPrinter] = []
    @Published var scanProbed: Int = 0
    @Published var scanTotal: Int = 254
    // True while a LAN discovery sweep is in flight. The Go `discover` helper is a
    // single blocking call (no streamed per-host progress), so the scanning view
    // shows a cycling, on-theme loader while this is true instead of a frozen 0/254.
    @Published var scanInProgress = false
    // The discovered printer the user tapped "Pair" on — carried into the token
    // step as context. Pairing itself always runs the real token→register flow.
    @Published var pairingTarget: DiscoveredPrinter?
    @Published var justPairedPrinter: Printer?

    // Token-based "register everything" flow.
    @Published var token: String = ""
    @Published var linkError: String?
    @Published var linkedPrinters: [Printer] = []

    // Bambu onboarding: opt-in (the SSDP scan only runs when the user has Bambu
    // printers, so the common Klipper flow isn't slowed). SSDP-found printers
    // await a user-entered access code.
    @Published var includeBambu = false
    @Published var bambuDiscovered: [BambuDevice] = []

    // Attention Mode group expansion (collapsed by default).
    @Published var idleExpanded = false
    @Published var offlineExpanded = false

    // Derived groupings used across the home view.
    var printing: [Printer] { printers.filter { $0.state == .printing } }
    var idle: [Printer]     { printers.filter { $0.state == .idle } }
    var errored: [Printer]  { printers.filter { $0.state == .error } }
    var offline: [Printer]  { printers.filter { $0.state == .offline } }

    // MARK: Agent seam
    //
    // The real connector agent (separate process) is the source of truth. Replace
    // `loadSample()` with a subscription to the agent's status stream
    // (AsyncStream / Combine) and publish updates into the @Published properties
    // above; the views react automatically. State selection (which of the five
    // views to show) follows the handoff mapping:
    //   linked + 0 printers       → .empty
    //   user "Add printer"        → .scanning
    //   user "Pair"               → .pairing
    //   pairing done (< ~6s ago)  → .justPaired (then auto → .attention)
    //   otherwise                 → .attention

    init(loadSample: Bool = true) {
        if loadSample { self.loadSample() }
    }

    /// The handoff's representative fleet — exercises every row/state.
    func loadSample() {
        printers = [
            Printer(id: "p1", name: "Voron-2.4-01", kind: .klipper, state: .printing,
                    progress: 0.62, job: "bracket_v4.gcode", eta: "1h 12m", layer: "284 / 612", location: "Shop floor"),
            Printer(id: "p2", name: "X1C-04", kind: .bambu, state: .printing,
                    progress: 0.18, job: "frame-rev2.3mf", eta: "3h 04m", layer: "92 / 540", location: "Lab"),
            Printer(id: "p3", name: "P1S-07", kind: .bambu, state: .printing,
                    progress: 0.93, job: "hinge_x3.gcode", eta: "8m", layer: "601 / 612", location: "Prototyping"),
            Printer(id: "p4", name: "Prusa-MK4-02", kind: .printer, state: .error,
                    error: "Thermal runaway", location: "Shop floor"),
            Printer(id: "p5", name: "Ender-5+-09", kind: .printer, state: .idle, temp: "24°C", location: "Lab"),
            Printer(id: "p6", name: "VT-350-03", kind: .klipper, state: .idle, temp: "26°C", location: "Production · Bay 2"),
            Printer(id: "p7", name: "X1C-11", kind: .bambu, state: .idle, temp: "23°C", location: "Lab"),
            Printer(id: "p8", name: "Voron-2.4-05", kind: .klipper, state: .offline, lastSeen: "6m ago", location: "Prototyping"),
        ]
        state = .attention
    }

    /// The launch model: a paired connector.json boots the connected home and
    /// stays paired; an unpaired (or absent) one starts the first-run flow.
    /// (`--fleet` sample home is handled by the App entry point.)
    static func bootstrap(configPath: String) -> FleetModel {
        let m = FleetModel(loadSample: false)
        if let config = ConnectorConfig.load(path: configPath) {
            m.applyPaired(config)
        } else {
            m.state = .empty
        }
        return m
    }

    /// Reflect an EXISTING pairing (from connector.json) on launch so a restart
    /// stays paired: show the connected home with the linked printers by name,
    /// not the first-run pairing flow. The agent reconnects with the saved
    /// credentials separately (AgentService). Live per-printer status is the
    /// deferred agent-seam work; until then they read as idle (a calm
    /// placeholder) — the web dashboard has the real live state.
    func applyPaired(_ config: ConnectorConfig) {
        if let ws = config.workspace { workspace = ws }
        printers = config.printers.map { p in
            Printer(id: p.name, name: p.name, kind: Self.kind(from: p.kind), state: .idle)
        }
        state = .attention
    }

    var linkedCount: Int { printers.count }

    // MARK: State machine (handoff §"State machine")
    // Driven by user intent here; in production the agent stream decides which
    // view to show. These also document the legal transitions.

    func showTokenEntry() {
        pairingTarget = nil // generic entry — not tied to a specific discovered printer
        linkError = nil
        state = .tokenEntry
    }

    /// Paste a pairing code → discover the LAN → register everything under that
    /// token → the web UI updates. Bambu printers (found via SSDP) need a
    /// user-entered access code first, so we discover up front and branch to the
    /// credentials step before the single register call.
    func register() {
        let trimmed = token.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { linkError = "Enter your pairing code."; return }
        linkError = nil
        state = .linking

        // Default (Klipper) path: register straight away — no Bambu pre-scan, so
        // no regression. Only when the user opts in do we SSDP-probe for Bambu.
        guard includeBambu else {
            runRegister(bambu: [])
            return
        }

        DiscoveryService.scan(bambuOnly: true) { [weak self] result in
            guard let self else { return }
            let bambu: [BambuDevice]
            switch result {
            case .success(let payload):
                bambu = (payload.bambu ?? []).map {
                    BambuDevice(host: $0.host, serial: $0.serial, model: $0.model, name: $0.name)
                }
            case .failure:
                bambu = [] // helper missing / no Bambu — go straight to register
            }
            if bambu.isEmpty {
                self.runRegister(bambu: [])
            } else {
                self.bambuDiscovered = bambu
                self.state = .bambuCredentials
            }
        }
    }

    /// Submit the entered Bambu access codes, then register everything.
    func confirmBambuAndRegister() {
        state = .linking
        runRegister(bambu: bambuDiscovered)
    }

    private func runRegister(bambu: [BambuDevice]) {
        let trimmed = token.trimmingCharacters(in: .whitespacesAndNewlines)
        RegisterService.register(token: trimmed, bambu: bambu) { [weak self] result in
            guard let self else { return }
            switch result {
            case .success(let payload):
                let linked = (payload.printers ?? []).map { p in
                    Printer(id: "\(p.id)", name: p.name, kind: .klipper, state: .idle, temp: "—")
                }
                self.linkedPrinters = linked
                self.printers = linked
                self.token = ""
                self.bambuDiscovered = []
                self.state = .justPaired
                // (Re)start the agent so the printers push telemetry → the web
                // UI shows them online. Restart picks up the rewritten config.
                AgentService.restart()
            case .failure(let err):
                self.linkError = err.localizedDescription
                self.state = .tokenEntry
            }
        }
    }

    func beginScan() {
        state = .scanning
        discovered = []
        scanProbed = 0
        scanTotal = 254
        scanInProgress = true
        DiscoveryService.scan { [weak self] result in
            guard let self else { return }
            self.scanInProgress = false
            switch result {
            case .success(let payload):
                self.scanTotal = max(payload.hosts_total, 1)
                self.scanProbed = payload.hosts_probed
                let hits = payload.printers.map { hit in
                    DiscoveredPrinter(id: "\(hit.host):\(hit.port)",
                                      name: hit.name,
                                      kind: Self.kind(from: hit.kind),
                                      detail: hit.detail,
                                      status: .discovered,
                                      host: hit.host)
                }
                // Hide printers already linked to this connector — a rescan should
                // surface only what's NEW, not re-offer ones you've added.
                let linkedHosts = ConnectorConfig.load(path: AgentService.configPath())?.registeredHosts ?? []
                self.discovered = Self.unlinkedDiscoveries(hits, linkedHosts: linkedHosts)
            case .failure:
                // No bundled helper (e.g. `swift run`) — keep the demo flowing.
                self.scanProbed = self.scanTotal
                self.discovered = Self.sampleDiscovered
            }
        }
    }

    // On-theme loader lines shown while a network scan is in flight. Blends
    // printer-firmware vocabulary with the LAN-discovery context so the wait
    // feels alive (and a little fun) rather than frozen.
    static let scanPhrases = [
        "Homing the axes…",
        "Heating the hotend…",
        "Listening for printers on the wire…",
        "Knocking on port 7125…",
        "Leveling the bed…",
        "Sniffing out Klipper boxes…",
        "Purging the nozzle…",
        "Following the filament…",
        "Tramming the gantry…",
        "Warming up the chamber…",
        "Pinging every spool on the subnet…",
        "Reticulating layers…",
    ]

    private static func kind(from raw: String) -> PrinterKind {
        switch raw {
        case "bambu":   return .bambu
        case "klipper": return .klipper
        default:        return .printer
        }
    }

    static let sampleDiscovered: [DiscoveredPrinter] = [
        DiscoveredPrinter(id: "d1", name: "voron-13.local", kind: .klipper,
                          detail: "Klipper · Moonraker · 10.0.1.54", status: .discovered, host: "10.0.1.54"),
        DiscoveredPrinter(id: "d2", name: "P1S · Shop", kind: .bambu,
                          detail: "Bambu LAN · X1 series · 10.0.1.62", status: .discovered, host: "10.0.1.62"),
        DiscoveredPrinter(id: "d3", name: "10.0.1.118", kind: .printer,
                          detail: "Probing… port 80, 5000, 7125", status: .checking, host: "10.0.1.118"),
    ]

    /// Discovered printers that aren't already linked to this connector.
    static func unlinkedDiscoveries(_ hits: [DiscoveredPrinter], linkedHosts: Set<String>) -> [DiscoveredPrinter] {
        hits.filter { !linkedHosts.contains($0.host) }
    }

    /// Tapping "Pair" on a discovered printer routes into the REAL token flow —
    /// pairing requires a code (the auth boundary), and `register` then discovers
    /// and links every printer on the network. (No fake handshake; the actual
    /// progress is shown by the token→register `.linking` step.) The tapped
    /// printer is carried as context for the token screen.
    func beginPairing(_ target: DiscoveredPrinter) {
        pairingTarget = target
        linkError = nil
        state = .tokenEntry
    }

    /// Just-paired auto-returns to Attention Mode after ~6s (handoff §behavior).
    func dismissJustPaired() {
        justPairedPrinter = nil
        pairingTarget = nil
        linkedPrinters = []
        discovered = [] // the printers we just linked are stale now — a rescan re-discovers
        state = .attention
    }

    func backToAttention() { state = .attention }
}
