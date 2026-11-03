import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
@testable import SubstrateBridge
import SubstrateCore
import SubstrateDevice
import Testing

@Suite("The device services' Swift halves, and what is recorded of them")
struct ServicesTests {
    @Test func aTransportOrSecureStoreMessageIsRecordedWithoutItsBody() throws {
        let bearer = TransportRequest(url: "https://example.com", method: "GET",
                                      headers: [TransportHeader(name: "Authorization", value: "Bearer s3cret")], timeoutMillis: 1)
        let request = try BridgeCodec.encode(op: .request, id: 1, body: DeviceRequest.transportFetch(request: bearer))
        #expect(!Redaction.redact(request, service: nil).contains("s3cret"))
        let stored = try BridgeCodec.encode(op: .request, id: 2, body: DeviceRequest.secureWrite(store: "s", account: "a", value: "dG9rZW4="))
        #expect(!Redaction.redact(stored, service: nil).contains("dG9rZW4="))
        // A reply carries no service of its own: its request's is passed in.
        let reply = try BridgeCodec.encode(op: .reply, id: 2, body: SecureItem.found(value: "dG9rZW4="))
        #expect(!Redaction.redact(reply, service: .secureStore).contains("dG9rZW4="))
        let dns = try BridgeCodec.encode(op: .request, id: 3, body: DeviceRequest.dnsTXT(name: "example.com", timeoutMillis: 1))
        #expect(Redaction.redact(dns, service: nil) == dns)
        #expect(Redaction.redact("Bearer s3cret", service: nil) == "<a message that is not an envelope, 13 bytes>")
    }

    @Test func theRuntimeRecordsMessagesRedacted() async throws {
        let (runtime, outlet, _) = started()
        let channel = try #require(runtime.device)
        let task = Task {
            try await channel.call(.secureRead(store: "s", account: "a"), as: SecureItem.self)
        }
        let request = try await outlet.next { $0.op == .request }
        _ = try post(runtime, Envelope(op: .reply, id: request.id, body: try BridgeCodec.value(SecureItem.found(value: "dG9rZW4="))))
        #expect(try await task.value == .found(value: "dG9rZW4="))
        #expect(!runtime.record.messages.joined().contains("dG9rZW4="))
        #expect(runtime.record.messages.contains { $0.contains("<redacted: secureStore>") })
    }

    @Test func aTXTAnswerKeepsEachOutcomeApart() {
        let name = "_substrate.example.com"
        #expect(BridgeTXTResolver.classify(.answered(rcode: 0, records: [["v=substrate1 ", "server=x"]]), name: name)
            == .records(["v=substrate1 server=x"]))
        #expect(BridgeTXTResolver.classify(.answered(rcode: 0, records: []), name: name) == .confirmedAbsent)
        #expect(BridgeTXTResolver.classify(.answered(rcode: 3, records: []), name: name) == .confirmedAbsent)
        guard case .unconfirmedAbsent = BridgeTXTResolver.classify(.answered(rcode: 2, records: []), name: name) else {
            Issue.record("SERVFAIL confirmed an absence")
            return
        }
        guard case .failed = BridgeTXTResolver.classify(.failed(reason: "timeout"), name: name) else {
            Issue.record("a failure was not one")
            return
        }
        let many = Array(repeating: ["x"], count: BridgeTXTResolver.maximumRecords + 1)
        guard case .failed = BridgeTXTResolver.classify(.answered(rcode: 0, records: many), name: name) else {
            Issue.record("too many records were kept")
            return
        }
        let long = [Array(repeating: String(repeating: "a", count: 255), count: 17)]
        guard case .failed = BridgeTXTResolver.classify(.answered(rcode: 0, records: long), name: name) else {
            Issue.record("a record past the bound was kept")
            return
        }
    }

    @Test func aRedirectIsTheAnswerAndABodyComesInlineOrAsAFile() throws {
        let request = URLRequest(url: URL(string: "https://example.com/a")!)
        let redirect = TransportOutcome.response(response: TransportResponse(
            status: 302, url: "https://example.com/a", headers: [TransportHeader(name: "Location", value: "https://elsewhere.example")]))
        let (empty, head) = try BridgeTransport.answer(try BridgeCodec.value(redirect), for: request)
        #expect(head.statusCode == 302)
        #expect(head.value(forHTTPHeaderField: "Location") == "https://elsewhere.example")
        #expect(empty.isEmpty)

        let inline = TransportOutcome.response(response: TransportResponse(status: 200, url: "https://example.com/a", headers: [],
                                                                           body: Data("hello".utf8).base64EncodedString()))
        #expect(try BridgeTransport.answer(try BridgeCodec.value(inline), for: request).0 == Data("hello".utf8))

        let file = FileManager.default.temporaryDirectory.appendingPathComponent("response-\(UUID().uuidString)")
        try Data(repeating: 7, count: 1_000_000).write(to: file)
        let spilled = TransportOutcome.response(response: TransportResponse(status: 200, url: "https://example.com/a", headers: [],
                                                                            bodyFile: file.path))
        #expect(try BridgeTransport.answer(try BridgeCodec.value(spilled), for: request).0.count == 1_000_000)
        #expect(!FileManager.default.fileExists(atPath: file.path))

        let offline = TransportOutcome.failed(kind: .offline, message: "no route")
        #expect(throws: URLError.self) { try BridgeTransport.answer(try BridgeCodec.value(offline), for: request) }
    }
}
