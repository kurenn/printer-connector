import XCTest
@testable import SpoolrConnect

/// Tests for StatusFile: JSON decoding and the load+map pipeline.
/// Pure logic over temp files — no timers, no FleetModel internals, no UI.
final class StatusFileTests: XCTestCase {

    // MARK: - Helpers

    private func writeTempStatus(_ json: String) -> String {
        let path = NSTemporaryDirectory() + "status-\(UUID().uuidString).json"
        try! json.write(toFile: path, atomically: true, encoding: .utf8)
        return path
    }

    private let fullStatusJSON = """
    {
        "schema_version": 1,
        "updated_at": "2026-05-26T12:00:00Z",
        "site_name": "Lab",
        "printers": [
            {
                "printer_id": 1,
                "name": "K1Max",
                "kind": "klipper",
                "reachable": true,
                "state": "printing",
                "progress": 0.42,
                "job": "benchy.gcode",
                "remaining_s": 1260,
                "layer_current": 120,
                "layer_total": 300,
                "nozzle_c": 210.5,
                "bed_c": 60.0
            },
            {
                "printer_id": 2,
                "name": "Ender-5",
                "kind": "printer",
                "reachable": true,
                "state": "idle",
                "nozzle_c": 25.0
            },
            {
                "printer_id": 3,
                "name": "X1C-04",
                "kind": "bambu",
                "reachable": false,
                "state": "idle"
            }
        ]
    }
    """

    // MARK: - load(path:)

    func testLoadReturnsParsedFileWhenPresent() {
        let path = writeTempStatus(fullStatusJSON)
        let file = StatusFile.load(path: path)
        XCTAssertNotNil(file)
        XCTAssertEqual(file?.schemaVersion, 1)
        XCTAssertEqual(file?.siteName, "Lab")
        XCTAssertEqual(file?.printers?.count, 3)
    }

    func testLoadReturnsNilForMissingFile() {
        let path = NSTemporaryDirectory() + "does-not-exist-\(UUID().uuidString).json"
        XCTAssertNil(StatusFile.load(path: path), "missing file must return nil, not crash")
    }

    func testLoadReturnsNilForMalformedJSON() {
        let path = writeTempStatus("{ not valid json !!!")
        XCTAssertNil(StatusFile.load(path: path), "malformed JSON must return nil, not crash")
    }

    func testLoadReturnsNilForEmptyFile() {
        let path = writeTempStatus("")
        XCTAssertNil(StatusFile.load(path: path), "empty file must return nil, not crash")
    }

    func testLoadHandlesAbsentOptionalFields() {
        // Minimal required fields only.
        let path = writeTempStatus("""
        {
            "schema_version": 1,
            "printers": [{"printer_id": 7, "name": "Minimal"}]
        }
        """)
        let file = StatusFile.load(path: path)
        XCTAssertNotNil(file)
        XCTAssertNil(file?.siteName)
        XCTAssertNil(file?.updatedAt)
        let printer = file?.printers?.first
        XCTAssertNotNil(printer)
        XCTAssertEqual(printer?.printerId, 7)
        XCTAssertEqual(printer?.name, "Minimal")
    }

    // MARK: - mapPrinter: printing printer

    func testPrintingPrinterMapsCorrectly() {
        let path = writeTempStatus(fullStatusJSON)
        let file = StatusFile.load(path: path)!
        let entry = file.printers![0]  // K1Max, printing
        let printer = StatusFile.mapPrinter(entry)

        XCTAssertEqual(printer.id, "1")
        XCTAssertEqual(printer.name, "K1Max")
        XCTAssertEqual(printer.kind, .klipper)
        XCTAssertEqual(printer.state, .printing)
        XCTAssertEqual(printer.progress, 0.42)
        XCTAssertEqual(printer.job, "benchy.gcode")
        XCTAssertEqual(printer.eta, "21m", "1260s / 60 = 21m")
        XCTAssertEqual(printer.layer, "120 / 300")
        XCTAssertEqual(printer.temp, "210°C", "Int(210.5) = 210")
        XCTAssertNil(printer.error)
    }

