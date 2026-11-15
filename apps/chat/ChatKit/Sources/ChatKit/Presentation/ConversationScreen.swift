import SubstrateCore
import SubstrateRuntime
import SubstrateScreens

/// One chat as a window draws it: its stored rows and its run as turns,
/// what became of the last send, and the composer.
public struct ConversationScreen: Codable, Equatable, Sendable {
    public var key: ChildKey
    public var thread: String?
    /// The record's path, for Copy Thread ID and the technical details.
    public var threadPath: String?
    public var title: String
    public var subtitle: String
    /// What a rename starts from.
    public var subject: String
    public var agent: String?
    public var agentName: String
    /// Who a new chat is with, and a few openers; nil once it has turns.
    public var hero: NewChatHero?
    /// The thread holds rows before the ones drawn: a row at the top that
    /// reads them when it comes on screen (`ChatApp.Command.olderRows`).
    public var older: OlderRows?
    public var turns: [TurnPresentation]
    /// Where what the person had not read begins, when the chat was opened
    /// with more than they had seen: a line above that turn, which the
    /// chat opens at. It stays while the chat is open.
    public var newLine: NewLine?
    /// A send is out and nothing has come back yet.
    public var typing: Bool
    public var status: [ConversationNote]
    /// What failed, where each is drawn: a card at its turn, one strip
    /// above the composer.
    public var failures: ScreenFailures
    /// The thread's reference and the run's figures, each labelled, where
    /// the owner asked for technical details.
    public var technical: [LabeledValue]?
    public var cards: [String: CardPresentation]
    /// The calls a sub-agent asked for in its own conversation, by the
    /// approval: they wait on the owner after the transcript, their cards
    /// in `cards`.
    public var approvals: [String]
    /// What the chat's tool results named each record, by `kind/id`.
    public var titles: [String: String]
    public var composer: ComposerPresentation
    public var streaming: Bool
    public var stopping: Bool
    public var expandThinking: Bool
    /// Moved by every send from this conversation: the transcript goes to
    /// its end, wherever it was.
    public var sends: Int
    /// Where the transcript stands when the chat is shown.
    public var opens: TranscriptOpening
    /// Moved each time the chat is asked to be put at a row again while it
    /// is shown (a message opened from a search): the transcript goes to
    /// `opens` once more.
    public var visit = 0
    /// Rows arrived above the ones drawn: the turn that stood first before
    /// them, which a transcript on screen is kept at.
    public var above: RowsAbove?

    /// How many messages stand below `turn`, the last one a reader can
    /// see: what Jump to Latest says. A message is what somebody said, the
    /// person or the assistant; a line about the run is none.
    public func messages(below turn: String?) -> Int {
        guard let turn, let index = turns.firstIndex(where: { $0.id == turn }) else { return 0 }
        return turns[(index + 1)...].count { turn in
            switch turn.body {
            case .user, .assistant: true
            default: false
            }
        }
    }
}

/// Where a transcript stands when its chat is shown: at its end, or with
/// one turn at the top of the screen (a chat opened at the first message
/// not yet read).
public enum TranscriptOpening: Codable, Equatable, Sendable {
    case end
    case turn(String)
}

/// What Jump to Latest says: how many messages are below.
public struct JumpToLatest: Equatable, Sendable {
    /// Beside the arrow; nil where nothing is counted.
    public var count: String?
    /// What a screen reader is told.
    public var label: String

    public init(below messages: Int) {
        count = messages > 0 ? (messages > 99 ? "99+" : "\(messages)") : nil
        label = messages > 0 ? "Jump to Latest, \(plural(messages, "message")) below" : "Jump to Latest"
    }
}

/// The row at the top of a transcript whose thread holds more.
public struct OlderRows: Codable, Equatable, Sendable {
    /// "Earlier messages", or "Loading earlier messages…".
    public var label: String
    public var loading: Bool
}

