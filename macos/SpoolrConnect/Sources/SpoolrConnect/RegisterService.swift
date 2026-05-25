import Foundation

/// Runs the bundled `printer-connector register --token <T>` helper: it
/// discovers every Moonraker printer on the LAN and registers them all under
/// one pairing token, then the web UI updates. Returns the registered printers.
enum RegisterService {
    struct Linked: Decodable {
        let id: Int
        let name: String
        let host: String?
    }

    struct Payload: Decodable {
        let connector_id: String?
        let count: Int?
        let printers: [Linked]?
        let error: String?
    }

    enum RegisterError: LocalizedError {
        case helperNotFound
        case message(String)

        var errorDescription: String? {
            switch self {
            case .helperNotFound: return "Connector helper isn't bundled in this build."
            case .message(let m): return m
            }
        }
    }

    static func register(token: String, completion: @escaping (Result<Payload, Error>) -> Void) {
        guard let bin = DiscoveryService.helperURL() else {
            completion(.failure(RegisterError.helperNotFound))
            return
        }
        DispatchQueue.global(qos: .userInitiated).async {
            let proc = Process()
            proc.executableURL = bin
            proc.arguments = ["register", "--token", token]
            let out = Pipe()
            proc.standardOutput = out
            proc.standardError = Pipe()
            do {
                try proc.run()
                let data = out.fileHandleForReading.readDataToEndOfFile()
                proc.waitUntilExit()
                let payload = try JSONDecoder().decode(Payload.self, from: data)
                DispatchQueue.main.async {
                    if let err = payload.error {
                        completion(.failure(RegisterError.message(err)))
                    } else {
                        completion(.success(payload))
                    }
                }
            } catch {
                DispatchQueue.main.async { completion(.failure(error)) }
            }
        }
    }
}
