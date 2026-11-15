import Foundation
import SubstrateDevice
import SubstrateRuntime
import SubstrateTesting
import Testing

/// The signposts a meter wrote, read back.
private final class RecordedSignposts: Signposts, @unchecked Sendable {
    private let lock = NSLock()
    private var kept: [String] = []

    var lines: [String] { lock.withLock { kept } }

    func begin(_ name: StaticString, _ detail: String) -> @Sendable () -> Void {
        lock.withLock { kept.append("begin \(name) \(detail)") }
        return { self.lock.withLock { self.kept.append("end \(name) \(detail)") } }
    }

    func event(_ name: StaticString, _ detail: String) {
        lock.withLock { kept.append("event \(name) \(detail)") }
    }
}

@Suite struct MeterTests {
    @Test func offItRecordsNothingAndBuildsNoDetail() {
        let meter = Meter()
        var built = 0
        func detail() -> String {
            built += 1
            return "row"
        }
        meter.count(.body, detail())
        #expect(meter.time(.present, detail()) { 7 } == 7)
        meter.record(.mainPass, detail(), nanoseconds: 5)
        #expect(meter.begin(.flush, detail()) == nil)
        #expect(built == 0)
        #expect(meter.take().isEmpty)
    }

    @Test func onItCountsAndTimesByPointAndDetailAndATakeStartsOver() {
        let meter = Meter()
        let signposts = RecordedSignposts()
        meter.start(signposts: signposts)
        meter.count(.body, "Turn")
        meter.count(.body, "Turn")
        meter.count(.body, "Row")
        meter.time(.present, "List") {}
        meter.record(.mainPass, nanoseconds: 3_000_000)
        meter.record(.mainPass, nanoseconds: 1_000_000)
        let taken = meter.take()
        #expect(taken.map { "\($0.point) \($0.detail) \($0.stat.count)" } == ["body Row 1", "body Turn 2", "main  2", "present List 1"])
        let main = try? #require(taken.first { $0.point == "main" })
        #expect(main?.stat.nanoseconds == 4_000_000)
        #expect(main?.stat.longest == 3_000_000)
        #expect(main?.stat.milliseconds == 4)
        #expect(signposts.lines == ["event body Turn", "event body Turn", "event body Row", "begin present List", "end present List"])
        // Counting went on from nothing, and stops with `stop`.
        meter.count(.body, "Turn")
        #expect(meter.stop().map(\.stat.count) == [1])
        meter.count(.body, "Turn")
        #expect(meter.take().isEmpty)
    }

    @Test func whatBeganInOneMeasurementIsNotCountedInTheNext() {
        let meter = Meter()
        meter.start()
        let span = meter.begin(.flush)
        meter.stop()
        // Ended after the stop, and after another start: counted in neither.
        meter.end(span)
        #expect(meter.take().isEmpty)
        meter.start()
        meter.end(span)
        meter.count(.body, "Turn")
        #expect(meter.stop().map(\.point) == ["body"])
    }

    /// What an app keeps for its own record, with no measurement on: the
    /// kept points alone, by point, with no detail built and no signpost.
    @Test func keepingItTimesTheKeptPointsAloneWhateverAMeasurementDoes() {
        let meter = Meter()
        var built = 0
        func detail() -> String {
            built += 1
            return "row"
        }
        // Not kept until asked.
        meter.time(.flush) {}
        #expect(meter.takeKept().isEmpty)
        meter.keep()
        meter.time(.flush, detail()) {}
        meter.time(.flush) {}
        meter.time(.present, detail()) {}
        meter.count(.body, detail())
        #expect(built == 0 && meter.take().isEmpty)
        let kept = meter.takeKept()
        #expect(kept.map { "\($0.point) \($0.detail) \($0.stat.count)" } == ["flush  2"])
        #expect(meter.takeKept().isEmpty)
        // Beside a measurement, each is counted in its own figures, and a
        // measurement that stops takes nothing from what is kept.
        meter.start()
        meter.time(.flush, "List") {}
        meter.time(.present, "List") {}
        #expect(meter.stop().map { "\($0.point) \($0.detail)" } == ["flush List", "present List"])
        meter.time(.flush) {}
        #expect(meter.takeKept().map(\.stat.count) == [2])
    }

    @Test func theRuntimeTimesItsPresentersItsFlushesAndItsPublishes() async throws {
        // The process's meter: other suites' runtimes count into it too
        // while it is on, so only this controller's own lines are read.
        Meter.shared.start()
        defer { Meter.shared.stop() }
        let harness = ControllerHarness(Metered())
        await harness.send(.add)
        await harness.send(.add)
        #expect(await harness.presentation == 2)
        let taken = Meter.shared.take()
        let presented = try #require(taken.first { $0.point == "present" && $0.detail == "Metered" })
        // Once at open, then once for each command.
        #expect(presented.stat.count == 3)
        #expect(taken.contains { $0.point == "flush" && $0.stat.count >= 2 })
        #expect(taken.contains { $0.point == "publish" && $0.stat.count >= 2 })
    }
}

/// A count, and nothing else: named for the one test that reads its lines.
private struct Metered: Controller {
    enum Command: Codable, Equatable, Sendable { case add }
    enum Event: Codable, Equatable, Sendable { case added }

    var initialState: Int { 0 }

    func events(for command: Command, in context: Enrichment<Metered>) -> [Event] { [.added] }
    func apply(_ event: Event, to state: Int) throws -> Int { state + 1 }
    func present(_ input: PresenterInput<Metered>) -> Int { input.state }
}
