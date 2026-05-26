import Foundation

// MARK: - StatusFile

/// Decodable model for the `status.json` the connector agent writes.
/// All fields are optional except `schema_version`; a missing or
/// malformed file always returns nil from `load(path:)` — never crashes.
struct StatusFile: Decodable {
    var schemaVersion: Int?
    var updatedAt: String?
    var siteName: String?
    var printers: [StatusPrinter]?

    enum CodingKeys: String, CodingKey {
        case schemaVersion = "schema_version"
        case updatedAt     = "updated_at"
        case siteName      = "site_name"
        case printers
    }

    /// Parse status.json at `path`. Returns nil if the file is missing,
    /// empty, or cannot be decoded — never throws, never crashes.
    static func load(path: String) -> StatusFile? {
        guard let data = FileManager.default.contents(atPath: path),
              !data.isEmpty else { return nil }
        return try? JSONDecoder().decode(StatusFile.self, from: data)
    }

    /// Convert a `StatusPrinter` entry to the UI `Printer` model.
    static func mapPrinter(_ entry: StatusPrinter) -> Printer {
        let id = String(entry.printerId)
        let name = entry.name
        let kind = StatusFile.kindFrom(entry.kind ?? "")
        let state = StatusFile.stateFrom(raw: entry.state ?? "", reachable: entry.reachable)

        // eta: format remaining_s as "<N>m"
        let eta: String?
        if let secs = entry.remainingS, secs > 0 {
            eta = "\(secs / 60)m"
        } else {
            eta = nil
        }

        // layer: "<current> / <total>" when both present
        let layer: String?
        if let cur = entry.layerCurrent, let total = entry.layerTotal {
            layer = "\(cur) / \(total)"
        } else {
            layer = nil
        }

        // temp: "<N>°C" from nozzle when present
        let temp: String?
        if let nozzle = entry.nozzleC {
            temp = "\(Int(nozzle))°C"
        } else {
            temp = nil
        }

        return Printer(
            id: id,
            name: name,
            kind: kind,
            state: state,
            progress: entry.progress,
            job: entry.job,
            eta: eta,
            layer: layer,
            temp: temp,
            error: entry.error
        )
    }

    // MARK: - Private helpers

    static func kindFrom(_ raw: String) -> PrinterKind {
        switch raw {
        case "bambu":   return .bambu
        case "klipper": return .klipper
        default:        return .printer
        }
    }

    static func stateFrom(raw: String, reachable: Bool?) -> PrinterState {
        // reachable == false always wins → offline
        if let r = reachable, !r { return .offline }
        switch raw {
        case "printing":          return .printing
        case "paused":            return .printing  // active job; no paused state
        case "idle", "complete":  return .idle
        case "error":             return .error
        case "offline":           return .offline
        default:                  return .idle
        }
    }
}

// MARK: - StatusPrinter

struct StatusPrinter: Decodable {
    var printerId: Int
    var name: String
    var kind: String?
    var reachable: Bool?
    var state: String?
    var progress: Double?
    var job: String?
    var remainingS: Int?
    var layerCurrent: Int?
    var layerTotal: Int?
    var nozzleC: Double?
    var bedC: Double?
    var error: String?

    enum CodingKeys: String, CodingKey {
        case printerId    = "printer_id"
        case name
        case kind
        case reachable
        case state
        case progress
        case job
        case remainingS   = "remaining_s"
        case layerCurrent = "layer_current"
        case layerTotal   = "layer_total"
        case nozzleC      = "nozzle_c"
        case bedC         = "bed_c"
        case error
    }
}
