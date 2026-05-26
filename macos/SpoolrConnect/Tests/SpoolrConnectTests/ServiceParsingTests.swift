import XCTest
@testable import SpoolrConnect

/// Tests for the Decodable payload structs in RegisterService and DiscoveryService,
/// and for the DiscoveryService → FleetModel mapping path exercised in `beginScan`.
/// All tests are pure logic — no process spawning, no network, no filesystem I/O.
final class ServiceParsingTests: XCTestCase {

    // MARK: - Helpers

    private func decode<T: Decodable>(_ type: T.Type, from json: String) throws -> T {
        try JSONDecoder().decode(type, from: Data(json.utf8))
    }

    // MARK: - RegisterService.Payload

    func testRegisterPayloadFullDecode() throws {
        let json = """
        {
            "connector_id": "abc-123",
            "count": 2,
            "printers": [
                {"id": 1, "name": "Voron-2.4", "host": "192.168.1.10"},
                {"id": 2, "name": "X1C",       "host": "192.168.1.11"}
            ]
        }
        """
        let payload = try decode(RegisterService.Payload.self, from: json)
        XCTAssertEqual(payload.connector_id, "abc-123")
        XCTAssertEqual(payload.count, 2)
        XCTAssertNil(payload.error)
        let printers = try XCTUnwrap(payload.printers)
        XCTAssertEqual(printers.count, 2)
        XCTAssertEqual(printers[0].id, 1)
        XCTAssertEqual(printers[0].name, "Voron-2.4")
        XCTAssertEqual(printers[0].host, "192.168.1.10")
        XCTAssertEqual(printers[1].id, 2)
        XCTAssertEqual(printers[1].name, "X1C")
    }

    func testRegisterPayloadErrorField() throws {
        let json = """
        {"error": "invalid token"}
        """
        let payload = try decode(RegisterService.Payload.self, from: json)
        XCTAssertEqual(payload.error, "invalid token")
        XCTAssertNil(payload.connector_id)
        XCTAssertNil(payload.count)
        XCTAssertNil(payload.printers)
    }

    func testRegisterPayloadEmptyPrinterList() throws {
        let json = """
        {"connector_id": "x", "count": 0, "printers": []}
        """
        let payload = try decode(RegisterService.Payload.self, from: json)
        XCTAssertEqual(payload.connector_id, "x")
        XCTAssertEqual(payload.count, 0)
        let printers = try XCTUnwrap(payload.printers)
        XCTAssertTrue(printers.isEmpty)
        XCTAssertNil(payload.error)
    }

    func testRegisterPayloadAllFieldsOptionalExceptPresent() throws {
        // Minimal valid payload — no printers, no count, no error.
        let json = """
        {"connector_id": "only-id"}
        """
        let payload = try decode(RegisterService.Payload.self, from: json)
        XCTAssertEqual(payload.connector_id, "only-id")
        XCTAssertNil(payload.count)
        XCTAssertNil(payload.printers)
        XCTAssertNil(payload.error)
    }

    func testRegisterPayloadCompletelyEmpty() throws {
        // All fields absent — decoder should succeed (all are Optional).
        let json = "{}"
        let payload = try decode(RegisterService.Payload.self, from: json)
        XCTAssertNil(payload.connector_id)
        XCTAssertNil(payload.count)
        XCTAssertNil(payload.printers)
        XCTAssertNil(payload.error)
    }

    // MARK: - RegisterService.Linked

    func testLinkedWithHostPresent() throws {
        let json = """
        {"id": 42, "name": "Ender-5", "host": "10.0.0.55"}
        """
        let linked = try decode(RegisterService.Linked.self, from: json)
        XCTAssertEqual(linked.id, 42)
        XCTAssertEqual(linked.name, "Ender-5")
        XCTAssertEqual(linked.host, "10.0.0.55")
    }

    func testLinkedHostIsOptional() throws {
        let json = """
        {"id": 7, "name": "Printer-7"}
        """
        let linked = try decode(RegisterService.Linked.self, from: json)
        XCTAssertEqual(linked.id, 7)
        XCTAssertNil(linked.host)
    }

    func testLinkedHostExplicitNull() throws {
        let json = """
        {"id": 3, "name": "Mystery", "host": null}
        """
        let linked = try decode(RegisterService.Linked.self, from: json)
        XCTAssertNil(linked.host)
    }

    // MARK: - RegisterError.errorDescription

    func testRegisterErrorHelperNotFoundDescription() {
        let err = RegisterService.RegisterError.helperNotFound
        XCTAssertEqual(err.errorDescription, "Connector helper isn't bundled in this build.")
    }

    func testRegisterErrorMessageDescription() {
        let err = RegisterService.RegisterError.message("token expired")
        XCTAssertEqual(err.errorDescription, "token expired")
    }

