import SubstrateDevice

public extension SessionController {
    /// What the session's screens show that no replay needs: it starts
    /// afresh at every launch.
    struct Transient: Equatable, Sendable {
        /// The entry check's question, while it waits (§5.7).
        public var entryCheck: EntryCheckRequest?
        public var scroll: ScrollTarget?
        /// The last refused command, in the engine's words.
        public var lastRefusal: String?
        /// A Start is under way: the cover shows progress, not "no session".
        public var starting = false
        /// The cover should run its Finish flow (§7.4).
        public var finishFlow = false
        /// Started once the open session is saved (§7.4).
        public var startAfterFinish: StartRequest?
        /// Why the last Save kept the session open.
        public var finishProblem: String?
        public var cover: CoverRequest?
        public var cue: SessionCue?
        /// What puts back the set Delete set removed last.
        public var undo: SessionEvent.RestoreSet?
        /// Where the owner's notification permission stands, once read.
        public var alerts: NotificationPermission?
        /// Journals set aside at an earlier launch, listed until removed.
        public var setAside: [UnreadableRecord] = []
        /// A launch with a session open shows the cover the first time the
        /// app is in the foreground (§1.4, §6.3).
        public var coverOnFirstForeground = true
        /// Moves with every scroll, cover and cue request.
        public var serial = 0

        public init() {}

        mutating func next() -> Int {
            serial += 1
            return serial
        }

        mutating func play(_ kind: SessionCue.Kind, onlyOnSessionScreen: Bool = false) {
            cue = SessionCue(kind: kind, onlyOnSessionScreen: onlyOnSessionScreen, serial: next())
        }

        mutating func show(cover shown: Bool) {
            cover = CoverRequest(shown: shown, serial: next())
        }

        mutating func scroll(row: RowID? = nil, slot: String? = nil) {
            scroll = ScrollTarget(row: row, slot: slot, serial: next())
        }
    }
}
