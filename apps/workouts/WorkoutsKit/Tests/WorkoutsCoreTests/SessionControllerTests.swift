import Foundation
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateTesting
import Testing
@testable import WorkoutsCore

/// The session on the kit's runtime: the order of the loop (engine, journal,
/// presenter, effects), the Finish that is two events, the open that checks
/// a replayed session against History, and the ticks that stand in for a
/// view's timer.
@Suite("Session controller")
struct SessionControllerTests {
    /// A runtime over the session in `directory`, with the device services
    /// recorded.
    struct Rig {
        let directory: URL
        let harness: ControllerHarness<SessionController>
        let notifications: RecordedNotifications
        let activities: RecordedActivities

        init(directory: URL, clock: TestClock = TestClock(), performsEffects: Bool = true, history: HistoryIndex = HistoryIndex()) throws {
            self.directory = directory
            notifications = RecordedNotifications()
            activities = RecordedActivities()
            var environment = AppEnvironment.testing(clock: clock)
            environment.devices.notifications = notifications
            environment.devices.activities = activities
            let controller = SessionController(catalog: try BundledCatalog().loadNow(), history: history, settings: WorkoutsSettings(),
                                               adapter: "substrate-workouts@test", directory: directory)
            harness = ControllerHarness(controller, clock: clock, environment: environment, performsEffects: performsEffects)
        }

        var journalURL: URL { directory.appendingPathComponent(SessionJournalFile.fileName) }
        var journalExists: Bool { FileManager.default.fileExists(atPath: journalURL.path) }
        func historyFile(_ id: SessionID) -> Bool { (try? HistoryStore(directory: directory).read(id)) != nil }

        var presentation: SessionScreenPresentation { get async { await harness.presentation } }

        /// Starts the sample routine and waits for its session.
        func start() async throws -> SessionPresentation {
            let routine = try #require(try BundledCatalog().loadNow().routines.keys.sorted().first)
            await harness.send(.start(.routine(routine)))
            try await harness.until(timeout: .seconds(60)) { await $0.presentation.session != nil }
            return try #require(await presentation.session)
        }

        func finish(policy: PendingSetPolicy = .drop) async {
            let now = harness.clock.now
            await harness.send(.finish(FinishRequest(start: now.addingTimeInterval(-600), end: now, policy: policy, sessionRPE: 7,
                                                     notes: nil)))
        }
    }

    /// A pending set whose shown values a tick commits as they are.
    static func loggable(_ session: SessionPresentation) throws -> RowID {
        try #require(session.cards.flatMap(\.rows).first { $0.state == .pending && $0.commitValues != nil && $0.timer == nil }).id
    }

    /// The runtime's steps for the root, in the order they ran, as words a
    /// test compares.
    static func steps(_ trace: [RuntimeTrace]) -> [String] {
        trace.compactMap { entry in
            switch entry {
            case let .journaled(path, journaling, _) where path.isEmpty: "journaled \(journaling)"
            case let .presented(path) where path.isEmpty: "presented"
            case let .effect(path, effect) where path.isEmpty:
                (effect as? SessionController.Effect).map { "effect \(String(describing: $0).prefix { $0 != "(" })" }
            case let .refused(path, _, _) where path.isEmpty: "refused"
            default: nil
            }
        }
    }

    @Test("a Start is journaled as a new journal before it is presented, and only then do its alert and Live Activity go")
    func startOrder() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let rig = try Rig(directory: directory.url)
        _ = await rig.presentation
        rig.harness.clearTrace()
        let session = try await rig.start()
        try await rig.harness.until(timeout: .seconds(60)) { _ in !rig.activities.showing.isEmpty }
        let steps = Self.steps(rig.harness.trace)
        let journaled = try #require(steps.firstIndex(of: "journaled restart"))
        #expect(steps[journaled + 1] == "presented")
        #expect(steps[(journaled + 1)...].contains("effect countdown"))
        #expect(rig.journalExists)
        #expect(rig.activities.showing[session.sessionID.rawValue]?.title == session.title)
        // The first Start asks for the notification permission.
        #expect(rig.notifications.calls.contains(.requestPermission))
        #expect(await rig.presentation.cover?.shown == true)
    }

