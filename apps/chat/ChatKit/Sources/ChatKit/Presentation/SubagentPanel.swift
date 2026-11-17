import Foundation
import SubstrateCore

/// A call to another assistant, as a panel of its own: who was asked, what,
/// how long it has run or took, what it answered or why it stopped, and what
/// it changed. Opened, the whole of each, its conversation, and the raw call.
public struct SubagentPanel: Codable, Hashable, Sendable {
    public enum State: String, Codable, Hashable, Sendable {
        /// The call holds its caller's turn while the helper works.
        case running
        /// It answered with a handle; its job runs on in the background.
        case working
        case finished
        /// It ran and stopped before it finished: a limit, a stop, an error.
        case stopped
        /// The call itself was refused or failed before the helper answered.
        case failed
    }

    /// The helper: "Architect".
    public var name: String
    public var state: State
    /// What the state is, in words: "Working", "Finished", "Didn't finish".
    public var stateWords: String
    public var clock: SubagentClock
    /// What it was asked, cut to a sentence or two: the line shown closed.
    public var asked: String
    /// The whole request, shown open.
    public var request: String
    /// "Asked", or "Followed up" where the call continued its conversation.
    public var askedLabel: String
    /// How it went, cut to a sentence or two: its answer's first paragraph
    /// in plain words, or why it did not finish. Nil while it works.
    public var outcome: String?
    /// "Answered", "Stopped", "Failed".
    public var outcomeLabel: String?
    /// The whole answer, Markdown, shown open.
    public var reply: String?
    /// Why it did not finish, whole: the server's sentence where it says
    /// one, else one made from the cause.
    public var stopped: String?
    /// The call it failed on, and the error that call answered.
    public var failedCall: FailedCall?
    /// What its conversation wrote: "Wrote 17 records in Places and
    /// Brands", "Changed nothing". Nil until the call says.
    public var changed: String?
    /// The kinds it wrote, as pills, opened.
    public var kinds: [String]
    /// "and 2 more kinds", past the pills.
    public var moreKinds: String?
    public var thread: ThreadLink?
    /// The tool, its arguments and its output, under a quiet "Raw call".
    public var raw: [RawLine]
    public var rawLabel: String

    /// The state and the time beside the name: "Working · 1:23",
    /// "Finished · 2 min 10 s". `now` is read only while it runs.
    public func status(at now: Date) -> String {
        guard let time = clock.words(at: now) else { return stateWords }
        return "\(stateWords) · \(time)"
    }

    /// The same, for a screen reader, whose counting a clock would not suit:
    /// "Working, for 1 min 23 s". The name beside it is read on its own.
    public func spoken(at now: Date) -> String {
        guard let time = clock.spoken(at: now) else { return stateWords }
        return "\(stateWords), \(time)"
    }
}

/// The call that ended a helper's run, as it named it.
public struct FailedCall: Codable, Hashable, Sendable {
    /// "Its last call, vocabulary, failed"
    public var label: String
    public var error: String
}

/// How long a helper has run or took, by the server's clock.
public struct SubagentClock: Codable, Hashable, Sendable {
    /// While it runs: when it started. The view counts from here, once a
    /// second, and only while something runs.
    public var since: Date?
    /// Once it ended: how long it took. Nil where nothing said when it
    /// started or ended.
    public var took: TimeInterval?

    public var ticks: Bool { since != nil }

    /// "1:23" while it runs, "2 min 10 s" once it ended.
    func words(at now: Date) -> String? {
        if let since { return Self.elapsed(max(0, now.timeIntervalSince(since))) }
        return took.map(Self.duration)
    }

    func spoken(at now: Date) -> String? {
        if let since { return "for \(Self.duration(max(0, now.timeIntervalSince(since))))" }
        return took.map { "took \(Self.duration($0))" }
    }

    /// A running clock: "0:07", "1:23", "1:02:03".
    public static func elapsed(_ seconds: TimeInterval) -> String {
        let whole = Int(seconds)
        let (h, m, s) = (whole / 3600, whole / 60 % 60, whole % 60)
        let ss = s < 10 ? "0\(s)" : "\(s)"
        if h > 0 { return "\(h):\(m < 10 ? "0\(m)" : "\(m)"):\(ss)" }
        return "\(m):\(ss)"
    }

