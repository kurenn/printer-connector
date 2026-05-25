import SwiftUI
import AppKit

// Status-bar (menubar) app. No Dock icon, no main window — equivalent to
// `LSUIElement = YES` via `.accessory` activation policy. The popover hosts the
// SwiftUI `RootView`; the tray icon toggles it.

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

final class AppDelegate: NSObject, NSApplicationDelegate {
    private var statusItem: NSStatusItem!
    private let popover = NSPopover()

    // Boot into the first-run Empty state so the full first-connection flow
    // (Empty → Scanning → Pairing → Just paired → Attention) can be walked from
    // the start — every relaunch is a clean reset. Pass `--fleet` to instead
    // load the sample fleet and land on the Attention Mode home.
    private let model: FleetModel = {
        if CommandLine.arguments.contains("--fleet") {
            return FleetModel(loadSample: true)
        }
        let m = FleetModel(loadSample: false)
        m.state = .empty
        return m
    }()

    func applicationDidFinishLaunching(_ notification: Notification) {
        installMainMenu()
        popover.behavior = .transient
        popover.animates = true
        popover.appearance = NSAppearance(named: .vibrantDark)
        popover.contentViewController = NSHostingController(rootView: RootView().environmentObject(model))

        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        if let button = statusItem.button {
            button.image = Self.trayIcon()
            button.image?.isTemplate = true
            button.action = #selector(togglePopover(_:))
            button.target = self
            button.toolTip = "Spoolr Connect"
        }

        // If a connector.json already exists (previously paired), run the agent
        // so telemetry keeps flowing while the app is open.
        if FileManager.default.fileExists(atPath: AgentService.configPath()) {
            AgentService.start()
        }
    }

    func applicationWillTerminate(_ notification: Notification) {
        AgentService.stop()
    }

    /// An .accessory app shows no menu bar, but a main menu is still searched
    /// for key equivalents — so an Edit menu is what makes ⌘X/⌘C/⌘V/⌘A reach
    /// the focused text field (e.g. pasting the pairing code).
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

    @objc private func togglePopover(_ sender: Any?) {
        guard let button = statusItem.button else { return }
        if popover.isShown {
            popover.performClose(sender)
        } else {
            popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
            // Activate the app + make the popover window key so SwiftUI controls
            // receive the FIRST click. Without this, an .accessory app's popover
            // window isn't key, so the first click just activates it and is
            // swallowed (buttons appear to "do nothing" until a second click).
            NSApp.activate(ignoringOtherApps: true)
            popover.contentViewController?.view.window?.makeKeyAndOrderFront(nil)
        }
    }

    /// SF Symbol stand-in for the Spoolr mark; production ships a template PDF
    /// (auto-tinted for dark/light menubars) + a coloured variant when printing.
    private static func trayIcon() -> NSImage? {
        NSImage(systemSymbolName: "circle.circle", accessibilityDescription: "Spoolr Connect")
    }
}
