import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
@testable import SubstrateBridge
import SubstrateCore
import SubstrateDevice
import Testing

@Suite("The device services' Swift halves, and what is recorded of them", .timeLimit(.minutes(2)))
struct ServicesTests {
    @Test func aTransportOrSecureStoreMessageIsRecordedWithoutItsBody() throws {
        let bearer = TransportRequest(url: "https://example.com", method: "GET",
                                      headers: [TransportHeader(name: "Authorization", value: "Bearer s3cret")], timeoutMillis: 1)
        let request = try BridgeCodec.encode(op: .request, id: 1, body: DeviceRequest.transportFetch(request: bearer))
        #expect(!Redaction.redact(request, call: .unknown).contains("s3cret"))
        let stored = try BridgeCodec.encode(op: .request, id: 2, body: DeviceRequest.secureWrite(store: "s", account: "a", value: "dG9rZW4="))
        #expect(!Redaction.redact(stored, call: .unknown).contains("dG9rZW4="))
        // A reply carries no service of its own: its request's is passed in.
        let reply = try BridgeCodec.encode(op: .reply, id: 2, body: SecureItem.found(value: "dG9rZW4="))
        #expect(!Redaction.redact(reply, call: .device(.secureStore)).contains("dG9rZW4="))
        let dns = try BridgeCodec.encode(op: .request, id: 3, body: DeviceRequest.dnsTXT(name: "example.com", timeoutMillis: 1))
        #expect(Redaction.redact(dns, call: .unknown) == dns)
        let dnsReply = try BridgeCodec.encode(op: .reply, id: 3, body: DNSAnswer.failed(reason: "timeout"))
        #expect(Redaction.redact(dnsReply, call: .device(.dns)) == dnsReply)
        #expect(Redaction.redact("Bearer s3cret", call: .unknown) == "<a message that is not an envelope, 13 bytes>")
        // What nothing waits on, and a request that does not read, keep no body.
        for (op, body) in [(Op.reply, "dG9rZW4="), (.chunk, "dG9rZW4="), (.error, "dG9rZW4=")] {
            let late = try BridgeCodec.encode(op: op, id: 9, body: JSONValue.string(body))
            #expect(Redaction.redact(late, call: .unknown).contains("<redacted: unknown call>"), "\(op)")
        }
        let odd = try BridgeCodec.encode(op: .request, id: 4, body: ["transportFetch": ["token": "s3cret"]] as JSONValue)
        #expect(Redaction.redact(odd, call: .unknown).contains("<redacted: unreadable request>"))
        // A handle's error names no call, and is kept.
        let handleError = BridgeCodec.failure(BridgeFailure(.swiftFailure, "the command failed"), handle: Handle(id: 1, generation: 1))
        #expect(Redaction.redact(handleError, call: .unknown) == handleError)
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

    @Test func aReplyAfterItsCallWasCancelledIsRecordedWithoutItsBody() async throws {
        let (runtime, outlet, _) = started()
        let channel = try #require(runtime.device)
        let task = Task { try await channel.call(.secureRead(store: "s", account: "a"), as: SecureItem.self) }
        let request = try await outlet.next { $0.op == .request }
        task.cancel()
        _ = try await outlet.next { $0.op == .cancel && $0.id == request.id }
        let found = try BridgeCodec.value(SecureItem.found(value: "dG9rZW4="))
        // The racing reply, and a duplicate of it, are refused and recorded redacted.
        for _ in 0 ..< 2 {
            let late = try post(runtime, Envelope(op: .reply, id: request.id, body: found))
            #expect(try BridgeCodec.decode(BridgeFailure.self, from: late.body).code == .unknownCall)
        }
        // A token endpoint's chunk and error for a stream already gone, likewise.
        _ = try post(runtime, Envelope(op: .chunk, id: request.id, seq: 0, body: ["line": ["text": "access_token=s3cret"]]))
        _ = try post(runtime, Envelope(op: .error, id: request.id, body: ["code": "kotlinException", "message": "s3cret"]))
        let recorded = runtime.record.messages.joined(separator: "\n")
        #expect(!recorded.contains("dG9rZW4="))
        #expect(!recorded.contains("s3cret"))
        #expect(recorded.contains("<redacted: unknown call>"))
    }

    @Test func aCredentialExchangeNeverSpillsAndASpilledBodyIsDeletedOnEveryPath() async throws {
        func request(_ url: String, _ headers: [String: String] = [:]) -> URLRequest {
            var request = URLRequest(url: URL(string: url)!)
            for (name, value) in headers { request.setValue(value, forHTTPHeaderField: name) }
            return request
        }
        #expect(BridgeTransport.carriesCredentials(request("https://example.com/oauth/token")))
        #expect(BridgeTransport.carriesCredentials(request("https://example.com/oauth/revoke")))
        #expect(BridgeTransport.carriesCredentials(request("https://example.com/api/v1/auth/login")))
        #expect(BridgeTransport.carriesCredentials(request("https://example.com/x", ["Content-Type": "application/x-www-form-urlencoded"])))
        #expect(BridgeTransport.carriesCredentials(request("https://example.com/x", ["Authorization": "Basic YTpi"])))
        #expect(!BridgeTransport.carriesCredentials(request("https://example.com/api/v1/records", ["Authorization": "Bearer s3cret"])))

        let (runtime, outlet, _) = started()
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("spill-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: folder) }
        let transport = BridgeTransport(channel: try #require(runtime.device), folder: folder)
        let large = Data(repeating: 1, count: TransportRequest.inlineLimit + 1)

        // A token request past the inline limit stays inline and is marked.
        var token = request("https://example.com/oauth/token", ["Content-Type": "application/x-www-form-urlencoded"])
        token.httpMethod = "POST"
        token.httpBody = large
        let tokenRequest = token
        let sending = Task { try await transport.data(for: tokenRequest).1.statusCode }
        let sent = try await outlet.next { $0.op == .request }
        guard case let .transportFetch(wire) = try BridgeCodec.decode(DeviceRequest.self, from: sent.body) else {
            Issue.record("not a fetch")
            return
        }
        #expect(wire.credentials && wire.bodyFile == nil && wire.body != nil)
        _ = try post(runtime, Envelope(op: .reply, id: sent.id, body: try BridgeCodec.value(TransportOutcome.failed(kind: .offline, message: "x"))))
        await #expect(throws: URLError.self) { try await sending.value }
        var tooLarge = token
        tooLarge.httpBody = Data(repeating: 1, count: TransportRequest.credentialLimit + 1)
        await #expect(throws: URLError.self) { try await transport.data(for: tooLarge) }

        // Any other large body spills, and its file is gone once the call is over, answered or not.
        var upload = request("https://example.com/api/v1/blobs", ["Authorization": "Bearer s3cret"])
        upload.httpMethod = "PUT"
        upload.httpBody = large
        let count = outlet.messages.count
        let uploadRequest = upload
        let uploading = Task { try await transport.data(for: uploadRequest).1.statusCode }
        let spilled = try await outlet.next(after: count) { $0.op == .request }
        guard case let .transportFetch(spilledWire) = try BridgeCodec.decode(DeviceRequest.self, from: spilled.body),
              let file = spilledWire.bodyFile
        else {
            Issue.record("the body did not spill")
            return
        }
        #expect(FileManager.default.fileExists(atPath: file))
        uploading.cancel()
        await #expect(throws: (any Error).self) { try await uploading.value }
        #expect(!FileManager.default.fileExists(atPath: file))

        // A reply Swift took whose body does not read still has its file deleted.
        let response = folder.appendingPathComponent("response-x")
        try Data("x".utf8).write(to: response)
        let unreadable: JSONValue = ["response": ["bodyFile": .string(response.path), "status": "not a number"]]
        #expect(throws: BridgeFailure.self) { try BridgeTransport.answer(unreadable, for: upload) }
        #expect(!FileManager.default.fileExists(atPath: response.path))
    }

    @Test func connectivityKeepsEveryPathAndEndsRatherThanSkipOne() async throws {
        let (runtime, outlet, _) = started()
        let connectivity = BridgeConnectivity(channel: try #require(runtime.device))
        let paths = connectivity.paths(capacity: 2)
        let request = try await outlet.next { $0.op == .request }
        let sent = [NetworkPath(online: true, expensive: false), NetworkPath(online: false, expensive: false),
                    NetworkPath(online: true, expensive: true), NetworkPath(online: false, expensive: true)]
        for (seq, path) in sent.enumerated() {
            _ = try post(runtime, Envelope(op: .chunk, id: request.id, seq: seq, body: try BridgeCodec.value(path)))
        }
        // The reader fell behind: Kotlin is told to stop, and the reader gets
        // what it had room for from the oldest on, then the end, never a gap.
        // How many fit depends only on when the pump between the two queues
        // ran, so the paths are held to that and not to a count: at least
        // the capacity, and short of all four, since one overflowed.
        _ = try await outlet.next { $0.op == .cancel && $0.id == request.id }
        var read: [NetworkPath] = []
        for await path in paths { read.append(path) }
        #expect(read == Array(sent.prefix(read.count)))
        #expect((2 ..< sent.count).contains(read.count), "\(read.count) paths read")
    }

    @Test func aLineStreamArrivesAsKotlinReadsIt() async throws {
        let (runtime, outlet, _) = started()
        let transport = BridgeTransport(channel: try #require(runtime.device), folder: FileManager.default.temporaryDirectory)
        let reading = Task { () -> (Int, [String]) in
            let stream = try await transport.lines(for: URLRequest(url: URL(string: "https://example.com/watch")!))
            var lines: [String] = []
            for try await line in stream.lines { lines.append(line) }
            return (stream.head.statusCode, lines)
        }
        let request = try await outlet.next { $0.op == .request }
        let chunks: [TransportChunk] = [.head(response: TransportResponse(status: 200, url: "https://example.com/watch", headers: [])),
                                        .line(text: #"{"n":1}"#), .line(text: #"{"n":2}"#)]
        for (seq, chunk) in chunks.enumerated() {
            _ = try post(runtime, Envelope(op: .chunk, id: request.id, seq: seq, body: try BridgeCodec.value(chunk)))
        }
        _ = try post(runtime, Envelope(op: .end, id: request.id))
        let (status, lines) = try await reading.value
        #expect(status == 200)
        #expect(lines == [#"{"n":1}"#, #"{"n":2}"#])
    }

    @Test func droppingALineStreamCancelsItOnKotlinsSide() async throws {
        let (runtime, outlet, _) = started()
        let transport = BridgeTransport(channel: try #require(runtime.device), folder: FileManager.default.temporaryDirectory)
        let opening = Task { try await transport.lines(for: URLRequest(url: URL(string: "https://example.com/watch")!)).head.statusCode }
        let request = try await outlet.next { $0.op == .request }
        _ = try post(runtime, Envelope(op: .chunk, id: request.id, seq: 0, body: try BridgeCodec.value(
            TransportChunk.head(response: TransportResponse(status: 200, url: "https://example.com/watch", headers: [])))))
        #expect(try await opening.value == 200)
        let cancel = try await outlet.next { $0.op == .cancel && $0.id == request.id }
        #expect(cancel.id == request.id)
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
