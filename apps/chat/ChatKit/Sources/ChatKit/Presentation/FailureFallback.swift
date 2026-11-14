import Foundation

/// Why a run ended, read off the server's own text: what the app did before
/// the server named a `cause`. `FailurePresenter` asks here only where a
/// thread, an `error` event or a `done` result carries no cause, so this
/// file is the whole of the matching and goes when no server that old is
/// left. The text is the agent loop's error, which leads with its own
/// wrapping (`substrate: validation failed: agent …: llm: …`) and quotes the
/// provider's answer somewhere inside, so it is matched, never shown as the
/// sentence.
enum FailureFallback {
    enum Cause: Equatable {
        /// The provider turned down the key on its record.
        case keyRefused
        /// The provider is turning requests down for now: a rate limit or a
        /// spent quota.
        case limited
        case other
    }

    /// The cause a server that names causes would have sent, where the
    /// text says: nil where it does not, which reads as the sentence for
    /// the status.
    static func cause(status: String, reason: String?) -> RunCause? {
        let line = reasonLine(reason) ?? ""
        switch status {
        case "error":
            switch cause(reason ?? "") {
            case .keyRefused: return .providerKeyRefused
            case .limited: return spent.contains(where: (reason ?? "").lowercased().contains) ? .providerBilling : .providerRateLimited
            case .other: return nil
            }
        case "overbudget":
            if line.hasPrefix("deadline exceeded") { return .deadline }
            if line.hasPrefix("max turns reached") { return .turnLimit }
            if line.hasPrefix("max tool calls reached") { return .toolCallLimit }
            return nil
        case "stopped":
            return line == shutdownReason ? .shutdown : .owner
        default:
            return nil
        }
    }

    /// Which limit an overbudget run reached, where its reason says, as a
    /// card's note: "It used all 8 of its turns."
    static func limit(status: String, reason: String?) -> String? {
        guard status == "overbudget", let line = reasonLine(reason) else { return nil }
        let words = limitWords(line)
        return words.hasPrefix("It reached a limit") ? nil : words
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
    static let keyRefused = [
        "unauthorized", "incorrect api key", "invalid api key", "invalid x-api-key", "authentication_error",
        "api_key_invalid", "api key not valid",
    ]
    static let limited = [
        "rate limit", "rate_limit", "too many requests", "insufficient_quota", "exceeded your current quota",
        "resource_exhausted",
    ]
    /// Of those, the ones that say the account is out of credit.
    static let spent = ["insufficient_quota", "exceeded your current quota"]
}
