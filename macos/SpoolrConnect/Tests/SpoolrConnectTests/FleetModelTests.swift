import XCTest
@testable import SpoolrConnect

/// Exercises the popover state machine (handoff §"State machine"). Pure model
/// logic — no UI — so it runs headlessly under `swift test`.
final class FleetModelTests: XCTestCase {

    func testSampleLoadsFleetIntoAttentionMode() {
        let model = FleetModel()
        XCTAssertFalse(model.printers.isEmpty)
        XCTAssertEqual(model.state, .attention)
        XCTAssertEqual(model.linkedCount, model.printers.count)
    }

    func testGroupingsPartitionByState() {
        let model = FleetModel()
        let total = model.printing.count + model.idle.count + model.errored.count + model.offline.count
        XCTAssertEqual(total, model.printers.count)
    }

    func testBeginScanEntersScanningWithDiscoveries() {
        let model = FleetModel()
        model.beginScan()
        XCTAssertEqual(model.state, .scanning)
        XCTAssertFalse(model.discovered.isEmpty)
        XCTAssertTrue(model.discovered.contains { $0.status == .discovered })
    }

    func testBeginPairingEntersPairingWithSteps() {
        let model = FleetModel()
        model.beginScan()
        let target = model.discovered.first { $0.status == .discovered }!
        model.beginPairing(target)
        XCTAssertEqual(model.state, .pairing)
        XCTAssertEqual(model.pairingTarget, target)
        XCTAssertEqual(model.pairSteps.count, 4)
        XCTAssertTrue(model.pairSteps.contains { $0.state == .active })
    }

    func testCompletePairingAddsPrinterAndShowsSuccess() {
        let model = FleetModel()
        model.beginScan()
        let target = model.discovered.first { $0.status == .discovered }!
        let before = model.printers.count
        model.beginPairing(target)
        model.completePairing()
        XCTAssertEqual(model.state, .justPaired)
        XCTAssertNotNil(model.justPairedPrinter)
        XCTAssertEqual(model.printers.count, before + 1)
    }

    func testCompletePairingIsIdempotentOnPrinterList() {
        let model = FleetModel()
        model.beginScan()
        let target = model.discovered.first { $0.status == .discovered }!
        model.beginPairing(target)
        model.completePairing()
        let count = model.printers.count
        model.completePairing() // same target, should not duplicate
        XCTAssertEqual(model.printers.count, count)
    }

    func testDismissJustPairedReturnsToAttention() {
        let model = FleetModel()
        model.beginScan()
        model.beginPairing(model.discovered.first { $0.status == .discovered }!)
        model.completePairing()
        model.dismissJustPaired()
        XCTAssertEqual(model.state, .attention)
        XCTAssertNil(model.justPairedPrinter)
    }
}
