import Foundation
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime

/// The single writer of the session (DESIGN.md §6.1), on the kit's runtime:
/// every change goes command, event, engine, journal, presenter, effects, in
/// that order, and the journal is written before anything can suspend, so a
/// crash after that write still restores the truth (§2.3, §6.3).
///
/// Its parent hands it what it presents with: the catalogue, the history
/// index and the settings, as part of its value. What the engine needs of
/// them goes into the event (a Start's routine and exercises, an added
/// exercise's rows from last time), so a replay never reads them.
///
/// A Finish is two events. `finishRequested` is applied and not journaled,
/// and its effect writes the history file; `finished` then removes the
/// journal, or `finishFailed` leaves the session open. A crash between them
/// replays to the session open, and the open's check against History closes
/// it when its file was written. Every Save is answered under its receipt,
/// saved or refused, so the sheet waits for its own outcome.
public struct SessionController: Controller, Equatable {
    public typealias Command = SessionCommand
    public typealias Presentation = SessionScreenPresentation

    public var catalog: Catalog
    public var history: HistoryIndex
    public var settings: WorkoutsSettings
    /// `substrate-workouts@0.1.0`, the session record's adapter.
    public var adapter: String
    /// `Application Support/Workouts/`: the journal and the history files.
    /// Nil in a preview, which keeps no journal and writes nothing.
    public var directory: URL?
    /// A preview's session, open from the start.
    public var preview: SessionState?

    public init(catalog: Catalog, history: HistoryIndex, settings: WorkoutsSettings, adapter: String, directory: URL?,
                preview: SessionState? = nil)
    {
        self.catalog = catalog
        self.history = history
        self.settings = settings
        self.adapter = adapter
        self.directory = directory
        self.preview = preview
    }

    /// The countdown notification's one identifier: adding it again replaces it.
    public static let countdownID = "workouts.countdown"
    /// Its category. No actions: a rest-over alert is read, not answered,
    /// and a tap opens the session where every control is.
    public static let countdownCategory = "workouts.countdown"
    public static let sessionKey = "session"

    /// The command a notification's response means: a tap on the rest
    /// notification reveals the session.
    public static func command(for response: NotificationResponse) -> SessionCommand? {
        response.category == countdownCategory && response.action == .open ? .reveal : nil
    }

    public var initialState: State { State(session: preview) }
    public var initialTransient: Transient { Transient() }

    public func journal(in environment: AppEnvironment) -> (any JournalStore<Event, State>)? {
        guard let directory, let folder = try? RepositoryFolder(app: directory, writingOptions: WorkoutsFiles.protectedWriting)
        else { return nil }
        return SessionJournalFile(folder: folder)
    }

    /// The history files, beside the journal.
    var historyStore: HistoryStore? { directory.map { HistoryStore(directory: $0) } }

    // MARK: The engine

