import Foundation

/// Runs the bundled connector agent (`printer-connector --config <path>`) as a
/// child process so the just-registered printers actually push telemetry — the
/// web UI then shows them online, matching the original connector's behavior.
/// Held as a child so it stops when the app quits (see AppDelegate).
enum AgentService {
    private static var process: Process?

    /// Default connector.json path written by `register` (~/Library/Application Support/Spoolr).
    static func configPath() -> String {
        let base = FileManager.default
            .urls(for: .applicationSupportDirectory, in: .userDomainMask).first
        return (base?.appendingPathComponent("Spoolr/connector.json").path)
            ?? NSHomeDirectory() + "/Library/Application Support/Spoolr/connector.json"
    }

    static func start() {
        if let p = process, p.isRunning { return }
        guard let bin = DiscoveryService.helperURL(),
              FileManager.default.fileExists(atPath: configPath()) else { return }
        let p = Process()
        p.executableURL = bin
        p.arguments = ["--config", configPath()]
        p.standardOutput = Pipe()
        p.standardError = Pipe()
        do {
            try p.run()
            process = p
        } catch {
            process = nil
        }
    }

    static func stop() {
        process?.terminate()
        process = nil
    }

    /// Reload after `register` rewrites connector.json (the running agent has
    /// the old config in memory).
    static func restart() {
        stop()
        start()
    }
}
