import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime
import Testing

@testable import SubstrateBridge
@testable import SubstrateBridgeCheck

/// One value of the round trip (app-kit.md §5.5): Swift encodes it into the
/// fixture file, Kotlin's test decodes and encodes it again, and Swift
/// decodes what Kotlin wrote back to this value. `input` is a hand-written
/// encoding Swift would not write (an explicit null), held to decode to the
/// same value on both sides.
struct Fixture: Sendable {
    let type: String
    let input: String?
    let encode: @Sendable () throws -> Data
    let matches: @Sendable (Data) throws -> Bool

    static func of<T: Bridged & Equatable>(_ type: String, _ value: T, input: String? = nil) -> Fixture {
        Fixture(
            type: type, input: input, encode: { try BridgeCodec.data(value) },
            matches: { try BridgeCodec.decode(T.self, from: JSONValue(data: $0)) == value })
    }
}

enum Fixtures {
    static let file = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
        .appendingPathComponent("../../android/fixtures/roundtrip.json").standardizedFileURL

    static let unicode = "🧪 emoji \u{1F600}, עברית, e\u{301}, tab\t, quote\", backslash\\, nul\u{0}, 中文, \u{FFFF}"

    static let all: [Fixture] = [
        // Payload enums: labelled payloads, none, nested enums.
        .of(
            "DeviceRequest",
            DeviceRequest.transportFetch(
                request: TransportRequest(
                    url: "https://example.com/ü?q=1&r=%20", method: "POST",
                    headers: [TransportHeader(name: "Content-Type", value: "application/json")], body: "AAECAwQ=", timeoutMillis: 30000))),
        .of("DeviceRequest", DeviceRequest.connectivityWatch),
        .of("DeviceRequest", DeviceRequest.secureWrite(store: "session", account: "a", value: "c2VjcmV0")),
        .of("DeviceRequest", DeviceRequest.dnsTXT(name: "_substrate.example.com", timeoutMillis: 5000)),
        .of("StartAnswer", StartAnswer.failed(failure: BridgeFailure(.protocolMismatch, "two trees"))),
        .of("StartAnswer", StartAnswer.started(fingerprint: "abc")),
        .of("TransportOutcome", TransportOutcome.failed(kind: .secureConnectionFailed, message: "handshake")),
        .of("TransportChunk", TransportChunk.line(text: unicode)),
        .of("SecureItem", SecureItem.missing),
        .of("SecureItem", SecureItem.reset(reason: "key invalidated")),
        .of("DNSAnswer", DNSAnswer.answered(rcode: 0, records: [["v=substrate1 ", "server=https://example.com"], []])),
        .of(
            "ListMessage",
            ListMessage.delta(
                list: "rows", base: 4, revision: 5, inserted: [ListRow(id: "b", value: ["text": "B"])],
                changed: [], removed: ["a"], order: ["b", "c"])),
        .of("CheckCommand", CheckCommand.secure(account: "a", value: nil)),
        .of("CheckCommand", CheckCommand.fetch(url: "http://127.0.0.1/", via: .urlSession)),
        // Unicode across the boundary.
        .of("CheckResult", CheckResult.ok(name: "echo", detail: unicode, millis: 0.125)),
        // Absent against null: Swift leaves a nil out; null in, nil out.
        .of("TransportRequest", TransportRequest(url: "https://example.com", method: "GET", headers: [], timeoutMillis: 1)),
        .of(
            "TransportRequest", TransportRequest(url: "https://example.com", method: "GET", headers: [], timeoutMillis: 1),
            input: #"{"url":"https://example.com","method":"GET","headers":[],"body":null,"bodyFile":null,"timeoutMillis":1,"credentials":false}"#),
        .of(
            "CheckPresentation", CheckPresentation(step: 1, result: nil, padding: nil),
            input: #"{"step":1,"result":null}"#),
        // Large integers and decimals: Int's extremes as Kotlin's Long, and
        // JSON numbers whose every digit a Double would lose.
        .of(
            "BridgeStats",
            {
                var stats = BridgeStats()
                stats.posted = Int.max
                stats.delivered = Int.min
                stats.dropped = 9_007_199_254_740_993
                return stats
            }()),
        .of("Handle", Handle(id: Int.max, generation: Int.max - 1)),
        .of(
            "Envelope",
            Envelope(
                op: .presentation, handle: Handle(id: 1, generation: 2), id: nil, seq: nil,
                body: [
                    "decimal": JSONValue.number(JSONNumber("0.1")!),
                    "long": JSONValue.number(JSONNumber("12345678901234567890.12345678901234567")!),
                    "negative": JSONValue.number(JSONNumber("-0.000000000000000000000000001")!),
                    "integer": JSONValue.number(JSONNumber("9223372036854775807")!),
                    "nested": ["list": [1, "two", true, .null], "empty": [:]],
                ])),
        // Past what a Decimal holds (38 digits, exponents to ±128) and what a
        // Double does (±1.8e308): only the exact reader and writer keep them,
        // on both sides.
        .of(
            "Envelope",
            Envelope(
                op: .command, handle: Handle(id: 3, generation: 1),
                body: [
                    "digits": JSONValue.number(JSONNumber("1234567890123456789012345678901234567890.0987654321")!),
                    "tiny": JSONValue.number(JSONNumber("-1.25e-400")!),
                    "huge": JSONValue.number(JSONNumber("9.87654321e+999")!),
                    "integer": JSONValue.number(JSONNumber("123456789012345678901234567890123456789012345678901234567890")!),
                ])),
        .of("RouteCommand", RouteCommand(path: ["a"], command: ["amount": JSONValue.number(JSONNumber("1e-200")!)])),
        .of("CheckResult", CheckResult.ok(name: "double", detail: "", millis: 1e-300)),
        .of("NetworkPath", NetworkPath(online: true, expensive: false)),
        // A keyed list whole, as a presentation holds it once Kotlin has put
        // its rows back, and a command that hands a value on with every digit.
        .of(
            "Ledger.Presentation",
            Ledger.Presentation(
                rows: KeyedList(
                    [LedgerRow(id: "a", text: unicode), LedgerRow(id: "b", text: "B")],
                    revision: 7),
                count: "2 rows", drawn: "Tuesday", note: nil)),
        // An instant crosses as RFC 3339, in whole seconds where it has no fraction.
        .of(
            "NotificationRequest",
            NotificationRequest(
                id: "due|a", title: "Buy milk", body: "Due today", fireAt: Date(timeIntervalSince1970: 1_793_696_400),
                thread: "today", timeSensitive: true, userInfo: ["task": "a"])),
        .of("Ledger.Command", Ledger.Command.note(value: ["n": JSONValue.number(JSONNumber("1234567890123456789012345678901234567890.5")!)])),
    ]

    struct Entry: Codable, Equatable {
        var type: String
        var json: JSONValue
    }

    /// The file's entries, as Swift writes them today: an `input` where a
    /// fixture has one, else Swift's own encoding.
    static func entries() throws -> [Entry] {
        try all.map { fixture in
            let data = try fixture.input.map { Data($0.utf8) } ?? fixture.encode()
            return Entry(type: fixture.type, json: try JSONValue(data: data))
        }
    }
}

@Suite("The Swift half of the round trip", .timeLimit(.minutes(2)))
struct RoundTripTests {
    /// The fixture file is what Swift writes; SUBSTRATE_UPDATE_FIXTURES=1
    /// writes it again after a change to a bridged value.
    @Test func theFixtureFileIsWhatSwiftWrites() throws {
        let entries = try Fixtures.entries()
        let text = String(decoding: JSONValue.array(entries.map { ["type": .string($0.type), "json": $0.json] }).data(pretty: true), as: UTF8.self)
        if ProcessInfo.processInfo.environment["SUBSTRATE_UPDATE_FIXTURES"] == "1" {
            try FileManager.default.createDirectory(at: Fixtures.file.deletingLastPathComponent(), withIntermediateDirectories: true)
            try text.write(to: Fixtures.file, atomically: true, encoding: .utf8)
        }
        let onDisk = try String(contentsOf: Fixtures.file, encoding: .utf8)
        #expect(onDisk == text, "stale: run with SUBSTRATE_UPDATE_FIXTURES=1")
    }

    @Test func swiftReadsItsOwnFixturesBack() throws {
        for (fixture, entry) in zip(Fixtures.all, try Fixtures.entries()) {
            #expect(try fixture.matches(entry.json.data(pretty: false)), "\(fixture.type)")
        }
    }

    /// The listed mirror of `ListMessage` the outbox writes is `ListMessage`.
    @Test func aKeyedListIsSentAsListMessage() async throws {
        let (runtime, outlet, _) = started()
        let handle = try open(runtime)
        _ = try post(runtime, Envelope(op: .command, handle: handle, body: ["rows": ["a", "b", "c"]]))
        let whole = try await outlet.next { $0.op == .list }
        #expect(
            try BridgeCodec.decode(ListMessage.self, from: whole.body)
                == .whole(
                    list: "rows", revision: 1,
                    rows: [
                        ListRow(id: "a", value: ["id": "a", "text": "a"]), ListRow(id: "b", value: ["id": "b", "text": "b"]),
                        ListRow(id: "c", value: ["id": "c", "text": "c"]),
                    ]))
        let count = outlet.messages.count
        _ = try post(runtime, Envelope(op: .command, handle: handle, body: ["rows": ["c", "b", "d"], "text": "b"]))
        let delta = try await outlet.next(after: count) { $0.op == .list }
        // Only the rows that changed are encoded: d inserted; c's text changed; b's did not.
        #expect(
            try BridgeCodec.decode(ListMessage.self, from: delta.body)
                == .delta(
                    list: "rows", base: 1, revision: 2, inserted: [ListRow(id: "d", value: ["id": "d", "text": "b"])],
                    changed: [ListRow(id: "c", value: ["id": "c", "text": "b"])], removed: ["a"], order: ["c", "b", "d"]))
        let before = outlet.messages.count
        _ = try post(runtime, Envelope(op: .resync, handle: handle, body: ["list": "rows"]))
        let again = try await outlet.next(after: before) { $0.op == .list }
        guard case let .whole(_, revision, rows) = try BridgeCodec.decode(ListMessage.self, from: again.body) else {
            Issue.record("a resync sent a delta")
            return
        }
        #expect(revision == 2)
        #expect(rows.map(\.id) == ["c", "b", "d"])
    }
}

/// The last leg: what Kotlin wrote back, read by Swift. `android:test` sets
/// SUBSTRATE_ROUNDTRIP_KOTLIN to the file kit-core's test wrote; without it
/// there is nothing to read.
@Suite("KotlinRoundTrip", .timeLimit(.minutes(10)))
struct KotlinRoundTrip {
    @Test(.enabled(if: ProcessInfo.processInfo.environment["SUBSTRATE_ROUNDTRIP_KOTLIN"] != nil))
    func swiftDecodesWhatKotlinWrote() throws {
        let path = try #require(ProcessInfo.processInfo.environment["SUBSTRATE_ROUNDTRIP_KOTLIN"])
        let written = try BridgeCodec.decode([Fixtures.Entry].self, from: JSONValue(data: Data(contentsOf: URL(fileURLWithPath: path))))
        #expect(written.count == Fixtures.all.count)
        for (fixture, entry) in zip(Fixtures.all, written) {
            #expect(entry.type == fixture.type)
            #expect(try fixture.matches(entry.json.data(pretty: false)), "Kotlin's \(fixture.type): \(entry.json)")
        }
    }
}
