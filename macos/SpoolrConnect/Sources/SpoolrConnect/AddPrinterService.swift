import Foundation

/// Runs the bundled `printer-connector add-printer` helper, which adds printers
/// to a connector that is **already paired** using its own credentials.
///
/// Pairing stays the auth boundary for a *new* connector — that's
/// `RegisterService` and its single-use token. Once paired, adding a printer to
/// the same connector needs no fresh grant, which is why the agent's periodic
/// re-discovery has always adopted new Moonraker printers on its own. Bambu
/// printers only ever needed a token because their access code can't be
/// discovered, so there was no non-pairing path to hand it over.
enum AddPrinterService {
    struct Added: Decodable {
        let id: Int
        let name: String
        let type: String?
    }

    struct Payload: Decodable {
        let added: [Added]?
        let count: Int?
        let note: String?
        let error: String?
    }

    enum AddError: LocalizedError {
        case helperNotFound
        case message(String)

        var errorDescription: String? {
            switch self {
            case .helperNotFound: return "Connector helper isn't bundled in this build."
            case .message(let m): return m
            }
        }
    }

    /// Adds a Bambu printer. The access code stays on this machine — only
    /// name/type/host reach the cloud.
    static func addBambu(_ device: BambuDevice,
                         completion: @escaping (Result<Payload, Error>) -> Void) {
        run(["add-printer", "--bambu",
             "\(device.host),\(device.serial),\(device.accessCode),\(device.name)"],
            completion: completion)
    }

    /// Adds a Moonraker printer. Nothing to prompt for — it needs no credentials.
    static func addMoonraker(host: String, port: Int, name: String,
                             completion: @escaping (Result<Payload, Error>) -> Void) {
        run(["add-printer", "--moonraker", "\(host),\(port),\(name)"], completion: completion)
    }

    private static func run(_ args: [String],
                            completion: @escaping (Result<Payload, Error>) -> Void) {
        guard let bin = DiscoveryService.helperURL() else {
            completion(.failure(AddError.helperNotFound))
            return
        }
        DispatchQueue.global(qos: .userInitiated).async {
            let proc = Process()
            proc.executableURL = bin
            proc.arguments = args
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
                        completion(.failure(AddError.message(err)))
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
