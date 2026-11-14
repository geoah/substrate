import Foundation

/// A failure as it is drawn: one sentence that says what happened, at most
/// one note, one button that does the next thing, and the details behind a
/// disclosure. The same value draws a card at a turn, the strip above the
/// composer and the strip at the top of the list.
public struct FailurePresentation: Codable, Hashable, Sendable, Identifiable {
    public enum Tone: String, Codable, Hashable, Sendable {
        /// Something failed.
        case bad
        /// Something was cut short, or did not get through.
        case warn
        /// Something the person did themselves, or a check under way.
        case quiet
    }

    /// Stable while the same failure stands in the same place.
    public var id: String
    public var tone: Tone
    public var sentence: String
    public var note: String?
    public var button: FailureButton?
    public var details: FailureDetails?
    /// A check is under way and nothing is asked of the person yet.
    public var working: Bool
    /// What takes it away where nothing else will; nil where the next
    /// success clears it.
    public var dismiss: FailureAction?

    public init(
        id: String, tone: Tone, sentence: String, note: String? = nil, button: FailureButton? = nil, details: FailureDetails? = nil,
        working: Bool = false, dismiss: FailureAction? = nil
    ) {
        self.id = id
        self.tone = tone
        self.sentence = sentence
        self.note = note
        self.button = button
        self.details = details
        self.working = working
        self.dismiss = dismiss
    }

    /// What VoiceOver reads for the whole card: the sentence, then the note.
    public var spoken: String { [sentence, note].compactMap { $0 }.joined(separator: " ") }
}

/// The one button of a failure.
public struct FailureButton: Codable, Hashable, Sendable {
    public var title: String
    public var action: FailureAction
    /// Its work is out: it shows so and cannot be pressed again.
    public var busy: Bool
    /// Why it cannot be pressed now, as the composer says it.
    public var blocked: String?
    /// A rate limit lifts then: the button counts down to it, and cannot be
    /// pressed before.
    public var enablesAt: Date?
    /// It leaves the app for the browser.
    public var external: Bool

    public init(title: String, action: FailureAction, busy: Bool = false, blocked: String? = nil, enablesAt: Date? = nil, external: Bool = false) {
        self.title = title
        self.action = action
        self.busy = busy
        self.blocked = blocked
        self.enablesAt = enablesAt
        self.external = external
    }

    /// The whole seconds left before it may be pressed; nil once it may.
    public func remaining(at now: Date) -> Int? {
        guard let enablesAt else { return nil }
        let left = Int(enablesAt.timeIntervalSince(now).rounded(.up))
        return left > 0 ? left : nil
    }

    /// The title as it reads now: "Try Again in 20 s" while it counts down.
    public func title(at now: Date) -> String {
        guard let left = remaining(at: now) else { return title }
        return left >= 120 ? "\(title) in \(Int((Double(left) / 60).rounded(.up))) min" : "\(title) in \(left) s"
    }

    public func enabled(at now: Date) -> Bool { !busy && blocked == nil && remaining(at: now) == nil }
}

/// What a failure's disclosure opens to, under a label that says whose
/// words they are.
public struct FailureDetails: Codable, Hashable, Sendable {
    /// "Details"
    public var title: String
    /// "The server said", or "This device reported" for the app's own
    /// account of a connection that failed.
    public var label: String
    public var text: String
}

/// What a failure's button does.
public enum FailureAction: Codable, Hashable, Sendable {
    /// Try Again and Continue: the thread run again with no new message.
    case resume
    /// Send Again: the composer's text.
    case send
    /// The stop, again.
    case stop
    /// The chat's rows read again after a run.
    case reload
    /// The stream of a reply being written, opened again.
    case reopen
    /// A chat's rows read again.
    case reloadRows(thread: String)
    case rename(thread: String, subject: String)
    /// The list read again, and the change feed opened.
    case refresh
    /// The app's own sheet for a provider's key.
    case addKey(provider: String)
    /// A page of the console on the signed-in server, in the browser.
    case open(String)
    case newChat(agent: String?)
    /// The failure above the composer was read.
    case dismiss
    case dismissRename
}
