import Foundation
import Observation
import os
import SubstrateKit
import UIKit
import WorkoutsCore

/// The session's presentation for the views, republished on the main actor
/// from the runtime's tree, and its commands sent back. Nothing here decides
/// anything: the session's controller (WorkoutsCore's `SessionController`)
/// is the one writer, on its own executor. What this adds is the device's
/// half of a presentation the views cannot draw: the haptic a cue asks for,
/// and the cover a request opens or closes.
@MainActor
@Observable
final class SessionModel {
    private(set) var screen = SessionScreenPresentation()
    /// The row the list brings into view next, set by the session or a view,
    /// and cleared by the list once it has scrolled.
    var scrollTarget: ScrollTarget?

    @ObservationIgnored weak var root: WorkoutsRoot?
    @ObservationIgnored private var playedCue = 0
    @ObservationIgnored private var followedCover = 0
    @ObservationIgnored private var followedScroll = 0
    @ObservationIgnored private var loggedRefusal: String?
    @ObservationIgnored private let logger = Logger(subsystem: "dev.reamde.substrate.workouts", category: "session")
    /// Previews draw a fixed presentation and send nothing.
    @ObservationIgnored private let preview: Bool

    init() {
        preview = false
    }

    /// A model over a fixed session, for previews.
    init(preview state: SessionState?, catalog: Catalog, history: HistoryIndex, settings: WorkoutsSettings, now: Date = .now) {
        preview = true
        let controller = SessionController(catalog: catalog, history: history, settings: settings, adapter: "preview", directory: nil,
                                           preview: state)
        screen = controller.present(PresenterInput(state: controller.initialState, transient: controller.initialTransient, now: now,
                                                   locale: .current, timeZone: .current))
    }

    func apply(_ next: SessionScreenPresentation) {
        guard next != screen else { return }
        screen = next
        if let cue = next.cue, cue.serial != playedCue {
            playedCue = cue.serial
            play(cue)
        }
        if let cover = next.cover, cover.serial != followedCover {
            followedCover = cover.serial
            root?.app.isSessionPresented = cover.shown
        }
        if let scroll = next.scroll, scroll.serial != followedScroll {
            followedScroll = scroll.serial
            scrollTarget = scroll
        }
        if let refusal = next.lastRefusal, refusal != loggedRefusal {
            logger.error("Refused: \(refusal, privacy: .public)")
        }
        loggedRefusal = next.lastRefusal
    }

    /// §9.3's haptic. A countdown that notifies stays quiet unless the
    /// session cover shows: elsewhere in the app the banner alerts instead.
    private func play(_ cue: SessionCue) {
        if cue.onlyOnSessionScreen, !SessionScreen.shared.isVisible { return }
        if [.countdownEnded, .shortCountdownEnded, .targetReached].contains(cue.kind),
           UIApplication.shared.applicationState != .active { return }
        let haptic: HapticCue = switch cue.kind {
        case .tick: .tick
        case .logged: .logged
        case .countdownEnded: .countdownEnded
        case .shortCountdownEnded: .shortCountdownEnded
        case .targetReached: .targetReached
        case .warning: .warning
        case .finishSaved: .finishSaved
        }
        Haptics.shared.play(haptic)
    }

    // MARK: What the views read

    var presentation: SessionPresentation? { screen.session }
    /// The session as the engine holds it: what the Finish sheet's numbers
    /// come from.
    var state: SessionState? { screen.state }
    var isActive: Bool { screen.session != nil }
    var isStarting: Bool { screen.starting }
    var entryCheck: EntryCheckRequest? { screen.entryCheck }
    var journalWriteFailed: Bool { screen.journalProblem != nil }
    /// The cover should run its Finish flow; reading it as done clears it.
    var pendingFinishFlow: Bool {
        get { screen.finishFlow }
        set { if !newValue, screen.finishFlow { send(.finishFlowShown) } }
    }

    /// Started once the open session is saved (§7.4); nil takes it back.
    var startAfterFinish: StartRequest? {
        get { screen.startAfterFinish }
        set { if newValue == nil, screen.startAfterFinish != nil { send(.cancelStartAfterFinish) } }
    }

    /// Notifications are refused or shown nowhere: the cover says so (§9.6).
    var alertsOff: Bool { screen.alerts == .off }
    /// Notifications are on but not time sensitive: a Focus may hold them back.
    var timeSensitiveOff: Bool { screen.alerts == .notTimeSensitive }
    /// Notifications have not been asked for; the first Start asks.
    var alertsNotAsked: Bool { screen.alerts == .notAsked }

