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
/// the bounded run every `swift test` makes. For a seed that broke a rule:
/// `CHATKIT_VERBOSE` adds every conversation's state and the last step's
/// trace to what it prints, `CHATKIT_TRACE` keeps the trace of the whole
/// run, `CHATKIT_WATCH=<text>` prints where that text is after each
/// step, and `CHATKIT_MARK=<thread>` what the app and the server hold of
/// that thread's mark.
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
    @Test func everySeedKeepsEveryRule() async throws {
        let plan = SeedPlan()
        var broken: [SeededRun.Violation] = []
        var noted: [SeededRun.Rule: [SeededRun.Violation]] = [:]
        var steps = 0
        var tally: [String: Int] = [:]
        try await withThrowingTaskGroup(of: SeededRun.self) { group in
            var waiting = plan.seeds[...]
            var running = 0
            while !waiting.isEmpty || running > 0 {
                while running < plan.width, let seed = waiting.popFirst() {
                    running += 1
                    group.addTask {
                        let run = try await SeededRun.ran(seed: seed, steps: plan.steps)
                        if plan.verbose { print(run.summary) }
                        return run
                    }
                }
                guard let run = try await group.next() else { break }
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
        // From the first seed that shows it today; the search goes on from
        // there, so a change to the draws still finds one.
        var first: SeededRun?
        for seed in UInt64(17) ... 217 where caught == nil {
            let run = try await SeededRun.ran(seed: seed, revision: 2, careless: true, steps: 80)
            if let violation = run.broken, violation.rule == SeededRun.Rule.doubled.rawValue { (caught, first) = (violation, run) }
        }
        let violation = try #require(caught, "no careless run in 200 seeds sent a message twice")
        let printed = "\(violation)"
        #expect(printed.contains("seed \(violation.seed) broke a rule at step \(violation.steps.count): no message is written twice"))
        #expect(printed.contains("replay: CHATKIT_SEED=\(violation.seed) CHATKIT_STEPS=\(violation.steps.count) swift test --filter SeededChat"))
        #expect(violation.steps.contains { $0.hasPrefix("sends ") })
        // The same seed again is the same run, to the last request.
        let again = try await SeededRun.ran(seed: violation.seed, revision: 2, careless: true, steps: 80)
        #expect(again.broken?.rule == violation.rule && again.steps == violation.steps && again.fingerprint == first?.fingerprint)
    }

    /// A seed is one run, whatever else the host is doing: alone, and
    /// beside other runs that take the processor from it, it takes the same
    /// steps, asks the server the same things and leaves the same rows.
    @Test func aSeedRunsTheSameWayWhateverRunsBesideIt() async throws {
        let seeds: [UInt64] = [3, 8, 21, 34]
        var alone: [UInt64: SeededRun] = [:]
        for seed in seeds { alone[seed] = try await SeededRun.ran(seed: seed, steps: 60) }
        let crowded = try await withThrowingTaskGroup(of: SeededRun.self) { group in
            for seed in seeds + (40 ..< 52) { group.addTask { try await SeededRun.ran(seed: seed, steps: 60) } }
            var out: [UInt64: SeededRun] = [:]
            for try await run in group { out[run.seed] = run }
            return out
        }
        for seed in seeds {
            #expect(crowded[seed]?.steps == alone[seed]?.steps, "seed \(seed) took other steps beside other runs")
            #expect(crowded[seed]?.fingerprint == alone[seed]?.fingerprint, "seed \(seed) came to another end beside other runs")
        }
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
        func statuses(_ seed: UInt64) async throws -> [String] {
            try await SeededThread.run {
                let thread = SeededThread.current!
                let clock = TestClock()
                let server = SeededServer(seed: seed, clock: clock)
                let port = server.connect()
                server.misbehave(0.6)
                var out: [String] = []
                for index in 0 ..< 40 {
                    var request = URLRequest(url: origin.appendingPathComponent("api/v1/substrate.reamde.dev/llm/thread/t\(index % 3)"))
                    request.timeoutInterval = 30
                    let reading = Task { () -> String in
                        do { return String(try await port.data(for: request).1.statusCode) } catch { return "\((error as? URLError)?.code.rawValue ?? 0)" }
                    }
                    // A hang and a delay wait on the clock.
                    await thread.idle()
                    while clock.sleeping > 0 {
                        clock.advance(by: 40)
                        await thread.idle()
                    }
                    out.append(await reading.value)
                }
                return out
            }
        }
        let first = try await statuses(11)
        #expect(try await statuses(11) == first)
        #expect(try await statuses(12) != first)
        // The thread is not there (404), and the draws are of many kinds:
        // a gateway's page, a rate limit, a timeout among them.
        #expect(Set(first).isSuperset(of: ["404", "502", "429", "200", "\(URLError.Code.timedOut.rawValue)"]) && Set(first).count >= 7)
    }
}

/// What a seeded run stands on: the runtime hands a seeded thread every job
/// enqueued from it. Where a toolchain stops doing that, this says so
/// first, and every seeded test fails with `SeededThread.Unordered`'s
/// sentence: none of them passes on the pool instead.
@Suite("A seeded run's thread", .timeLimit(.minutes(10)))
struct SeededThreads {
    private actor Counted {
        private(set) var order: [Int] = []
        private(set) var strays = 0
        func add(_ number: Int, on thread: SeededThread) {
            order.append(number)
            if SeededThread.current !== thread { strays += 1 }
        }
    }

    /// Unstructured tasks, a detached one, a group's children, hops to an
    /// actor, yields and a continuation resumed by another task.
    private func work() async throws -> (order: [Int], strays: Int, idleAt: Int) {
        try await SeededThread.run {
            let thread = SeededThread.current!
            let counted = Counted()
            for number in 0 ..< 20 {
                Task {
                    for _ in 0 ..< number % 4 { await Task.yield() }
                    await counted.add(number, on: thread)
                }
            }
            Task.detached { await counted.add(100, on: thread) }
            await withTaskGroup(of: Void.self) { group in
                for number in 200 ..< 206 { group.addTask { await counted.add(number, on: thread) } }
            }
            let parked = AsyncStream<Int>.makeStream()
            Task {
                for await number in parked.stream { await counted.add(number, on: thread) }
                await counted.add(399, on: thread)
            }
            parked.continuation.yield(300)
            parked.continuation.finish()
            // Idle is exact: everything started above has run to its end.
            await thread.idle()
            let idleAt = await counted.order.count
            return (await counted.order, await counted.strays, idleAt)
        }
    }

    @Test func everyJobStartedOnItRunsOnItInTheOrderItWasEnqueued() async throws {
        let first = try await work()
        #expect(first.strays == 0 && first.idleAt == 29 && SeededThread.current == nil)
        // The same order every time, alone and beside others.
        let beside = try await withThrowingTaskGroup(of: [Int].self) { group in
            for _ in 0 ..< 8 { group.addTask { try await work().order } }
            var out: Set<[Int]> = []
            for try await order in group { out.insert(order) }
            return out
        }
        #expect(beside == [first.order])
    }
}