    /// A length of time in words: "less than a second", "45 s", "2 min
    /// 10 s", "1 h 5 min".
    public static func duration(_ seconds: TimeInterval) -> String {
        let whole = Int(seconds.rounded())
        if whole < 1 { return "less than a second" }
        if whole < 60 { return "\(whole) s" }
        if whole < 3600 { return whole % 60 == 0 ? "\(whole / 60) min" : "\(whole / 60) min \(whole % 60) s" }
        let minutes = whole / 60 % 60
        return minutes == 0 ? "\(whole / 3600) h" : "\(whole / 3600) h \(minutes) min"
    }
}

extension TurnPresenter {
    /// The panel of a call to a sub-agent; nil for any other call, and for a
    /// call that waits on the owner, which its card says.
    static func subagentPanel(_ call: ToolCall, _ resolved: ResolvedTool, status: ToolRowPresentation.Status, raw: [RawLine]) -> SubagentPanel? {
        guard let sub = resolved.subagent, status != .held, status != .declined else { return nil }
        let name = agentName(sub)
        let args = JSONValue.parse(call.arguments)
        let request = (args?["input"]?.text ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        let payload = JSONValue.parse(call.output)
        let answer = (payload?["reply"]?.text ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        let background = call.job != nil
        let ended = call.job.map { $0 != "working" } ?? (call.ok != nil)

        let state: SubagentPanel.State
        if call.ok == nil {
            state = .running
        } else if call.job == "working" {
            state = .working
        } else if call.ok == false && payload?["error"] != nil && payload?["reply"] == nil {
            state = .failed
        } else if call.ok == false || call.job == "failed" || call.job == "canceled" {
            state = .stopped
        } else if let run = payload?["status"]?.text, run != "ok" {
            state = .stopped
        } else {
            state = .finished
        }

        // The job's own times for a call that ran in the background, the
        // call's for one that held its caller's turn. The handle's end
        // stands in for the job's start where only the stream has spoken.
        let start = background ? (call.jobStartedAt ?? call.finishedAt ?? call.startedAt) : call.startedAt
        let end = background ? call.jobFinishedAt : call.finishedAt
        var clock = SubagentClock()
        if state == .running || state == .working {
            clock.since = start
        } else if ended, let start, let end, end >= start {
            clock.took = end.timeIntervalSince(start)
        }

        var outcome: String?
        var outcomeLabel: String?
        var stopped: String?
        var failedCall: FailedCall?
        switch state {
        case .running, .working:
            break
        case .finished:
            outcomeLabel = "Answered"
            outcome = answer.isEmpty ? "It answered with nothing." : summary(of: answer)
        case .failed:
            outcomeLabel = "Failed"
            stopped = toolFailure(call.output) ?? "The call failed."
            outcome = stopped
        case .stopped:
            outcomeLabel = "Stopped"
            stopped = stoppedSentence(payload, name: name, job: call.job)
            outcome = stopped
            if let failed = payload?["failedCall"], let tool = failed["tool"]?.text {
                failedCall = FailedCall(label: "Its last call, \(tool), failed", error: failed["error"]?.text ?? "")
            }
        }

        var changed: String?
        var kinds: [String] = []
        var moreKinds: String?
        if let wrote = call.subagentWrites, ended {
            changed = changedSentence(records: wrote.records, kinds: wrote.kinds, moreKinds: wrote.moreKinds)
            kinds = wrote.records > 0 ? wrote.kinds : []
            if wrote.records > 0, wrote.moreKinds > 0 { moreKinds = "and \(plural(wrote.moreKinds, "more kind", "more kinds"))" }
        }

        let thread = childThread(call, resolved) ?? payload?["thread"]?.text
        let followsUp = args?["thread"]?.text?.isEmpty == false
        return SubagentPanel(
            name: name, state: state,
            stateWords: stateWords(state),
            clock: clock,
            asked: request.isEmpty ? "Nothing was asked in words." : summary(of: request, markdown: false),
            request: request,
            askedLabel: followsUp ? "Followed up" : "Asked",
            outcome: outcome, outcomeLabel: outcomeLabel,
            reply: answer.isEmpty ? nil : answer,
            stopped: stopped, failedCall: failedCall,
            changed: changed, kinds: kinds, moreKinds: moreKinds,
            thread: thread.map { ThreadLink(thread: $0, label: "Open its conversation") },
            raw: raw, rawLabel: "Raw call")
    }

    static func stateWords(_ state: SubagentPanel.State) -> String {
        switch state {
        case .running: "Working"
        case .working: "Working in the background"
        case .finished: "Finished"
        case .stopped: "Didn't finish"
        case .failed: "Failed"
        }
    }

    /// Why a helper's run did not finish: the server's own sentence where it
    /// wrote one, else one from the cause or the status an older server
    /// gave, with its reason.
    static func stoppedSentence(_ payload: JSONValue?, name: String, job: String?) -> String {
        if let stopped = payload?["stopped"]?.text?.trimmingCharacters(in: .whitespacesAndNewlines), !stopped.isEmpty { return stopped }
        let reason = payload?["reason"]?.text.flatMap(reasonLine)
        switch payload?["cause"]?.text {
        case "deadline": return "\(name) ran out of time."
        case "turn_limit": return "\(name) used all the turns it may take."
        case "tool_call_limit": return "\(name) made as many calls as it may."
        case "requested": return "\(name) was stopped."
        case "shutdown", "interrupted": return "\(name) was cut short when the server stopped."
        case "allowance_spent": return "\(name) stopped: today's allowance for the assistants is spent."
        default: break
        }
        if job == "canceled" { return "\(name)'s work in the background was canceled." }
        switch payload?["status"]?.text {
        case "overbudget": return "\(name) didn't finish. " + limitWords(reason ?? "")
        case "stopped": return "\(name) was stopped."
        default:
            if let reason { return "\(name) didn't finish: \(reason)" }
            if let error = toolFailure(payload.map { $0.jsonText }) { return "\(name) didn't finish: \(error)" }
            return "\(name) didn't finish."
        }
    }

    /// What a helper's conversation wrote, in one line.
    static func changedSentence(records: Int, kinds: [String], moreKinds: Int) -> String {
        guard records > 0 else { return "Changed nothing" }
        let head = "Wrote \(plural(records, "record"))"
        guard !kinds.isEmpty else { return head }
        var names = kinds.map(kindDisplayPlural)
        if moreKinds > 0 { names.append(plural(moreKinds, "other kind", "other kinds")) }
        return "\(head) in \(joinWords(names))"
    }

    /// The longest a closed panel's line of what was asked or answered runs.
    static let panelLine = 180

    /// A text cut to its first sentence or two, for a closed panel: its
    /// first paragraph (a reply's, read as Markdown and said in plain
    /// words, its headings passed over), then whole sentences while they
    /// fit, else cut at a word and marked.
    static func summary(of text: String, markdown: Bool = true) -> String {
        let paragraphs = text.components(separatedBy: "\n\n").map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty }
        let lead =
            markdown
            ? paragraphs.first { paragraph in
                !paragraph.hasPrefix("#") && !paragraph.hasPrefix("```") && !paragraph.hasPrefix("~~~") && !paragraph.hasPrefix("|")
                    && !paragraph.allSatisfy { "-*_ ".contains($0) }
            } ?? paragraphs.first ?? text
            : text
        let flat = markdown ? plainText(markdown: lead) : lead.split(whereSeparator: \.isWhitespace).joined(separator: " ")
        guard flat.count > panelLine else { return flat }
        // Whole sentences while they fit.
        var kept = ""
        var rest = Substring(flat)
        // A sentence ends where the next starts with a capital or a digit,
        // so "e.g. a trips package" does not end one.
        while let end = rest.firstRange(of: /[.!?]["”')]?(?=\s+["“'(]?[A-Z0-9]|\s*$)/) {
            let sentence = rest[..<end.upperBound]
            guard kept.count + sentence.count <= panelLine else { break }
            kept += sentence
            rest = rest[end.upperBound...]
        }
        // Sentences that fill the line end it; a short one (a preamble,
        // "Read only, send and draft nothing.") says too little alone, and
        // the next is cut at a word after it.
        if kept.count >= panelLine / 2 { return kept.trimmingCharacters(in: .whitespaces) }
        var cut = flat.prefix(panelLine)
        if let space = cut.lastIndex(of: " "), cut.distance(from: cut.startIndex, to: space) > panelLine / 2 { cut = cut[..<space] }
        return cut.trimmingCharacters(in: .whitespaces.union(.punctuationCharacters)) + "…"
    }
}
