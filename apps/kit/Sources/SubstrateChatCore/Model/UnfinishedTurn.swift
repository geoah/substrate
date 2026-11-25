import Foundation

/// How a chat turn that ended before it finished is marked in the list
/// (the console's `unfinishedTurn`, web/console/src/lib/agent-runs.ts). What
/// the chat itself says of it is its failure card (`FailurePresenter`).
public enum UnfinishedTurn {
    public enum Tone: String, Codable, Hashable, Sendable {
        /// A stop somebody asked for.
        case quiet
        /// A turn something else cut short: a restart, a limit.
        case warn
        /// A failure.
        case bad
    }
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