    // MARK: - mapPrinter: idle printer

    func testIdlePrinterMapsCorrectly() {
        let path = writeTempStatus(fullStatusJSON)
        let file = StatusFile.load(path: path)!
        let entry = file.printers![1]  // Ender-5, idle
        let printer = StatusFile.mapPrinter(entry)

        XCTAssertEqual(printer.id, "2")
        XCTAssertEqual(printer.name, "Ender-5")
        XCTAssertEqual(printer.kind, .printer)
        XCTAssertEqual(printer.state, .idle)
        XCTAssertNil(printer.progress)
        XCTAssertNil(printer.job)
        XCTAssertNil(printer.eta)
        XCTAssertNil(printer.layer)
        XCTAssertEqual(printer.temp, "25°C")
    }

    // MARK: - mapPrinter: unreachable → .offline

    func testUnreachablePrinterMapsToOfflineRegardlessOfState() {
        let path = writeTempStatus(fullStatusJSON)
        let file = StatusFile.load(path: path)!
        let entry = file.printers![2]  // X1C-04, reachable: false
        let printer = StatusFile.mapPrinter(entry)

        XCTAssertEqual(printer.id, "3")
        XCTAssertEqual(printer.kind, .bambu)
        XCTAssertEqual(printer.state, .offline,
                       "reachable: false must override state field and map to .offline")
    }

    // MARK: - State mapping edge cases

    func testStateOfflineString() {
        XCTAssertEqual(StatusFile.stateFrom(raw: "offline", reachable: nil), .offline)
    }

    func testStateErrorString() {
        XCTAssertEqual(StatusFile.stateFrom(raw: "error", reachable: nil), .error)
    }

    func testStatePausedTreatedAsPrinting() {
        // PrinterState has no .paused; a paused job is still an active job.
        XCTAssertEqual(StatusFile.stateFrom(raw: "paused", reachable: nil), .printing,
                       "paused → printing (active job; no paused case in PrinterState)")
    }

    func testStateCompleteEquivalentToIdle() {
        XCTAssertEqual(StatusFile.stateFrom(raw: "complete", reachable: nil), .idle)
    }

    func testStateUnknownFallsBackToIdle() {
        XCTAssertEqual(StatusFile.stateFrom(raw: "unknown_future_value", reachable: nil), .idle)
    }

    func testReachableFalseOverridesErrorState() {
        // Even an errored printer reads as offline when unreachable.
        XCTAssertEqual(StatusFile.stateFrom(raw: "error", reachable: false), .offline)
    }

    func testReachableTrueDoesNotOverrideState() {
        XCTAssertEqual(StatusFile.stateFrom(raw: "error", reachable: true), .error)
    }

    // MARK: - Kind mapping

    func testKindBambu() {
        XCTAssertEqual(StatusFile.kindFrom("bambu"), .bambu)
    }

    func testKindKlipper() {
        XCTAssertEqual(StatusFile.kindFrom("klipper"), .klipper)
    }

    func testKindFallbackToPrinter() {
        XCTAssertEqual(StatusFile.kindFrom(""), .printer)
        XCTAssertEqual(StatusFile.kindFrom("octoprint"), .printer)
        XCTAssertEqual(StatusFile.kindFrom("prusa"), .printer)
    }

    // MARK: - ETA / layer / temp formatting

    func testEtaFormatsRemainingSeconds() {
        let entry = StatusPrinter(printerId: 99, name: "Test", remainingS: 3600)
        let printer = StatusFile.mapPrinter(entry)
        XCTAssertEqual(printer.eta, "60m")
    }

    func testEtaIsNilWhenRemainingAbsent() {
        let entry = StatusPrinter(printerId: 99, name: "Test")
        let printer = StatusFile.mapPrinter(entry)
        XCTAssertNil(printer.eta)
    }

    func testLayerFormattedWhenBothPresent() {
        let entry = StatusPrinter(printerId: 99, name: "Test", layerCurrent: 42, layerTotal: 100)
        let printer = StatusFile.mapPrinter(entry)
        XCTAssertEqual(printer.layer, "42 / 100")
    }

