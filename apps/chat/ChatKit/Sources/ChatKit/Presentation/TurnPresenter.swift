import Foundation
import SubstrateCore
import SubstrateRuntime

/// Turns as a transcript draws them, in the console's words.
enum TurnPresenter {
    static func present(_ turns: [Turn], agent: Agent?, liveKey: String?, titles: [String: String]) -> [TurnPresentation] {
        // The suggestions a later turn decided: their card shows there,
        // under the decision, rather than twice.
        let decided = Set(turns.compactMap { decisionNotice($0)?.requestID })
        return turns.map { present($0, agent: agent, live: $0.id == liveKey, decided: decided, titles: titles) }
    }

    /// The instant a turn is presented at, and the instant it must be
    /// presented again where its rows were left as they were because the
    /// last parse was too recent.
    final class Pace {
        let now: Date
        /// A reply being written is revealed at a steady rate
        /// (`TextReveal`); false where motion is reduced, when each piece
        /// is drawn as it arrives.
        let reveals: Bool
        private(set) var due: Date?

        init(now: Date, reveals: Bool = false) {
            self.now = now
            self.reveals = reveals
        }

        func wait(until instant: Date?) {
            guard let instant else { return }
            due = min(due ?? instant, instant)
        }
    }

    /// - Parameters:
    ///   - kept: where a live turn's tool rows are kept, so a call's row is
    ///     made once and not at every piece of the reply after it.
    ///   - scope: the conversation the parts are named under.
    ///   - pace: where there is one, a reply being written is parsed no
    ///     more often than `Markdown.parsesPerSecond`.
    static func present(
        _ turn: Turn, agent: Agent?, live: Bool, decided: Set<String>, titles: [String: String], kept: PresenterParts? = nil,
        scope: String = "", pace: Pace? = nil
    ) -> TurnPresentation {
        switch turn.role {
        case .user:
            if let delivery = deliveryNotice(turn) {
                return TurnPresentation(id: turn.id, body: .trigger("When \(delivery.title ?? kindDisplayName(delivery.kind)) changed"))
            }
            return TurnPresentation(
                id: turn.id,
                body: .user(
                    UserTurn(
                        text: turn.content, pending: turn.pending,
                        about: turn.contextRecords ?? [], aboutLabel: "About")))
        case .assistant:
            return TurnPresentation(
                id: turn.id,
                body: .assistant(
                    assistant(turn, agent: agent, live: live, decided: decided, titles: titles, kept: kept, scope: scope, pace: pace)))
        case .summary:
            return TurnPresentation(id: turn.id, body: .summary(SummaryTurn(label: "Earlier messages were summarized", content: turn.content)))
        case .system:
            if let decision = decisionNotice(turn) {
                let text =
                    decision.accepted
                    ? (decision.adjusted ? "You applied the suggestion, with your changes" : "You applied the suggestion")
                    : "You dismissed the suggestion"
                return TurnPresentation(
                    id: turn.id,
                    body: .decision(
                        OutcomeLine(text: text, done: decision.accepted),
                        suggestion: decision.requestID))
            }
            if let answer = interactionNotice(turn) {
                return TurnPresentation(
                    id: turn.id,
                    body: .answer(
                        OutcomeLine(
                            text: answer.answered ? "You answered" : "You dismissed the questions",
                            done: answer.answered)))
            }
            if let ended = jobNotice(turn) {
                let name = jobName(ended, agent: agent)
                return TurnPresentation(
                    id: turn.id,
                    body: .jobEnded(
                        OutcomeLine(
                            text: ended.ok ? "\(name) finished in the background" : "\(name) didn't finish in the background",
                            done: ended.ok)))
            }
            if let ended = approvalNotice(turn) {
                let text =
                    !ended.approved
                    ? "You declined \(ended.tool)"
                    : ended.ok == false
                        ? "You approved \(ended.tool), but it didn't work" + (ended.error.map { ": \($0)" } ?? "")
                        : "You approved \(ended.tool), and it ran"
                return TurnPresentation(id: turn.id, body: .approvalEnded(OutcomeLine(text: text, done: ended.approved && ended.ok != false)))
            }
            return TurnPresentation(id: turn.id, body: .system(turn.content))
        }
    }

