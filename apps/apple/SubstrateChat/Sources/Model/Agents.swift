import Foundation

// The chat's words, derived from records: what an agent is called, what a
// thread is called, what a tool call did. Ported from the console's
// web/console/src/lib/agent-chat.ts; display names are the app's own and are
// never sent to the API.

enum HostFunction {
    static let query = "\(Kinds.core)/query"
    static let write = "\(Kinds.core)/write"
    static let propose = "\(Kinds.core)/propose"
    static let ask = "\(Kinds.core)/ask"
    static let all = [query, write, propose, ask]
}

/// An agent's name as a person reads it: its local name, words split.
func agentName(_ id: String) -> String {
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
    /// The `llm/provider` row it completes against, by id.
    var providerID: String? { record["provider"]?.referenceID }
    var model: String? { record["model"]?.text }
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
            return AgentTool(function: fn, name: alias ?? (fn.split(separator: "/").last.map(String.init) ?? fn),
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

// MARK: Threads

/// The modes a person reads as a conversation: every run of a root agent.
/// A sub-agent's thread and a judge's verdict are parts of another run.
let conversationModes = ["chat", "manual", "call", "record", "schedule", "webhook"]

/// An `llm/thread` row, read for the chat list.
struct ChatThread: Hashable, Sendable, Identifiable {
    let record: Record
    var id: String { record.id }
    var agentID: String? {
        guard let path = record["agent"]?.referencePath else { return nil }
        let prefix = "\(Kinds.agent)/"
        return path.hasPrefix(prefix) ? String(path.dropFirst(prefix.count)) : path
    }

    var subject: String? { record["subject"]?.text }
    var mode: String { record["mode"]?.string ?? "chat" }
    var status: String { record["status"]?.string ?? "" }
    var running: Bool { status == "running" }
    var reason: String? { record["reason"]?.text }
    /// The start of the last reply a turn settled with, as plain text: the
    /// server keeps it as the model wrote it, Markdown and all.
    var preview: String? { record["lastReply"]?.text.map(plainText(markdown:)) }
    var startedAt: Date { record["startedAt"]?.string.flatMap(Timestamp.parse) ?? record.createdDate }
    /// The row's last write, which every settle makes.
    var activeAt: Date { record.updatedDate }
    var automated: Bool { mode != "chat" }

    func title(opening: String?) -> String { threadTitle(subject: subject, opening: opening) }

    var tally: [String] {
        let p = record.properties
        var out: [String] = []
        if !status.isEmpty { out.append(reason.map { "\(status) (\($0))" } ?? status) }
        if let n = p["turns"]?.int { out.append(plural(n, "turn")) }
        if let n = p["toolCalls"]?.int { out.append(plural(n, "tool call")) }
        if let n = p["totalTokens"]?.int {
            var words = plural(n, "token")
            if let i = p["promptTokens"]?.int, let o = p["completionTokens"]?.int {
                words += " (\(i.formatted()) in, \(o.formatted()) out)"
            }
            out.append(words)
        }
        if let cost = p["costUSD"]?.number { out.append(String(format: "$%.4f", cost)) }
        return out
    }
}

private let titleLength = 64

private func cutTitle(_ line: String) -> String {
    line.count > titleLength ? String(line.prefix(titleLength - 1)).trimmingCharacters(in: .whitespaces) + "…" : line
}

/// What a thread is called: its subject, else its opening message's first
/// line, cut short; a triggered thread's delivery envelope reads as what
/// fired rather than as JSON.
func threadTitle(subject: String?, opening: String?) -> String {
    if let subject = subject?.trimmingCharacters(in: .whitespacesAndNewlines), !subject.isEmpty {
        return cutTitle(subject)
    }
    let text = (opening ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
    if text.isEmpty { return "New chat" }
    if let delivery = deliveryNotice(Turn(id: "", role: .user, content: text)) {
        return "When \(delivery.title ?? kindDisplayName(delivery.kind)) changed"
    }
    return cutTitle(text.split(separator: "\n", omittingEmptySubsequences: true).first.map {
        $0.trimmingCharacters(in: .whitespaces)
    } ?? text)
}

/// Each thread's opening user message, from user messages in turn order.
func openingMessages(_ messages: [Record]) -> [String: String] {
    var out: [String: String] = [:]
    for message in messages {
        guard let thread = message["thread"]?.referenceID, out[thread] == nil else { continue }
        out[thread] = message["content"]?.string ?? ""
    }
    return out
}

enum DayGroup: String, CaseIterable, Sendable {
    case today = "Today"
    case yesterday = "Yesterday"
    case week = "Previous 7 Days"
    case earlier = "Earlier"

    static func of(_ date: Date, now: Date = Date(), calendar: Calendar = .current) -> DayGroup {
        let start = calendar.startOfDay(for: now)
        if date >= start { return .today }
        if let y = calendar.date(byAdding: .day, value: -1, to: start), date >= y { return .yesterday }
        if let w = calendar.date(byAdding: .day, value: -7, to: start), date >= w { return .week }
        return .earlier
    }
}

/// Markdown as one line of plain text, for a preview: no markers, no fences,
/// no table rules, whitespace collapsed.
func plainText(markdown: String) -> String {
    var lines: [String] = []
    for raw in markdown.split(separator: "\n", omittingEmptySubsequences: true) {
        var line = raw.trimmingCharacters(in: .whitespaces)
        if line.hasPrefix("```") || line.hasPrefix("~~~") { continue }
        if line.allSatisfy({ "|:- ".contains($0) }) { continue }
        while let first = line.first, "#>".contains(first) { line = String(line.dropFirst()).trimmingCharacters(in: .whitespaces) }
        for marker in ["- [ ] ", "- [x] ", "- [X] ", "- ", "* ", "+ "] where line.hasPrefix(marker) {
            line = String(line.dropFirst(marker.count))
            break
        }
        if line.hasPrefix("|") {
            line = line.split(separator: "|").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }.joined(separator: ", ")
        }
        let inline = (try? AttributedString(markdown: line, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace)))
            .map { String($0.characters) } ?? line
        lines.append(inline)
    }
    return lines.joined(separator: " ").split(whereSeparator: \.isWhitespace).joined(separator: " ")
}

// MARK: Kinds in words

/// A kind's name in words: its last segment, split. "Calendar event".
func kindDisplayName(_ kind: String) -> String {
    let name = kind.split(separator: "/").last.map(String.init) ?? kind
    return name.splittingWords().joined(separator: " ").lowercased().capitalizedFirst
}

func kindDisplayPlural(_ kind: String) -> String {
    var words = kindDisplayName(kind).split(separator: " ").map(String.init)
    guard let last = words.popLast() else { return "" }
    return (words + [pluralWord(last)]).joined(separator: " ")
}

private func pluralWord(_ word: String) -> String {
    let lower = word.lowercased()
    if lower == "person" { return word.prefix(1) == "P" ? "People" : "people" }
    if lower.hasSuffix("y"), let before = lower.dropLast().last, !"aeiou".contains(before) {
        return String(word.dropLast()) + "ies"
    }
    if ["s", "x", "z", "ch", "sh"].contains(where: lower.hasSuffix) { return word + "es" }
    return word + "s"
}

func plural(_ n: Int, _ one: String, _ many: String? = nil) -> String {
    "\(n.formatted()) \(n == 1 ? one : (many ?? one + "s"))"
}

/// "Tasks", "Tasks and Notes", "Tasks, Notes and People".
func joinWords(_ words: [String]) -> String {
    guard words.count > 1 else { return words.first ?? "" }
    return words.dropLast().joined(separator: ", ") + " and " + words.last!
}

extension String {
    var capitalizedFirst: String { prefix(1).uppercased() + dropFirst() }

    var lowercasedFirst: String {
        // "API keys" and "Gmail threads" keep their first word's casing.
        let first = split(separator: " ").first.map(String.init) ?? self
        if first.dropFirst().contains(where: \.isUppercase) { return self }
        return prefix(1).lowercased() + dropFirst()
    }

    /// `dueAt` → [due, At]; `snake_case-name` → [snake, case, name].
    func splittingWords() -> [String] {
        var words: [String] = []
        var current = ""
        var previous: Character?
        for ch in self {
            if ch == "_" || ch == "-" || ch == " " {
                if !current.isEmpty { words.append(current) }
                current = ""
            } else if ch.isUppercase, let p = previous, p.isLowercase || p.isNumber {
                words.append(current)
                current = String(ch)
            } else {
                current.append(ch)
            }
            previous = ch
        }
        if !current.isEmpty { words.append(current) }
        return words
    }
}
