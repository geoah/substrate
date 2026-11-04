import Foundation
import SubstrateDevice
import SubstrateRuntime

public extension SessionController {
    /// What the session's screens show that no replay needs: it starts
    /// afresh at every launch.
    struct Transient: Equatable, Sendable {
        /// The entry check's question, while it waits (§5.7).
        public var entryCheck: EntryCheckRequest?
        /// The row or card the list brings into view.
        public var scroll = Signal<ScrollTarget>()
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
        /// How each Save went, by its receipt: nil inside is saved.
        public var finishOutcomes: [UUID: String?] = [:]
        /// The session cover opened or closed, at Start, Finish, Discard,
        /// the first foreground with a session open, and a tap that reveals
        /// the session.
        public var cover = Signal<Bool>()
        /// The session cover is on screen: the countdown's alert stays quiet
        /// in front of it, where the rest bar and the haptic say it (§9.2).
        public var coverVisible = false
        public var cue = Signal<SessionCue>()
        /// What puts back the set Delete set removed last.
        public var undo: SessionEvent.RestoreSet?
        /// Where the owner's notification permission stands, once read.
        public var alerts: NotificationPermission?
        /// Journals set aside at an earlier launch, listed until removed.
        public var setAside: [UnreadableRecord] = []
        /// A launch with a session open shows the cover the first time the
        /// app is in the foreground (§1.4, §6.3).
        public var coverOnFirstForeground = true

        public init() {}

        mutating func play(_ kind: SessionCue.Kind, onlyOnSessionScreen: Bool = false) {
            cue.raise(SessionCue(kind: kind, onlyOnSessionScreen: onlyOnSessionScreen))
        }

        mutating func show(cover shown: Bool) {
            cover.raise(shown)
        }

        mutating func scroll(row: RowID? = nil, slot: String? = nil) {
            scroll.raise(ScrollTarget(row: row, slot: slot))
        }
    }
}
