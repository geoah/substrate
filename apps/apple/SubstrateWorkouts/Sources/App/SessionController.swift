import Foundation
import Observation
import os
import WorkoutsCore

/// What a view, a Live Activity intent or a notification action asks of the
/// session. A command is an engine event minus what the controller adds: ids
/// (UUIDv7), the instant, and values that need history (the values a tick
/// commits, an added exercise's rows from last time).
enum SessionCommand: Equatable {
    /// A field focused, or a row's RPE or set-kind control opened (§4.8).
    case touchRow(RowID)
    case edit(RowID, SetValuesPatch)
    case setRowNote(RowID, String?)
    /// PREVIOUS tapped: last time's values, a rated RPE as `copied` (§5.4).
    case copyPrevious(RowID)
    /// The tick: commits what the row shows, after the entry check (§5.7).
    case tick(RowID)
    case untick(RowID)
    case addSet(slot: String, role: Role)
    case removeSet(RowID)
    case changeSetKind(RowID, SetKind)
    case setRowRest(RowID, Seconds?)
    /// `after`: the slot whose exercise menu opened the picker; nil appends.
    case addExercise(Exercise, after: String?)
    case removeExercise(slot: String)
    case reorderExercises([String])
    case replaceExercise(slot: String, with: Exercise)
    case groupExercises(slot: String, partner: String)
    case ungroupExercise(slot: String)
    /// "Ungroup all" in a group's menu.
    case ungroupAll(group: String)
    case setGroup(SessionEvent.SetGroup)
    case setSlotRest(slot: String, kind: SlotRestKind, seconds: Seconds?)
    case setNote(NoteTarget, String?)
    case pauseClock
    case resumeClock
    /// The toolbar's standalone timer.
    case startTimer(Seconds)
    case adjustRest(Seconds)
    case skipRest
    case startTimedSet(RowID)
    case stopTimedSet(RowID)
    case cancelTimedSet(RowID)
    /// The Live Activity's Done: ticks the row only while it is still pending
    /// and still Up next, with the values it shows (§9.1).
    case completeUpNext(RowID)
}

/// How a new session starts: from a routine or from nothing.
enum StartRequest: Equatable {
    case routine(RoutineID)
    case empty
}

/// A set far from the recent ones, waiting for the owner's answer (§5.7).
struct EntryCheckRequest: Equatable {
    var row: RowID
    var values: SetValues
    var warning: EntryWarning
    var measurement: MeasurementType
    /// A timed set's Stop rather than a tick.
    var isStop: Bool
}

/// A row the session list should bring into view; the token makes a repeat
/// request for the same row a change.
struct ScrollTarget: Equatable {
    var row: RowID?
    var slot: String?
    var token = UUID()
}

/// The single writer of the session (DESIGN.md §6.1). Every change goes
/// command, event, engine, journal, coordinator, presenter, in that order:
/// the journal is written before any `await`, so a crash after that line
/// still restores the truth (§2.3, §6.3).
@MainActor
@Observable
final class SessionController {
    static let shared = SessionController(app: .shared)

    let app: AppModel

    /// The session in progress; nil when none is open.
    private(set) var state: SessionState?
    /// What every session screen shows, rebuilt after each change and when a
    /// countdown's end changes the time-dependent parts.
    private(set) var presentation: SessionPresentation?
    /// The entry check's question, while it waits (§5.7).
    var entryCheck: EntryCheckRequest?
    var scrollTarget: ScrollTarget?
    /// The cover should run its Finish flow (the start conflict's "Finish it
    /// and start this one"); the cover clears it when it does.
    var pendingFinishFlow = false
    /// A Start is under way: the cover shows progress rather than "no session".
    private(set) var isStarting = false
    /// Started once the open session is saved (§7.4).
    var startAfterFinish: StartRequest?
    /// The last refused command, for the log and the warning haptic.
    private(set) var lastRefusal: String?
    /// The last journal write failed (a full disk): ticks live only in
    /// memory until a write succeeds again. The cover and Finish say so.
    private(set) var journalWriteFailed = false

