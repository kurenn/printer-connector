import SwiftUI
import AppKit

// Status-bar (menubar) app. No Dock icon, no main window — `.accessory` policy
// (equivalent to LSUIElement). A borderless panel hosts the SwiftUI RootView and
// is positioned explicitly below the status item, clamped to the visible screen
// so it can never overlap the menu bar.

@main
enum SpoolrConnectMain {
    static func main() {
        let app = NSApplication.shared
        let delegate = AppDelegate()
        app.delegate = delegate
        app.setActivationPolicy(.accessory)
        app.run()
    }
}

/// Borderless panels can't become key by default — override so the pairing-code
/// text field accepts focus + paste.
final class KeyablePanel: NSPanel {
    override var canBecomeKey: Bool { true }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    private var statusItem: NSStatusItem!
    private var panel: KeyablePanel!
    private var clickMonitor: Any?

    // Reflect the persisted pairing on launch. If connector.json already has a
    // connector_id we're paired — boot into the connected home (and AgentService
    // reconnects below with the saved credentials), so a restart NEVER forces a
    // re-pair with a fresh token. Only a truly unpaired install starts in Empty.
    // `--fleet` still shows the sample home.
    private let model: FleetModel = {
        if CommandLine.arguments.contains("--fleet") {
            return FleetModel(loadSample: true)
        }
        return FleetModel.bootstrap(configPath: AgentService.configPath())
    }()

    // Initialized inside applicationDidFinishLaunching because UpdateChecker is
    // @MainActor and AppDelegate property initializers run nonisolated.
    private var updateChecker: UpdateChecker!

    func applicationDidFinishLaunching(_ notification: Notification) {
        installMainMenu()
        updateChecker = UpdateChecker()

        let hosting = NSHostingController(
            rootView: RootView()
                .environmentObject(model)
                .environmentObject(updateChecker)
        )
        hosting.sizingOptions = .preferredContentSize // panel tracks SwiftUI size

        let p = KeyablePanel(
            contentRect: NSRect(x: 0, y: 0, width: Int(Theme.popoverWidth), height: 480),
            styleMask: [.borderless, .nonactivatingPanel],
            backing: .buffered, defer: false
        )
        p.isFloatingPanel = true
        p.level = .popUpMenu
        p.backgroundColor = .clear
        p.isOpaque = false
        p.hasShadow = true
        p.isMovable = false
        p.hidesOnDeactivate = false
        p.contentViewController = hosting
        panel = p

        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        if let button = statusItem.button {
            button.image = Self.trayIcon()
            button.image?.isTemplate = true
            button.action = #selector(toggle(_:))
            button.target = self
            button.toolTip = "Spoolr Connect"
        }

        if FileManager.default.fileExists(atPath: AgentService.configPath()) {
            AgentService.start()
        }

        // Begin polling GitHub /releases/latest for newer builds (debounced
        // on-launch + 24h timer). See UpdateChecker for the cadence rules.
        updateChecker.start()
    }

    func applicationWillTerminate(_ notification: Notification) {
        AgentService.stop()
    }

    // MARK: Panel show/hide

    @objc private func toggle(_ sender: Any?) {
        panel.isVisible ? close() : open()
    }

    private func open() {
        positionBelowStatusItem()
        panel.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        // Dismiss when the user clicks anything outside the panel.
        clickMonitor = NSEvent.addGlobalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown]) { [weak self] _ in
            self?.close()
        }
    }

    private func close() {
        panel.orderOut(nil)
        if let m = clickMonitor { NSEvent.removeMonitor(m); clickMonitor = nil }
    }

    /// Place the panel centered under the status item, clamped to the visible
    /// frame so its top stays below the menu bar and it never runs off-screen.
    private func positionBelowStatusItem() {
        guard let button = statusItem.button, let bw = button.window else { return }
        let anchor = bw.convertToScreen(button.convert(button.bounds, to: nil))
        let size = panel.frame.size

        var x = anchor.midX - size.width / 2
        var y = anchor.minY - size.height - 6 // just below the status item

        let screen = bw.screen ?? NSScreen.main
        if let vf = screen?.visibleFrame {
            x = min(max(x, vf.minX + 8), vf.maxX - size.width - 8)
            if y + size.height > vf.maxY { y = vf.maxY - size.height } // never over the menu bar
            if y < vf.minY + 8 { y = vf.minY + 8 }
        }
        panel.setFrameOrigin(NSPoint(x: x, y: y))
    }

    // MARK: Menu (enables ⌘X/⌘C/⌘V/⌘A even with no visible menu bar)

    private func installMainMenu() {
        let mainMenu = NSMenu()

        let appItem = NSMenuItem()
        mainMenu.addItem(appItem)
        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "Quit Spoolr Connect",
                        action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appItem.submenu = appMenu

        let editItem = NSMenuItem()
        mainMenu.addItem(editItem)
        let editMenu = NSMenu(title: "Edit")
        editMenu.addItem(withTitle: "Undo", action: Selector(("undo:")), keyEquivalent: "z")
        editMenu.addItem(withTitle: "Redo", action: Selector(("redo:")), keyEquivalent: "Z")
        editMenu.addItem(.separator())
        editMenu.addItem(withTitle: "Cut", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        editMenu.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        editItem.submenu = editMenu

        NSApp.mainMenu = mainMenu
    }

    /// The Spoolr mark (ring + dot) drawn as a template image so macOS auto-tints
    /// it for light/dark menu bars — matches the brand's monochrome compact mark.
    private static func trayIcon() -> NSImage {
        let image = NSImage(size: NSSize(width: 18, height: 18), flipped: false) { rect in
            let lineWidth: CGFloat = 2
            let ring = NSBezierPath(ovalIn: rect.insetBy(dx: lineWidth / 2 + 1, dy: lineWidth / 2 + 1))
            ring.lineWidth = lineWidth
            NSColor.black.setStroke()
            ring.stroke()
            let r: CGFloat = 3
            let dot = NSBezierPath(ovalIn: NSRect(x: rect.midX - r, y: rect.midY - r, width: r * 2, height: r * 2))
            NSColor.black.setFill()
            dot.fill()
            return true
        }
        image.isTemplate = true
        return image
    }
}
