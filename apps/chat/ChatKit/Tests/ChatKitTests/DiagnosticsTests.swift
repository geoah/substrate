import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import ChatKit

/// The diagnostics file a person can share (Settings, Share Diagnostics):
/// decisions and timings, and never a word of a message, a title, a
/// record's name, a server's address or a token.
@Suite("The diagnostics file", .timeLimit(.minutes(10)))
struct Diagnostics {
    /// A line is made of words of this source, numbers and words of closed
    /// sets. Whatever else it is handed is written as `other`, and a
    /// failure is written as the value it is, without what the server said
    /// or the device saw.
    @Test func aLineHoldsOnlyWordsOfClosedSetsAndNumbers() {
        var failure = Failure(.reply, .run(status: "error", cause: "provider_overloaded"), said: "Anthropic said: overloaded, ada@example.com", retryAfter: 20)
        failure.seen = "The connection to s.example.com was lost."
        #expect(Diagnostic("failure").with(failure).text == "failure attempt=reply why=run status=error cause=provider_overloaded retry_after=20")
        // A cause, a status, a code and a reason this build does not know
        // are the server's words: none is written.
        let newer = Failure(.reply, .run(status: "Paused by Ada", cause: "the model at s.example.com is down"))
        #expect(Diagnostic("failure").with(newer).text == "failure attempt=reply why=run status=other cause=other")
        let refused = Failure(.send, .http(status: 409, code: "say 1.", reason: "thread_busy"), said: "the thread t1 is busy")
        #expect(Diagnostic("failure").with(refused).text == "failure attempt=send why=http status=409 code=other reason=thread_busy")
        let known = Failure(.send, .http(status: 429, code: "rate_limited", reason: "my secret"))
        #expect(Diagnostic("failure").with(known).text == "failure attempt=send why=http status=429 code=rate_limited reason=other")
        #expect(Diagnostic("cut").with(Failure(.reply, .cut("length"))).text == "cut attempt=reply why=cut by=length")
        #expect(Diagnostic("cut").with(Failure(.reply, .cut("Because I said so"))).text == "cut attempt=reply why=cut by=other")
        // Every reason there is has its word, and each is one word.
        let every: [Failure.Why] = [
            .unanswered, .offline, .unreachable, .timeout, .streamEnded, .sessionEnded, .needsApproval, .notTheAPI(status: 502), .ended, .closed,
            .interrupted, .disk,
        ]
        for why in every {
            let line = Diagnostic("failure").with(Failure(.poll, why)).text
            #expect(SeededRun.shaped("2026-11-15T10:00:00.000Z \(line)"), "\(line)")
        }
        // The seeded rule holds a line to the file's own vocabulary: a
        // sentence the server says in lower-case words is no line of it
        // (the review's finding: it passed as one).
        #expect(!SeededRun.shaped("2026-11-15T10:00:00.000Z a turn is running on the thread"))
        #expect(!SeededRun.shaped("2026-11-15T10:00:00.000Z failure attempt=reply why=run cause=tomatoes"))
        #expect(!SeededRun.shaped("2026-11-15T10:00:00.000Z failure subject=other"))
        #expect(SeededRun.shaped("2026-11-15T10:00:00.000Z chat 2 failure attempt=reply why=run status=error cause=other"))
        // A number, a yes or a no, a time.
        let facts = Diagnostic("send", chat: "t1").with("copy", 2).with("joins", false).with("seconds", seconds: 1.5).with("none", nil as Int?)
        #expect(facts.text == "send copy=2 joins=no seconds=1.500")
    }

    /// The ring: appended a line at a time, bounded, kept by the folder
    /// across launches, and oldest first when it is read.
    @Test func theFileIsABoundedRingKeptAcrossLaunches() throws {
        let root = temporaryFolder("diagnostics")
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        var file = DiagnosticsFile(root: root, writingOptions: [])
        file.limit = 400
        #expect(file.lines().isEmpty)
        let lines = (0 ..< 40).map { "2026-11-15T10:00:\(String(format: "%02d", $0)).000Z step number=\($0)" }
        for line in lines.prefix(20) { try file.append([line]) }
        // Another launch: the same folder, the same lines, and on from them.
        var again = DiagnosticsFile(root: root, writingOptions: [])
        again.limit = 400
        for line in lines.dropFirst(20) { try again.append([line]) }
        let held = again.lines()
        // The newest are all there, in order; the oldest went with the
        // file before the one before.
        #expect(held.last == lines.last && held == Array(lines.suffix(held.count)))
        #expect(held.count < lines.count && held.count >= 400 / 60)
        let size = { (name: String) in ((try? FileManager.default.attributesOfItem(atPath: root.appendingPathComponent(name).path))?[.size] as? Int) ?? 0 }
        #expect(size(DiagnosticsFile.name) <= 400 && size(DiagnosticsFile.older) <= 400)
        // A batch larger than a file is held to the bound too, by its
        // newest lines (the review's finding: it was written whole).
        try again.append(lines + lines)
        #expect(size(DiagnosticsFile.name) <= 400 && size(DiagnosticsFile.older) <= 400 && again.lines().last == lines.last)
    }

