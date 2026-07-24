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

// MARK: - Bambu visibility
//
// A scan used to drop discovered Bambu printers on the floor: the helper
// reports them under their own `bambu` key (they need an access code before
// they can be linked) and the scan handler only read `printers`. These cover
// the mapping that now surfaces them.

extension FleetModelTests {

    private func bambuHit(host: String = "192.168.68.78",
                          serial: String = "0300CA612001784",
                          model: String = "",
                          name: String = "Bambu Lab printer") -> DiscoveryService.BambuHit {
        // Decoded rather than constructed so the test also pins the wire shape.
        let json = """
        {"host":"\(host)","serial":"\(serial)","model":"\(model)","name":"\(name)"}
        """
        return try! JSONDecoder().decode(DiscoveryService.BambuHit.self,
                                         from: Data(json.utf8))
    }

    func testBambuHitBecomesADiscoveredPrinter() {
        let d = FleetModel.discovered(fromBambu: bambuHit(model: "P1S"))
        XCTAssertEqual(d.kind, .bambu)
        XCTAssertEqual(d.host, "192.168.68.78")
        XCTAssertEqual(d.detail, "Bambu Lab · P1S · 192.168.68.78")
    }

    /// A printer found by its TLS certificate reports no model — the detail line
    /// must fall back to the serial instead of rendering a dangling separator.
    func testBambuDetailFallsBackToSerialWhenModelUnknown() {
        let d = FleetModel.discovered(fromBambu: bambuHit(model: ""))
        XCTAssertEqual(d.detail, "Bambu Lab · 0300CA612001784 · 192.168.68.78")
        XCTAssertFalse(d.detail.contains("·  ·"), "no empty segment in the detail line")
    }

    /// Two printers on the same host must stay distinct, and a Bambu is keyed by
    /// serial (it has no Moonraker port to key on).
    func testBambuIdIsKeyedBySerial() {
        let a = FleetModel.discovered(fromBambu: bambuHit(serial: "AAA"))
        let b = FleetModel.discovered(fromBambu: bambuHit(serial: "BBB"))
        XCTAssertNotEqual(a.id, b.id)
        XCTAssertEqual(a.id, "bambu:AAA")
    }

    /// Bambu rows go through the same already-linked filter as Moonraker rows,
    /// so a rescan doesn't re-offer a printer you've added.
    func testLinkedBambuIsHiddenOnRescan() {
        let d = FleetModel.discovered(fromBambu: bambuHit())
        let kept = FleetModel.unlinkedDiscoveries([d], linkedHosts: [])
        let hidden = FleetModel.unlinkedDiscoveries([d], linkedHosts: ["192.168.68.78"])
        XCTAssertEqual(kept.count, 1)
        XCTAssertTrue(hidden.isEmpty)
    }

    /// An empty name (possible from a sparse beacon) still renders something
    /// identifiable rather than a blank row.
    func testBambuWithNoNameGetsAFallbackLabel() {
        let d = FleetModel.discovered(fromBambu: bambuHit(name: ""))
        XCTAssertEqual(d.name, "Bambu Lab printer")
    }
}

// MARK: - Token-free add for an already-paired connector
//
// A pairing token is the auth boundary for a NEW connector. Once paired, the
// connector adds printers with its own credentials, so demanding a token just
// sent people to the dashboard for a code that changed nothing.

extension FleetModelTests {

    /// Bambu rows are keyed "bambu:<serial>" so the access code can be attached
    /// to the right printer without re-deriving it from the scan payload.
    func testSerialIsRecoveredFromADiscoveredBambuID() {
        XCTAssertEqual(FleetModel.serial(fromDiscoveredID: "bambu:0300CA612001784"),
                       "0300CA612001784")
    }

    func testSerialIsEmptyForNonBambuIDs() {
        XCTAssertEqual(FleetModel.serial(fromDiscoveredID: "192.168.68.70:7125"), "")
    }

    /// Moonraker rows are keyed "<host>:<port>" — the port must survive so a
    /// printer on a non-default port is added correctly.
    func testPortIsRecoveredFromADiscoveredMoonrakerID() {
        XCTAssertEqual(FleetModel.port(fromDiscoveredID: "192.168.68.70:7130"), 7130)
    }

    /// A malformed or Bambu id falls back to Moonraker's default rather than
    /// producing port 0.
    func testPortFallsBackToTheDefault() {
        XCTAssertEqual(FleetModel.port(fromDiscoveredID: "bambu:SERIAL"), 7125)
        XCTAssertEqual(FleetModel.port(fromDiscoveredID: "garbage"), 7125)
    }

    /// An unpaired connector must still go through the token flow — that's the
    /// real auth boundary and this change must not weaken it.
    func testUnpairedConnectorStillRoutesToTheTokenFlow() {
        let model = FleetModel()
        model.isPaired = false
        model.beginPairing(Self.bambuRow())
        XCTAssertEqual(model.state, .tokenEntry,
                       "an unpaired connector must still ask for a pairing token")
    }

    /// Once paired, tapping a Bambu asks for the one thing that can't be
    /// discovered — the access code — instead of a pairing token that would
    /// grant nothing new.
    func testPairedConnectorAsksForTheAccessCodeNotAToken() {
        let model = FleetModel()
        model.isPaired = true
        model.beginPairing(Self.bambuRow())
        XCTAssertEqual(model.state, .bambuCredentials)
        XCTAssertEqual(model.bambuDiscovered.count, 1)
        XCTAssertEqual(model.bambuDiscovered.first?.serial, "SER",
                       "the serial must carry over so the code attaches to the right printer")
    }

    private static func bambuRow() -> DiscoveredPrinter {
        FleetModel.discovered(fromBambu: try! JSONDecoder().decode(
            DiscoveryService.BambuHit.self,
            from: Data(#"{"host":"10.0.0.5","serial":"SER","model":"","name":"B"}"#.utf8)))
    }
}
