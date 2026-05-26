import XCTest
@testable import SpoolrConnect

/// Covers persisted-pairing recognition on launch: parsing `connector.json` and
/// reflecting it into the model so a restart stays paired instead of re-prompting
/// for a token. Pure logic over a temp file — runs headlessly under `swift test`.
final class ConnectorConfigTests: XCTestCase {

    private func writeTemp(_ json: String) -> String {
        let path = NSTemporaryDirectory() + "connector-\(UUID().uuidString).json"
        try! json.write(toFile: path, atomically: true, encoding: .utf8)
        return path
    }

    func testLoadReturnsNilForMissingFile() {
        XCTAssertNil(ConnectorConfig.load(path: NSTemporaryDirectory() + "does-not-exist-\(UUID()).json"))
    }

    func testLoadReturnsNilWhenUnpaired() {
        // A connector.json with only a cloud_url (the dev pre-seed) is NOT paired.
        let path = writeTemp(#"{"cloud_url":"http://localhost:3000"}"#)
        XCTAssertNil(ConnectorConfig.load(path: path), "no connector_id ⇒ not paired ⇒ nil")
    }

    func testLoadReturnsNilWhenConnectorIdEmpty() {
        let path = writeTemp(#"{"connector_id":"","printers":[]}"#)
        XCTAssertNil(ConnectorConfig.load(path: path))
    }

    func testLoadParsesPairedConfig() {
        let path = writeTemp("""
        {"cloud_url":"http://localhost:3000","connector_id":"2","site_name":"Lab",
         "printers":[{"printer_id":1,"type":"klipper","name":"K1Max-1814","base_url":"http://x","ui_port":80},
                     {"printer_id":2,"type":"bambu","name":"X1C-11"}]}
        """)
        let cfg = ConnectorConfig.load(path: path)
        XCTAssertNotNil(cfg)
        XCTAssertEqual(cfg?.connectorId, "2")
        XCTAssertEqual(cfg?.workspace, "Lab")
        XCTAssertEqual(cfg?.printers.map(\.name), ["K1Max-1814", "X1C-11"])
        XCTAssertEqual(cfg?.printers.map(\.kind), ["klipper", "bambu"])
    }

    func testLoadCoercesNumericConnectorId() {
        // Defensive: tolerate a connector_id serialized as a JSON number, not string.
        let path = writeTemp(#"{"connector_id":2,"printers":[]}"#)
        XCTAssertEqual(ConnectorConfig.load(path: path)?.connectorId, "2")
    }

    func testLoadSkipsPrintersMissingAName() {
        let path = writeTemp(#"{"connector_id":"7","printers":[{"printer_id":1,"type":"klipper"},{"printer_id":2,"type":"bambu","name":"X1C"}]}"#)
        XCTAssertEqual(ConnectorConfig.load(path: path)?.printers.map(\.name), ["X1C"])
    }

    func testLoadCollectsRegisteredHostsFromBaseURL() {
        // The host is collected even for an unnamed printer, so a freshly-paired
        // (not-yet-named) printer is still deduped from a rescan.
        let path = writeTemp("""
        {"connector_id":"2","printers":[
          {"printer_id":1,"name":"K1","type":"klipper","base_url":"http://192.168.68.70:7125","ui_port":80},
          {"printer_id":2,"name":"viktor","base_url":"http://192.168.68.81:7125"},
          {"printer_id":3,"base_url":"http://192.168.68.87:7125"}
        ]}
        """)
        XCTAssertEqual(ConnectorConfig.load(path: path)?.registeredHosts,
                       ["192.168.68.70", "192.168.68.81", "192.168.68.87"])
    }

    // MARK: - applyPaired reflects the config into the connected home

    func testApplyPairedEntersAttentionWithLinkedPrinters() {
        let path = writeTemp("""
        {"connector_id":"2","site_name":"Lab",
         "printers":[{"name":"K1Max","type":"klipper"},{"name":"X1C","type":"bambu"},{"name":"Mystery","type":""}]}
        """)
        let cfg = ConnectorConfig.load(path: path)!
        let model = FleetModel(loadSample: false)
        model.state = .empty
        model.applyPaired(cfg)

        XCTAssertEqual(model.state, .attention, "a paired config boots into the connected home, not the pairing flow")
        XCTAssertEqual(model.workspace, "Lab")
        XCTAssertEqual(model.printers.map(\.name), ["K1Max", "X1C", "Mystery"])
        XCTAssertEqual(model.printers.map(\.kind), [.klipper, .bambu, .printer])
        XCTAssertEqual(model.linkedCount, 3)
        XCTAssertTrue(model.printers.allSatisfy { $0.state == .idle },
                      "live per-printer status is deferred; config-loaded printers read as idle")
    }

    func testApplyPairedKeepsDefaultWorkspaceWhenNoSiteName() {
        let path = writeTemp(#"{"connector_id":"2","printers":[{"name":"K1Max","type":"klipper"}]}"#)
        let model = FleetModel(loadSample: false)
        let defaultWorkspace = model.workspace
        model.applyPaired(ConnectorConfig.load(path: path)!)
        XCTAssertEqual(model.workspace, defaultWorkspace, "absent site_name leaves the workspace untouched")
    }

    // MARK: - bootstrap (the launch decision the App entry point delegates to)

    func testBootstrapPairedConfigEntersConnectedHome() {
        let path = writeTemp(#"{"connector_id":"2","printers":[{"name":"K1Max","type":"klipper"}]}"#)
        let model = FleetModel.bootstrap(configPath: path)
        XCTAssertEqual(model.state, .attention)
        XCTAssertEqual(model.printers.map(\.name), ["K1Max"])
    }

    func testBootstrapMissingConfigStartsFirstRun() {
        let model = FleetModel.bootstrap(configPath: NSTemporaryDirectory() + "missing-\(UUID()).json")
        XCTAssertEqual(model.state, .empty)
        XCTAssertTrue(model.printers.isEmpty)
    }

    func testBootstrapUnpairedConfigStartsFirstRun() {
        let path = writeTemp(#"{"cloud_url":"http://localhost:3000"}"#)
        let model = FleetModel.bootstrap(configPath: path)
        XCTAssertEqual(model.state, .empty, "a cloud_url-only pre-seed is not a pairing")
    }
}
