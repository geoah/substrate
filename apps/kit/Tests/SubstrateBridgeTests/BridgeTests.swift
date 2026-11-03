import Foundation
@testable import SubstrateBridge
import SubstrateCore
import Testing

@Suite("The bridge's runtime, off Android")
struct BridgeTests {
    @Test func aMessageBeforeStartIsRefused() throws {
        let runtime = BridgeRuntime()
        let reply = try post(runtime, Envelope(op: .open, body: ["root": "test"]))
        #expect(reply.op == .error)
        #expect(try BridgeCodec.decode(BridgeFailure.self, from: reply.body).code == .notStarted)
    }

    @Test func aFingerprintMismatchIsTerminalAndNothingIsHalfStarted() throws {
        let runtime = BridgeRuntime()
        var prepared = false
        let answer = runtime.start(config(fingerprint: "kotlin's"), outlet: Outlet(), fingerprint: "swift's", prepare: {
            prepared = true
            return nil
        })
        guard case let .failed(failure) = answer else { Issue.record("started: \(answer)"); return }
        #expect(failure.code == .protocolMismatch)
        #expect(!prepared)
        #expect(runtime.environment == nil)
        // A second start answers what the first came to.
        #expect(runtime.start(config(fingerprint: "swift's"), outlet: Outlet(), fingerprint: "swift's") == answer)
        let reply = try post(runtime, Envelope(op: .open, body: ["root": "test"]))
        #expect(try BridgeCodec.decode(BridgeFailure.self, from: reply.body).code == .notStarted)
    }

    @Test func aFailedPrepareLeavesTheBridgeUnstarted() {
        let runtime = BridgeRuntime()
        let answer = runtime.start(config(fingerprint: "x"), outlet: Outlet(), fingerprint: "x", prepare: {
            BridgeFailure(.swiftFailure, "no Looper")
        })
        #expect(answer == .failed(failure: BridgeFailure(.swiftFailure, "no Looper")))
        #expect(runtime.environment == nil)
    }

    @Test func theEnvironmentCarriesTheDevicesSettings() throws {
        let (runtime, _, _) = started()
        let environment = try #require(runtime.environment)
        #expect(environment.locale.identifier == "en-GB" || environment.locale.identifier == "en_GB")
        #expect(environment.timeZone.identifier == "Europe/London")
        #expect(environment.folders.data.path == "/tmp/data")
    }

