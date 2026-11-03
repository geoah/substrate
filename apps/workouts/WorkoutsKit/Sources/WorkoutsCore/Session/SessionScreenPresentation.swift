import SubstrateDevice

/// Everything the session's screens draw: the session as the presenter
/// words it, and what waits on the owner around it.
public struct SessionScreenPresentation: Codable, Equatable, Sendable {
    public var session: SessionPresentation?
    /// The session as the engine holds it: what the Finish sheet's numbers
    /// are worked out from as its draft moves (`FinishPresenter.summary`).
    public var state: SessionState?
    public var starting: Bool
    public var finishing: Bool
    public var entryCheck: EntryCheckRequest?
    public var scroll: ScrollTarget?
    public var lastRefusal: String?
    /// Why the last Save kept the session open.
    public var finishProblem: String?
    /// The journal could not be written (a full disk): ticks live only in
    /// memory until a write succeeds again. The cover and Finish say so.
    public var journalProblem: String?
    public var cover: CoverRequest?
    public var cue: SessionCue?
    public var finishFlow: Bool
    public var startAfterFinish: StartRequest?
    public var undo: SessionEvent.RestoreSet?
    public var alerts: NotificationPermission?
    /// Journals that did not load, set aside (§2.3).
    public var setAside: [UnreadableRecord]

    public init(session: SessionPresentation? = nil, state: SessionState? = nil, starting: Bool = false, finishing: Bool = false,
                entryCheck: EntryCheckRequest? = nil, scroll: ScrollTarget? = nil, lastRefusal: String? = nil, finishProblem: String? = nil,
                journalProblem: String? = nil, cover: CoverRequest? = nil, cue: SessionCue? = nil, finishFlow: Bool = false,
                startAfterFinish: StartRequest? = nil, undo: SessionEvent.RestoreSet? = nil, alerts: NotificationPermission? = nil,
                setAside: [UnreadableRecord] = [])
    {
        self.session = session
        self.state = state
        self.starting = starting
        self.finishing = finishing
        self.entryCheck = entryCheck
        self.scroll = scroll
        self.lastRefusal = lastRefusal
        self.finishProblem = finishProblem
        self.journalProblem = journalProblem
        self.cover = cover
        self.cue = cue
        self.finishFlow = finishFlow
        self.startAfterFinish = startAfterFinish
        self.undo = undo
        self.alerts = alerts
        self.setAside = setAside
    }

    public var isActive: Bool { session != nil }
}