    func testLayerNilWhenOnlyCurrentPresent() {
        let entry = StatusPrinter(printerId: 99, name: "Test", layerCurrent: 42)
        let printer = StatusFile.mapPrinter(entry)
        XCTAssertNil(printer.layer, "layer requires BOTH current and total")
    }

    func testLayerNilWhenOnlyTotalPresent() {
        let entry = StatusPrinter(printerId: 99, name: "Test", layerTotal: 100)
        let printer = StatusFile.mapPrinter(entry)
        XCTAssertNil(printer.layer)
    }

    func testTempFormatsNozzleTemp() {
        let entry = StatusPrinter(printerId: 99, name: "Test", nozzleC: 215.9)
        let printer = StatusFile.mapPrinter(entry)
        XCTAssertEqual(printer.temp, "215°C", "truncates to Int — no rounding up")
    }

    func testTempNilWhenNozzleAbsent() {
        let entry = StatusPrinter(printerId: 99, name: "Test")
        let printer = StatusFile.mapPrinter(entry)
        XCTAssertNil(printer.temp)
    }

    // MARK: - Error field pass-through

    func testErrorFieldMappedWhenPresent() {
        let path = writeTempStatus("""
        {
            "schema_version": 1,
            "printers": [{
                "printer_id": 5,
                "name": "Broken",
                "kind": "printer",
                "reachable": true,
                "state": "error",
                "error": "Thermal runaway detected"
            }]
        }
        """)
        let file = StatusFile.load(path: path)!
        let printer = StatusFile.mapPrinter(file.printers![0])
        XCTAssertEqual(printer.state, .error)
        XCTAssertEqual(printer.error, "Thermal runaway detected")
    }

    // MARK: - Staleness guard
    //
    // status.json without a recent `updated_at` means the agent isn't writing —
    // the popover must NOT keep showing the last captured frame as "printing".

    func testParseUpdatedAtAcceptsRFC3339() throws {
        // Round-trip: format a known Date, parse it back, expect the same instant.
        let original = Date(timeIntervalSince1970: 1_780_056_000)
        let formatted = ISO8601DateFormatter().string(from: original)
        let parsed = try XCTUnwrap(StatusFile.parseUpdatedAt(formatted))
        XCTAssertEqual(parsed.timeIntervalSince1970, original.timeIntervalSince1970, accuracy: 1.0)
    }

    func testParseUpdatedAtReturnsNilForGarbage() {
        XCTAssertNil(StatusFile.parseUpdatedAt(nil))
        XCTAssertNil(StatusFile.parseUpdatedAt(""))
        XCTAssertNil(StatusFile.parseUpdatedAt("not-a-date"))
    }

    func testIsStaleFalseForRecentTimestamp() {
        let now = Date()
        let recent = ISO8601DateFormatter().string(from: now.addingTimeInterval(-30))
        XCTAssertFalse(StatusFile.isStale(updatedAt: recent, now: now))
    }

    func testIsStaleTrueForOldTimestamp() {
        let now = Date()
        let old = ISO8601DateFormatter().string(from: now.addingTimeInterval(-StatusFile.stalenessThreshold - 1))
        XCTAssertTrue(StatusFile.isStale(updatedAt: old, now: now))
    }

    func testIsStaleTrueForMissingOrUnparseableTimestamp() {
        XCTAssertTrue(StatusFile.isStale(updatedAt: nil))
        XCTAssertTrue(StatusFile.isStale(updatedAt: ""))
        XCTAssertTrue(StatusFile.isStale(updatedAt: "garbage"))
    }

    func testCustomThresholdHonored() {
        let now = Date()
        let twoSecondsAgo = ISO8601DateFormatter().string(from: now.addingTimeInterval(-2))
        XCTAssertFalse(StatusFile.isStale(updatedAt: twoSecondsAgo, now: now, threshold: 5))
        XCTAssertTrue(StatusFile.isStale(updatedAt: twoSecondsAgo, now: now, threshold: 1))
    }
}
