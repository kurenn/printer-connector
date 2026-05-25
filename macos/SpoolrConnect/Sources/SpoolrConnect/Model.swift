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

enum PairStepState { case done, active, pending }

struct PairStep: Identifiable, Equatable {
    let id = UUID()
    var label: String
    var state: PairStepState
    var time: String?
}

/// The five popover views. Maps to agent state per the handoff's state machine.
enum PopoverState: Equatable {
    case attention   // home (Attention Mode)
    case empty       // linked, 0 printers
    case tokenEntry  // paste a pairing code (token → register all)
    case scanning    // per-printer discovery in progress
    case linking         // discovering + registering all under a token
    case bambuCredentials // enter access codes for discovered Bambu printers
    case pairing         // handshake with a single discovered printer
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
    @Published var pairingTarget: DiscoveredPrinter?
    @Published var pairSteps: [PairStep] = []
    @Published var justPairedPrinter: Printer?

    // Token-based "register everything" flow.
    @Published var token: String = ""
    @Published var linkError: String?
    @Published var linkedPrinters: [Printer] = []

    // Bambu onboarding: SSDP-found printers awaiting a user-entered access code.
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

    var linkedCount: Int { printers.count }

    // MARK: State machine (handoff §"State machine")
    // Driven by user intent here; in production the agent stream decides which
    // view to show. These also document the legal transitions.

    func showTokenEntry() {
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
        DiscoveryService.scan { [weak self] result in
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
        DiscoveryService.scan { [weak self] result in
            guard let self else { return }
            switch result {
            case .success(let payload):
                self.scanTotal = max(payload.hosts_total, 1)
                self.scanProbed = payload.hosts_probed
                self.discovered = payload.printers.map { hit in
                    DiscoveredPrinter(id: "\(hit.host):\(hit.port)",
                                      name: hit.name,
                                      kind: Self.kind(from: hit.kind),
                                      detail: hit.detail,
                                      status: .discovered,
                                      host: hit.host)
                }
            case .failure:
                // No bundled helper (e.g. `swift run`) — keep the demo flowing.
                self.scanProbed = self.scanTotal
                self.discovered = Self.sampleDiscovered
            }
        }
    }

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

    func beginPairing(_ target: DiscoveredPrinter) {
        pairingTarget = target
        pairSteps = [
            PairStep(label: "TCP handshake", state: .done, time: "0.4s"),
            PairStep(label: "Pushing pairing key", state: .done, time: "0.3s"),
            PairStep(label: "Reading capabilities", state: .active, time: nil),
            PairStep(label: "Subscribing to status", state: .pending, time: nil),
        ]
        state = .pairing
    }

    func completePairing() {
        guard let target = pairingTarget else { state = .attention; return }
        let paired = Printer(id: target.id,
                             name: target.name.replacingOccurrences(of: ".local", with: ""),
                             kind: target.kind, state: .idle, temp: "24°C", location: nil)
        justPairedPrinter = paired
        if !printers.contains(where: { $0.id == paired.id }) { printers.append(paired) }
        state = .justPaired
    }

    /// Just-paired auto-returns to Attention Mode after ~6s (handoff §behavior).
    func dismissJustPaired() {
        justPairedPrinter = nil
        pairingTarget = nil
        linkedPrinters = []
        state = .attention
    }

    func backToAttention() { state = .attention }
}