    /// What ran in the background, by the callable behind the name the
    /// model saw: a sub-agent by its name, a function by its own.
    static func jobName(_ ended: JobNotice, agent: Agent?) -> String {
        let resolved = resolveTool(agent: agent, name: ended.name, callable: ended.callable)
        if let sub = resolved.subagent { return agentName(sub) }
        let fn = resolved.function ?? ended.name
        let local = fn.split(separator: "/").last.map(String.init) ?? fn
        let words = local.splittingWords().joined(separator: " ").lowercased()
        return words.isEmpty ? "A tool" : words.capitalizedFirst
    }

    /// What a reply's rows are made from.
    struct BlockReads: Equatable, Sendable {
        var text: String
        var open: Bool
    }

    /// A reply's rows, and where each row of the transcript starts in them.
    struct Blocks: Equatable {
        var rows: [MarkdownRow]
        var pages: [Int]

        /// - Parameter memo: the lines read for the reply's code at its
        ///   last parse (`CodeMemo`).
        init(_ text: String, open: Bool, memo: CodeMemo? = nil) {
            rows = Markdown.rows(text, open: open, memo: memo)
            pages = Markdown.pages(rows)
        }
    }

    /// What a tool's row is made from.
    struct ToolReads: Equatable, Sendable {
        var call: ToolCall
        var agent: Record?
        var decided: Set<String>
        var titles: [String: String]
    }

    private static func assistant(
        _ turn: Turn, agent: Agent?, live: Bool, decided: Set<String>, titles: [String: String], kept: PresenterParts?, scope: String,
        pace: Pace?
    ) -> AssistantTurn {
        var reasoning: ReasoningPresentation?
        if let text = turn.reasoning, !text.isEmpty {
            let thinking = turn.thinking && live
            reasoning = ReasoningPresentation(
                label: thinking ? "Thinking…" : thoughtFor(turn.reasoningMillis), thinking: thinking, text: text,
                tail: thinking ? thinkingTail(text) : "")
        }
        // A reply being written is drawn up to where its reveal has got
        // to, and is still being written, to everything that draws it,
        // until all of it is drawn: a reply that ended a moment ago, or
        // whose row has just been read, goes on arriving at the same rate.
        var written = turn.content
        var live = live
        if let kept {
            let name = "\(scope)/reveal/\(turn.id)"
            let total = turn.content.unicodeScalars.count
            if let pace, pace.reveals, live || kept.state(name, as: TextReveal.self)?.waits == true {
                let (shown, due) = kept.state(name, from: TextReveal()) { reveal in
                    let due = reveal.advance(to: total, at: pace.now)
                    return (reveal.shown, due)
                }
                pace.wait(until: due)
                if shown < total {
                    written = String(String.UnicodeScalarView(turn.content.unicodeScalars.prefix(shown)))
                    live = true
                }
            } else if live {
                // Nothing is paced: what is held is drawn, and a reveal
                // that starts later starts from there.
                kept.state(name, from: TextReveal()) { $0.finish(total) }
            }
        }
        let linked = live ? [] : (turn.mentions ?? []).filter { !$0.missing }.map { RecordReference(kind: $0.kind, id: $0.id) }
        // A reply cites its pages once it is finished: a streaming one is
        // still choosing them.
        let cited = live || written.isEmpty ? [] : replyLinks(written)
        // Parsed here, on the runtime's executor, and once for a text: a
        // piece of reasoning or a tool's event leaves the reply's rows as
        // they were.
        let reads = BlockReads(text: written, open: live)
        let name = "\(scope)/blocks/\(turn.id)"
        // The lines read for the reply's code, kept beside its rows under
        // their name and forgotten with them: a reply is parsed whole at
        // each flush, and a code block's finished lines are read once.
        let memo = kept?.state(name, from: CodeMemo()) { $0 }
        let blocks: Blocks
        if let kept, let pace, live {
            // While it is written, no more often than the limit: sooner
            // than that the rows stay, and the turn is presented again when
            // the next parse may start.
            let paced = kept.part(name, reading: reads, at: pace.now, every: 1 / Markdown.parsesPerSecond) {
                Blocks(reads.text, open: reads.open, memo: memo)
            }
            pace.wait(until: paced.due)
            blocks = paced.value
        } else {
            blocks =
                kept?.part(name, reading: reads) { Blocks(reads.text, open: reads.open, memo: memo) }
                ?? Blocks(reads.text, open: reads.open)
        }
        return AssistantTurn(
            live: live, reasoning: reasoning, text: written.isEmpty ? nil : written,
            blocks: KeyedList(blocks.rows), pages: blocks.pages,
            missing: (turn.mentions ?? []).filter(\.missing).map(\.path),
            typing: live && written.isEmpty && turn.tools.isEmpty && turn.reasoning == nil,
            linked: linked, linkedLabel: "Linked records",
            sources: cited.isEmpty
                ? nil
                : SourceList(
                    heading: "Sources", label: "Sources",
                    pills: cited.enumerated().map { index, link in pill(link.url, title: link.text, index: index + 1) }, empty: nil),
            tools: turn.tools.enumerated().map { index, call in
                let id = "\(turn.id)#\(index)"
                guard let kept else { return tool(call, id: id, agent: agent, decided: decided, titles: titles) }
                return kept.part(
                    "\(scope)/tool/\(id)", reading: ToolReads(call: call, agent: agent?.record, decided: decided, titles: titles)
                ) { tool(call, id: id, agent: agent, decided: decided, titles: titles) }
            })
    }

