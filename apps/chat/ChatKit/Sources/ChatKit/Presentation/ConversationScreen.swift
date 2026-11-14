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
    /// "Older messages aren't shown here."
    public var olderHidden: String?
    public var turns: [TurnPresentation]
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