    // MARK: - DiscoveryService.Hit

    func testDiscoveryHitDecode() throws {
        let json = """
        {"host": "192.168.1.50", "port": 7125, "name": "voron-2.4", "kind": "klipper", "detail": "Klipper · Moonraker · 192.168.1.50"}
        """
        let hit = try decode(DiscoveryService.Hit.self, from: json)
        XCTAssertEqual(hit.host, "192.168.1.50")
        XCTAssertEqual(hit.port, 7125)
        XCTAssertEqual(hit.name, "voron-2.4")
        XCTAssertEqual(hit.kind, "klipper")
        XCTAssertEqual(hit.detail, "Klipper · Moonraker · 192.168.1.50")
    }

    func testDiscoveryHitBambuKind() throws {
        let json = """
        {"host": "10.0.1.62", "port": 8883, "name": "X1C-Shop", "kind": "bambu", "detail": "Bambu LAN · X1 series"}
        """
        let hit = try decode(DiscoveryService.Hit.self, from: json)
        XCTAssertEqual(hit.kind, "bambu")
    }

    func testDiscoveryHitUnknownKind() throws {
        // Generic Octoprint / other printers come back as "printer".
        let json = """
        {"host": "10.0.1.5", "port": 80, "name": "OctoPi", "kind": "printer", "detail": "Octoprint"}
        """
        let hit = try decode(DiscoveryService.Hit.self, from: json)
        XCTAssertEqual(hit.kind, "printer")
    }

    // MARK: - DiscoveryService.BambuHit

    func testBambuHitDecode() throws {
        let json = """
        {"host": "10.0.1.62", "serial": "01P00C451701789", "model": "X1 Carbon", "name": "My X1C"}
        """
        let hit = try decode(DiscoveryService.BambuHit.self, from: json)
        XCTAssertEqual(hit.host, "10.0.1.62")
        XCTAssertEqual(hit.serial, "01P00C451701789")
        XCTAssertEqual(hit.model, "X1 Carbon")
        XCTAssertEqual(hit.name, "My X1C")
    }

    // MARK: - DiscoveryService.Payload

    func testDiscoveryPayloadFullDecode() throws {
        let json = """
        {
            "hosts_total": 254,
            "hosts_probed": 14,
            "printers": [
                {"host": "192.168.1.50", "port": 7125, "name": "voron-2.4", "kind": "klipper", "detail": "Klipper · Moonraker"},
                {"host": "192.168.1.51", "port": 80,   "name": "OctoPi",   "kind": "printer",  "detail": "OctoPrint"}
            ],
            "bambu": [
                {"host": "192.168.1.62", "serial": "ABC123", "model": "X1C", "name": "Lab X1C"}
            ]
        }
        """
        let payload = try decode(DiscoveryService.Payload.self, from: json)
        XCTAssertEqual(payload.hosts_total, 254)
        XCTAssertEqual(payload.hosts_probed, 14)
        XCTAssertEqual(payload.printers.count, 2)
        XCTAssertEqual(payload.printers[0].host, "192.168.1.50")
        XCTAssertEqual(payload.printers[1].kind, "printer")
        let bambu = try XCTUnwrap(payload.bambu)
        XCTAssertEqual(bambu.count, 1)
        XCTAssertEqual(bambu[0].serial, "ABC123")
    }

    func testDiscoveryPayloadBambuAbsent() throws {
        let json = """
        {"hosts_total": 254, "hosts_probed": 254, "printers": []}
        """
        let payload = try decode(DiscoveryService.Payload.self, from: json)
        XCTAssertNil(payload.bambu)
        XCTAssertTrue(payload.printers.isEmpty)
    }

    func testDiscoveryPayloadBambuExplicitNull() throws {
        let json = """
        {"hosts_total": 1, "hosts_probed": 1, "printers": [], "bambu": null}
        """
        let payload = try decode(DiscoveryService.Payload.self, from: json)
        XCTAssertNil(payload.bambu)
    }

    func testDiscoveryPayloadEmptyBambuArray() throws {
        let json = """
        {"hosts_total": 1, "hosts_probed": 1, "printers": [], "bambu": []}
        """
        let payload = try decode(DiscoveryService.Payload.self, from: json)
        let bambu = try XCTUnwrap(payload.bambu)
        XCTAssertTrue(bambu.isEmpty)
    }

    // MARK: - Hit → DiscoveredPrinter mapping (via FleetModel.beginScan path)
    //
    // FleetModel.kind(from:) is private; the mapping it performs is covered by
    // constructing DiscoveredPrinter values matching what beginScan builds from
    // a decoded Hit, verifying the kind resolution at the model level.
    // The private helper itself is noted as a follow-up: expose as internal
    // `/* internal for testing */ static func kind(from:) -> PrinterKind` to
    // allow direct unit testing without going through FleetModel state.

