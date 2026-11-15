import Foundation
import SubstrateCore

/// A question's, a suggestion's or an approval's card, inline in the thread
/// that asked.
///
/// A card stands in the transcript only while it waits on the person, or
/// until its record is read. Once answered, decided, approved or declined
/// it is a mark on the tool call that asked (`CardResolution`, on the
/// call's row), which opens to what was asked and what was answered.
public struct CardPresentation: Codable, Equatable, Sendable {
    public var question: QuestionCard?
    public var suggestion: SuggestionCard?
    public var approval: ApprovalCard?
    /// How it ended, once it has: what its call's row says in its place.
    public var resolution: CardResolution?

    /// It waits on the person, as far as the app has read.
    public var waits: Bool {
        if let question { return question.pending }
        if let approval { return approval.pending }
        if let suggestion { return !suggestion.decided }
        return false
    }

    /// It is drawn as a card under its call: it waits, or its record is
    /// not read yet. Questions being answered in the person's own words
    /// are aside: the composer says so in their place.
    public var stands: Bool {
        if let question { return (question.loading || question.pending) && !question.aside }
        if let approval { return approval.loading || approval.pending }
        if let suggestion { return !suggestion.decided }
        return false
    }
}

/// How a card ended, as its call's row says it: the mark, who decided and
/// what, and, opened, what was asked and what was answered.
public struct CardResolution: Codable, Hashable, Sendable {
    public enum Mark: String, Codable, Hashable, Sendable {
        /// Answered, applied, or approved and run.
        case done
        /// Dismissed or declined: the person's own no.
        case declined
        /// Approved, and the call then failed.
        case failed
        /// Approved, and the call is running.
        case running
    }

    /// One thing that was asked, with what was answered.
    public struct Fact: Codable, Hashable, Sendable {
        public var label: String
        public var value: String

        public init(label: String, value: String) {
            self.label = label
            self.value = value
        }
    }

    public var card: CardReference
    public var mark: Mark
    /// "You answered", "You declined gmail_send": said beside the mark.
    public var label: String
    /// When, where the record says.
    public var at: Date?
    public var facts: [Fact]
    /// A decided suggestion: the change, applied or not taken.
    public var panel: ChangePanelPresentation?
    /// What was asked is on a record not read yet: read when the row is
    /// opened.
    public var loading: Bool
}

/// The agent's questions. Prompts and labels are the model's and are drawn
/// as plain text; what is sent is the stored values.
public struct QuestionCard: Codable, Equatable, Sendable {
    public struct Question: Codable, Equatable, Sendable, Identifiable {
        public var id: String
        public var prompt: String
        public var multi: Bool
        public var options: [Option]
    }

    public struct Option: Codable, Equatable, Sendable, Identifiable {
        public var id: String { value }
        public var value: String
        public var label: String
        public var description: String?
        /// Chosen in the answer the owner gave.
        public var chosen: Bool
    }

    public var title: String
    /// Until the record is read the card claims no state.
    public var loading: Bool
    public var pending: Bool
    public var questions: [Question]
    /// The answer or the dismissal that did not go through, drawn in the
    /// card with its button.
    public var failure: FailurePresentation?
    public var busy: String?
    public var answer: String
    public var dismiss: String
    /// "Answer in your own words": the card leaves the transcript and the
    /// composer takes the cursor.
    public var words: String
    /// Being answered in the person's own words: off the transcript until
    /// the answer is sent or the choice is taken back.
    public var aside: Bool
}

/// A suggested change: the record, each property before and after, Apply
/// and Dismiss; once decided, the change applied or the one not taken.
public struct SuggestionCard: Codable, Equatable, Sendable {
    public var decided: Bool
    public var panel: ChangePanelPresentation
    public var rationale: String?
    /// What Apply writes, as shown: the decision is refused if the request
    /// moved since.
    public var values: [ValueLine]
    public var canDecide: Bool
    /// Where the console decides it, for a token without `decide:`.
    public var console: String?
    public var consoleNote: String?
    public var apply: String
    public var destructive: Bool
    public var dismiss: String
    /// The decision that did not go through, drawn in the card with its
    /// button.
    public var failure: FailurePresentation?
    public var busy: String?
}

/// A call an agent made that waits for the owner (decision record 0254):
/// who asks, the tool, and exactly what it would run with, then Approve or
/// Decline; once decided, where the call ended. The arguments are the
/// model's and are drawn as plain text; the decision sends the state alone.
public struct ApprovalCard: Codable, Equatable, Sendable {
    public enum Tone: String, Codable, Equatable, Sendable {
        case waiting, running, done, failed, declined
    }

    public struct Argument: Codable, Equatable, Sendable, Identifiable {
        public var id: String { name }
        public var name: String
        public var label: String
        public var value: String
    }

    public var title: String
    /// Until the record is read the card claims no state.
    public var loading: Bool
    public var pending: Bool
    public var tone: Tone
    /// "Secretary wants to run"
    public var asks: String
    public var tool: String
    public var arguments: [Argument]
    /// Why an approved call did not work.
    public var error: String?
    /// The decision that did not go through, drawn in the card with its
    /// button.
    public var failure: FailurePresentation?
    public var busy: String?
    public var approve: String
    public var decline: String
}
