import XCTest
@testable import SpoolrConnect

/// Tests for the pure helpers on UpdateChecker: tag normalization, numeric
/// version compare, "is X newer than Y", and parsing a GitHub /releases/latest
/// payload. No network, no UserDefaults — exercising the public class-level
/// static funcs.
final class UpdateCheckerTests: XCTestCase {

    // MARK: - normalize

    func testNormalizeStripsLeadingV() {
        XCTAssertEqual(UpdateChecker.normalize("v0.5.0"), "0.5.0")
        XCTAssertEqual(UpdateChecker.normalize("0.5.0"), "0.5.0")
    }

    func testNormalizeStripsSuffix() {
        XCTAssertEqual(UpdateChecker.normalize("v0.6.0-rc.1"), "0.6.0")
        XCTAssertEqual(UpdateChecker.normalize("1.2.3-beta"), "1.2.3")
    }

    // MARK: - compare (assumes pre-normalized inputs)

    func testCompareNumeric() {
        XCTAssertEqual(UpdateChecker.compare("0.5.0", "0.5.0"), .orderedSame)
        XCTAssertEqual(UpdateChecker.compare("0.5.0", "0.6.0"), .orderedAscending)
        XCTAssertEqual(UpdateChecker.compare("0.6.0", "0.5.0"), .orderedDescending)
        // Numeric — not lexicographic — so 0.10.0 > 0.9.0.
        XCTAssertEqual(UpdateChecker.compare("0.10.0", "0.9.0"), .orderedDescending)
    }

    func testCompareTrailingZeros() {
        XCTAssertEqual(UpdateChecker.compare("0.5", "0.5.0"), .orderedSame)
        XCTAssertEqual(UpdateChecker.compare("0.5.0", "0.5"), .orderedSame)
        XCTAssertEqual(UpdateChecker.compare("1", "1.0.0"), .orderedSame)
    }

    // MARK: - isNewer

    func testIsNewerHandlesPrefixAndSuffix() {
        // v-prefix on either side: stripped before compare.
        XCTAssertTrue(UpdateChecker.isNewer("v0.6.0", than: "0.5.0"))
        XCTAssertTrue(UpdateChecker.isNewer("0.6.0", than: "v0.5.0"))
        XCTAssertFalse(UpdateChecker.isNewer("v0.5.0", than: "v0.5.0"))
        XCTAssertFalse(UpdateChecker.isNewer("v0.4.0", than: "v0.5.0"))
        // Pre-release suffix is ignored, so v0.6.0-rc.1 is not newer than 0.6.0.
        XCTAssertFalse(UpdateChecker.isNewer("v0.6.0-rc.1", than: "0.6.0"))
    }

    // MARK: - parseRelease

    func testParseReleaseExtractsTagAndURL() throws {
        let json = """
        {
          "tag_name": "v0.6.0",
          "name": "v0.6.0",
          "html_url": "https://github.com/kurenn/printer-connector/releases/tag/v0.6.0",
          "body": "release notes here"
        }
        """
        let parsed = try XCTUnwrap(UpdateChecker.parseRelease(data: Data(json.utf8)))
        XCTAssertEqual(parsed.tag, "v0.6.0")
        XCTAssertEqual(parsed.url.absoluteString,
                       "https://github.com/kurenn/printer-connector/releases/tag/v0.6.0")
    }

    func testParseReleaseReturnsNilOnGarbage() {
        XCTAssertNil(UpdateChecker.parseRelease(data: Data("not json".utf8)))
        XCTAssertNil(UpdateChecker.parseRelease(data: Data("{}".utf8)))
    }
}