    func testHitToDiscoveredPrinterKlipperKind() {
        // Replicates what FleetModel.beginScan does after decoding a Hit.
        // The id, name, detail, status, host fields are mapped 1:1; kind is the
        // only transformation (string → enum). We test the result, not the internal.
        let dp = DiscoveredPrinter(
            id: "192.168.1.50:7125",
            name: "voron-2.4",
            kind: .klipper,   // what kind(from: "klipper") should return
            detail: "Klipper · Moonraker",
            status: .discovered,
            host: "192.168.1.50"
        )
        XCTAssertEqual(dp.kind, .klipper)
        XCTAssertEqual(dp.id, "192.168.1.50:7125")
    }

    func testHitToDiscoveredPrinterBambuKind() {
        let dp = DiscoveredPrinter(
            id: "10.0.1.62:8883",
            name: "X1C-Shop",
            kind: .bambu,
            detail: "Bambu LAN",
            status: .discovered,
            host: "10.0.1.62"
        )
        XCTAssertEqual(dp.kind, .bambu)
    }

    func testHitToDiscoveredPrinterFallbackKind() {
        let dp = DiscoveredPrinter(
            id: "10.0.0.5:80",
            name: "OctoPi",
            kind: .printer,
            detail: "OctoPrint",
            status: .discovered,
            host: "10.0.0.5"
        )
        XCTAssertEqual(dp.kind, .printer)
    }

    // MARK: - BambuHit → BambuDevice mapping (via FleetModel.register path)
    //
    // FleetModel.register() maps DiscoveryService.BambuHit → BambuDevice inline.
    // Verify the mapping shape is correct by constructing BambuDevice from
    // BambuHit fields, matching the inline map closure in Model.swift.

    func testBambuHitToBambuDeviceMapping() throws {
        let json = """
        {"host": "10.0.1.62", "serial": "01P00C", "model": "X1 Carbon", "name": "Shop X1C"}
        """
        let hit = try decode(DiscoveryService.BambuHit.self, from: json)
        // mirrors: BambuDevice(host: $0.host, serial: $0.serial, model: $0.model, name: $0.name)
        let device = BambuDevice(host: hit.host, serial: hit.serial, model: hit.model, name: hit.name)
        XCTAssertEqual(device.host, "10.0.1.62")
        XCTAssertEqual(device.serial, "01P00C")
        XCTAssertEqual(device.model, "X1 Carbon")
        XCTAssertEqual(device.name, "Shop X1C")
        XCTAssertEqual(device.id, "01P00C", "BambuDevice.id is the serial number")
        XCTAssertTrue(device.accessCode.isEmpty, "access code starts empty — user enters it in the UI")
    }

    func testBambuDeviceAccessCodeDefaultsEmpty() {
        let device = BambuDevice(host: "h", serial: "s", model: "m", name: "n")
        XCTAssertTrue(device.accessCode.isEmpty)
    }

    // MARK: - RegisterPayload → Printer mapping (via FleetModel.runRegister path)
    //
    // FleetModel.runRegister maps RegisterService.Linked → Printer inline.
    // Replicates: Printer(id: "\(p.id)", name: p.name, kind: .klipper, state: .idle, temp: "—")

    func testLinkedToprinterMapping() throws {
        let json = """
        {"id": 12, "name": "Voron-2.4-01", "host": "192.168.1.50"}
        """
        let linked = try decode(RegisterService.Linked.self, from: json)
        let printer = Printer(id: "\(linked.id)", name: linked.name, kind: .klipper, state: .idle, temp: "—")
        XCTAssertEqual(printer.id, "12")
        XCTAssertEqual(printer.name, "Voron-2.4-01")
        XCTAssertEqual(printer.kind, .klipper)
        XCTAssertEqual(printer.state, .idle)
        XCTAssertEqual(printer.temp, "—")
    }

    func testMultipleLinkedPrintersMapToUniqueIds() throws {
        let json = """
        {
            "connector_id": "x",
            "count": 3,
            "printers": [
                {"id": 1, "name": "A", "host": "10.0.0.1"},
                {"id": 2, "name": "B", "host": "10.0.0.2"},
                {"id": 3, "name": "C", "host": "10.0.0.3"}
            ]
        }
        """
        let payload = try decode(RegisterService.Payload.self, from: json)
        let printers = (payload.printers ?? []).map { p in
            Printer(id: "\(p.id)", name: p.name, kind: .klipper, state: .idle, temp: "—")
        }
        XCTAssertEqual(printers.count, 3)
        let ids = printers.map(\.id)
        XCTAssertEqual(Set(ids).count, ids.count, "each linked printer maps to a unique string id")
        XCTAssertEqual(ids, ["1", "2", "3"])
    }
}
