import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import ChatKit

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// The seeds a run takes, from the environment: `CHATKIT_SEED` replays
/// one; `CHATKIT_SEEDS` runs that many from `CHATKIT_FIRST_SEED` on, each
/// of `CHATKIT_STEPS` steps, `CHATKIT_WIDTH` at a time. With none set it is
/// the bounded run every `swift test` makes.
private struct SeedPlan {
    var seeds: [UInt64]
    var steps: Int
    var width: Int
    /// `CHATKIT_VERBOSE` prints a line per seed: what the run reached.
    var verbose: Bool

    init() {
        let environment = ProcessInfo.processInfo.environment
        func number(_ name: String) -> Int? { environment[name].flatMap(Int.init) }
        verbose = environment["CHATKIT_VERBOSE"] != nil
        steps = number("CHATKIT_STEPS") ?? 60
        width = max(1, number("CHATKIT_WIDTH") ?? 4)
        if let one = environment["CHATKIT_SEED"].flatMap(UInt64.init) {
            seeds = [one]
        } else {
            let first = UInt64(number("CHATKIT_FIRST_SEED") ?? 1)
            seeds = (0 ..< UInt64(number("CHATKIT_SEEDS") ?? 24)).map { first + $0 }
        }
    }
}

@Suite("A server that misbehaves by a seed", .timeLimit(.minutes(240)))
struct SeededChat {
    @Test func everySeedKeepsEveryRule() async {
        let plan = SeedPlan()
        var broken: [SeededRun.Violation] = []
        var noted: [SeededRun.Rule: [SeededRun.Violation]] = [:]
        var steps = 0
        var tally: [String: Int] = [:]
        await withTaskGroup(of: SeededRun.self) { group in
            var waiting = plan.seeds[...]
            var running = 0
            while !waiting.isEmpty || running > 0 {
                while running < plan.width, let seed = waiting.popFirst() {
                    running += 1
                    group.addTask {
                        var run = SeededRun(seed: seed)
                        await run.run(steps: plan.steps)
                        if plan.verbose { print(run.summary) }
                        return run
                    }
                }
                guard let run = await group.next() else { break }
                running -= 1
                steps += run.steps.count
                tally.merge(run.tally) { $0 + $1 }
                if let violation = run.broken { broken.append(violation) }
                for (rule, violation) in run.noted { noted[rule, default: []].append(violation) }
            }
        }
        for violation in broken.sorted(by: { $0.seed < $1.seed }) { Issue.record("\(violation)") }
        // A rule the app is known to break is no failure of this run: it
        // is said once, with the first seed that shows it.
        for (rule, found) in noted.sorted(by: { $0.key.rawValue < $1.key.rawValue }) {
            let first = found.min { $0.seed < $1.seed }!
            withKnownIssue("\(rule.rawValue): \(SeededRun.known[rule] ?? "")", isIntermittent: true) {
                Issue.record("known, in \(found.count) of \(plan.seeds.count) seeds (\(found.map(\.seed).sorted().prefix(12))); the first:\n\(first)")
            }
        }
        if plan.verbose || plan.seeds.count > 24 {
            print(
                "seeded: \(plan.seeds.count) seeds, \(steps) steps, \(broken.count) broke a rule, \(noted.values.reduce(0) { $0 + $1.count }) met a known one")
            for (name, count) in tally.sorted(by: { $0.key < $1.key }) { print("seeded:   \(count) × \(name)") }
        }
    }

    /// The rules have teeth: a person who sends a text again without
    /// looking, on a server that takes no id, sends it twice, and the run
    /// says so with the seed and the steps that replay it.
    @Test func aMessageSentTwiceIsCaughtWithItsSeedAndItsSteps() async throws {
        var caught: SeededRun.Violation?
        for seed in UInt64(1) ... 200 where caught == nil {
            var run = SeededRun(seed: seed, revision: 2, careless: true)
            await run.run(steps: 80)
            if let violation = run.broken, violation.rule == SeededRun.Rule.doubled.rawValue { caught = violation }
        }
        let violation = try #require(caught, "no careless run in 200 seeds sent a message twice")
        let printed = "\(violation)"
        #expect(printed.contains("seed \(violation.seed) broke a rule at step \(violation.steps.count): no message is written twice"))
        #expect(printed.contains("replay: CHATKIT_SEED=\(violation.seed) CHATKIT_STEPS=\(violation.steps.count) swift test --filter SeededChat"))
        #expect(violation.steps.contains { $0.hasPrefix("sends ") })
        // The same seed again breaks the same rule.
        var again = SeededRun(seed: violation.seed, revision: 2, careless: true)
        await again.run(steps: 80)
        #expect(again.broken?.rule == violation.rule)
    }
}