    @Test func malformedMessagesAreStructuredErrors() throws {
        let (runtime, _, _) = started()
        for text in ["", "not json", #"{"v":2,"op":"open"}"#, #"{"v":1,"op":"teleport"}"#, #"{"v":1,"op":"command"}"#,
                     #"{"v":1,"op":"presentation"}"#, #"{"v":1,"op":"open","body":{"nope":1}}"#]
        {
            let reply = try BridgeCodec.decodeEnvelope(runtime.post(text))
            #expect(reply.op == .error, "\(text)")
            #expect(try BridgeCodec.decode(BridgeFailure.self, from: reply.body).code == .malformed, "\(text)")
        }
        let unknown = try post(runtime, Envelope(op: .open, body: ["root": "nothing"]))
        #expect(try BridgeCodec.decode(BridgeFailure.self, from: unknown.body).code == .unknownRoot)
    }

    @Test func commandsArriveInOrderAndPresentationsComeBack() async throws {
        let (runtime, outlet, box) = started()
        let handle = try open(runtime)
        #expect(handle == Handle(id: 1, generation: 1))
        for n in 1 ... 50 {
            #expect(try post(runtime, Envelope(op: .command, handle: handle, body: ["n": JSONValue(integerLiteral: n)])).op == .accepted)
        }
        _ = try await post(runtime, Envelope(op: .command, handle: handle, body: ["present": 1]))
        _ = try await outlet.next { $0.op == .presentation }
        let commands = await box.value?.commands.compactMap { $0["n"]?.int }
        #expect(commands == Array(1 ... 50))
    }

    @Test func aThrowingRootReportsAFailureForItsHandleAndStaysOpen() async throws {
        let (runtime, outlet, _) = started()
        let handle = try open(runtime)
        _ = try post(runtime, Envelope(op: .command, handle: handle, body: ["throw": true]))
        let error = try await outlet.next { $0.op == .error && $0.handle == handle }
        #expect(try BridgeCodec.decode(BridgeFailure.self, from: error.body).code == .swiftFailure)
        #expect(try post(runtime, Envelope(op: .command, handle: handle, body: ["present": 1])).op == .accepted)
    }

    @Test func closeIsIdempotentAndAnOldGenerationIsDroppedAndCounted() async throws {
        let (runtime, outlet, box) = started()
        let first = try open(runtime)
        #expect(try post(runtime, Envelope(op: .close, handle: first)).op == .accepted)
        #expect(try post(runtime, Envelope(op: .close, handle: first)).op == .accepted)
        _ = try await outlet.next { $0.op == .closed && $0.handle == first }
        #expect(await box.value?.closed == true)
        let second = try open(runtime)
        #expect(second == Handle(id: 1, generation: 2))
        let stale = try post(runtime, Envelope(op: .command, handle: first, body: ["present": 1]))
        #expect(try BridgeCodec.decode(BridgeFailure.self, from: stale.body).code == .staleHandle)
        #expect(runtime.stats.dropped == 1)
        #expect(runtime.stats.open == 1)
        let never = try post(runtime, Envelope(op: .close, handle: Handle(id: 9, generation: 1)))
        #expect(try BridgeCodec.decode(BridgeFailure.self, from: never.body).code == .staleHandle)
        // Nothing for the closed handle comes after `closed`.
        let closedAt = try #require(outlet.messages.lastIndex { $0.op == .closed && $0.handle == first })
        #expect(!outlet.messages.dropFirst(closedAt + 1).contains { $0.handle == first })
    }

    @Test func aCallIsAnsweredExactlyOnce() async throws {
        let (runtime, outlet, _) = started()
        let handle = try open(runtime)
        _ = try post(runtime, Envelope(op: .command, handle: handle, body: ["call": true]))
        let request = try await outlet.next { $0.op == .request }
        #expect(try BridgeCodec.decode(DeviceRequest.self, from: request.body) == .dnsTXT(name: "example.com", timeoutMillis: 100))
        let answer = try BridgeCodec.value(DNSAnswer.answered(rcode: 3, records: []))
        #expect(try post(runtime, Envelope(op: .reply, handle: handle, id: request.id, body: answer)).op == .accepted)
        let again = try post(runtime, Envelope(op: .reply, handle: handle, id: request.id, body: answer))
        #expect(try BridgeCodec.decode(BridgeFailure.self, from: again.body).code == .unknownCall)
        #expect(runtime.stats.refused == 1)
        let shown = try await outlet.next { $0.op == .presentation }
        #expect(shown.body?["answer"]?.string?.contains("answered") == true)
    }

    @Test func closingAHandleCancelsWhatItWaitsOnOnceAndTellsKotlin() async throws {
        let (runtime, outlet, _) = started()
        let handle = try open(runtime)
        _ = try post(runtime, Envelope(op: .command, handle: handle, body: ["call": true]))
        let request = try await outlet.next { $0.op == .request }
        _ = try post(runtime, Envelope(op: .close, handle: handle))
        let cancel = try await outlet.next { $0.op == .cancel }
        #expect(cancel.id == request.id)
        let closed = try await outlet.next { $0.op == .closed }
        #expect(outlet.messages.firstIndex(of: cancel)! < outlet.messages.firstIndex(of: closed)!)
        // The root saw `cancelled`, and its presentation was not delivered after the close.
        #expect(!outlet.messages.contains { $0.op == .presentation })
        let late = try post(runtime, Envelope(op: .reply, id: request.id, body: .null))
        #expect(try BridgeCodec.decode(BridgeFailure.self, from: late.body).code == .unknownCall)
        #expect(runtime.stats.cancelled == 1)
        #expect(runtime.calls.outstanding == 0)
    }

    @Test func cancellingTheCallerCancelsTheCall() async throws {
        let (runtime, outlet, _) = started()
        let channel = try #require(runtime.device)
        let task = Task { try await channel.call(.connectivityCurrent) }
        let request = try await outlet.next { $0.op == .request }
        task.cancel()
        await #expect(throws: BridgeFailure.self) { try await task.value }
        let cancel = try await outlet.next { $0.op == .cancel }
        #expect(cancel.id == request.id)
    }

    @Test func aStreamTakesItsChunksInOrderAndEndsOnce() async throws {
        let (runtime, outlet, _) = started()
        let handle = try open(runtime)
        _ = try post(runtime, Envelope(op: .command, handle: handle, body: ["stream": 16]))
        let request = try await outlet.next { $0.op == .request }
        for seq in 0 ..< 5 {
            #expect(try post(runtime, Envelope(op: .chunk, id: request.id, seq: seq, body: JSONValue(integerLiteral: seq))).op == .accepted)
        }
        #expect(try post(runtime, Envelope(op: .end, id: request.id)).op == .accepted)
        let twice = try post(runtime, Envelope(op: .end, id: request.id))
        #expect(try BridgeCodec.decode(BridgeFailure.self, from: twice.body).code == .unknownCall)
        let after = try post(runtime, Envelope(op: .chunk, id: request.id, seq: 5, body: 5))
        #expect(try BridgeCodec.decode(BridgeFailure.self, from: after.body).code == .unknownCall)
        let shown = try await outlet.next { $0.op == .presentation }
        #expect(shown.body?["streamed"] == [0, 1, 2, 3, 4])
    }

    @Test func aChunkOutOfOrderFailsTheStream() async throws {
        let (runtime, outlet, _) = started()
        let handle = try open(runtime)
        _ = try post(runtime, Envelope(op: .command, handle: handle, body: ["stream": 16]))
        let request = try await outlet.next { $0.op == .request }
        _ = try post(runtime, Envelope(op: .chunk, id: request.id, seq: 0, body: 0))
        let skipped = try post(runtime, Envelope(op: .chunk, id: request.id, seq: 2, body: 2))
        #expect(try BridgeCodec.decode(BridgeFailure.self, from: skipped.body).code == .malformed)
        let shown = try await outlet.next { $0.op == .presentation }
        #expect(shown.body?["streamFailure"] == "malformed")
    }

    @Test func aFullStreamOverflowsCancelsAndTellsTheReader() async throws {
        let (runtime, outlet, _) = started()
        let handle = try open(runtime)
        // A reader that waits a long time per value, behind a queue of two.
        _ = try post(runtime, Envelope(op: .command, handle: handle, body: ["stream": 2, "pause": 300]))
        let request = try await outlet.next { $0.op == .request }
        var overflowedAt: Int?
        for seq in 0 ..< 10 {
            let reply = try post(runtime, Envelope(op: .chunk, id: request.id, seq: seq, body: JSONValue(integerLiteral: seq)))
            if reply.op == .error { overflowedAt = seq; break }
        }
        let cancel = try await outlet.next { $0.op == .cancel && $0.id == request.id }
        #expect(cancel.id == request.id)
        #expect(runtime.stats.overflowed == 1)
        // The chunk after the overflow is refused: the stream ended once.
        let next = try post(runtime, Envelope(op: .chunk, id: request.id, seq: (overflowedAt ?? 9) + 1, body: 0))
        #expect(next.op == .error)
        let shown = try await outlet.next { $0.op == .presentation }
        #expect(shown.body?["streamFailure"] == "overflow")
    }

    @Test func presentationsCoalesceWhileKotlinIsSlowAndTheNewestArrives() async throws {
        let (runtime, outlet, _) = started()
        let handle = try open(runtime)
        outlet.hold()
        _ = try post(runtime, Envelope(op: .command, handle: handle, body: ["present": 500]))
        try await Task.sleep(for: .milliseconds(100))
        outlet.release()
        let last = try await outlet.next { $0.op == .presentation && $0.body?["step"]?.int == 500 }
        #expect(last.body?["step"] == 500)
        let delivered = outlet.messages.filter { $0.op == .presentation }.count
        #expect(delivered < 500)
        #expect(runtime.stats.coalesced == 500 - delivered)
        // In order: each one newer than the last.
        let steps = outlet.messages.filter { $0.op == .presentation }.compactMap { $0.body?["step"]?.int }
        #expect(steps == steps.sorted())
    }

    @Test func kotlinCanAskForTheCounters() async throws {
        let (runtime, outlet, _) = started()
        _ = try post(runtime, Envelope(op: .request, id: 77, body: try BridgeCodec.value(BridgeQuery.stats)))
        let reply = try await outlet.next { $0.op == .reply && $0.id == 77 }
        #expect(try BridgeCodec.decode(BridgeStats.self, from: reply.body).posted >= 1)
    }
}