    private func lines(_ rig: ChatRig) -> [String] { DiagnosticsFile(root: rig.root, writingOptions: []).lines() }

    /// A chat's own story, as the file tells it: the send and its copy,
    /// the stream, the failure by its cause, the notification, the mark.
    /// And nothing of what was said in it.
    @Test func aRunIsToldAsDecisionsAndNeverInItsWords() async throws {
        let app = Answering(agents: 7, records: 9)
        try await app.signIn()
        let key = try await app.chat([.piece(0), .piece(1)], failing: true, saying: "Where is the canary?")
        app.work(3)
        try await app.settled(key)
        await app.rig.harness.send(.followEnded(key: key, by: .wheel))
        await app.rig.harness.send(.diagnostics(shown: true))
        try await app.rig.harness.until { harness in
            let report = await harness.presentation.diagnostics
            return report != nil && report?.empty == nil
        }
        let report = try #require(await app.rig.harness.presentation.diagnostics)
        let held = lines(app.rig)
        // What is shown is what is shared, and is the folder's every line.
        #expect(report.text.hasPrefix("Substrate Chat diagnostics\nreport platform=macos agents=7 records=9 lines=\(held.count)\n"))
        #expect(report.text.hasSuffix(held.joined(separator: "\n")))
        let said = held.map { $0.split(separator: " ").dropFirst().joined(separator: " ") }
        #expect(said.contains("launch platform=macos folder_read=no unsent=0"))
        #expect(said.contains("features agents=7 records=9 flags=yes"))
        #expect(said.contains("chat 1 send id=yes joins=no typed=yes placed=no"))
        #expect(said.contains("chat 1 send out copy=1 try=1"))
        #expect(said.contains("chat 1 stream opened phase=streaming"))
        // The turn failed: said by its cause, then joined again to read
        // how it ended, and its rows read.
        #expect(said.contains("chat 1 stream ended phase=streaming attempt=send why=run status=error cause=provider_overloaded"))
        #expect(said.contains("chat 1 run of=attach attempt=0") && said.contains("chat 1 stream idle status=error"))
        #expect(said.contains("chat 1 handover rows=2 settles=1"))
        #expect(said.contains("chat 1 follow stopped by=wheel"))
        #expect(said.contains { $0.hasPrefix("timing of=flush count=") })
        for line in held {
            #expect(SeededRun.shaped(line), "\(line)")
            for word in ["canary", "Where", "example.com", "secret", "Helper", "t1", "r1.0", "overloaded_error", "run ended"] {
                #expect(!line.contains(word), "\(word) in \(line)")
            }
        }
        // Closed, the tree carries it no longer.
        await app.rig.harness.send(.diagnostics(shown: false))
        #expect(await app.rig.harness.presentation.diagnostics == nil)
    }

    /// A sign-out takes the file with the folder; a launch after a kill
    /// goes on from it.
    @Test func itIsKeptAcrossALaunchAndGoesWithASignOut() async throws {
        let app = Answering()
        try await app.signIn()
        try await app.rig.harness.until { _ in lines(app.rig).contains { $0.contains(" launch ") } }
        await app.rig.runtime.close()
        let again = app.relaunched()
        try await again.signIn()
        try await again.harness.until { _ in lines(again).count { $0.contains(" launch ") } == 2 }
        await again.signOut()
        try await again.harness.until { _ in !FileManager.default.fileExists(atPath: again.root.appendingPathComponent(DiagnosticsFile.name).path) }
        #expect(lines(again).isEmpty)
    }

    /// What the kept points cost is said by point, with a count and two
    /// times, and a point nothing reached says nothing.
    @Test func timingsAreSaidByPoint() {
        var flush = Meter.Entry(point: "flush", detail: "", stat: Meter.Stat())
        (flush.stat.count, flush.stat.nanoseconds, flush.stat.longest) = (4, 6_000_000, 3_000_000)
        let idle = Meter.Entry(point: "parse", detail: "", stat: Meter.Stat())
        let odd = Meter.Entry(point: "a point named by somebody", detail: "", stat: flush.stat)
        #expect(
            ChatApp.timings([flush, idle, odd]).map(\.text) == [
                "timing of=flush count=4 mean_ms=1.50 longest_ms=3.00", "timing of=other count=4 mean_ms=1.50 longest_ms=3.00",
            ])
    }
}