    @ObservationIgnored private var events: [EventEnvelope] = []
    @ObservationIgnored private var journal: ActiveSessionJournal?
    @ObservationIgnored private(set) var effectsRevision = 0
    /// The outbox, through `SubstrateLink`: a no-op until the app is signed in.
    private let sink: any SessionSyncSink<EventEnvelope, SessionState> = LinkSink()
    @ObservationIgnored private var preparing: Task<Void, Never>?
    @ObservationIgnored private var refreshTask: Task<Void, Never>?
    /// A launch with a session open shows the cover the first time the app is
    /// in the foreground (§1.4, §6.3).
    @ObservationIgnored private var coverOnFirstForeground = true
    @ObservationIgnored private var startInFlight = false
    /// When `load` restored the alert and the Live Activity.
    @ObservationIgnored private var restoredAtLoad: Date?
    /// Previews: no journal, no platform effects.
    let effectsEnabled: Bool

    let logger = Logger(subsystem: "dev.reamde.substrate.workouts", category: "session")

    init(app: AppModel) {
        self.app = app
        effectsEnabled = app.directory != nil
    }

    /// A controller over a fixed state, for previews.
    init(app: AppModel, previewState: SessionState?, now: Date = .now) {
        self.app = app
        effectsEnabled = false
        state = previewState
        preparing = Task {}
        present(now: now)
    }

    var isActive: Bool { state != nil }

    /// `substrate-workouts@0.1.0`, the session record's adapter.
    static var adapter: String {
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0"
        return "substrate-workouts@\(version)"
    }

    // MARK: - Launch and foreground (§6.3)

    /// Loads the catalogue, the history and the journal, once; later calls
    /// wait for the first. Every entry point awaits it before it acts, so a
    /// Live Activity intent that launched the app in the background folds the
    /// journal before its command runs.
    func prepare() async {
        if let preparing { return await preparing.value }
        let task = Task { await load() }
        preparing = task
        await task.value
    }

