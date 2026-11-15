import Foundation
import SubstrateAPI
import SubstrateCore

/// The cards a conversation's turns show, from what the list holds of each:
/// the transcript first (the `propose` call, the decision the substrate
/// wrote), so a card draws at once and whatever the token may read; the
/// record, read when it can be, adds the version to act on, and a
/// suggestion's target its name and its values before.
enum CardPresenter {
    /// A card's failure, drawn in the card: the one component every failure
    /// is drawn by, with the button that tries its write, or its read,
    /// again where trying again can help.
    static func failure(_ card: ChatAppState.Card?, _ reference: CardReference) -> FailurePresentation? {
        guard let problem = card?.problem else { return nil }
        var context = FailurePresenter.Context()
        context.card = reference
        context.busy = card?.busy != nil
        return FailurePresenter.present(problem, context)
    }

    /// - Parameters:
    ///   - outcome: what the thread's own rows say became of the questions,
    ///     where they say: a resolved card is then never drawn as loading.
    ///   - aside: the person is answering them in their own words.
    ///   - pending: the record as the list of what waits holds it, where
    ///     the card's own read has not answered yet.
    static func question(
        _ id: String, card: ChatAppState.Card?, pending: Record? = nil, outcome: InteractionNotice? = nil, aside: Bool = false
    ) -> QuestionCard {
        // The thread's own rows say it ended: a list read before that is
        // older than they are.
        let record = card?.record ?? (outcome == nil ? pending : nil)
        let state = record?["state"]?.string ?? outcome.map { $0.answered ? "answered" : "dismissed" } ?? "pending"
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
        // The dismissal an answer in the person's own words makes is out:
        // the card stays aside until the server answers it.
        return QuestionCard(
            title: title, loading: record == nil && outcome == nil, pending: pending, questions: questions,
            failure: pending ? failure(card, .question(id)) : nil, busy: card?.busy, answer: "Answer", dismiss: "Dismiss",
            words: "Answer in Your Own Words", aside: pending && (aside || card?.busy == ChatAppState.Card.words))
    }

