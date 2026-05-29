import XCTest
@testable import SpoolrConnect

/// Tests for the pure `FleetModel.computeAgentHealth` decision and a
/// quick smoke test on `LaunchAtLogin.isEnabled` (read-only — registering
/// the test process at login on a CI runner is the wrong behaviour, so
/// `setEnabled` is intentionally not exercised here).
final class AgentHealthTests: XCTestCase {

    // MARK: - computeAgentHealth

    func testHealthIsDownWhenProcessNotRunning() {
        // Even with a perfectly fresh updated_at, a dead subprocess wins.
        let fresh = ISO8601DateFormatter().string(from: Date())
        let h = FleetModel.computeAgentHealth(processAlive: false, updatedAt: fresh)
        XCTAssertEqual(h, .down)
    }

    func testHealthIsStalledWhenProcessAliveButFileIsOld() {
        let now = Date()
        let old = ISO8601DateFormatter().string(
            from: now.addingTimeInterval(-StatusFile.stalenessThreshold - 5))
        let h = FleetModel.computeAgentHealth(processAlive: true, updatedAt: old, now: now)
        XCTAssertEqual(h, .stalled)
    }

    func testHealthIsStalledWhenUpdatedAtMissing() {
        // A running agent with no updated_at yet (first cycle, malformed
        // file) is still treated as not-yet-fresh.
        let h = FleetModel.computeAgentHealth(processAlive: true, updatedAt: nil)
        XCTAssertEqual(h, .stalled)
    }

    func testHealthIsHealthyWhenProcessAliveAndFileFresh() {
        let now = Date()
        let recent = ISO8601DateFormatter().string(from: now.addingTimeInterval(-10))
        let h = FleetModel.computeAgentHealth(processAlive: true, updatedAt: recent, now: now)
        XCTAssertEqual(h, .healthy)
    }

    // MARK: - LaunchAtLogin

    func testLaunchAtLoginIsEnabledIsReadable() {
        // We don't assert a value — only that the SMAppService read path
        // doesn't crash on the test runner. A failing call here would
        // signal a framework / availability regression.
        _ = LaunchAtLogin.isEnabled
    }
}
