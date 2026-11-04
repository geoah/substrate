import Foundation
import SubstrateDevice
import SubstrateRuntime

/// The session's screens as one value: the session presented, and what the
/// cover, its sheets and the platform read beside it.
public struct SessionScreenPresentation: Codable, Equatable, Sendable {
    public var session: SessionPresentation?
    /// The session as the engine holds it: what the Finish sheet's numbers
    /// are worked out from as its draft moves (`FinishPresenter.summary`).
    public var state: SessionState?
    public var starting: Bool
    public var finishing: Bool
    public var entryCheck: EntryCheckRequest?
    /// The row or card the list brings into view, once.
    public var scroll: Signal<ScrollTarget>
    public var lastRefusal: String?
    /// Why the last Save kept the session open.
    public var finishProblem: String?
    /// How each Save went, by its receipt: nil inside is saved. A Save
    /// waits for its own, never for the last one's words.
    public var finishOutcomes: [UUID: String?]
    /// The journal could not be written (a full disk): ticks live only in
    /// memory until a write succeeds again. The cover and Finish say so.
    public var journalProblem: String?
    /// The cover opened or closed, once per request.
    public var cover: Signal<Bool>
    /// The cover is on screen, as the app last said.
    public var coverVisible: Bool
    /// The haptic to play, once per cue.
    public var cue: Signal<SessionCue>
    public var finishFlow: Bool
    public var startAfterFinish: StartRequest?
    public var undo: SessionEvent.RestoreSet?
    public var alerts: NotificationPermission?
    /// Journals that did not load, set aside (§2.3).
    public var setAside: [UnreadableRecord]

    public init(
        session: SessionPresentation? = nil, state: SessionState? = nil, starting: Bool = false, finishing: Bool = false,
        entryCheck: EntryCheckRequest? = nil, scroll: Signal<ScrollTarget> = Signal(), lastRefusal: String? = nil,
        finishProblem: String? = nil, finishOutcomes: [UUID: String?] = [:], journalProblem: String? = nil,
        cover: Signal<Bool> = Signal(), coverVisible: Bool = false, cue: Signal<SessionCue> = Signal(), finishFlow: Bool = false,
        startAfterFinish: StartRequest? = nil, undo: SessionEvent.RestoreSet? = nil, alerts: NotificationPermission? = nil,
        setAside: [UnreadableRecord] = []
    ) {
        self.session = session
        self.state = state
        self.starting = starting
        self.finishing = finishing
        self.entryCheck = entryCheck
        self.scroll = scroll
        self.lastRefusal = lastRefusal
        self.finishProblem = finishProblem
        self.finishOutcomes = finishOutcomes
        self.journalProblem = journalProblem
        self.cover = cover
        self.coverVisible = coverVisible
        self.cue = cue
        self.finishFlow = finishFlow
        self.startAfterFinish = startAfterFinish
        self.undo = undo
        self.alerts = alerts
        self.setAside = setAside
    }

    public var isActive: Bool { session != nil }
}
