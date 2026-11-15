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
    ///   - words: an answer in the person's own words is offered: not on
    ///     questions of another thread than the chat's (a sub-agent's are
    ///     answered in its own), where the message would name them.
    static func question(
        _ id: String, card: ChatAppState.Card?, pending: Record? = nil, outcome: InteractionNotice? = nil, aside: Bool = false,
        words: Bool = true
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
                : state == "answered" ? "Answered" : state == "replied" ? "Answered in a message" : "Dismissed"
        // The dismissal an answer in the person's own words makes is out:
        // the card stays aside until the server answers it.
        return QuestionCard(
            title: title, loading: record == nil && outcome == nil, pending: pending, questions: questions,
            failure: pending ? failure(card, .question(id)) : nil, busy: card?.busy, answer: "Answer", dismiss: "Dismiss",
            words: words ? "Answer in Your Own Words" : nil, aside: pending && (aside || card?.busy == ChatAppState.Card.words))
    }

    /// How a question's card ended, for its call's row: who answered and
    /// when, and, once the record is read, each question with what was
    /// chosen. One answered in a message (`replied`, `agents` 7) has no
    /// choices: each question is shown with the message, where the rows
    /// read hold it, and `repliedIn` names it for the row to point at.
    ///
    /// - Parameter said: the words of the message a question was replied
    ///   in, by the message's id, as the chat's rows hold it.
    static func resolved(
        question id: String, card: ChatAppState.Card?, pending: Record? = nil, outcome: InteractionNotice?, by hand: Hand = .unsaid,
        said: (String) -> String? = { _ in nil }
    ) -> CardResolution? {
        let record = card?.record ?? (outcome == nil ? pending : nil)
        guard let state = record?["state"]?.string ?? outcome.map({ $0.answered ? "answered" : "dismissed" }), state != "pending"
        else { return nil }
        let made = question(id, card: card, pending: pending, outcome: outcome)
        let count = max(made.questions.count, outcome?.answers.count ?? 0)
        let at = record?["resolvedAt"]?.string.flatMap(Timestamp.parse)
        if state == "replied" {
            let message = record?["repliedIn"]?.referenceID
            let words = message.flatMap(said)
            let facts = made.questions.map { CardResolution.Fact(label: $0.prompt, value: words ?? "Answered in a message") }
            return CardResolution(
                card: .question(id), mark: .done,
                label: OutcomeWords.say(
                    hand, you: "You answered in your own words", other: "Someone else answered in their own words",
                    plain: "Answered in a message"),
                at: at, facts: facts, panel: nil, loading: record == nil, repliedIn: message)
        }
        let answered = state == "answered"
        let facts = made.questions.map { question in
            let chosen = question.options.filter(\.chosen).map(\.label)
            return CardResolution.Fact(label: question.prompt, value: chosen.isEmpty ? "No answer" : chosen.joined(separator: ", "))
        }
        return CardResolution(
            card: .question(id), mark: answered ? .done : .declined, label: OutcomeWords.questions(answered: answered, count: count, hand),
            at: at, facts: facts, panel: nil, loading: record == nil)
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
    static func resolved(
        suggestion id: String, made: SuggestionCard, card: ChatAppState.Card?, outcome: DecisionNotice?, by hand: Hand = .unsaid
    ) -> CardResolution? {
        guard made.decided else { return nil }
        let applied = made.panel.heading.badge == .applied
        // The request's own last write is its decision: it is frozen after.
        let at = card?.record.flatMap { $0["decision"]?.string == "proposed" ? nil : $0.updatedDate }
        return CardResolution(
            card: .suggestion(id), mark: applied ? .done : .declined,
            label: OutcomeWords.suggestion(applied: applied, adjusted: outcome?.adjusted == true, it: "it", hand),
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
    static func resolved(
        approval id: String, made: ApprovalCard, card: ChatAppState.Card?, pending: Record?, by hand: Hand = .unsaid
    ) -> CardResolution? {
        guard !made.loading, !made.pending else { return nil }
        let tool = made.tool.isEmpty ? "it" : made.tool
        let (mark, label): (CardResolution.Mark, String) =
            switch made.tone {
            case .running: (.running, OutcomeWords.approval(tool: tool, .running, hand))
            case .done: (.done, OutcomeWords.approval(tool: tool, .ran, hand))
            case .failed: (.failed, OutcomeWords.approval(tool: tool, .failed, hand))
            case .declined, .waiting: (.declined, OutcomeWords.approval(tool: tool, .declined, hand))
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

    /// The cards whose call is drawn with how they ended on it.
    static func ended(_ turns: [TurnPresentation]) -> Set<String> {
        var out: Set<String> = []
        for turn in turns {
            guard case let .assistant(reply) = turn.body else { continue }
            for tool in reply.tools where tool.resolution != nil { if let card = tool.asked { out.insert(card.id) } }
        }
        return out
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

/// How a card ended, in words, by whose hand it was. Said with "You" only
/// where the server said the card is the person's own; with "Someone else"
/// where it links another person; and with neither where nothing tells.
enum OutcomeWords {
    static func say(_ hand: Hand, you: String, other: String, plain: String) -> String {
        switch hand {
        case .you: you
        case .other: other
        case .unsaid: plain
        }
    }

    static func questions(answered: Bool, count: Int, _ hand: Hand) -> String {
        if answered { return say(hand, you: "You answered", other: "Someone else answered", plain: "Answered") }
        let what = count > 1 ? "the questions" : "the question"
        return say(hand, you: "You dismissed \(what)", other: "Someone else dismissed \(what)", plain: "Dismissed")
    }

    enum Approval { case running, ran, failed, declined }

    static func approval(tool: String, _ ended: Approval, _ hand: Hand) -> String {
        switch ended {
        case .declined: return say(hand, you: "You declined \(tool)", other: "Someone else declined \(tool)", plain: "\(tool) was declined")
        case .running, .ran, .failed:
            let then = ended == .running ? ", and it is running" : ended == .ran ? ", and it ran" : ", but it didn't work"
            return say(
                hand, you: "You approved \(tool)\(then)", other: "Someone else approved \(tool)\(then)", plain: "\(tool) was approved\(then)")
        }
    }

    /// - Parameter it: "it" on the call's row, "the suggestion" on a line
    ///   of its own.
    static func suggestion(applied: Bool, adjusted: Bool, it: String, _ hand: Hand) -> String {
        guard applied else {
            return say(
                hand, you: "You dismissed \(it)", other: "Someone else dismissed \(it)", plain: it == "it" ? "Dismissed" : "The suggestion was dismissed")
        }
        return say(
            hand, you: "You applied \(it)" + (adjusted ? ", with your changes" : ""),
            other: "Someone else applied \(it)" + (adjusted ? ", with their changes" : ""),
            plain: (it == "it" ? "Applied" : "The suggestion was applied") + (adjusted ? ", with changes" : ""))
    }

    /// The line the thread's rows hold for an outcome, said by whose hand
    /// it was: the words `TurnPresenter` gives it are the person's own.
    static func line(_ line: OutcomeLine, answer: InteractionNotice?, approval: ApprovalNotice?, decision: DecisionNotice?, _ hand: Hand) -> OutcomeLine {
        guard hand != .you else { return line }
        var out = line
        if let answer {
            out.text = questions(answered: answer.answered, count: 2, hand)
            if !answer.answered, hand == .unsaid { out.text = "The questions were dismissed" }
        } else if let approval {
            let ended: Approval = !approval.approved ? .declined : approval.ok == false ? .failed : .ran
            out.text = self.approval(tool: approval.tool, ended, hand) + (ended == .failed ? approval.error.map { ": \($0)" } ?? "" : "")
        } else if let decision {
            out.text = suggestion(applied: decision.accepted, adjusted: decision.adjusted, it: "the suggestion", hand)
        }
        return out
    }
}