    static func pill(_ url: String, title: String?, index: Int?, snippet: String? = nil) -> SourcePill {
        let host = webHost(url)
        return SourcePill(index: index, url: url, host: host, title: title == host ? nil : title, snippet: snippet)
    }

    /// A result of a known shape as pills: a search's hits numbered, a page
    /// read as its one pill.
    static func sources(_ result: ToolResult) -> SourceList {
        switch result {
        case let .webHits(hits):
            SourceList(
                heading: nil, label: "Results",
                pills: hits.enumerated().map { index, hit in pill(hit.url, title: hit.title, index: index + 1, snippet: hit.snippet) },
                empty: hits.isEmpty ? "No results." : nil)
        case let .webPage(page):
            SourceList(heading: nil, label: "Page", pills: [pill(page.url, title: page.title, index: nil)], empty: nil)
        }
    }

    /// The newest of a model's thinking: its last few lines' worth, from
    /// the start of a word.
    static let tailLength = 320

    static func thinkingTail(_ text: String) -> String {
        let scalars = text.unicodeScalars
        guard scalars.count > tailLength else { return text }
        var tail = Substring(scalars.suffix(tailLength))
        if let space = tail.firstIndex(where: \.isWhitespace) { tail = tail[tail.index(after: space)...] }
        return String(tail)
    }

    /// What a call runs, for a line that says it is running: a sub-agent by
    /// its name, the substrate's own functions by what they do, any other
    /// by its own words.
    static func runningWords(_ call: ToolCall, agent: Agent?) -> String {
        let resolved = resolveTool(agent: agent, name: call.name, callable: call.callable)
        if let sub = resolved.subagent { return "Asking \(agentName(sub))…" }
        switch resolved.function {
        case HostFunction.query: return "Looking through your records…"
        case HostFunction.write, HostFunction.edit: return "Writing a record…"
        case HostFunction.propose: return "Suggesting a change…"
        case HostFunction.ask: return "Asking you…"
        case HostFunction.wait: return "Waiting for work in the background…"
        case HostFunction.vocabulary: return "Declaring a kind…"
        default: break
        }
        let fn = resolved.function ?? call.name
        let local = fn.split(separator: "/").last.map(String.init) ?? fn
        let words = local.splittingWords().joined(separator: " ").lowercased()
        return words.isEmpty ? "Running a tool…" : "Running \(words)…"
    }

    static func thoughtFor(_ millis: Int?) -> String {
        guard let millis, millis > 0 else { return "Thought" }
        let seconds = max(1, Int((Double(millis) / 1000).rounded()))
        return seconds < 60 ? "Thought for \(seconds) s" : "Thought for \(seconds / 60) min \(seconds % 60) s"
    }

