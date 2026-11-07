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
    /// The last turn ended before it finished, and nothing runs now.
    public var unfinished: UnfinishedNotice?
    public var outcomes: [OutcomePresentation]
    /// The thread's reference and the run's figures, each labelled, where
    /// the owner asked for technical details.
    public var technical: [LabeledValue]?
    public var cards: [String: CardPresentation]
    /// What the chat's tool results named each record, by `kind/id`.
    public var titles: [String: String]
    public var composer: ComposerPresentation
    public var streaming: Bool
    public var stopping: Bool
    public var error: String?
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

/// A line about the run: working elsewhere, an answer arriving in part, an
/// error, a reload that did not land, a chat that is gone.
public struct ConversationNote: Codable, Hashable, Sendable {
    public enum Kind: String, Codable, Hashable, Sendable {
        case partial, background, failed, reloadFailed, deleted
    }

    public var kind: Kind
    public var title: String?
    public var message: String
    /// "Try Again", for a reload that did not land.
    public var action: String?
    /// The server's own words for a failed run, beneath the card's.
    public var detail: String?

    public init(kind: Kind, title: String?, message: String, action: String? = nil, detail: String? = nil) {
        self.kind = kind
        self.title = title
        self.message = message
        self.action = action
        self.detail = detail
    }
}

/// A chat whose last turn ended before it finished: what happened, why, and
/// a way to go on.
public struct UnfinishedNotice: Codable, Hashable, Sendable {
    public var tone: UnfinishedTurn.Tone
    public var title: String
    public var detail: String?
    /// "Continue"
    public var action: String
    /// Why Continue cannot send now, as the composer says it.
    public var blocked: String?
    /// What Continue does: "Sends “Continue where you left off.”"
    public var hint: String
}

/// A send that started no turn, as its card says it.
public struct OutcomePresentation: Codable, Hashable, Sendable, Identifiable {
    public enum Kind: String, Codable, Hashable, Sendable { case refused, checking, unknown }

    public var id: String
    public var kind: Kind
    public var title: String?
    public var message: String
    /// The text, to copy; nil while it is checked.
    public var text: String?
}

public struct ComposerPresentation: Codable, Equatable, Sendable {
    public var draft: String
    /// Moved when the app rewrote the draft (a send cleared it, a refusal
    /// gave it back): the field takes the text.
    public var draftRevision: Int
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

/// An agent whose provider has no key cannot answer: said before anybody
/// types.
public struct KeylessCallout: Codable, Hashable, Sendable {
    public var provider: String
    public var title: String
    public var message: String
    public var action: String
}
