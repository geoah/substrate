import Foundation

/// A turn that ended in an error, as its card says it: what happened, in
/// words, and the server's own text as a separate fact beneath them. The
/// server's text is the agent loop's error, which leads with its own
/// wrapping (`substrate: validation failed: agent …: llm: …`) and quotes the
/// provider's answer somewhere inside, so it is matched, never shown as the
/// headline.
enum TurnFailureWords {
    enum Cause: Equatable {
        /// The provider turned down the key on its record.
        case keyRefused
        /// The provider is turning requests down for now: a rate limit or a
        /// spent quota.
        case limited
        case other
    }

    static func note(_ text: String, agent: String, provider: String?) -> ConversationNote {
        switch cause(text) {
        case .keyRefused:
            ConversationNote(kind: .failed, title: "\(provider ?? "The provider") refused the API key",
                             message: "\(agent) couldn't answer. Replace the key on \(provider.map { "\($0)'s" } ?? "the") provider record, then send again.",
                             detail: text)
        case .limited:
            ConversationNote(kind: .failed, title: "\(provider ?? "The provider") is turning requests down",
                             message: "\(agent) couldn't answer: a rate limit or a spent quota. Try again later.", detail: text)
        case .other:
            ConversationNote(kind: .failed, title: "\(agent) couldn't answer", message: "The run ended in an error.", detail: text)
        }
    }

    static func cause(_ text: String) -> Cause {
        let lower = text.lowercased()
        if keyRefused.contains(where: lower.contains) || lower.range(of: #"\b401\b"#, options: .regularExpression) != nil {
            return .keyRefused
        }
        if limited.contains(where: lower.contains) || lower.range(of: #"\b429\b"#, options: .regularExpression) != nil {
            return .limited
        }
        return .other
    }

    /// How OpenAI, Anthropic and Gemini word a refused key.
    static let keyRefused = ["unauthorized", "incorrect api key", "invalid api key", "invalid x-api-key", "authentication_error",
                             "api_key_invalid", "api key not valid"]
    static let limited = ["rate limit", "rate_limit", "too many requests", "insufficient_quota", "exceeded your current quota",
                          "resource_exhausted"]
}
