import Foundation
import SubstrateKit
import XCTest

@testable import SubstrateSync

/// Decision 0215: every package Sync's approval installs reaches no further
/// than the scopes it asks for, or the server blocks the row
/// `reaches-past-scopes`. The reach is read from the closures the server
/// ships, in this checkout: each function's `permissions.reads.kinds` and
/// `writes`, and each record trigger's selector kinds.
final class ReachTests: XCTestCase {
    static let kinds = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        .appendingPathComponent("kinds/providers.substrate.reamde.dev")

    /// The internal kinds the closures declare, which a glob never covers.
    struct Reach {
        var reads: Set<String> = []
        var writes: Set<String> = []
        var internalKinds: Set<String> = []
    }

    /// A line-shaped read of the closures' YAML: the lists under
    /// `permissions.reads.kinds`, `permissions.writes` and
    /// `source.record.kinds`, and each kind's `purpose: internal`. The files
    /// are formatted by the repo's yamlfmt, two spaces an indent, so a list's
    /// items sit two columns in from their key.
    static func reach(of package: String) throws -> Reach {
        var out = Reach()
        let directory = kinds.appendingPathComponent(package)
        let files = try FileManager.default.contentsOfDirectory(atPath: directory.path).filter { $0.hasSuffix(".yaml") }.sorted()
        XCTAssertFalse(files.isEmpty, "no closure at \(directory.path)")
        for file in files {
            let lines = try String(contentsOf: directory.appendingPathComponent(file), encoding: .utf8).split(separator: "\n", omittingEmptySubsequences: false)
            var path: [(indent: Int, key: String)] = []
            var kindID: String?
            for raw in lines {
                let line = raw.replacingOccurrences(of: "\t", with: "  ")
                let trimmed = line.trimmingCharacters(in: .whitespaces)
                if trimmed.isEmpty || trimmed.hasPrefix("#") { continue }
                if trimmed == "---" { path = []; kindID = nil; continue }
                let indent = line.prefix { $0 == " " }.count
                while let last = path.last, last.indent >= indent { path.removeLast() }
                if trimmed.hasPrefix("- ") {
                    let item = trimmed.dropFirst(2).trimmingCharacters(in: .whitespaces)
                    let keys = path.map(\.key)
                    if keys.suffix(3) == ["permissions", "reads", "kinds"] { out.reads.insert(item) }
                    if keys.suffix(2) == ["permissions", "writes"] { out.writes.insert(item) }
                    if keys.suffix(3) == ["source", "record", "kinds"] { out.reads.insert(item) }
                    continue
                }
                guard let colon = trimmed.firstIndex(of: ":") else { continue }
                let key = String(trimmed[..<colon])
                let value = trimmed[trimmed.index(after: colon)...].trimmingCharacters(in: .whitespaces)
                if path.map(\.key) == ["metadata"], key == "id" { kindID = value }
                if path.map(\.key) == ["data"], key == "purpose", value == "internal", let kindID { out.internalKinds.insert(kindID) }
                path.append((indent, key))
            }
        }
        return out
    }

    /// apps/apple/scopes.json is what the server's first-party reach test
    /// reads in place of a hand copy of this manifest, so the two cannot
    /// drift: the scopes in order, and the packages the details ask for.
    func testTheRequestIsTheOneScopesJSONRecordsForTheServersReachTest() throws {
        let apple = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
        struct Recorded: Decodable {
            var scopes: [String]
            var packages: [String]
        }
        struct Requests: Decodable { var sync: Recorded }
        let recorded = try JSONDecoder().decode(Requests.self, from: Data(contentsOf: apple.appendingPathComponent("scopes.json"))).sync
        XCTAssertEqual(recorded.scopes, SyncManifest.scopes)
        XCTAssertEqual(recorded.packages, [AppModel.appleProvider, AppModel.healthProvider])
    }

    func testSyncsScopesCoverWhatItsPackagesCodeReaches() throws {
        var reach = Reach()
        for package in ["apple", "applehealth"] {
            let one = try Self.reach(of: package)
            reach.reads.formUnion(one.reads)
            reach.writes.formUnion(one.writes)
            reach.internalKinds.formUnion(one.internalKinds)
        }
        // The applehealth closure's code is what the request widens for.
        XCTAssertTrue(reach.writes.contains("providers.substrate.reamde.dev/applehealth/account"))
        XCTAssertTrue(reach.writes.contains("substrate.reamde.dev/health/workout"))
        XCTAssertTrue(reach.reads.contains("substrate.reamde.dev/observations/trace"))
        XCTAssertTrue(reach.internalKinds.contains("providers.substrate.reamde.dev/applehealth/account"))
        let grants = Grants(SyncManifest.scopes)
        let uncovered = reach.writes.sorted().filter { !grants.covers("write:\($0)", internal: reach.internalKinds.contains($0)) }
            + reach.reads.subtracting(reach.writes).sorted().filter { !grants.covers("read:\($0)", internal: reach.internalKinds.contains($0)) }
        XCTAssertEqual(uncovered, [], "the server would block the applehealth row reaches-past-scopes")
        // The test is live: without the account's own scope the glob would not cover it.
        let narrower = Grants(SyncManifest.scopes.filter { $0 != "write:providers.substrate.reamde.dev/applehealth/account" })
        XCTAssertFalse(narrower.covers("write:providers.substrate.reamde.dev/applehealth/account", internal: true))
    }
}
