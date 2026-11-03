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
            return TurnPresentation(id: turn.id, body: .user(UserTurn(text: turn.content, pending: turn.pending,
                                                                      about: turn.contextRecords ?? [], aboutLabel: "About")))
        case .assistant:
            return TurnPresentation(id: turn.id, body: .assistant(assistant(turn, agent: agent, live: live, decided: decided, titles: titles)))
        case .summary:
            return TurnPresentation(id: turn.id, body: .summary(SummaryTurn(label: "Earlier messages were summarized", content: turn.content)))
        case .system:
            if let decision = decisionNotice(turn) {
                let text = decision.accepted
                    ? (decision.adjusted ? "You applied the suggestion, with your changes" : "You applied the suggestion")
                    : "You dismissed the suggestion"
                return TurnPresentation(id: turn.id, body: .decision(OutcomeLine(text: text, done: decision.accepted),
                                                                     suggestion: decision.requestID))
            }
            if let answer = interactionNotice(turn) {
                return TurnPresentation(id: turn.id, body: .answer(OutcomeLine(text: answer.answered ? "You answered" : "You dismissed the questions",
                                                                               done: answer.answered)))
            }
            return TurnPresentation(id: turn.id, body: .system(turn.content))
        }
    }

    private static func assistant(_ turn: Turn, agent: Agent?, live: Bool, decided: Set<String>, titles: [String: String]) -> AssistantTurn {
        var reasoning: ReasoningPresentation?
        if let text = turn.reasoning, !text.isEmpty {
            let thinking = turn.thinking && live
            reasoning = ReasoningPresentation(label: thinking ? "Thinking…" : thoughtFor(turn.reasoningMillis), thinking: thinking, text: text)
        }
        let linked = live ? [] : (turn.mentions ?? []).filter { !$0.missing }.map { RecordReference(kind: $0.kind, id: $0.id) }
        return AssistantTurn(
            live: live, reasoning: reasoning, text: turn.content.isEmpty ? nil : turn.content,
            missing: (turn.mentions ?? []).filter(\.missing).map(\.path),
            typing: live && turn.content.isEmpty && turn.tools.isEmpty && turn.reasoning == nil,
            linked: linked, linkedLabel: "Linked records",
            tools: turn.tools.enumerated().map { index, call in
                tool(call, id: "\(turn.id)#\(index)", agent: agent, decided: decided, titles: titles)
            })
    }

    static func thoughtFor(_ millis: Int?) -> String {
        guard let millis, millis > 0 else { return "Thought" }
        let seconds = max(1, Int((Double(millis) / 1000).rounded()))
        return seconds < 60 ? "Thought for \(seconds) s" : "Thought for \(seconds / 60) min \(seconds % 60) s"
    }

    static func tool(_ call: ToolCall, id: String, agent: Agent?, decided: Set<String>, titles: [String: String]) -> ToolRowPresentation {
        let resolved = resolveTool(agent: agent, name: call.name, callable: call.callable)
        let details = toolDetails(call, resolved).compactMap { detail -> ToolDetailPresentation? in
            switch detail {
            case .changed, .wrote: return nil
            case let .failed(reason): return .failed(reason ?? "It failed.")
            case let .found(records, total):
                return .found(total == 0 ? "Found nothing." : "Found \(plural(total, "record")).",
                              records.map { RecordReference(kind: $0.kind, id: $0.id) })
            case let .reply(agent, text): return .reply(heading: "\(agentName(agent)) said", text: text)
            case let .output(raw): return .output(raw)
            }
        }
        var raw = [RawLine(label: "Tool", value: call.callable ?? call.name)]
        if !call.arguments.isEmpty { raw.append(RawLine(label: "Arguments", value: pretty(call.arguments))) }
        if let output = call.output { raw.append(RawLine(label: "Output", value: pretty(output))) }
        var attachment: ToolRowPresentation.Attachment?
        if let request = requestID(of: call) {
            if !decided.contains(request) { attachment = .suggestion(request) }
        } else if let interaction = interactionID(of: call) {
            attachment = .questions(interaction)
        } else if resolved.function == HostFunction.write {
            attachment = writeChange(call).map { .change(panel($0.titled(titles), titles: titles)) }
        } else if call.ok == true {
            let stamped = stampedChanges(call)
            let wrote = call.subagentWrites.flatMap {
                $0.records > 0 ? wroteSentence(records: $0.records, kinds: $0.kinds, moreKinds: $0.moreKinds) : nil
            }
            if !stamped.isEmpty || wrote != nil {
                attachment = .changes(ChangeListPresentation(headings: stamped.prefix(8).map { heading($0.titled(titles), tense: .done) },
                                                             more: stamped.count > 8 ? "and \(stamped.count - 8) more" : nil, wrote: wrote))
            }
        }
        return ToolRowPresentation(id: id, status: call.ok == nil ? .running : call.ok == true ? .done : .failed,
                                   summary: toolSummary(call, resolved), details: details, raw: raw, attachment: attachment)
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
        let symbol: ChangeHeading.Symbol = switch change.op {
        case .delete: .delete
        case .create: .create
        case .put, .patch: .edit
        }
        let verb: String = switch tense {
        case .done:
            switch change.op {
            case .create: "Added"
            case .put: "Saved"
            case .patch: "Changed"
            case .delete: "Deleted"
            }
        case .suggests, .suggested:
            (tense == .suggests ? "Suggests " : "Suggested ") + {
                switch change.op {
                case .create: "adding"
                case .put: "saving"
                case .patch: "changing"
                case .delete: "deleting"
                }
            }()
        }
        return ChangeHeading(symbol: symbol, verb: verb, record: pill, recordTitle: change.title, words: words, badge: badge,
                             badgeLabel: badge.map { $0 == .applied ? "Applied" : "Dismissed" })
    }

    /// Past this many values a panel says how many more there are.
    static let shownValues = 8

    static func panel(_ change: RecordChange, titles: [String: String], tense: Tense = .done, badge: ChangeHeading.Badge? = nil,
                      note: String? = nil, struck: Bool = false, tint: ChangePanelPresentation.Tint = .plain) -> ChangePanelPresentation
    {
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