    /// The Finish dialog's and sheet's numbers for the session as it stands
    /// (§7.7): what Save writes with the unticked rows treated as `policy`.
    func finishSummary(start: Date? = nil, end: Date, policy: PendingSetPolicy) -> FinishSummary? {
        guard let state, let presentation else { return nil }
        return FinishPresenter.summary(state, presentation: presentation, policy: policy, start: start, end: end,
                                       healthAllowed: healthSharingAllowed)
    }

    // MARK: Commands

    func send(_ command: SessionCommand) {
        guard !preview else { return }
        root?.send(command)
    }

    /// Runs a command from a view without waiting for it.
    func run(_ command: SessionCommand) { send(command) }

    func start(_ request: StartRequest) async {
        send(.start(request))
        await root?.until { $0.app?.session.map { $0.session != nil || !$0.starting } ?? false }
    }

    func finishThenStart(_ request: StartRequest) { send(.finishThenStart(request)) }

    func discardThenStart(_ request: StartRequest) async { send(.discardThenStart(request)) }

    func discard() async {
        send(.discard)
        await root?.until { $0.app?.session?.session == nil }
    }

    /// Save in the Finish sheet: returns false, the session kept open, when
    /// the engine refuses or the history file cannot be written.
    @discardableResult
    func finish(start: Date, end: Date, policy: PendingSetPolicy, sessionRPE: Int?, notes: String?) async -> Bool {
        guard !preview, let id = presentation?.sessionID else { return false }
        let refusal = screen.lastRefusal
        send(.finish(FinishRequest(start: start, end: end, policy: policy, sessionRPE: sessionRPE, notes: notes)))
        await root?.until { tree in
            guard let screen = tree.app?.session else { return true }
            return screen.session?.sessionID != id || screen.finishProblem != nil
                || (!screen.finishing && screen.lastRefusal != nil && screen.lastRefusal != refusal)
        }
        return presentation?.sessionID != id
    }

    /// Delete set with an Undo (§7.6a): what puts it back, or nil when
    /// nothing was removed.
    func removeUndoably(_ row: RowID) async -> SessionEvent.RestoreSet? {
        guard let state, let restore = SessionEngine.restoration(of: row, in: state) else { return nil }
        send(.removeUndoably(row))
        await root?.until { $0.app?.session?.state?.rows[row] == nil || $0.app?.session?.session == nil }
        return self.state?.rows[row] == nil ? restore : nil
    }

    @discardableResult
    func restore(_ restore: SessionEvent.RestoreSet) async -> Bool {
        guard isActive else { return false }
        send(.restoreSet(restore))
        return true
    }

    func answerEntryCheck(_ answer: EntryAnswer) async { send(.answerEntryCheck(answer)) }

    func dismissEntryCheck() { if entryCheck != nil { send(.dismissEntryCheck) } }

    /// A Live Activity button's action, once the journal is folded (§9.1).
    func handle(_ action: LiveActivityAction) async {
        let command: SessionCommand = switch action {
        case let .adjustRest(seconds): .adjustRest(seconds)
        case .skipRest: .skipRest
        case let .completeUpNext(rowID): .completeUpNext(RowID(rowID))
        case let .stopTimedSet(rowID): .stopFromLockScreen(RowID(rowID))
        }
        await root?.perform(command)
    }

    // MARK: Apple Health, for the screens

    /// Where Apple Health sharing stands (observed through the exporter).
    var healthAccess: HealthAccess { preview ? .authorized : HealthExporter.shared.access }

    /// Whether Finish's last line can say the session goes to Apple Health too.
    var healthSharingAllowed: Bool {
        switch healthAccess {
        case .authorized, .notDetermined: true
        case .denied, .unavailable: false
        }
    }

    /// Settings' "Connect".
    func connectHealth() async {
        guard !preview else { return }
        await HealthExporter.shared.connect()
    }

    /// The session detail's "Try again".
    func retryHealth(_ id: SessionID) async {
        guard !preview else { return }
        await HealthExporter.shared.retry(id)
    }

    /// The latest body mass in Apple Health, for Settings; nil without one or without access.
    func latestHealthBodyMass() async -> Mass? {
        guard !preview else { return nil }
        return await BodyMassReader.shared.latest()
    }
}
