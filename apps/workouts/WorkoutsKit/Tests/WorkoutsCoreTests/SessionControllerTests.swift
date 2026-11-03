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
        let health: RecordedHealth

        init(directory: URL, clock: TestClock = TestClock(), performsEffects: Bool = true, history: HistoryIndex = HistoryIndex()) throws {
            self.directory = directory
            notifications = RecordedNotifications()
            activities = RecordedActivities()
            health = RecordedHealth()
            var environment = AppEnvironment.testing(clock: clock)
            environment.devices.notifications = notifications
            environment.devices.activities = activities
            environment.workouts = health
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

        /// Save, answered under the receipt it returns.
        @discardableResult
        func finish(policy: PendingSetPolicy = .drop) async -> UUID {
            let now = harness.clock.now
            let request = FinishRequest(start: now.addingTimeInterval(-600), end: now, policy: policy, sessionRPE: 7, notes: nil)
            await harness.send(.finish(request))
            return request.receipt
        }

        /// How the Save under `receipt` went, once it is answered.
        func outcome(_ receipt: UUID) async throws -> String? {
            try await harness.until(timeout: .seconds(60)) { await $0.presentation.finishOutcomes[receipt] != nil }
            return try #require(await presentation.finishOutcomes[receipt])
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
        #expect(await rig.presentation.cover.value == true)
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
        #expect(await rig.presentation.cue.value?.kind == .logged)
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
        #expect(await rig.presentation.cover.value == false)
        #expect(await rig.presentation.cue.value?.kind == .finishSaved)
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
        #expect(await rig.presentation.cue.value?.kind == .warning)
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
        #expect(effects == ["registerCategory", "restore", "readAlerts", "listSetAside", "checkHistory"])
        // A replayed event runs no effect of its own: only the open's.
        let replayed = relaunched.harness.trace.contains { if case .replayed(_, 1) = $0 { true } else { false } }
        #expect(replayed)
        guard case let .restore(sync)? = relaunched.harness.effects.dropFirst().first else { Issue.record("no restore"); return }
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
        try await rig.harness.until(timeout: .seconds(60)) { await $0.presentation.cue.value.map { [.countdownEnded, .shortCountdownEnded].contains($0.kind) } ?? false }
        #expect(Self.steps(rig.harness.trace).contains("presented"))
        #expect(!Self.steps(rig.harness.trace).contains { $0.hasPrefix("journaled") })
    }

    @Test("a set far from the recent ones waits on the entry check, which is transient; Keep commits it")
    func entryCheck() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let clock = TestClock()
        // The first working set the sample routine starts with, and three
        // past sessions of its exercise at 35 kg × 12: ten times as much is
        // far from them.
        let probe = try Rig(directory: directory.url, clock: clock)
        let started = try await probe.start()
        let state = try #require(await probe.harness.state.session)
        let row = try #require(started.cards.flatMap(\.rows).first { row in
            guard let held = state.rows[row.id], held.isHead, held.role == .working, row.state == .pending,
                  row.commitValues?.load != nil else { return false }
            return state.exercise(ofRow: row.id)?.measurement == .weightreps
        })
        let exercise = try #require(state.exercise(ofRow: row.id)).id
        let unit = try #require(row.commitValues?.load?.unit)
        await probe.harness.runtime.close()
        try FileManager.default.removeItem(at: probe.journalURL)
        let past = (1 ... 3).map { week in
            Self.pastSession("p\(week)", at: clock.now.addingTimeInterval(TimeInterval(-week * 7 * 86400)), exercise: exercise,
                             load: Mass(value: 35, unit: unit), reps: 12)
        }

        let rig = try Rig(directory: directory.url, clock: clock, history: HistoryIndex(past))
        let session = try await rig.start()
        let asked = try #require(session.cards.flatMap(\.rows).first { $0.id == row.id })
        #expect(asked.state == .pending)
        await rig.harness.send(.edit(row.id, SetValuesPatch(load: .set(Mass(value: 350, unit: unit)), reps: .set(12))))
        await rig.harness.send(.tick(row.id))
        let question = try #require(await rig.presentation.entryCheck)
        #expect(question.row == row.id)
        #expect(question.warning.correctedLoad == Mass(value: 35, unit: unit))
        #expect(await rig.harness.state.session?.rows[row.id]?.isCompleted == false)
        #expect(await rig.presentation.cue.value?.kind == .warning)
        await rig.harness.send(.answerEntryCheck(.keep))
        #expect(await rig.presentation.entryCheck == nil)
        #expect(await rig.harness.state.session?.rows[row.id]?.isCompleted == true)
    }

    /// One finished session of `exercise`: a working set of `load` × `reps`.
    static func pastSession(_ id: String, at date: Date, exercise: ExerciseID, load: Mass, reps: Int) -> SessionRecords {
        let session = SessionID(id)
        let set = PerformedSet(id: SetID("\(id)-0"), at: date.addingTimeInterval(60), session: session, exercise: exercise, slot: "s",
                               order: 1, ordinal: 1, loadMode: .external, load: load, reps: reps, rpe: 8,
                               target: SetTarget(repsMin: 8, repsMax: 12, rpeMin: 8, rpeMax: 8))
        return SessionRecords(session: TrainingSession(id: session, at: date, endsAt: date.addingTimeInterval(3600), status: .finished,
                                                       slots: [SessionSlot(key: "s", exercise: exercise)], timezone: "UTC"),
                              sets: [set])
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
        #expect(await rig.presentation.cue.value?.kind == .warning)
    }

    // MARK: Save, answered under its receipt

    @Test("a Save that failed and then one that succeeded are each answered under their own receipt")
    func failedThenSaved() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let rig = try Rig(directory: directory.url)
        let session = try await rig.start()
        let blocker = directory.url.appendingPathComponent("history")
        try Data().write(to: blocker)
        let failed = await rig.finish()
        let reason = try await rig.outcome(failed)
        #expect(reason?.hasPrefix("The session could not be saved on this iPhone") == true)
        #expect(await rig.presentation.session?.sessionID == session.sessionID)
        // The disk has room again: the next Save is answered by its own
        // outcome, never by the last one's problem still shown.
        try FileManager.default.removeItem(at: blocker)
        let saved = await rig.finish()
        #expect(try await rig.outcome(saved) == nil)
        #expect(await rig.presentation.session == nil)
        #expect(rig.historyFile(session.sessionID))
    }

    @Test("two Saves the engine refuses in the same words are each answered")
    func refusedTwiceAlike() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let rig = try Rig(directory: directory.url, performsEffects: false)
        await rig.harness.send(.start(.empty))
        let start = try #require(rig.harness.effects.compactMap { if case let .prepareStart(request) = $0 { request } else { nil } }.first)
        let controller = SessionController(catalog: Catalog(), history: HistoryIndex(), settings: WorkoutsSettings(), adapter: "test",
                                           directory: directory.url)
        await rig.harness.deliver(controller.start(start, bodyMass: nil, environment: rig.harness.environment))
        // The first Save's history file is still being written (held):
        // the two after it are refused alike, and neither waits forever.
        let first = await rig.finish()
        let second = await rig.finish()
        let third = await rig.finish()
        let refusal = try await rig.outcome(second)
        #expect(refusal != nil)
        #expect(try await rig.outcome(third) == refusal)
        #expect(await rig.presentation.finishOutcomes[first] == nil)
        #expect(await rig.harness.state.finishing != nil)
    }

    @Test("a Save with no session open is refused under its receipt")
    func refusedWithNoSession() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let rig = try Rig(directory: directory.url)
        _ = await rig.presentation
        let receipt = await rig.finish()
        #expect(try await rig.outcome(receipt) == String(describing: EngineError.noSession))
    }

    // MARK: Start

    @Test("a Start the engine refuses once the device has answered leaves the cover's progress")
    func startRefusedMidFlight() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let rig = try Rig(directory: directory.url)
        let open = try await rig.start()
        // A second Start already under way when the first one's session
        // opened (a Live Activity's, a second tap): the engine refuses it.
        await rig.harness.deliver(.starting(.empty))
        #expect(await rig.presentation.starting)
        try await rig.harness.until(timeout: .seconds(60)) { await !$0.presentation.starting }
        #expect(await rig.presentation.lastRefusal == String(describing: EngineError.sessionActive))
        #expect(await rig.presentation.session?.sessionID == open.sessionID)
    }

    // MARK: The countdown's alert, and a tap on it

    @Test("the alert is quiet in front of the cover and shows away from it, and its category is registered at open")
    func quietInFrontOfTheCover() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let rig = try Rig(directory: directory.url)
        let session = try await rig.start()
        await rig.harness.send(.tick(try Self.loggable(session)))
        func alert() async throws -> NotificationRequest {
            try await rig.harness.until(timeout: .seconds(60)) { _ in rig.notifications.pending[SessionController.countdownID] != nil }
            return try #require(rig.notifications.pending[SessionController.countdownID])
        }
        #expect(try await alert().showsWhileActive)
        await rig.harness.send(.coverVisible(true))
        try await rig.harness.until(timeout: .seconds(60)) { _ in
            rig.notifications.pending[SessionController.countdownID]?.showsWhileActive == false
        }
        await rig.harness.send(.coverVisible(false))
        try await rig.harness.until(timeout: .seconds(60)) { _ in
            rig.notifications.pending[SessionController.countdownID]?.showsWhileActive == true
        }
        #expect(rig.notifications.categories == [NotificationCategory(id: SessionController.countdownCategory)])
    }

    @Test("a tap on the rest notification reveals the session: the cover, and Up next brought into view")
    func tapReveals() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let rig = try Rig(directory: directory.url)
        let session = try await rig.start()
        let tap = NotificationResponse(id: SessionController.countdownID, category: SessionController.countdownCategory, action: .open)
        let command = try #require(SessionController.command(for: tap))
        #expect(command == .reveal)
        #expect(SessionController.command(for: NotificationResponse(id: "x", category: "other", action: .open)) == nil)
        let before = await rig.presentation.cover.serial
        await rig.harness.send(command)
        let presented = await rig.presentation
        #expect(presented.cover.value == true && presented.cover.serial == before + 1)
        #expect(presented.scroll.value?.row == session.upNext?.row)
    }

    // MARK: A session History already holds

    @Test("a replayed session History already holds closes, and its Health save, never queued, is queued again")
    func alreadyFinishedRecoversHealth() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let entry = try await Self.finishedAndReplayed(in: directory.url, health: HealthSaveState(status: .pending))
        let relaunched = try Rig(directory: directory.url)
        try await relaunched.harness.until(timeout: .seconds(60)) { await $0.state.session == nil }
        try await relaunched.harness.until(timeout: .seconds(60)) { _ in relaunched.health.recovered == [entry.id] }
        #expect(relaunched.harness.effects.contains(.health(.recover(entry.id))))
    }

    @Test("a replayed session History already holds with its Health save done closes and saves nothing again")
    func alreadyFinishedWithHealthSaved() async throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let entry = try await Self.finishedAndReplayed(in: directory.url, health: HealthSaveState(status: .saved))
        let relaunched = try Rig(directory: directory.url)
        try await relaunched.harness.until(timeout: .seconds(60)) { await $0.state.session == nil }
        let closed = relaunched.harness.effects.contains { if case .restore(nil) = $0 { true } else { false } }
        #expect(closed)
        #expect(!relaunched.harness.effects.contains(.health(.recover(entry.id))))
        #expect(relaunched.health.recovered.isEmpty)
        #expect(!relaunched.journalExists)
    }

    /// A session whose history file was written, with `health`, and whose
    /// journal the app died before removing.
    static func finishedAndReplayed(in directory: URL, health: HealthSaveState) async throws -> HistoryEntry {
        let clock = TestClock()
        let first = try Rig(directory: directory, clock: clock, performsEffects: false)
        await first.harness.send(.start(.empty))
        let start = try #require(first.harness.effects.compactMap { if case let .prepareStart(request) = $0 { request } else { nil } }.first)
        let controller = SessionController(catalog: Catalog(), history: HistoryIndex(), settings: WorkoutsSettings(), adapter: "test",
                                           directory: directory)
        await first.harness.deliver(controller.start(start, bodyMass: nil, environment: first.harness.environment))
        await first.finish()
        var entry = try #require(await first.harness.state.finishing).entry
        entry.local = HistoryLocal(health: health)
        try HistoryStore(directory: directory).write(entry)
        await first.harness.runtime.close()
        return entry
    }
}

/// Workouts' Health services, recorded: what was queued, recovered and worked.
final class RecordedHealth: WorkoutsServices, @unchecked Sendable {
    private let lock = NSLock()
    private var kept: [SessionID] = []

    var recovered: [SessionID] { lock.withLock { kept } }

    func bodyMass(typed: Mass?) async -> Mass? { typed }
    func queueHealthSave(_ session: SessionID) async {}
    func recoverHealthSave(_ session: SessionID) async { lock.withLock { kept.append(session) } }
    func processHealth(_ trigger: HealthTrigger) async {}
}