    static func tool(_ call: ToolCall, id: String, agent: Agent?, decided: Set<String>, titles: [String: String]) -> ToolRowPresentation {
        let resolved = resolveTool(agent: agent, name: call.name, callable: call.callable)
        // A function whose result has a shape the app knows draws it under
        // the line, so its payload is not shown again as output. The host
        // functions and sub-agents have details of their own.
        let known = resolved.function.flatMap { HostFunction.all.contains($0) ? nil : toolResult(call) }
        // A background call answers with its handle, which is not its result.
        let working = call.job == "working"
        let request = requestID(of: call)
        // A failed call that landed a request was not a failure: the owner's
        // policy held the write for review, and the suggestion under it is
        // where it waits.
        let held = call.ok == false && request != nil
        let interaction = interactionID(of: call)
        // A call that waits for the owner's approval has not run: its card
        // says what it would do, and its answer is only the handle.
        let approval = approvalID(of: call)
        // What the call changed. A suggestion, questions and an approval are
        // cards of their own, and a write's change is its panel.
        let changes =
            request == nil && interaction == nil && approval == nil && resolved.function != HostFunction.write && call.ok == true
            ? stampedChanges(call) : []
        // Several records, and the changes of a call whose line says
        // something else (a function's own words), show under the line; one
        // record the line already names keeps its link in the details.
        let host = HostFunction.all.contains(resolved.function ?? "")
        let changesInline = changes.count > 1 || (!host && resolved.subagent == nil)
        var chips: [RecordChips] = []
        var details: [ToolDetailPresentation] = []
        for detail in toolDetails(call, resolved) {
            switch detail {
            case let .failed(reason):
                if !held { details.append(.failed(reason ?? "It failed.")) }
            case let .found(records, total):
                let chipped = records.map { RecordChip(record: RecordReference(kind: $0.kind, id: $0.id), title: $0.title ?? titles["\($0.kind)/\($0.id)"]) }
                if records.count > 1 {
                    chips.append(RecordChips(lead: nil, label: "Records found", records: chipped, total: total))
                } else {
                    details.append(.found(total == 0 ? "Found nothing." : "Found \(plural(total, "record")).", chipped.map(\.record)))
                }
            case .changed:
                guard !changes.isEmpty else { break }
                if changesInline {
                    chips += changeChips(changes, titles: titles)
                } else {
                    details.append(.changed(changeChips(changes, titles: titles)))
                }
            case let .wrote(records, kinds, moreKinds):
                details.append(.wrote(wroteSentence(records: records, kinds: kinds, moreKinds: moreKinds)))
            case let .reply(agent, text):
                details.append(.reply(heading: "\(agentName(agent)) said", text: text))
            case let .output(raw):
                if known == nil, !working, approval == nil { details.append(.output(raw)) }
            }
        }
        // The line names the one record the call worked on: by the heading
        // its answer carried, else the one the chat's lookups returned;
        // failing both, the view reads it.
        let line = toolLine(call, resolved)
        let subject = toolSubject(call, resolved)
        let named = subject.flatMap { $0.title ?? titles["\($0.kind)/\($0.id)"] }
        var unnamed: UnnamedSubject?
        if let subject, named == nil, let before = line.before {
            unnamed = UnnamedSubject(record: subject.reference, before: before, after: line.after)
        }
        var raw = [RawLine(label: "Tool", value: call.callable ?? call.name)]
        if !call.arguments.isEmpty { raw.append(RawLine(label: "Arguments", value: pretty(call.arguments))) }
        if let output = call.output { raw.append(RawLine(label: "Output", value: pretty(output))) }
        let status: ToolRowPresentation.Status =
            if working {
                .working
            } else if call.ok == nil {
                .running
            } else if held || approval != nil {
                .held
            } else if call.ok == false || call.job == "failed" || call.job == "canceled" {
                .failed
            } else {
                .done
            }
        var attachment: ToolRowPresentation.Attachment?
        if let request {
            if !decided.contains(request) { attachment = .suggestion(request) }
        } else if let interaction {
            attachment = .questions(interaction)
        } else if let approval {
            attachment = .approval(approval)
        } else if resolved.function == HostFunction.write {
            attachment = writeChange(call).map { .change(panel($0.titled(titles), titles: titles)) }
        }
        return ToolRowPresentation(
            id: id, status: status,
            statusLabel: status == .working
                ? "Working in the background" : approval != nil ? "Asked for your approval" : status == .held ? "Waiting for you" : nil,
            summary: line.said(title: named), unnamed: unnamed, details: details, raw: raw, sources: known.map(sources), chips: chips,
            thread: childThread(call, resolved).map { ThreadLink(thread: $0, label: "Open its conversation") },
            asked: request.map { .suggestion($0) } ?? interaction.map { .question($0) } ?? approval.map { .approval($0) },
            attachment: attachment)
    }

