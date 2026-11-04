import SubstrateAPI
import SubstrateCore

/// The cards a conversation's turns show, from what the list holds of each:
/// the transcript first (the `propose` call, the decision the substrate
/// wrote), so a card draws at once and whatever the token may read; the
/// record, read when it can be, adds the version to act on, and a
/// suggestion's target its name and its values before.
enum CardPresenter {
    static func question(_ id: String, card: ChatAppState.Card?) -> QuestionCard {
        let record = card?.record
        let state = record?["state"]?.string ?? "pending"
        // Until the record is read the card claims no state: a resolved one
        // must not flash as waiting.
        let pending = record != nil && state == "pending"
        var answered: [String: Set<String>] = [:]
        for answer in record?["answers"]?.array ?? [] {
            if let q = answer["question"]?.string { answered[q] = Set(answer["selected"]?.array?.compactMap(\.string) ?? []) }
        }
        let questions = (record?["questions"]?.array ?? []).compactMap { q -> QuestionCard.Question? in
            guard let id = q["id"]?.text, let prompt = q["prompt"]?.text else { return nil }
            let options = (q["options"]?.array ?? []).compactMap { o -> QuestionCard.Option? in
                guard let value = o["value"]?.text else { return nil }
                return QuestionCard.Option(
                    value: value, label: o["label"]?.text ?? value, description: o["description"]?.text,
                    chosen: answered[id, default: []].contains(value))
            }
            return QuestionCard.Question(id: id, prompt: prompt, multi: q["multi"]?.bool == true, options: options)
        }
        let title =
            record == nil
            ? "Questions"
            : pending
                ? "A few questions before it goes on"
                : state == "answered" ? "You answered" : "You dismissed these"
        return QuestionCard(
            title: title, loading: record == nil, pending: pending, questions: questions,
            problem: card?.problem.map { "That didn't go through: \($0)" }, busy: card?.busy, answer: "Answer", dismiss: "Dismiss")
    }

    static func suggestion(
        _ id: String, card: ChatAppState.Card?, pending: Record?, call: ToolCall?, outcome: DecisionNotice?,
        session: ChatSession?, titles: [String: String]
    ) -> SuggestionCard {
        let held = card?.record ?? pending
        let asked = call.flatMap(proposedChange)
        let decision = held?["decision"]?.string ?? outcome.map { $0.accepted ? "accepted" : "rejected" } ?? "proposed"
        let op = held?["op"]?.string ?? outcome?.op ?? asked?.change.op.rawValue ?? "patch"
        let targetPath = held?["target"]?.referencePath ?? outcome?.target
        let kind = held?["targetKind"]?.text ?? targetPath.map { $0.split(separator: "/").dropLast().joined(separator: "/") } ?? asked?.change.kind
        let targetID =
            held?["targetId"]?.text ?? held?["target"]?.referenceID
            ?? outcome?.target?.split(separator: "/").last.map(String.init) ?? asked?.change.id
        // What the change writes: the owner's adjusted values where they
        // applied their own, else the agent's.
        var values = held?["diff"]?["properties"]?.object ?? asked?.change.values ?? [:]
        if decision == "accepted", let adjusted = held?["adjustedDiff"]?["properties"]?.object ?? outcome?.adjustedValues {
            values = adjusted
        }
        let target = card?.target
        let change = RecordChange(
            op: op, kind: kind, id: targetID, title: recordTitle(target.map { .object(JSONObject($0.properties)) }),
            values: values)
        let rationale = held?["rationale"]?.text ?? asked?.rationale
        let lines = values.keys.sorted().map { key in
            ValueLine(key: key, label: propertyLabel(key), value: values[key] ?? .null, before: op == "patch" ? target?[key] : nil)
        }
        let canDecide = session?.canDecide ?? true
        let panel: ChangePanelPresentation =
            switch decision {
            case "proposed": TurnPresenter.panel(change, titles: titles, tense: .suggests, tint: .suggested)
            case "accepted": TurnPresenter.panel(change, titles: titles, badge: .applied, note: rationale, tint: .applied)
            default: TurnPresenter.panel(change, titles: titles, tense: .suggested, badge: .dismissed, note: rationale, struck: true)
            }
        return SuggestionCard(
            decided: decision != "proposed", panel: panel, rationale: rationale, values: lines, canDecide: canDecide,
            console: canDecide ? nil : session?.console("change-requests/\(segment(id))"),
            consoleNote: canDecide ? nil : "Substrate Chat wasn't approved to apply suggestions. Decide this one in the console.",
            apply: op == "create" ? "Add It" : op == "delete" ? "Delete It" : "Apply", destructive: op == "delete", dismiss: "Dismiss",
            problem: decision == "proposed" ? card?.problem.map { "That didn't go through: \($0)" } : nil, busy: card?.busy)
    }

    /// Each suggestion's `propose` call, by the request it landed.
    static func proposals(_ turns: [Turn]) -> [String: ToolCall] {
        var out: [String: ToolCall] = [:]
        for turn in turns where turn.role == .assistant {
            for call in turn.tools {
                if let id = requestID(of: call) { out[id] = call }
            }
        }
        return out
    }
}
