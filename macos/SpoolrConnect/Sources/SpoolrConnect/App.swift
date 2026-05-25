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
    }

    @objc private func togglePopover(_ sender: Any?) {
        guard let button = statusItem.button else { return }
        if popover.isShown {
            popover.performClose(sender)
        } else {
            popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
            popover.contentViewController?.view.window?.makeKey()
        }
    }

    /// SF Symbol stand-in for the Spoolr mark; production ships a template PDF
    /// (auto-tinted for dark/light menubars) + a coloured variant when printing.
    private static func trayIcon() -> NSImage? {
        NSImage(systemSymbolName: "circle.circle", accessibilityDescription: "Spoolr Connect")
    }
}
