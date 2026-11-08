import Foundation
import SubstrateCore

// The chat's words, derived from records: what an agent is called, what a
// thread is called, what a tool call did. Ported from the console's
// web/console/src/lib/agent-chat.ts; display names are the app's own and are
// never sent to the API.

enum HostFunction {
    static let query = "\(Kinds.core)/query"
    static let write = "\(Kinds.core)/write"
    static let propose = "\(Kinds.core)/propose"
    static let ask = "\(Kinds.core)/ask"
    static let wait = "\(Kinds.core)/wait"
    static let edit = "\(Kinds.core)/edit"
    static let vocabulary = "\(Kinds.core)/vocabulary"
    /// Every function whose body is the engine's own.
    static let all = [query, write, propose, ask, wait, edit, vocabulary]
    /// The ones whose answers the details read themselves; the rest show
    /// what they returned, as a function tool's do (the console's rule).
    static let detailed = [query, write, propose, ask]
}

/// An agent's name as a person reads it: its local name, words split.
public func agentName(_ id: String) -> String {
    let local = id.split(whereSeparator: { $0 == "/" || $0 == ":" }).last.map(String.init) ?? id
    let spaced = local.splittingWords().joined(separator: " ").lowercased()
    return spaced.isEmpty ? id : spaced.capitalizedFirst
}

struct AgentTool: Hashable, Sendable {
    /// The function's identity, `<authority>/<package>/<name>`.
    var function: String
    /// The model-facing name the transcript records.
    var name: String
    var description: String?
}

/// A `substrate.reamde.dev/core/agent` row, read for chat.
struct Agent: Hashable, Sendable, Identifiable {
    enum Purpose: String, Sendable { case primary, supporting, `internal` }

    let record: Record
    var id: String { record.id }
    var name: String { agentName(record.id) }
    var description: String? { record["description"]?.text }
    /// The `llm/provider` row it names, by id. An agent on `runsOn` names
    /// none: the state's `provider(of:)` says what it runs on.
    var providerID: String? { record["provider"]?.referenceID }
    var model: String? { record["model"]?.text }
    /// The `llm/model` records it runs on, best first (decision record
    /// 0242); nil for an agent that says what it runs on another way.
    var runsOn: [String]? { record["runsOn"]?.array?.compactMap { referent($0, pin: Kinds.model) } }
    /// Callable only by other agents: the chat API refuses it.
    var chatCapable: Bool { record["hiddenFromChat"]?.bool != true }
    var purpose: Purpose {
        switch record["purpose"]?.string {
        case "supporting": .supporting
        case "internal": .internal
        default: .primary
        }
    }

    var tools: [AgentTool] {
        (record["tools"]?.array ?? []).compactMap { entry in
            guard let held = entry["function"]?.referencePath else { return nil }
            let prefix = "\(Kinds.core)/function/"
            let fn = held.hasPrefix(prefix) ? String(held.dropFirst(prefix.count)) : held
            let alias = entry["name"]?.text
            return AgentTool(
                function: fn, name: alias ?? (fn.split(separator: "/").last.map(String.init) ?? fn),
                description: entry["description"]?.text)
        }
    }

    /// The agents it may hand work to, by record id.
    var subagents: [String] {
        (record["subagents"]?.array ?? []).compactMap { item in
            guard let held = item.referencePath else { return nil }
            let prefix = "\(Kinds.agent)/"
            return held.hasPrefix(prefix) ? String(held.dropFirst(prefix.count)) : held
        }
    }

    /// A few openers for an empty chat, by what it can do.
    var examplePrompts: [String] {
        let fns = tools.map(\.function)
        var out: [String] = []
        if fns.contains(HostFunction.query) { out += ["What’s due this week?", "What changed in my data today?"] }
        if fns.contains(HostFunction.write) {
            out.append("Tidy up the titles of my recent notes")
        } else if fns.contains(HostFunction.propose) {
            out.append("Is anything overdue that should be rescheduled?")
        }
        if out.isEmpty { out.append("What can you help me with?") }
        return Array(out.prefix(3))
    }
}

/// The agents a chat offers by default: the primary ones a person can chat
/// with, or every one a person can chat with when none is primary (the
/// console's `agentListing`).
func defaultOffered(_ agents: [Agent]) -> Set<String> {
    let talkable = agents.filter(\.chatCapable)
    let primary = talkable.filter { $0.purpose == .primary }
    return Set((primary.isEmpty ? talkable : primary).map(\.id))
}
