import Foundation

/// A chat turn that ended before it finished, said for the person reading
/// the chat (the console's `unfinishedTurn`, web/console/src/lib/agent-runs.ts).
public struct UnfinishedTurn: Codable, Hashable, Sendable {
    public enum Tone: String, Codable, Hashable, Sendable {
        /// A stop somebody asked for.
        case quiet
        /// A turn something else cut short: a restart, a limit.
        case warn
        /// A failure.
        case bad
    }

    public var tone: Tone
    /// What happened, naming the agent.
    public var title: String
    /// Why, when the thread says.
    public var detail: String?
}

/// The reason the loop settles a turn the server's shutdown ended with
/// (`reasonShutdown` in internal/engine/agentloop.go).
let shutdownReason = "stopped: the server shut down during the turn"

/// A thread's `reason` as its first line: the loop's own words, before any
/// trace beneath them.
func reasonLine(_ reason: String?) -> String? {
    let line = reason?.split(separator: "\n", omittingEmptySubsequences: false).first?.trimmingCharacters(in: .whitespacesAndNewlines)
    return line?.isEmpty == false ? line : nil
}

/// How a thread's last turn ended when it didn't finish, as the tone its
/// notice and its mark take; nil for one still running or one that finished.
func unfinishedTone(status: String, reason: String?) -> UnfinishedTurn.Tone? {
    switch status {
    case "stopped": reasonLine(reason) == shutdownReason ? .warn : .quiet
    case "overbudget": .warn
    case "error": .bad
    default: nil
    }
}

/// Why a thread's last turn didn't finish, from its stored `status` and
/// `reason`; nil for one still running or one that finished. The reasons
/// read here are the loop's own, and one this does not know is shown as the
/// loop wrote it.
func unfinishedTurn(status: String, reason: String?, name: String) -> UnfinishedTurn? {
    let line = reasonLine(reason) ?? ""
    switch unfinishedTone(status: status, reason: reason) {
    case nil:
        return nil
    case .quiet:
        return UnfinishedTurn(tone: .quiet, title: "\(name) was stopped before it finished")
    case .warn where status == "stopped":
        return UnfinishedTurn(tone: .warn, title: "\(name) didn’t finish", detail: "The server restarted while it was working.")
    case .warn:
        return UnfinishedTurn(tone: .warn, title: "\(name) didn’t finish", detail: limitWords(line))
    case .bad:
        return UnfinishedTurn(tone: .bad, title: "\(name) ran into a problem and didn’t finish", detail: line.isEmpty ? nil : "What failed: \(line)")
    }
}

/// An overbudget run's reason, in words (`deadlineReason` and the two budget
/// reasons in internal/engine/agentloop.go).
func limitWords(_ reason: String) -> String {
    if reason.hasPrefix("deadline exceeded") { return "It ran out of time." }
    if let turns = budget(reason, "max turns reached") { return "It used all \(turns) of its turns." }
    if let calls = budget(reason, "max tool calls reached") { return "It used all \(calls) of its tool calls." }
    return reason.isEmpty ? "It reached a limit." : "It reached a limit: \(reason)"
}

/// The number in a budget reason the loop words as `<words> (<n>)`.
private func budget(_ reason: String, _ words: String) -> String? {
    let head = "\(words) ("
    guard reason.hasPrefix(head), reason.hasSuffix(")") else { return nil }
    let n = reason.dropFirst(head.count).dropLast()
    return !n.isEmpty && n.allSatisfy({ $0.isASCII && $0.isNumber }) ? String(n) : nil
}
