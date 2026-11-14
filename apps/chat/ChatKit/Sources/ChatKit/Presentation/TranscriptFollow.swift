/// Whether a transcript follows its end, as a value the view keeps and
/// asks: the rule is here, away from the scroll view, so a test holds it.
///
/// Following is the reader's intent, and only the reader changes it. A
/// touch or a scroll lets go; coming to rest near the end takes it up
/// again; a send and Jump to Latest take it up from anywhere. What the
/// transcript does never changes it: a reply that grew by a screen while
/// the app was away, a keyboard that rose, a window made narrower all move
/// the end, and a transcript that was following goes there, while one that
/// was not stays where it was. Asking "is the end on screen?" after the
/// content moved, as the view once did, answers no exactly when following
/// matters.
public struct TranscriptFollow: Equatable, Sendable {
    /// Coming to rest within this many points of the end follows it again.
    public static let near = 80.0

    /// What the view does about the scroll position.
    public enum Move: Equatable, Sendable {
        case stay
        /// To the end of the content.
        case end(animated: Bool)
    }

    /// The transcript goes where its end goes.
    public private(set) var following: Bool

    /// - Parameter opening: a chat shown at its end follows it; one shown
    ///   at a turn (its first unread message) stays there until the reader
    ///   comes down.
    public init(opening: TranscriptOpening = .end) {
        following = opening == .end
    }

    /// A finger or a wheel took hold of the transcript.
    public mutating func touched() {
        following = false
    }

    /// The transcript came to rest, `distance` points of content below the
    /// screen's end.
    public mutating func rested(distance: Double) -> Move {
        following = distance <= Self.near
        // Near is followed from the end itself.
        return following && distance > 0.5 ? .end(animated: true) : .stay
    }

    /// The content's height or the room for it changed: a reply grew, rows
    /// landed, the keyboard showed or hid, the device turned, the window
    /// was resized, the text size changed.
    public func resized() -> Move {
        following ? .end(animated: false) : .stay
    }

    /// The app or the window is in front again.
    public func returned() -> Move {
        following ? .end(animated: false) : .stay
    }

    /// A message was sent from here: to the end, from wherever it was.
    public mutating func sent() -> Move {
        following = true
        return .end(animated: true)
    }

    /// Jump to Latest.
    public mutating func jumped() -> Move {
        following = true
        return .end(animated: true)
    }
}
