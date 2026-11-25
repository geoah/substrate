import Foundation
import SubstrateCore

/// An `llm/provider` row.
struct Provider: Hashable, Sendable, Identifiable {
    let record: Record
    var id: String { record.id }
    var name: String { providerName(record.id) }
    var wire: String? { record["wire"]?.text }
    var baseURL: String? { record["baseURL"]?.text }
    /// A secret reads back redacted: set or not set is all the app can know.
    var hasKey: Bool { record["apiKey"]?.text != nil }

    /// Where its completions go. Only `anthropic` (and the default wire) have
    /// an endpoint of their own; an `openai` or `azure` row needs `baseURL`.
    var endpoint: String {
        if let baseURL { return baseURL }
        switch wire {
        case "openai", "azure": return "No endpoint set"
        default: return "The provider's default endpoint"
        }
    }
}

func providerName(_ id: String) -> String {
    switch id.lowercased() {
    case "openai": "OpenAI"
    case "anthropic": "Anthropic"
    case "gemini": "Gemini"
    case "azure": "Azure"
    default: id.capitalizedFirst
    }
}
