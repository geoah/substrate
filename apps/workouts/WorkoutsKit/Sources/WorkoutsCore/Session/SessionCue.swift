/// A haptic the session asks for (§9.3), drawn by the platform. `serial`
/// moves with each, so the same cue twice plays twice.
public struct SessionCue: Codable, Equatable, Sendable {
    public enum Kind: String, Codable, Equatable, Sendable {
        /// The Table's tick.
        case tick
        /// Rows' Log, once the set is in the journal.
        case logged
        case countdownEnded
        case shortCountdownEnded
        case targetReached
        /// A refusal, or a set the entry check questions.
        case warning
        case finishSaved
    }

    public var kind: Kind
    /// Played only while the session cover shows: a countdown that
    /// notifies alerts through the banner elsewhere in the app.
    public var onlyOnSessionScreen: Bool
    public var serial: Int

    public init(kind: Kind, onlyOnSessionScreen: Bool = false, serial: Int) {
        self.kind = kind
        self.onlyOnSessionScreen = onlyOnSessionScreen
        self.serial = serial
    }
}
