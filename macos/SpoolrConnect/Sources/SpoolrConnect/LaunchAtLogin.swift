import Foundation
import ServiceManagement

/// Tiny wrapper around `SMAppService.mainApp` (macOS 13+) for the right-click
/// "Launch at Login" toggle. The system enables the helper at user login by
/// registering the main app bundle itself — no separate LoginItem helper
/// target needed, since this is a status-bar app already.
///
/// Errors are swallowed at the call site: if registration fails the toggle
/// just reflects the actual `status` after the attempt, so the menu state
/// stays honest rather than lying to the user about something Apple's API
/// returned a failure for.
enum LaunchAtLogin {

    /// True when the system has the main app registered to launch at login.
    static var isEnabled: Bool {
        SMAppService.mainApp.status == .enabled
    }

    /// Flip the registration to match `enabled`. No-op if already in that
    /// state, so it's safe to call from a menu toggle.
    static func setEnabled(_ enabled: Bool) {
        let service = SMAppService.mainApp
        do {
            if enabled {
                if service.status != .enabled {
                    try service.register()
                }
            } else {
                if service.status == .enabled {
                    try service.unregister()
                }
            }
        } catch {
            // Swallow — the menu toggle will reflect the post-attempt status
            // on next read, which is the source of truth. Logging is noisy
            // for a feature where the user can simply try again.
        }
    }
}
