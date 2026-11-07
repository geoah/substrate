import Foundation
import SubstrateCore

/// Turns as a transcript draws them, in the console's words.
enum TurnPresenter {
    static func present(_ turns: [Turn], agent: Agent?, liveKey: String?, titles: [String: String]) -> [TurnPresentation] {
        // The suggestions a later turn decided: their card shows there,
        // under the decision, rather than twice.
        let decided = Set(turns.compactMap { decisionNotice($0)?.requestID })
        return turns.map { present($0, agent: agent, live: $0.id == liveKey, decided: decided, titles: titles) }
    }

    static func present(_ turn: Turn, agent: Agent?, live: Bool, decided: Set<String>, titles: [String: String]) -> TurnPresentation {
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
            return TurnPresentation(id: turn.id, body: .assistant(assistant(turn, agent: agent, live: live, decided: decided, titles: titles)))
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

    private static func assistant(_ turn: Turn, agent: Agent?, live: Bool, decided: Set<String>, titles: [String: String]) -> AssistantTurn {
        var reasoning: ReasoningPresentation?
        if let text = turn.reasoning, !text.isEmpty {
            let thinking = turn.thinking && live
            reasoning = ReasoningPresentation(label: thinking ? "Thinking…" : thoughtFor(turn.reasoningMillis), thinking: thinking, text: text)
        }
        let linked = live ? [] : (turn.mentions ?? []).filter { !$0.missing }.map { RecordReference(kind: $0.kind, id: $0.id) }
        // A reply cites its pages once it is finished: a streaming one is
        // still choosing them.
        let cited = live || turn.content.isEmpty ? [] : replyLinks(turn.content)
        return AssistantTurn(
            live: live, reasoning: reasoning, text: turn.content.isEmpty ? nil : turn.content,
            missing: (turn.mentions ?? []).filter(\.missing).map(\.path),
            typing: live && turn.content.isEmpty && turn.tools.isEmpty && turn.reasoning == nil,
            linked: linked, linkedLabel: "Linked records",
            sources: cited.isEmpty
                ? nil
                : SourceList(
                    heading: "Sources", label: "Sources",
                    pills: cited.enumerated().map { index, link in pill(link.url, title: link.text, index: index + 1) }, empty: nil),
            tools: turn.tools.enumerated().map { index, call in
                tool(call, id: "\(turn.id)#\(index)", agent: agent, decided: decided, titles: titles)
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
        // What the call changed. A suggestion and questions are cards of
        // their own, and a write's change is its panel.
        let changes = request == nil && interaction == nil && resolved.function != HostFunction.write && call.ok == true ? stampedChanges(call) : []
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
                if known == nil, !working { details.append(.output(raw)) }
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
            } else if held {
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
        } else if resolved.function == HostFunction.write {
            attachment = writeChange(call).map { .change(panel($0.titled(titles), titles: titles)) }
        }
        return ToolRowPresentation(
            id: id, status: status,
            statusLabel: status == .working ? "Working in the background" : status == .held ? "Waiting for you" : nil,
            summary: line.said(title: named), unnamed: unnamed, details: details, raw: raw, sources: known.map(sources), chips: chips,
            thread: childThread(call, resolved).map { ThreadLink(thread: $0, label: "Open its conversation") }, attachment: attachment)
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