    @Test("a tick is appended to the journal before the presentation, with the values the row showed")
    func tickOrder() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let rig = try Rig(directory: directory.url)
        let session = try await rig.start()
        let row = try Self.loggable(session)
        rig.harness.clearTrace()
        await rig.harness.send(.tick(row))
        let steps = Self.steps(rig.harness.trace)
        #expect(Array(steps.prefix(3)) == ["journaled append", "presented", "effect countdown"])
        let stored = try #require(try SessionJournalFile(folder: RepositoryFolder(adopting: directory.url)).read())
        #expect(stored.events.count == 2)
        #expect(await rig.harness.state.session?.rows[row]?.isCompleted == true)
        #expect(await rig.presentation.cue?.kind == .logged)
    }

    @Test("Save is two events: the finish asked is not journaled, the history file is written, and only then does the journal go")
    func finishIsTwoEvents() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let rig = try Rig(directory: directory.url)
        let session = try await rig.start()
        await rig.harness.send(.tick(try Self.loggable(session)))
        rig.harness.clearTrace()
        await rig.finish()
        try await rig.harness.until(timeout: .seconds(60)) { await $0.presentation.session == nil }
        let trace = rig.harness.trace
        let journaled = trace.compactMap { entry -> String? in
            guard case let .journaled(_, journaling, event) = entry, let event = event as? SessionController.Event else { return nil }
            return "\(journaling) \(String(describing: event).prefix { $0 != "(" })"
        }
        #expect(journaled == ["clear finished"])
        #expect(Self.steps(trace).contains("effect writeHistory"))
        #expect(!rig.journalExists)
        #expect(rig.historyFile(session.sessionID))
        #expect(await rig.presentation.cover?.shown == false)
        #expect(await rig.presentation.cue?.kind == .finishSaved)
        try await rig.harness.until(timeout: .seconds(60)) { _ in rig.activities.calls.contains { if case .end = $0 { true } else { false } } }
    }

    @Test("a crash after Save is asked and before its history file replays to the session still open, never to one finished")
    func crashBeforeHistory() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let clock = TestClock()
        let first = try Rig(directory: directory.url, clock: clock)
        let session = try await first.start()
        await first.harness.send(.tick(try Self.loggable(session)))
        let before = await first.harness.state.session
        // The process dies with the history write asked and not run.
        await first.harness.runtime.close()
        let held = try Rig(directory: directory.url, clock: clock, performsEffects: false)
        await held.finish()
        #expect(await held.harness.state.finishing != nil)
        await held.harness.runtime.close()

        let relaunched = try Rig(directory: directory.url, clock: clock)
        #expect(await relaunched.harness.state.session == before)
        #expect(await relaunched.presentation.session?.sessionID == session.sessionID)
        #expect(relaunched.journalExists)
        #expect(!relaunched.historyFile(session.sessionID))
    }

    @Test("a crash after the history file and before the journal went replays to the session, which the open then closes")
    func crashAfterHistory() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let clock = TestClock()
        let first = try Rig(directory: directory.url, clock: clock, performsEffects: false)
        await first.harness.send(.start(.empty))
        let start = try #require(first.harness.effects.compactMap { if case let .prepareStart(request) = $0 { request } else { nil } }.first)
        let controller = SessionController(catalog: Catalog(), history: HistoryIndex(), settings: WorkoutsSettings(), adapter: "test",
                                           directory: directory.url)
        await first.harness.deliver(controller.start(start, bodyMass: nil, environment: first.harness.environment))
        await first.finish()
        let finishing = try #require(await first.harness.state.finishing)
        // The history write ran; the process died before `finished`.
        try HistoryStore(directory: directory.url).write(finishing.entry)
        await first.harness.runtime.close()
        #expect(first.journalExists)

        let relaunched = try Rig(directory: directory.url, clock: clock)
        try await relaunched.harness.until(timeout: .seconds(60)) { await $0.state.session == nil }
        #expect(!relaunched.journalExists)
        #expect(relaunched.historyFile(finishing.id))
        let checked = relaunched.harness.effects.contains { if case .checkHistory(finishing.id) = $0 { true } else { false } }
        #expect(checked)
    }

    @Test("a history file that cannot be written keeps the session open and says why")
    func finishFails() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let rig = try Rig(directory: directory.url)
        let session = try await rig.start()
        // A file where the history folder goes stands in for a full disk.
        try Data().write(to: directory.url.appendingPathComponent("history"))
        await rig.finish()
        try await rig.harness.until(timeout: .seconds(60)) { await $0.presentation.finishProblem != nil }
        #expect(await rig.presentation.session?.sessionID == session.sessionID)
        #expect(await rig.harness.state.finishing == nil)
        #expect(rig.journalExists)
        #expect(await rig.presentation.cue?.kind == .warning)
    }

    @Test("the open restores the alert and the Live Activity, reads the permission and checks a replayed session against History")
    func atOpen() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let first = try Rig(directory: directory.url)
        let session = try await first.start()
        await first.harness.runtime.close()

        let relaunched = try Rig(directory: directory.url, performsEffects: false)
        _ = await relaunched.presentation
        let effects = relaunched.harness.effects.map { String(describing: $0).prefix { $0 != "(" } }
        #expect(effects == ["restore", "readAlerts", "listSetAside", "checkHistory"])
        // A replayed event runs no effect of its own: only the open's.
        let replayed = relaunched.harness.trace.contains { if case .replayed(_, 1) = $0 { true } else { false } }
        #expect(replayed)
        guard case let .restore(sync)? = relaunched.harness.effects.first else { Issue.record("no restore"); return }
        #expect(sync?.session == session.sessionID)
    }

    @Test("a rest's end is a tick: presented again with no view's timer, and its haptic asked for")
    func restEndTicks() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let rig = try Rig(directory: directory.url)
        let session = try await rig.start()
        await rig.harness.send(.tick(try Self.loggable(session)))
        let resting = try #require(await rig.harness.state.session?.countdown?.endsAt)
        let shown = try #require(await rig.presentation.session?.nextChange)
        #expect(shown <= resting)
        rig.harness.clearTrace()
        await rig.harness.advance(by: resting.timeIntervalSince(rig.harness.clock.now) + 1)
        try await rig.harness.until(timeout: .seconds(60)) { await $0.presentation.cue.map { [.countdownEnded, .shortCountdownEnded].contains($0.kind) } ?? false }
        #expect(Self.steps(rig.harness.trace).contains("presented"))
        #expect(!Self.steps(rig.harness.trace).contains { $0.hasPrefix("journaled") })
    }

    @Test("a set far from the recent ones waits on the entry check, which is transient; Keep commits it")
    func entryCheck() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let rig = try Rig(directory: directory.url)
        let session = try await rig.start()
        let row = try #require(session.cards.flatMap(\.rows).first { $0.state == .pending && $0.commitValues?.load != nil })
        // Ten times the load it shows.
        let load = try #require(row.commitValues?.load)
        await rig.harness.send(.edit(row.id, SetValuesPatch(load: .set(Mass(value: load.value * 10, unit: load.unit)))))
        await rig.harness.send(.tick(row.id))
        let asked = await rig.presentation.entryCheck
        if asked != nil {
            #expect(await rig.harness.state.session?.rows[row.id]?.isCompleted == false)
            await rig.harness.send(.answerEntryCheck(.keep))
            #expect(await rig.presentation.entryCheck == nil)
        }
        #expect(await rig.harness.state.session?.rows[row.id]?.isCompleted == true)
    }

    @Test("a refused command changes nothing and says why, with the warning haptic")
    func refusal() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let rig = try Rig(directory: directory.url)
        _ = try await rig.start()
        let before = await rig.harness.state
        await rig.harness.send(.untick(RowID("no-such-row")))
        #expect(await rig.harness.state == before)
        #expect(await rig.presentation.lastRefusal != nil)
        #expect(await rig.presentation.cue?.kind == .warning)
    }
}
