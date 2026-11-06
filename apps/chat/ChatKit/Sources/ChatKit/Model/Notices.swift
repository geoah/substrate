import Foundation
import SubstrateCore

/// A system turn's proposal decision (internal/engine/agentdecision.go).
struct DecisionNotice: Hashable, Sendable {
    var requestID: String
    var accepted: Bool
    var op: String
    var target: String?
    /// The values the owner applied in place of the agent's, when they
    /// changed them.
    var adjustedValues: [String: JSONValue]?

    var adjusted: Bool { adjustedValues != nil }
}

/// A system turn's interaction resolution.
struct InteractionNotice: Hashable, Sendable {
    struct Answer: Hashable, Sendable {
        var question: String
        var selected: [String]
    }

    var interactionID: String
    var answered: Bool
    var answers: [Answer]
}

/// A triggered thread's opening message: the delivery envelope a trigger
/// wrote as its first user turn.
struct DeliveryNotice: Hashable, Sendable {
    var op: String
    var kind: String
    var id: String
    var title: String?
}

private func envelope(_ turn: Turn, role: Turn.Role) -> [String: JSONValue]? {
    guard turn.role == role, turn.content.trimmingCharacters(in: .whitespaces).hasPrefix("{") else { return nil }
    return JSONValue.parse(turn.content)?.object
}

func decisionNotice(_ turn: Turn) -> DecisionNotice? {
    guard let env = envelope(turn, role: .system), env["event"]?.string == "proposalDecision" else { return nil }
    let decision = env["decision"]?.string
    guard decision == "accepted" || decision == "rejected",
        let request = env["request"]?.string, let id = request.split(separator: "/").last.map(String.init)
    else { return nil }
    return DecisionNotice(
        requestID: id, accepted: decision == "accepted", op: env["op"]?.text ?? "patch",
        target: env["target"]?.text, adjustedValues: env["adjustedDiff"]?["properties"]?.object)
}

func interactionNotice(_ turn: Turn) -> InteractionNotice? {
    guard let env = envelope(turn, role: .system) else { return nil }
    let event = env["event"]?.string
    guard event == "interactionAnswered" || event == "interactionDismissed",
        let path = env["interaction"]?.string, let id = path.split(separator: "/").last.map(String.init)
    else { return nil }
    let answers = (env["answers"]?.array ?? []).compactMap { item -> InteractionNotice.Answer? in
        guard let q = item["question"]?.text else { return nil }
        return InteractionNotice.Answer(question: q, selected: item["selected"]?.array?.compactMap(\.string) ?? [])
    }
    return InteractionNotice(interactionID: id, answered: event == "interactionAnswered", answers: answers)
}

func deliveryNotice(_ turn: Turn) -> DeliveryNotice? {
    guard let env = envelope(turn, role: .user), let change = env["change"]?.object,
        let record = env["record"]?.object,
        let op = change["op"]?.text, change["kind"]?.text != nil, change["id"]?.text != nil,
        let kind = record["kind"]?.text, let id = record["id"]?.text
    else { return nil }
    let props = record["properties"]?.object ?? [:]
    return DeliveryNotice(op: op, kind: kind, id: id, title: props["title"]?.text ?? props["name"]?.text)
}

// MARK: Cards a call landed

/// A minted record id: a UUIDv7 as 26 lowercase Crockford base32 characters
/// (decision 0228), or the twelve lowercase base32 characters ids were minted
/// as before it.
func isMintedID(_ s: String) -> Bool {
    if s.count == 26, let first = s.first, ("0" ... "7").contains(first) {
        let crockford = Set("0123456789abcdefghjkmnpqrstvwxyz")
        return s.allSatisfy { crockford.contains($0) }
    }
    return s.count == 12 && s.allSatisfy { ("a" ... "z").contains($0) || ("2" ... "7").contains($0) }
}

/// The change request a settled call landed: from the engine's stamp where
/// the row carries one, else (a live card, an older row) from the payload,
/// checked strictly, because the NAME is an alias and not provenance.
func requestID(of call: ToolCall) -> String? {
    if let stamped = call.changes?.first(where: { $0.kind == Kinds.request && $0.op == "put" }) { return stamped.id }
    guard call.ok == true else { return nil }
    if let callable = call.callable {
        guard callable == "function:substrate.reamde.dev:core:propose" else { return nil }
    } else if call.name != "propose" {
        return nil
    }
    guard let object = JSONValue.parse(call.output)?.object, object.count == 1,
        let id = object["id"]?.string, isMintedID(id)
    else { return nil }
    return id
}

/// The interaction a settled `ask` landed, from the engine's stamp.
func interactionID(of call: ToolCall) -> String? {
    call.changes?.first {
        ($0.kind == Kinds.interaction || $0.kind == Kinds.interactionBeforeMove) && $0.op == "put"
    }?.id
}
