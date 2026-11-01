import Foundation

/// One record a call or a decision wrote, as its panel shows it: what
/// happened, to what, and the values written. Read from the transcript alone,
/// so it shows whatever the token may read and without a request.
struct RecordChange: Hashable, Sendable {
    enum Op: String, Hashable, Sendable { case create, put, patch, delete }

    var op: Op
    var kind: String?
    var id: String?
    /// The record's own heading, where the call's answer carried it.
    var title: String?
    /// What was written: the properties the call named, never the whole record.
    var values: [String: JSONValue] = [:]

    init(op: Op, kind: String?, id: String?, title: String? = nil, values: [String: JSONValue] = [:]) {
        self.op = op
        self.kind = kind
        self.id = id
        self.title = title
        self.values = values
    }

    init(op raw: String?, kind: String?, id: String?, title: String? = nil, values: [String: JSONValue] = [:]) {
        let op: Op = switch raw {
        case "create": .create
        case "put": .put
        case "delete": .delete
        default: .patch
        }
        self.init(op: op, kind: kind, id: id, title: title, values: values)
    }
}

/// A record's heading from its properties: the derived `title`, else the
/// property kinds most often title themselves from.
func recordTitle(_ properties: JSONValue?) -> String? {
    properties?["title"]?.text ?? properties?["name"]?.text ?? properties?["summary"]?.text ?? properties?["subject"]?.text
}

/// What a settled `write` wrote: the op and values it was asked for, and the
/// record it answered with.
func writeChange(_ call: ToolCall) -> RecordChange? {
    guard call.ok == true, let args = JSONValue.parse(call.arguments)?.object else { return nil }
    var input = args["input"]
    // The input may arrive as an encoded object.
    if let s = input?.string { input = JSONValue.parse(s) }
    let record = JSONValue.parse(call.output)?["record"]
    return RecordChange(op: args["op"]?.string, kind: record?["kind"]?.text ?? args["kind"]?.text,
                        id: record?["id"]?.text ?? args["id"]?.text,
                        title: recordTitle(record?["properties"]),
                        values: input?["properties"]?.object ?? [:])
}

/// What a `propose` call asked for, and why.
func proposedChange(_ call: ToolCall) -> (change: RecordChange, rationale: String?)? {
    guard let args = JSONValue.parse(call.arguments)?.object else { return nil }
    let op = args["op"]?.string ?? "patch"
    let id = op == "create" ? args["id"]?.text : (args["target"]?.text).map { $0.split(separator: "/").last.map(String.init) ?? $0 }
    var diff = args["diff"]
    if let s = diff?.string { diff = JSONValue.parse(s) }
    return (RecordChange(op: op, kind: args["kind"]?.text, id: id, values: diff?["properties"]?.object ?? [:]),
            args["rationale"]?.text)
}

/// The records a call's persisted row says it changed, other than the
/// suggestions and questions that have cards of their own.
func stampedChanges(_ call: ToolCall) -> [RecordChange] {
    (call.changes ?? []).filter { $0.kind != Kinds.request && $0.kind != Kinds.interaction && $0.kind != Kinds.interactionBeforeMove }
        .map { RecordChange(op: $0.op, kind: $0.kind, id: $0.id) }
}

/// A value as a person reads it.
func valueWords(_ value: JSONValue) -> String {
    switch value {
    case .null: return "Empty"
    case let .string(s): return s.isEmpty ? "Empty" : s
    case let .bool(b): return b ? "Yes" : "No"
    case let .number(n): return n.rounded() == n && abs(n) < 1e15 ? String(Int(n)) : String(n)
    case let .array(items):
        return items.isEmpty ? "None" : items.map(valueWords).joined(separator: ", ")
    case let .object(o):
        if let path = o["ref"]?.text {
            let parts = path.split(separator: "/").map(String.init)
            return parts.count > 1 ? "\(kindDisplayName(parts.dropLast().joined(separator: "/"))) \(parts.last!)" : path
        }
        // A quantity or an amount: the number, then its unit or currency.
        if let n = o["value"] ?? o["amount"], let unit = (o["unit"] ?? o["currency"])?.referenceID {
            return "\(valueWords(n)) \(unit)"
        }
        return value.jsonText
    }
}

/// Every record title the chat's tool results carried, by `kind/id`: what
/// the agent read or wrote names the records its later changes touch, even
/// where the token may not read them itself.
func recordTitles(_ turns: [Turn]) -> [String: String] {
    var out: [String: String] = [:]
    func note(_ value: JSONValue?) {
        guard let kind = value?["kind"]?.text, let id = value?["id"]?.text,
              let title = recordTitle(value?["properties"]) else { return }
        out["\(kind)/\(id)"] = title
    }
    for turn in turns {
        for call in turn.tools {
            guard call.ok == true, let payload = JSONValue.parse(call.output) else { continue }
            payload["records"]?.array?.forEach(note)
            note(payload["record"])
        }
    }
    return out
}

/// The titles above, kept until the chat's settled results change: a
/// streaming turn redraws the transcript far more often than a call settles.
@MainActor
final class TitleIndex {
    private var key = -1
    private var titles: [String: String] = [:]

    func titles(for turns: [Turn]) -> [String: String] {
        var h = Hasher()
        h.combine(turns.count)
        for turn in turns {
            for call in turn.tools where call.output != nil { h.combine(call.id) }
        }
        let key = h.finalize()
        if key != self.key {
            self.key = key
            titles = recordTitles(turns)
        }
        return titles
    }
}