@Suite("The seeded server keeps the truth", .timeLimit(.minutes(10)))
struct SeededServerTruth {
    private func post(_ port: SeededServer.Port, _ path: String, _ body: String) async throws -> (status: Int, lines: [String]) {
        var request = URLRequest(url: origin.appendingPathComponent(path))
        request.httpMethod = "POST"
        request.httpBody = Data(body.utf8)
        let stream = try await port.lines(for: request)
        var lines: [String] = []
        for try await line in stream.lines { lines.append(line) }
        return (stream.head.statusCode, lines)
    }

    private let chat = "api/v1/substrate.reamde.dev/core/agent/x.example.com/pkg/helper/chat"

    @Test func aMessageUnderAnIdIsWrittenOnceAndARepeatRunsNothing() async throws {
        let server = SeededServer(seed: 7, clock: TestClock())
        let port = server.connect()
        // The turn runs detached: the first request's stream is left open.
        var request = URLRequest(url: origin.appendingPathComponent(chat))
        request.httpMethod = "POST"
        request.httpBody = Data(#"{"thread":"","message":"Hi","messageId":"a1"}"#.utf8)
        let first = try await port.lines(for: request)
        var opened = first.lines.makeAsyncIterator()
        #expect(try await opened.next() == #"{"kind":"thread","thread":"t1"}"#)
        #expect(server.rows(of: "t1").map(\.id) == ["a1"] && server.running("t1"))
        server.finishTurns()
        // The same body again: nothing written, nothing run, the same
        // thread, and the one line an attach answers.
        let again = try await post(port, chat, #"{"thread":"","message":"Hi","messageId":"a1"}"#)
        #expect(again.lines == [#"{"kind":"thread","thread":"t1"}"#, #"{"kind":"idle","status":"ok","thread":"t1"}"#])
        #expect(server.threadIDs == ["t1"] && server.userRows(saying: "Hi") == 1)
        // Another text under the id, and the id on another thread, are refused.
        #expect(try await post(port, chat, #"{"thread":"","message":"Other","messageId":"a1"}"#).status == 409)
        #expect(try await post(port, chat, #"{"thread":"t9","message":"Hi","messageId":"a1"}"#).status == 409)
        #expect(server.rows(of: "t1").filter { $0.role == "user" }.count == 1)
        // A continue of a finished thread has nothing to take up.
        let resumed = try await post(port, "api/v1/substrate.reamde.dev/llm/thread/t1/continue", "{}")
        #expect(resumed.status == 409 && resumed.lines.joined().contains("nothing_to_continue"))
    }

    @Test func anOlderServerRefusesTheFieldAndHasNoContinue() async throws {
        let server = SeededServer(seed: 7, clock: TestClock(), revision: 2)
        let port = server.connect()
        let refused = try await post(port, chat, #"{"thread":"","message":"Hi","messageId":"a1"}"#)
        #expect(refused.status == 400 && refused.lines.joined().contains(#"unknown field \"messageId\""#))
        #expect(server.threadIDs.isEmpty)
        #expect(try await post(port, "api/v1/substrate.reamde.dev/llm/thread/t1/continue", "{}").status == 404)
    }

    @Test func aKilledLaunchReachesNothing() async throws {
        let server = SeededServer(seed: 7, clock: TestClock())
        let old = server.connect()
        _ = server.connect()
        await #expect(throws: CancellationError.self) {
            _ = try await post(old, chat, #"{"thread":"","message":"Hi","messageId":"a1"}"#)
        }
        #expect(server.threadIDs.isEmpty)
    }

    @Test func theSameSeedDrawsTheSameFaults() async throws {
        func statuses(_ seed: UInt64) async -> [String] {
            let clock = TestClock()
            let server = SeededServer(seed: seed, clock: clock)
            let port = server.connect()
            server.misbehave(0.6)
            var out: [String] = []
            for index in 0 ..< 40 {
                var request = URLRequest(url: origin.appendingPathComponent("api/v1/substrate.reamde.dev/llm/thread/t\(index % 3)"))
                request.timeoutInterval = 30
                let answered = Answered()
                let reading = Task { () -> String in
                    defer { answered.set() }
                    do { return String(try await port.data(for: request).1.statusCode) } catch { return "\((error as? URLError)?.code.rawValue ?? 0)" }
                }
                // A hang and a delay wait on the clock.
                while !answered.isSet {
                    await Task.yield()
                    if clock.sleeping > 0 { clock.advance(by: 40) }
                }
                out.append(await reading.value)
            }
            return out
        }
        let first = await statuses(11)
        #expect(await statuses(11) == first)
        #expect(await statuses(12) != first)
        // The thread is not there (404), and the draws are of many kinds:
        // a gateway's page, a rate limit, a timeout among them.
        #expect(Set(first).isSuperset(of: ["404", "502", "429", "200", "\(URLError.Code.timedOut.rawValue)"]) && Set(first).count >= 7)
    }
}

/// Whether a request has been answered yet.
private final class Answered: @unchecked Sendable {
    private let lock = NSLock()
    private var done = false

    func set() { lock.withLock { done = true } }
    var isSet: Bool { lock.withLock { done } }
}