    public func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        switch event {
        case let .engine(envelope):
            next.session = try SessionEngine.apply(envelope, to: state.session)
            next.lastAt = envelope.at
            if case .start = envelope.event { next.revision = 1 } else { next.revision += 1 }
        case let .discard(envelope, _, _):
            _ = try SessionEngine.apply(envelope, to: state.session)
            next = State()
        case let .finishRequested(envelope, finished):
            guard state.finishing == nil else { throw EngineError.noSession }
            _ = try SessionEngine.apply(envelope, to: state.session)
            next.finishing = finished
        case .finished, .alreadyFinished:
            next = State()
        case .finishFailed:
            next.finishing = nil
        case let .noted(.refuse(error)), let .finishRefused(_, error):
            throw error
        case .opened, .foregrounded, .starting, .startDropped, .tick, .alertsRead, .setAsideFound, .noted:
            break
        }
        return next
    }

    /// An engine event is the journal's; a Start begins it again, and a
    /// Discard and a finished session end it. A Finish asked is not kept
    /// until its history file is written.
    public func journaling(of event: Event) -> Journaling {
        switch event {
        case let .engine(envelope):
            if case .start = envelope.event { return .restart }
            return .append
        case .discard, .finished, .alreadyFinished:
            return .clear
        default:
            return .skip
        }
    }

    // MARK: Transient state

    public func transient(after event: Event, applied state: State, from transient: Transient) -> Transient {
        var next = transient
        switch event {
        case let .engine(envelope):
            next.lastRefusal = nil
            switch envelope.event {
            case .start:
                next.starting = false
                next.coverOnFirstForeground = false
                next.undo = nil
                next.show(cover: true)
            case let .completeSet(complete), let .stopTimedSet(complete):
                // A question still up for this row is answered by this commit.
                if next.entryCheck?.row == complete.row { next.entryCheck = nil }
                next.play(settings.sessionLayout == .rows ? .logged : .tick)
            default:
                break
            }
        case let .discard(_, _, then):
            next = cleared(next)
            if then == nil { next.show(cover: false) } else { next.starting = true }
        case .finishRequested:
            next.finishProblem = nil
        case let .finished(finished):
            next = cleared(next)
            next.play(.finishSaved)
            next.startAfterFinish = nil
            if let receipt = finished.receipt { next.finishOutcomes[receipt] = .some(nil) }
            // The cover stays up for the next session.
            if finished.then == nil { next.show(cover: false) } else { next.starting = true }
        case let .finishFailed(reason, receipt):
            next.finishProblem = reason
            if let receipt { next.finishOutcomes[receipt] = .some(reason) }
            next.play(.warning)
        case .alreadyFinished:
            next = cleared(next)
        case .foregrounded:
            if state.session != nil, next.coverOnFirstForeground { next.show(cover: true) }
            next.coverOnFirstForeground = false
        case .starting:
            next.starting = true
        case .startDropped:
            next.starting = false
        case let .tick(instant):
            if let cue = countdownCue(state.session, at: instant) { next.play(cue.kind, onlyOnSessionScreen: cue.onlyOnScreen) }
        case let .alertsRead(permission):
            next.alerts = permission
        case let .setAsideFound(records):
            next.setAside = records
        case .opened, .finishRefused:
            break
        case let .noted(note):
            switch note {
            case let .asked(request):
                next.entryCheck = request
                next.play(.warning)
            case let .answered(edit):
                if edit, let row = next.entryCheck?.row { next.scroll(row: row) }
                next.entryCheck = nil
            case let .scroll(target):
                if let target { next.scroll(row: target.row, slot: target.slot) } else { next.scroll.clear() }
            case let .finishFlow(request):
                next.startAfterFinish = request
                next.finishFlow = true
                next.show(cover: true)
            case .finishFlowShown:
                next.finishFlow = false
            case let .startAfterFinish(request):
                next.startAfterFinish = request
            case let .coverVisible(visible):
                next.coverVisible = visible
            case let .reveal(row):
                next.show(cover: true)
                if let row { next.scroll(row: row) }
            case .leftForTheApp, .refuse:
                break
            }
        }
        return next
    }

    public func transient(afterRefusing event: Event, because error: any Error, from transient: Transient) -> Transient {
        var next = transient
        let refusal = String(describing: error)
        next.lastRefusal = refusal
        next.play(.warning)
        switch event {
        case let .engine(envelope):
            // A Start the engine refuses once the device has answered: the
            // cover leaves its progress for what is open, or for nothing.
            if case .start = envelope.event { next.starting = false }
        case let .finishRequested(_, finished):
            if let receipt = finished.receipt { next.finishOutcomes[receipt] = .some(refusal) }
        case let .finishRefused(receipt, _):
            if let receipt { next.finishOutcomes[receipt] = .some(refusal) }
        default:
            break
        }
        return next
    }

    /// What a session's end leaves of the screens' state.
    private func cleared(_ transient: Transient) -> Transient {
        var next = transient
        next.entryCheck = nil
        next.scroll.clear()
        next.undo = nil
        next.finishFlow = false
        next.finishProblem = nil
        return next
    }

    /// §9.3's haptic when a countdown reaches its end: a timed set's target,
    /// a rest that notifies (played on the session screen alone: elsewhere
    /// the banner alerts), or a short one.
    private func countdownCue(_ session: SessionState?, at instant: Date) -> (kind: SessionCue.Kind, onlyOnScreen: Bool)? {
        guard let session, session.phase == .active, let countdown = session.countdown, let end = countdown.endsAt,
              abs(end.timeIntervalSince(instant)) < 1
        else { return nil }
        if countdown.timedRow != nil { return (.targetReached, false) }
        return countdown.notifies ? (.countdownEnded, true) : (.shortCountdownEnded, false)
    }

    // MARK: The presenter

    /// The session in progress is presented with the RPE pre-fill when the
    /// cover shows Rows (§4.14), in the environment's calendar.
    func presenterSettings(calendar: Calendar) -> PresenterSettings {
        PresenterSettings(calendar: calendar, defaultUnit: settings.defaultUnit, countWarmupsInVolume: settings.countWarmupsInVolume,
                          prefillsRPE: settings.sessionLayout == .rows)
    }

    public func present(_ input: PresenterInput<SessionController>) -> SessionScreenPresentation {
        let state = input.state
        let transient = input.transient
        let session = state.session.map {
            SessionPresenter.present($0, catalog: catalog, history: history, settings: presenterSettings(calendar: input.calendar),
                                     now: input.now)
        }
        // Rows' Log says the set is in the journal: with the write failed,
        // it is not, and the cover says so instead.
        var cue = transient.cue
        if input.journalProblem != nil, cue.value?.kind == .logged { cue.clear() }
        return SessionScreenPresentation(
            session: session, state: state.session, starting: transient.starting, finishing: state.finishing != nil,
            entryCheck: transient.entryCheck, scroll: transient.scroll, lastRefusal: transient.lastRefusal,
            finishProblem: transient.finishProblem, finishOutcomes: transient.finishOutcomes, journalProblem: input.journalProblem,
            cover: transient.cover, coverVisible: transient.coverVisible, cue: cue, finishFlow: transient.finishFlow,
            startAfterFinish: transient.startAfterFinish, undo: transient.undo, alerts: transient.alerts, setAside: transient.setAside)
    }

    public func nextChange(of presentation: SessionScreenPresentation) -> Date? { presentation.session?.nextChange }

    public func event(atTick instant: Date) -> Event? { .tick(instant) }

    public func event(atOpen replayed: Bool) -> Event? { .opened(replayed: replayed) }
}
