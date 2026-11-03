/// A question the owner answers before something is given up, in its
/// words: the app's own `question` it confirms, the title, what goes with
/// it, and the two answers.
public struct ConfirmationPresentation<Question: Codable & Hashable & Sendable>: Codable, Hashable, Sendable {
    public var question: Question
    public var title: String
    public var message: String
    /// The destructive answer.
    public var confirm: String
    public var cancel: String

    public init(question: Question, title: String, message: String, confirm: String, cancel: String = "Cancel") {
        self.question = question
        self.title = title
        self.message = message
        self.confirm = confirm
        self.cancel = cancel
    }
}
