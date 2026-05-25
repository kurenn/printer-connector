import Foundation

/// Bridges the SwiftUI app to the Go connector's real LAN discovery. Runs the
/// bundled `printer-connector discover` helper (Contents/Resources) and decodes
/// its JSON. When the helper isn't present (e.g. `swift run` during dev), the
/// caller falls back to sample data so the flow still demos.
enum DiscoveryService {
    struct Hit: Decodable {
        let host: String
        let port: Int
        let name: String
        let kind: String
        let detail: String
    }

    struct Payload: Decodable {
        let hosts_total: Int
        let hosts_probed: Int
        let printers: [Hit]
    }

    enum DiscoveryError: Error { case helperNotFound }

    /// The bundled Go helper, if packaged into the .app.
    static func helperURL() -> URL? {
        Bundle.main.url(forResource: "printer-connector", withExtension: nil)
    }

    static func scan(completion: @escaping (Result<Payload, Error>) -> Void) {
        guard let bin = helperURL() else {
            completion(.failure(DiscoveryError.helperNotFound))
            return
        }
        DispatchQueue.global(qos: .userInitiated).async {
            let proc = Process()
            proc.executableURL = bin
            proc.arguments = ["discover"]
            let out = Pipe()
            proc.standardOutput = out
            proc.standardError = Pipe()
            do {
                try proc.run()
                let data = out.fileHandleForReading.readDataToEndOfFile()
                proc.waitUntilExit()
                let payload = try JSONDecoder().decode(Payload.self, from: data)
                DispatchQueue.main.async { completion(.success(payload)) }
            } catch {
                DispatchQueue.main.async { completion(.failure(error)) }
            }
        }
    }
}
