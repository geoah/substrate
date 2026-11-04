import SubstrateCore

/// A question's or a suggestion's card, inline in the thread that asked.
public struct CardPresentation: Codable, Equatable, Sendable {
    public var question: QuestionCard?
    public var suggestion: SuggestionCard?
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
    public var problem: String?
    public var busy: String?
    public var answer: String
    public var dismiss: String
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
    public var problem: String?
    public var busy: String?
}
