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
    /// When the chat last happened, which the list orders, groups and
    /// stamps it by: while a turn runs, the row's last write; else when its
    /// last turn settled. Not `updatedAt` once settled: a write that is no
    /// turn (a migration stamping `lastMessage`, a recount of what waits)
    /// moves it, and one such write dates every chat it touched to that
    /// instant. A settled row without `finishedAt` falls back to its last
    /// write.
    var activeAt: Date {
        if !running, let settled = record["finishedAt"]?.string.flatMap(Timestamp.parse) { return settled }
        return record.updatedDate
    }

    /// The row's last write: what the server orders a list of threads by,
    /// so where the window of a list read so far ends.
    var writtenAt: Date { record.updatedDate }
    var automated: Bool { mode != "chat" }
    /// The newest message the thread held when it last settled, which a
    /// person's mark is compared with; nil from a server older than marks,
    /// and on a thread that holds none.
    var lastMessage: String? { record["lastMessage"]?.referenceID }
    /// How many of its questions, suggestions and calls to approve wait on
    /// a person, as a server that keeps marks counts them on the row.
    var waiting: Int { max(0, record["waitingOnPerson"]?.int ?? 0) }

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
    return cutTitle(
        text.split(separator: "\n", omittingEmptySubsequences: true).first.map {
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