/// Rows read in above a transcript: the turn that was its first, by the id
/// it is drawn under, and how many times rows have arrived so.
public struct RowsAbove: Codable, Equatable, Sendable {
    public var turn: String
    public var count: Int
}

/// The line above the first turn the person had not read.
public struct NewLine: Codable, Equatable, Sendable {
    /// The turn it stands above.
    public var turn: String
    /// "New"
    public var label: String
    /// What a screen reader says of it.
    public var spoken: String
}

public struct NewChatHero: Codable, Equatable, Sendable {
    public var title: String
    public var description: String?
    public var prompts: [String]
    /// Said in place of the rest when there is no assistant.
    public var empty: String?
    /// The agent's provider has no key: said above the openers, in the
    /// transcript, so the composer's bar never grows over them.
    public var keyless: KeylessCallout?
}

/// A line about the run that is no failure: working elsewhere, an answer
/// arriving in part, a chat that is gone. What failed is `failures`.
public struct ConversationNote: Codable, Hashable, Sendable {
    public enum Kind: String, Codable, Hashable, Sendable {
        case partial, background, deleted
    }

    public var kind: Kind
    public var title: String?
    public var message: String

    public init(kind: Kind, title: String?, message: String) {
        self.kind = kind
        self.title = title
        self.message = message
    }
}

public struct ComposerPresentation: Codable, Equatable, Sendable {
    /// The draft as the app last wrote it, or as the chat held it when it
    /// was shown: what a field starts from. What is typed is the field's
    /// own, and is not here.
    public var draft: String
    /// Moved when the app rewrote the draft (a send cleared it, a refusal
    /// gave it back, dictation landed, the chat was shown again): the field
    /// takes the text.
    public var draftRevision: Int
    /// The text the rewrites since the field last spoke stand in place of.
    /// A field holding more than this typed it while a rewrite was on its
    /// way, and keeps it after the new text (`DraftReports`).
    public var draftReplaced: String
    /// The text the last rewrite alone stands in place of, for a field that
    /// took the one before it.
    public var draftBefore: String
    /// The texts the rewrites put into the draft that are in no other
    /// place (a message given back, a thread's kept draft), each with the
    /// revision that carried it: a field that keeps its own text over a
    /// rewrite takes these all the same.
    public var draftGiven: [DraftGiven] = []
    public var placeholder: String
    /// Why a send cannot go now.
    public var blocked: String?
    /// Said even while the field is empty (offline).
    public var blockedWhenEmpty: Bool
    public var keyless: KeylessCallout?
    /// A new chat picks its agent in the composer.
    public var picksAgent: Bool
    public var agentLabel: String
    public var listenOnOpen: Bool
    public var canSend: Bool
    /// What is happening while a reply is on its way; nil when nothing is.
    public var status: ComposerStatus?
}

/// What the composer says is happening while a reply is on its way, from
/// the send to the end: the stream opening, the model thinking or writing,
/// the tool that is running, a stream that dropped opening again. A state,
/// never a failure.
public struct ComposerStatus: Codable, Hashable, Sendable {
    public enum Kind: String, Codable, Hashable, Sendable, CaseIterable {
        case connecting, thinking, writing, tool, reconnecting, stopping
    }

    public var kind: Kind
    public var words: String

    public init(kind: Kind, words: String) {
        self.kind = kind
        self.words = words
    }
}

/// A text a rewrite put into the composer that must not be lost with the
/// rewrite: where it goes beside what the field holds, and the draft
/// revision that carried it.
public struct DraftGiven: Codable, Equatable, Sendable {
    public var text: String
    /// Under what the field holds (a kept draft, the older text) or above
    /// it (a message, ahead of what was typed since).
    public var below: Bool
    public var revision: Int
}

/// An agent whose provider has no key cannot answer: said before anybody
/// types.
public struct KeylessCallout: Codable, Hashable, Sendable {
    public var provider: String
    public var title: String
    public var message: String
    public var action: String
}
