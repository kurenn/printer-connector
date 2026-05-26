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

    func testScanPhrasesAreNonEmptyAndUnique() {
        XCTAssertFalse(FleetModel.scanPhrases.isEmpty)
        XCTAssertTrue(FleetModel.scanPhrases.allSatisfy { !$0.trimmingCharacters(in: .whitespaces).isEmpty })
        XCTAssertEqual(FleetModel.scanPhrases.count, Set(FleetModel.scanPhrases).count,
                       "loader phrases should be unique")
    }

    func testBeginPairingRoutesIntoRealTokenFlow() {
        let model = FleetModel()
        model.beginScan()
        let target = model.discovered.first { $0.status == .discovered }!
        model.beginPairing(target)
        // No fake handshake: tapping a discovered printer routes into the REAL
        // token→register flow, carrying the tapped printer as context.
        XCTAssertEqual(model.state, .tokenEntry)
        XCTAssertEqual(model.pairingTarget, target)
        XCTAssertNil(model.linkError)
    }

    func testShowTokenEntryClearsPairingTargetContext() {
        let model = FleetModel()
        model.beginScan()
        model.beginPairing(model.discovered.first { $0.status == .discovered }!)
        model.showTokenEntry() // generic entry (e.g. "Add more printers") — no specific target
        XCTAssertEqual(model.state, .tokenEntry)
        XCTAssertNil(model.pairingTarget)
    }

    func testDismissJustPairedReturnsToAttention() {
        let model = FleetModel()
        model.state = .justPaired
        model.justPairedPrinter = model.printers.first
        model.dismissJustPaired()
        XCTAssertEqual(model.state, .attention)
        XCTAssertNil(model.justPairedPrinter)
    }

    // MARK: - Rescan hides already-linked printers (issue: don't re-offer added ones)

    func testUnlinkedDiscoveriesHidesAlreadyLinkedHosts() {
        let hits = [
            DiscoveredPrinter(id: "a", name: "K1", kind: .klipper, detail: "", status: .discovered, host: "192.168.1.70"),
            DiscoveredPrinter(id: "b", name: "new", kind: .klipper, detail: "", status: .discovered, host: "192.168.1.99"),
        ]
        let result = FleetModel.unlinkedDiscoveries(hits, linkedHosts: ["192.168.1.70"])
        XCTAssertEqual(result.map(\.host), ["192.168.1.99"], "already-linked host is hidden; the new one remains")
    }

    func testUnlinkedDiscoveriesKeepsAllWhenNothingLinked() {
        let hits = [DiscoveredPrinter(id: "a", name: "K1", kind: .klipper, detail: "", status: .discovered, host: "10.0.0.5")]
        XCTAssertEqual(FleetModel.unlinkedDiscoveries(hits, linkedHosts: []).count, 1)
    }
}
