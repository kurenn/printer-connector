import Foundation

/// The subset of `connector.json` (written by `register`, read by the agent) that
/// the menu-bar UI needs to recognize an EXISTING pairing on launch — so a
/// restart stays paired and reconnects, instead of re-prompting for a token.
///
/// Parsed leniently via JSONSerialization (not Codable) so a connector_id that's
/// a string OR a number, and any missing fields, never make us fall back to the
/// "please pair" screen on a connector we're actually paired to.
struct ConnectorConfig {
    let connectorId: String
    let workspace: String?
    let printers: [(name: String, kind: String)]

    /// Returns the parsed config only when it represents a real pairing (a
    /// non-empty connector_id). Missing file / unparseable / unpaired → nil.
    static func load(path: String) -> ConnectorConfig? {
        guard let data = FileManager.default.contents(atPath: path),
              let root = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
        else { return nil }

        let rawId = root["connector_id"]
        let connectorId = (rawId as? String)
            ?? (rawId as? NSNumber)?.stringValue
        guard let id = connectorId, !id.isEmpty else { return nil } // not paired yet

        let workspace = (root["site_name"] as? String)?.nonEmpty
        let printers = (root["printers"] as? [[String: Any]] ?? []).compactMap { p -> (name: String, kind: String)? in
            guard let name = (p["name"] as? String)?.nonEmpty else { return nil }
            return (name: name, kind: (p["type"] as? String) ?? "")
        }
        return ConnectorConfig(connectorId: id, workspace: workspace, printers: printers)
    }
}

private extension String {
    var nonEmpty: String? { isEmpty ? nil : self }
}
