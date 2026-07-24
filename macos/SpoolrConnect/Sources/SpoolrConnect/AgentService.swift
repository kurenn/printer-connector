import Foundation

/// Runs the bundled connector agent (`printer-connector --config <path>`) as a
/// child process so the just-registered printers actually push telemetry — the
/// web UI then shows them online, matching the original connector's behavior.
/// Held as a child so it stops when the app quits (see AppDelegate).
enum AgentService {
    private static var process: Process?

    /// True if the bundled agent subprocess is currently alive. Used by the
    /// popover's health indicator and the right-click "Restart Agent" path
    /// to decide whether to show the agent as down.
    static var isRunning: Bool { process?.isRunning ?? false }

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
        // Clear orphans before spawning, or we end up with two agents polling the
        // same connector (see killStrayAgents).
        killStrayAgents()
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
        killStrayAgents()
    }

    /// Terminates every bundled agent on this machine, not just the one this app
    /// instance spawned.
    ///
    /// `process` only ever referenced our own child, so an agent orphaned by a
    /// previous launch (app relaunched, crashed, or upgraded underneath itself)
    /// survived — and because `start()` guarded on that same reference, it saw
    /// "nothing running" and spawned a second agent beside it. Two agents then
    /// polled the same connector, and each restart reset a lazily-connecting
    /// driver's session, so a Bambu printer could keep getting skipped by the
    /// first snapshot cycle and linger as offline.
    ///
    /// Matching on the helper's own path is deliberate: the app's executable is
    /// Contents/MacOS/SpoolrConnect, so this can never target the app itself.
    private static func killStrayAgents() {
        guard let bin = DiscoveryService.helperURL() else { return }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/usr/bin/pkill")
        p.arguments = ["-f", bin.path]
        p.standardOutput = Pipe()
        p.standardError = Pipe()
        try? p.run()
        p.waitUntilExit()
    }

    /// Reload after `register` rewrites connector.json (the running agent has
    /// the old config in memory).
    static func restart() {
        stop()
        start()
    }
}