    /// How a question's card ended, for its call's row: who answered and
    /// when, and, once the record is read, each question with what was
    /// chosen.
    static func resolved(question id: String, card: ChatAppState.Card?, pending: Record? = nil, outcome: InteractionNotice?) -> CardResolution? {
        let record = card?.record ?? (outcome == nil ? pending : nil)
        guard let state = record?["state"]?.string ?? outcome.map({ $0.answered ? "answered" : "dismissed" }), state != "pending"
        else { return nil }
        let answered = state == "answered"
        let made = question(id, card: card, pending: pending, outcome: outcome)
        let facts = made.questions.map { question in
            let chosen = question.options.filter(\.chosen).map(\.label)
            return CardResolution.Fact(label: question.prompt, value: chosen.isEmpty ? "No answer" : chosen.joined(separator: ", "))
        }
        let count = max(made.questions.count, outcome?.answers.count ?? 0)
        return CardResolution(
            card: .question(id), mark: answered ? .done : .declined,
            label: answered ? "You answered" : count > 1 ? "You dismissed the questions" : "You dismissed the question",
            at: record?["resolvedAt"]?.string.flatMap(Timestamp.parse), facts: facts, panel: nil, loading: record == nil)
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
            failure: decision == "proposed" ? failure(card, .suggestion(id)) : nil, busy: card?.busy)
    }

    /// How a suggestion's card ended, for its call's row: the change as it
    /// was applied or not taken.
    static func resolved(suggestion id: String, made: SuggestionCard, card: ChatAppState.Card?, outcome: DecisionNotice?) -> CardResolution? {
        guard made.decided else { return nil }
        let applied = made.panel.heading.badge == .applied
        // The request's own last write is its decision: it is frozen after.
        let at = card?.record.flatMap { $0["decision"]?.string == "proposed" ? nil : $0.updatedDate }
        return CardResolution(
            card: .suggestion(id), mark: applied ? .done : .declined,
            label: applied ? (outcome?.adjusted == true ? "You applied it, with your changes" : "You applied it") : "You dismissed it",
            at: at, facts: [], panel: made.panel, loading: false)
    }

    static func approval(_ id: String, card: ChatAppState.Card?, pending: Record?, outcome: ApprovalNotice?) -> ApprovalCard {
        // The card's own read is the newer: the list holds only the ones
        // still waiting.
        let record = card?.record ?? pending
        let state = record?["state"]?.string ?? outcome.map { $0.approved ? ($0.ok == false ? "failed" : "done") : "denied" }
        let tone: ApprovalCard.Tone =
            switch state {
            case "approved", "running": .running
            case "done": .done
            case "failed": .failed
            case "denied": .declined
            default: .waiting
            }
        let title =
            switch state {
            case nil: "Approval"
            case "approved": "Approved, starting"
            case "running": "Approved, running"
            case "done": "Approved and done"
            case "failed": "Approved, but it didn't work"
            case "denied": "Declined"
            default: "Waiting for your approval"
            }
        let agent = referent(record?["agent"], pin: Kinds.agent).map(agentName) ?? "The agent"
        let arguments = (record?["arguments"]?.object ?? [:]).filter { $0.value != .null && $0.value != .string("") }
            .sorted { $0.key < $1.key }
            .map { ApprovalCard.Argument(name: $0.key, label: propertyLabel($0.key), value: valueWords($0.value)) }
        let pending = state == "pending"
        return ApprovalCard(
            title: title, loading: record == nil && outcome == nil, pending: pending, tone: tone,
            asks: pending ? "\(agent) wants to run" : "\(agent) asked to run",
            tool: record?["tool"]?.text ?? outcome?.tool ?? "", arguments: arguments,
            error: state == "failed" ? record?["error"]?.text ?? outcome?.error : nil,
            failure: pending ? failure(card, .approval(id)) : nil, busy: card?.busy, approve: "Approve", decline: "Decline")
    }

    /// How an approval's card ended, for its call's row: the decision, how
    /// an approved call went, and exactly what it was asked to run with.
    static func resolved(approval id: String, made: ApprovalCard, card: ChatAppState.Card?, pending: Record?) -> CardResolution? {
        guard !made.loading, !made.pending else { return nil }
        let tool = made.tool.isEmpty ? "it" : made.tool
        let (mark, label): (CardResolution.Mark, String) =
            switch made.tone {
            case .running: (.running, "You approved \(tool), and it is running")
            case .done: (.done, "You approved \(tool), and it ran")
            case .failed: (.failed, "You approved \(tool), but it didn't work")
            case .declined, .waiting: (.declined, "You declined \(tool)")
            }
        var facts = made.arguments.map { CardResolution.Fact(label: $0.label, value: $0.value) }
        if let error = made.error { facts.append(CardResolution.Fact(label: "What went wrong", value: error)) }
        let record = card?.record ?? pending
        return CardResolution(
            card: .approval(id), mark: mark, label: label, at: record?["decidedAt"]?.string.flatMap(Timestamp.parse), facts: facts,
            panel: nil, loading: record == nil)
    }

    // MARK: The fold

    /// The turns with each card's state on the call that asked. A card
    /// that waits on the person stands under its call, which says so; one
    /// that ended is the call's own mark, with no card under it; questions
    /// being answered in the person's own words are aside, and their call
    /// says that. A turn none of whose calls landed a card is as it was.
    static func fold(_ turns: [TurnPresentation], cards: [String: CardPresentation]) -> [TurnPresentation] {
        turns.map { turn in
            guard case var .assistant(reply) = turn.body, reply.tools.contains(where: { $0.asked != nil }) else { return turn }
            reply.tools = reply.tools.map { fold($0, cards: cards) }
            return TurnPresentation(id: turn.id, body: .assistant(reply))
        }
    }

    static func fold(_ tool: ToolRowPresentation, cards: [String: CardPresentation]) -> ToolRowPresentation {
        guard let reference = tool.asked, let card = cards[reference.id] else { return tool }
        var tool = tool
        if let resolution = card.resolution {
            tool.attachment = nil
            tool.resolution = resolution
            tool.statusLabel = resolution.label
            tool.status =
                switch resolution.mark {
                case .done: .done
                case .declined: .declined
                case .failed: .failed
                case .running: .running
                }
        } else if card.question?.aside == true {
            tool.attachment = nil
            tool.status = .held
            tool.statusLabel = "You are answering in your own words"
        } else if card.waits {
            tool.status = .held
            tool.statusLabel =
                switch reference {
                case .question: "Waiting for your answer"
                case .approval: "Waiting for your approval"
                case .suggestion: "Waiting for you"
                }
        }
        return tool
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
