import Foundation
import SubstrateCore
import SubstrateScreens

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

    /// The run's figures, each a labelled fact: its status, turns, tool
    /// calls, tokens and cost, where the row holds them.
    var tally: [LabeledValue] {
        let p = record.properties
        var out: [LabeledValue] = []
        if !status.isEmpty { out.append(LabeledValue("Status", reason.map { "\(status) (\($0))" } ?? status)) }
        if let n = p["turns"]?.int { out.append(LabeledValue("Turns", n.formatted())) }
        if let n = p["toolCalls"]?.int { out.append(LabeledValue("Tool calls", n.formatted())) }
        if let n = p["totalTokens"]?.int {
            var words = n.formatted()
            if let i = p["promptTokens"]?.int, let o = p["completionTokens"]?.int {
                words += " (\(i.formatted()) in, \(o.formatted()) out)"
            }
            out.append(LabeledValue("Tokens", words))
        }
        if let cost = p["costUSD"]?.number { out.append(LabeledValue("Cost", String(format: "$%.4f", cost))) }
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