    private func load() async {
        app.loadCatalog()
        await app.loadHistory()
        guard let directory = app.directory else { return }
        let journal = ActiveSessionJournal(directory: directory)
        self.journal = journal
        // Journals set aside at an earlier launch stay listed in Settings until removed.
        let files = (try? FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil)) ?? []
        for file in files where file.lastPathComponent.hasPrefix("active-session.unreadable-") {
            app.noteUnreadableSession(UnreadableRecord(source: file.lastPathComponent, kind: "session in progress",
                                                       reason: "Set aside at launch: it could not be read"))
        }
        do {
            if let loaded = try journal.load(fold: { events throws in try SessionEngine.fold(events) }) {
                let id = loaded.state.session.id
                if let saved = app.history.first(where: { $0.id == id }) {
                    // Finish wrote the history file and the app died before it
                    // deleted the journal: the session is finished, not open.
                    logger.error("The journal's session is already in History; the journal is deleted")
                    try journal.delete()
                    if saved.local.health.status == .pending, saved.records.session.healthKitWorkout == nil {
                        platformRecoverHealthSave(id)
                    }
                } else {
                    if !loaded.cacheMatched {
                        logger.error("The journal's cached state differed from its fold; the fold was kept and written back")
                    }
                    events = loaded.events
                    state = loaded.state
                    effectsRevision = loaded.events.count
                    present()
                }
            }
        } catch {
            logger.fault("The journal could not be read or folded: \(String(describing: error), privacy: .public)")
            // Another format, an event that no longer decodes, a fold the
            // engine refuses: kept aside, where the next Start cannot write
            // over it, and listed in Settings. A file the phone could not
            // read at all (a lock, an I/O error) stays where it is.
            if !(error is CocoaError) {
                do {
                    let aside = try journal.setAside(at: .now)
                    logger.fault("The journal was moved to \(aside.lastPathComponent, privacy: .public)")
                    app.noteUnreadableSession(UnreadableRecord(source: aside.lastPathComponent, kind: "session in progress",
                                                               reason: String(describing: error)))
                } catch {
                    logger.fault("The journal could not be moved aside: \(String(describing: error), privacy: .public)")
                }
            }
        }
        // With no session open this ends any Live Activity left behind.
        await platformRestore(state, presentation: presentation, revision: effectsRevision)
        restoredAtLoad = .now
    }

    /// The app came to the foreground (§6.3): re-apply the alert and the
    /// Live Activity (an expired rest ends quietly: the notification already
    /// alerted), re-read the alert settings, and work the Health queue. A
    /// launch with a session open shows its cover the first time.
    func sceneBecameActive() async {
        await prepare()
        present()
        if state != nil, coverOnFirstForeground { app.isSessionPresented = true }
        coverOnFirstForeground = false
        // Right after a foreground launch `load` has just restored; after a
        // launch in the background (an intent) the activity may still be missing.
        if let restored = restoredAtLoad, Date.now.timeIntervalSince(restored) < 2 {
            restoredAtLoad = nil
        } else {
            restoredAtLoad = nil
            await platformRestore(state, presentation: presentation, revision: effectsRevision)
        }
        await platformProcessHealthQueue()
    }

    /// Notifications are refused or shown nowhere: the cover says so (§9.6).
    var alertsOff: Bool { platformAlertsOff }
    /// Notifications are on but not time sensitive: a Focus may hold them back.
    var timeSensitiveOff: Bool { platformTimeSensitiveOff }
    /// Notifications have not been asked for; the first Start asks.
    var alertsNotAsked: Bool { platformAlertsNotAsked }

    // MARK: - Start, Finish, Discard

    /// Starts a session. The caller has already asked about an open one (§7.4).
    func start(_ request: StartRequest) async {
        // One Start at a time: two that overlapped across the awaits below
        // would both reset the event list.
        guard !startInFlight else { return }
        startInFlight = true
        isStarting = true
        defer {
            startInFlight = false
            isStarting = false
        }
        await prepare()
        guard state == nil else {
            logger.error("Start while a session is open; ignored")
            return
        }
        let catalog = app.catalog ?? Catalog()
        var routine: Routine?
        if case let .routine(id) = request {
            guard let found = catalog.routines[id] else {
                logger.error("Start of an unknown routine \(id.rawValue, privacy: .public)")
                return
            }
            routine = found
        }
        var exercises: [ExerciseID: Exercise] = [:]
        for slot in routine?.slots ?? [] {
            if let exercise = catalog.exercises[slot.exercise] { exercises[exercise.id] = exercise }
        }
        var firstSides: [String: Side] = [:]
        for slot in routine?.slots ?? [] where slot.sideOrder == .weakerfirst {
            if let side = Previous.weakerSide(of: slot.exercise, history: app.historyIndex) { firstSides[slot.key] = side }
        }
        let bodyMass = await platformBodyMass(typed: app.settings.typedBodyMass)
        // Asked at the first Start (§9.6), before the first alert is scheduled.
        await platformRequestNotifications()
        // No await from here to the journal write: nothing can come between.
        let now = Date.now.flooredToSecond
        let zone = TimeZone.current
        let start = SessionEvent.Start(
            session: SessionID(UUIDv7.make(at: now)),
            routine: routine,
            exercises: Array(exercises.values).sorted { $0.id < $1.id },
            settings: app.settings.session,
            bodyMass: bodyMass,
            timeZone: zone.identifier,
            utcOffset: zone.secondsFromGMT(for: now) / 60,
            rowIDs: rowIDs(SessionEngine.rowIDCount(for: routine, exercises: exercises), at: now),
            firstSides: firstSides
        )
        if effectsEnabled, let directory = app.directory { journal = ActiveSessionJournal(directory: directory) }
        events = []
        // The first sync requests the Live Activity, in the foreground (§9.1).
        guard await apply(.start(start), now: now) else { return }
        app.isSessionPresented = true
        coverOnFirstForeground = false
    }

    /// The start conflict's "Finish it and start this one" (§7.4): open the
    /// cover and its Finish flow; `request` starts after Save.
    func finishThenStart(_ request: StartRequest) {
        startAfterFinish = request
        app.isSessionPresented = true
        pendingFinishFlow = true
    }

    /// The start conflict's "Discard it and start this one", after its second question.
    func discardThenStart(_ request: StartRequest) async {
        await discard()
        await start(request)
    }

    /// The Finish dialog's and sheet's numbers for the session as it stands
    /// (§7.7): what Save writes with the unticked rows treated as `policy`.
    func finishSummary(start: Date? = nil, end: Date, policy: PendingSetPolicy) -> FinishSummary? {
        guard let state, let presentation else { return nil }
        return FinishPresenter.summary(state, presentation: presentation, policy: policy, start: start, end: end,
                                       healthAllowed: platformHealthSharingAllowed())
    }

    /// Save in the Finish sheet (§6.1, §7.7): applies `finish`, built from the
    /// same projection the sheet showed for `policy`, writes the history
    /// file, deletes the journal, ends the Live Activity with the summary,
    /// clears the notification and queues the Health save without waiting
    /// for it. Returns false, keeping the session open, when the
    /// engine refuses or the history file cannot be written.
    @discardableResult
    func finish(start: Date, end: Date, policy: PendingSetPolicy, sessionRPE: Int?, notes: String?) async -> Bool {
        let trimmed = notes?.trimmingCharacters(in: .whitespacesAndNewlines)
        // `finish` keeps the notes when given none, so clearing them is its own event.
        if trimmed?.isEmpty == true, self.state?.session.notes != nil {
            await apply(.setNote(target: .session, text: nil), now: .now)
        }
        guard let state, let presentation else { return false }
        let ended = end.flooredToSecond
        // The sheet's numbers came from this projection: Save sends its `unticked` as it is.
        let projection = FinishPresenter.projection(state, presentation: presentation, policy: policy, end: ended) { _ in
            SetID(UUIDv7.make(at: ended))
        }
        let event = SessionEvent.finish(projection.event(
            start: start.flooredToSecond,
            end: ended,
            sessionRPE: sessionRPE.map { Decimal($0) },
            notes: trimmed.flatMap { $0.isEmpty ? nil : $0 },
            utcOffset: TimeZone.current.secondsFromGMT(for: ended) / 60
        ))
        let envelope = envelope(event, now: .now)
        let finished: SessionState
        do {
            finished = try SessionEngine.apply(envelope, to: state)
        } catch {
            refuse(error, event: event)
            return false
        }
        let records = SessionEngine.records(finished, adapter: Self.adapter)
        do {
            try app.save(HistoryEntry(records: records))
        } catch {
            logger.fault("The history file could not be written: \(String(describing: error), privacy: .public)")
            Haptics.shared.play(.warning)
            return false
        }
        // Queued before the journal goes and before any await, so no death
        // between here and the coordinator can lose the Health save.
        platformQueueHealthSave(state.session.id)
        do {
            try journal?.delete()
        } catch {
            logger.error("The journal could not be deleted: \(String(describing: error), privacy: .public)")
        }
        let id = state.session.id
        clearSession()
        Haptics.shared.play(.finishSaved)
        let next = startAfterFinish
        startAfterFinish = nil
        if let next {
            // The cover stays up for the next session; Save returns at once.
            isStarting = true
            Task { await self.start(next) }
        } else {
            app.isSessionPresented = false
        }
        let sink = sink
        Task.detached { await sink.finished(records) }
        await platformSessionFinished(id, records: records, summary: SessionPresenter.finishedSummary(records))
        return true
    }

    /// Discard (§6.1): applies `discard`, deletes the journal, ends the Live
    /// Activity at once and clears the notification. Nothing is recorded.
    func discard() async {
        guard let state else { return }
        let envelope = envelope(.discard, now: .now)
        do {
            _ = try SessionEngine.apply(envelope, to: state)
        } catch {
            refuse(error, event: .discard)
            return
        }
        do {
            try journal?.delete()
        } catch {
            logger.error("The journal could not be deleted: \(String(describing: error), privacy: .public)")
        }
        let id = state.session.id
        clearSession()
        app.isSessionPresented = false
        let sink = sink
        Task.detached { await sink.discarded(id) }
        await platformSessionDiscarded(id)
    }

    private func clearSession() {
        refreshTask?.cancel()
        journalWriteFailed = false
        state = nil
        presentation = nil
        events = []
        entryCheck = nil
        scrollTarget = nil
    }

    // MARK: - Commands

    /// Runs a command from a view without waiting for its effects.
    func run(_ command: SessionCommand) {
        Task { await send(command) }
    }

    /// The only way anything changes a session (§6.1).
    func send(_ command: SessionCommand) async {
        await prepare()
        guard let state else {
            logger.error("Command with no session open: \(String(describing: command), privacy: .public)")
            return
        }
        let now = Date.now
        switch command {
        case let .touchRow(row):
            // A done row, or a second touch that would change nothing, sends no event.
            let clockOpen = state.restClock.map { $0.endedAt == nil } ?? false
            guard state.rows[row]?.isPending == true, state.focus != row || clockOpen else { return }
            await apply(.touchRow(row: row), now: now)
        case let .edit(row, patch):
            await apply(.editSet(SessionEvent.EditSet(row: row, values: patch, segmentRowIDs: rowIDs(1, at: now))), now: now)
        case let .setRowNote(row, text):
            let note: FieldPatch<String> = text.map { $0.isEmpty ? .cleared : .set($0) } ?? .cleared
            await apply(.editSet(SessionEvent.EditSet(row: row, note: note)), now: now)
        case let .copyPrevious(row):
            guard let previous = presentation?.row(row)?.previous, state.rows[row]?.isPending == true else { return }
            await apply(.editSet(SessionEvent.EditSet(row: row, values: previous.copy)), now: now)
        case let .tick(row):
            await tick(row, stop: false, now: now)
        case let .untick(row):
            await apply(.uncompleteSet(row: row), now: now)
        case let .addSet(slot, role):
            await apply(.addSet(SessionEvent.AddSet(slot: slot, row: RowID(UUIDv7.make(at: now)), role: role,
                                                    segmentRowIDs: rowIDs(1, at: now))), now: now)
        case let .removeSet(row):
            await apply(.removeSet(row: row), now: now)
        case let .changeSetKind(row, kind):
            await apply(.changeSetKind(SessionEvent.ChangeSetKind(row: row, kind: kind, segmentRowIDs: rowIDs(3, at: now))), now: now)
        case let .setRowRest(row, seconds):
            await apply(.setRowRest(row: row, seconds: seconds), now: now)
        case let .addExercise(exercise, after):
            await addExercise(exercise, after: after, now: now)
        case let .removeExercise(slot):
            await apply(.removeExercise(slot: slot), now: now)
        case let .reorderExercises(slots):
            await apply(.reorderExercises(slots: slots), now: now)
        case let .replaceExercise(slot, exercise):
            let key = slotKey(for: exercise, in: state)
            guard await apply(.replaceExercise(SessionEvent.ReplaceExercise(slot: slot, newSlot: key, exercise: exercise,
                                                                            segmentRowIDs: rowIDs(8, at: now))), now: now)
            else { return }
            if exercise.isLocal {
                let sink = sink
                Task.detached { await sink.exerciseAdded(exercise) }
            }
        case let .groupExercises(slot, partner):
            await apply(.groupExercises(slot: slot, partner: partner, group: nil), now: now)
        case let .ungroupExercise(slot):
            await apply(.ungroupExercise(slot: slot), now: now)
        case let .ungroupAll(group):
            // One member at a time; the group goes when one member is left.
            for member in state.members(ofGroup: group).dropLast() {
                guard await apply(.ungroupExercise(slot: member.key), now: now) else { break }
            }
        case let .setGroup(change):
            await apply(.setGroup(change), now: now)
        case let .setSlotRest(slot, kind, seconds):
            await apply(.setSlotRest(slot: slot, rest: kind, seconds: seconds), now: now)
        case let .setNote(target, text):
            await apply(.setNote(target: target, text: text), now: now)
        case .pauseClock:
            await apply(.pauseClock, now: now)
        case .resumeClock:
            await apply(.resumeClock, now: now)
        case let .startTimer(seconds):
            await apply(.startRest(seconds: seconds), now: now)
        case let .adjustRest(seconds):
            await apply(.adjustRest(seconds: seconds), now: now)
        case .skipRest:
            await apply(.skipRest, now: now)
        case let .startTimedSet(row):
            guard let presented = presentation?.row(row), case let .ready(target)? = presented.timer else {
                refuse(EngineError.noTimer, event: nil)
                return
            }
            await apply(.startTimedSet(row: row, target: target), now: now)
        case let .stopTimedSet(row):
            await tick(row, stop: true, now: now)
        case let .cancelTimedSet(row):
            await apply(.cancelTimedSet(row: row), now: now)
        case let .completeUpNext(row):
            // Only while that row is still Up next and pending: a tap on a
            // Lock Screen that fell behind must not tick another set.
            guard presentation?.upNext?.row == row, let values = presentation?.row(row)?.commitValues else {
                logger.info("Done for a row that is no longer Up next; ignored")
                return
            }
            // No dialog can ask from the Lock Screen: a set the entry check
            // would question (a typed 400 for 40) waits for the owner in the
            // app, where its question is or will be. The widget hides Done then.
            guard !questioned(row, values: values, now: now) else { return }
            await commit(row, values: values, stop: false, now: now)
        }
    }

    /// A tick or a Stop: the shown values, through the entry check (§5.7).
    private func tick(_ row: RowID, stop: Bool, now: Date) async {
        guard let state, let presented = presentation?.row(row),
              let values = stop ? presented.stopValues : presented.commitValues,
              let measurement = state.exercise(ofRow: row)?.measurement
        else {
            refuse(EngineError.incompleteValues(missing: presentation?.row(row)?.missing ?? []), event: nil)
            return
        }
        if let warning = EntryCheck.warning(for: row, values: values, in: state, history: app.historyIndex, now: now,
                                            calendar: app.presenterSettings.calendar)
        {
            entryCheck = EntryCheckRequest(row: row, values: values, warning: warning, measurement: measurement, isStop: stop)
            Haptics.shared.play(.warning)
            return
        }
        await commit(row, values: values, stop: stop, now: now)
    }

    /// Whether the entry check would ask about the set: from the Lock Screen,
    /// which can show no dialog, nothing is committed then.
    private func questioned(_ row: RowID, values: SetValues, now: Date) -> Bool {
        guard let state, EntryCheck.warning(for: row, values: values, in: state, history: app.historyIndex, now: now,
                                            calendar: app.presenterSettings.calendar) != nil
        else { return false }
        logger.info("A Lock Screen action for a set the entry check questions; left for the app")
        return true
    }

    /// The entry check's answer.
    enum EntryAnswer { case useCorrected, keep, edit }

    func answerEntryCheck(_ answer: EntryAnswer) async {
        guard let request = entryCheck else { return }
        entryCheck = nil
        switch answer {
        case .edit:
            scrollTarget = ScrollTarget(row: request.row)
        case .keep:
            await commit(request.row, values: request.values, stop: request.isStop, now: .now)
        case .useCorrected:
            let values = request.warning.corrected(request.values, measurement: request.measurement)
            await commit(request.row, values: values, stop: request.isStop, now: .now)
        }
    }

    private func commit(_ row: RowID, values: SetValues, stop: Bool, now: Date) async {
        guard let state else { return }
        // A question still up for this row is answered by this commit.
        if entryCheck?.row == row { entryCheck = nil }
        let noLighterLoad = SuggestionInputs.noLighterLoad(afterTicking: row, with: values, in: state, history: app.historyIndex,
                                                           now: now, calendar: app.presenterSettings.calendar)
        let at = now.flooredToSecond
        let complete = SessionEvent.CompleteSet(row: row, recordID: SetID(UUIDv7.make(at: at)), values: values,
                                                segmentRowIDs: rowIDs(2, at: at), noLighterLoad: noLighterLoad)
        guard await apply(stop ? .stopTimedSet(complete) : .completeSet(complete), now: now) else { return }
        // Rows' Log is a success once the set is in the journal; the Table's tick keeps its tap (§9.3).
        Haptics.shared.play(app.settings.sessionLayout == .rows ? .logged : .tick)
        // §7.6: with superset auto-scroll on and a 0 s transition, bring the
        // next member's row into view. No other tick scrolls. Rows follows
        // its open row on its own (§7.6a), so the Table only.
        if app.settings.sessionLayout == .table, app.settings.supersetAutoScroll, let next = self.state, next.countdown == nil,
           next.restClock?.slot == .transition, let upNext = presentation?.upNext
        {
            scrollTarget = ScrollTarget(row: upNext.row)
        }
    }

    private func addExercise(_ exercise: Exercise, after: String?, now: Date) async {
        guard let state else { return }
        let shape = Previous.lastShape(of: exercise.id, history: app.historyIndex, excluding: state.session.id)
        let unilateral = exercise.resolvedLaterality == .unilateral
        let count = shape.roles.count * (unilateral ? 2 : 1)
        let firstSide = unilateral ? Previous.weakerSide(of: exercise.id, history: app.historyIndex, excluding: state.session.id) : nil
        let key = slotKey(for: exercise, in: state)
        let added = SessionEvent.AddExercise(slot: key, exercise: exercise, roles: shape.roles, rowIDs: rowIDs(count, at: now),
                                             rests: shape.rests, after: after, firstSide: firstSide)
        guard await apply(.addExercise(added), now: now) else { return }
        scrollTarget = ScrollTarget(slot: key)
        if exercise.isLocal {
            let sink = sink
            Task.detached { await sink.exerciseAdded(exercise) }
        }
    }

    /// A slot key for an exercise added in the gym: its id, then `-2`, `-3`.
    private func slotKey(for exercise: Exercise, in state: SessionState) -> String {
        let taken = Set(state.slots.map(\.key))
        var key = exercise.id.rawValue
        var suffix = 2
        while taken.contains(key) {
            key = "\(exercise.id.rawValue)-\(suffix)"
            suffix += 1
        }
        return key
    }

    // MARK: - The loop

    private func rowIDs(_ count: Int, at date: Date) -> [RowID] {
        (0 ..< max(0, count)).map { _ in RowID(UUIDv7.make(at: date)) }
    }

    /// The event with its id and instant: floored to the second and never
    /// before the previous event's (§4.4 invariant 9).
    private func envelope(_ event: SessionEvent, now: Date) -> EventEnvelope {
        var at = now.flooredToSecond
        if let last = events.last?.at, at < last { at = last }
        return EventEnvelope(id: EventID(UUIDv7.make(at: at)), at: at, event: event)
    }

    /// Applies one event: the engine, the effects revision, the journal
    /// (before any `await`), the presentation, then the coordinator and the
    /// sync sink. A refusal is a warning haptic and no change.
    @discardableResult
    private func apply(_ event: SessionEvent, now: Date) async -> Bool {
        let envelope = envelope(event, now: now)
        let next: SessionState
        do {
            next = try SessionEngine.apply(envelope, to: state)
        } catch {
            refuse(error, event: event)
            return false
        }
        effectsRevision += 1
        let revision = effectsRevision
        events.append(envelope)
        do {
            try journal?.write(events: events, state: next)
            journalWriteFailed = false
        } catch {
            journalWriteFailed = true
            logger.fault("The journal could not be written: \(String(describing: error), privacy: .public)")
        }
        state = next
        present(now: now)
        if let presentation {
            await platformSync(next, presentation: presentation, revision: revision, at: envelope.at)
        }
        let sink = sink
        Task.detached { await sink.applied(envelope, state: next) }
        return true
    }

    private func refuse(_ error: any Error, event: SessionEvent?) {
        let text = String(describing: error)
        lastRefusal = text
        Haptics.shared.play(.warning)
        logger.error("Refused \(event.map { String(describing: $0) } ?? "command", privacy: .public): \(text, privacy: .public)")
    }

    // MARK: - Presenting

    /// Rebuilds the presentation, and schedules the next rebuild at the
    /// instant its time-dependent parts change.
    func present(now: Date = .now) {
        guard let state else {
            presentation = nil
            return
        }
        presentation = SessionPresenter.present(state, catalog: app.catalog, history: app.historyIndex,
                                                settings: app.sessionPresenterSettings, now: now)
        scheduleRefresh()
    }

    private func scheduleRefresh() {
        refreshTask?.cancel()
        guard effectsEnabled, let change = presentation?.nextChange else { return }
        refreshTask = Task { [weak self] in
            let delay = change.timeIntervalSinceNow
            if delay > 0 { try? await Task.sleep(for: .seconds(delay)) }
            guard !Task.isCancelled, let self else { return }
            // The countdown's end haptic is the coordinator's (§9.3); this
            // only redraws "Rest over" and then the Up next chip.
            present()
        }
    }

    // MARK: - Live Activity buttons

    /// A Live Activity button's action, mapped to its command once the
    /// journal is folded (§9.1).
    func handle(_ action: LiveActivityAction) async {
        await prepare()
        // The app may have been suspended since the last presentation.
        present()
        switch action {
        case let .adjustRest(seconds): await send(.adjustRest(seconds))
        case .skipRest: await send(.skipRest)
        case let .completeUpNext(rowID): await send(.completeUpNext(RowID(rowID)))
        case let .stopTimedSet(rowID):
            // No dialog can ask from the Lock Screen: Stop commits as shown.
            let row = RowID(rowID)
            guard let values = presentation?.row(row)?.stopValues else {
                logger.info("Stop for a row whose timer is not running; ignored")
                return
            }
            guard !questioned(row, values: values, now: .now) else { return }
            await commit(row, values: values, stop: true, now: .now)
        }
    }

    // MARK: - Alerts and Apple Health, for the screens

    /// Where Apple Health sharing stands (observed through the exporter).
    var healthAccess: HealthAccess { platformHealthAccess() }

    /// Settings' "Connect".
    func connectHealth() async {
        await platformConnectHealth()
    }

    /// The session detail's "Try again".
    func retryHealth(_ id: SessionID) async {
        await platformRetryHealth(id)
    }
}