    /// A call's changes as pills, one run per verb in the order they
    /// happened: "Saved [a] [b]", then "Deleted [c]".
    static func changeChips(_ changes: [ChangeStamp], titles: [String: String]) -> [RecordChips] {
        var runs: [(op: String, changes: [ChangeStamp])] = []
        for change in changes {
            if let last = runs.indices.last, runs[last].op == change.op {
                runs[last].changes.append(change)
            } else {
                runs.append((change.op, [change]))
            }
        }
        return runs.map { run in
            let verb = changeVerbs[run.op] ?? "Wrote"
            return RecordChips(
                lead: verb, label: "\(verb) records",
                records: run.changes.map { RecordChip(record: RecordReference(kind: $0.kind, id: $0.id), title: titles["\($0.kind)/\($0.id)"]) },
                total: run.changes.count)
        }
    }

    private static func pretty(_ text: String) -> String {
        guard let value = JSONValue.parse(text), case .object = value else { return text }
        return String(decoding: value.data(pretty: true), as: UTF8.self)
    }

    // MARK: Changes

    enum Tense { case done, suggests, suggested }

    static func heading(_ change: RecordChange, tense: Tense, badge: ChangeHeading.Badge? = nil) -> ChangeHeading {
        let kind = change.kind.map { kindDisplayName($0).lowercasedFirst } ?? "record"
        let words = change.title.map { "“\($0)”" } ?? (change.op == .create ? "a new \(kind)" : "a \(kind)")
        // Nothing to open before a suggested record exists, nor after a
        // delete: those say the kind instead.
        let pill = change.op != .delete && !(change.op == .create && tense != .done) ? change.reference : nil
        let symbol: ChangeHeading.Symbol =
            switch change.op {
            case .delete: .delete
            case .create: .create
            case .put, .patch: .edit
            }
        let verb: String =
            switch tense {
            case .done:
                switch change.op {
                case .create: "Added"
                case .put: "Saved"
                case .patch: "Changed"
                case .delete: "Deleted"
                }
            case .suggests, .suggested:
                (tense == .suggests ? "Suggests " : "Suggested ")
                    + {
                        switch change.op {
                        case .create: "adding"
                        case .put: "saving"
                        case .patch: "changing"
                        case .delete: "deleting"
                        }
                    }()
            }
        return ChangeHeading(
            symbol: symbol, verb: verb, record: pill, recordTitle: change.title, words: words, badge: badge,
            badgeLabel: badge.map { $0 == .applied ? "Applied" : "Dismissed" })
    }

    /// Past this many values a panel says how many more there are.
    static let shownValues = 8

    static func panel(
        _ change: RecordChange, titles: [String: String], tense: Tense = .done, badge: ChangeHeading.Badge? = nil,
        note: String? = nil, struck: Bool = false, tint: ChangePanelPresentation.Tint = .plain
    ) -> ChangePanelPresentation {
        let change = change.titled(titles)
        // The heading already names the record.
        let keys = change.values.keys.sorted().filter { key in
            !(["name", "title", "summary", "subject"].contains(key) && change.values[key]?.text == change.title && change.title != nil)
        }
        return ChangePanelPresentation(
            heading: heading(change, tense: tense, badge: badge), note: note,
            values: keys.prefix(shownValues).map { ValueLine(key: $0, label: propertyLabel($0), value: change.values[$0] ?? .null) },
            more: keys.count > shownValues ? "and \(keys.count - shownValues) more" : nil, struck: struck, tint: tint)
    }
}

extension RecordChange {
    /// This change with a title from the chat where it has none of its own.
    func titled(_ titles: [String: String]) -> RecordChange {
        guard title == nil, let kind, let id, let known = titles["\(kind)/\(id)"] else { return self }
        var out = self
        out.title = known
        return out
    }
}
