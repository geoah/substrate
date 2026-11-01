import Foundation
import SubstrateKit

/// What a call's model-facing name stands for on the agent that made it.
struct ResolvedTool: Hashable, Sendable {
    var function: String?
    var subagent: String?
    var description: String?
}

/// The stamped `callable` decides where the call carries one: the name is the
/// agent's alias, which it may have renamed since, while the stamp names what
/// ran. Without a stamp the name resolves on the agent as declared now.
func resolveTool(agent: Agent?, name: String, callable: String?) -> ResolvedTool {
    let tools = agent?.tools ?? []
    if let callable {
        let parts = callable.split(separator: ":", omittingEmptySubsequences: false).map(String.init)
        if parts.count == 4 {
            let id = parts[1 ... 3].joined(separator: "/")
            if parts[0] == "agent" { return ResolvedTool(subagent: id) }
            if parts[0] == "function" {
                let tool = tools.first { $0.name == name && $0.function == id }
                return ResolvedTool(function: id, description: tool?.description)
            }
        }
    }
    if let tool = tools.first(where: { $0.name == name }) {
        return ResolvedTool(function: tool.function, description: tool.description)
    }
    if let sub = agent?.subagents.first(where: { $0.split(separator: "/").last.map(String.init) == name }) {
        return ResolvedTool(subagent: sub)
    }
    // An agent the app could not read still names the host functions by
    // their local names, which is what an unaliased entry records.
    if agent == nil, let host = HostFunction.all.first(where: { $0.split(separator: "/").last.map(String.init) == name }) {
        return ResolvedTool(function: host)
    }
    return ResolvedTool()
}

private func kinds(in args: [String: JSONValue]) -> [String] {
    args["filter"]?["kinds"]?.array?.compactMap(\.text) ?? []
}

/// What a tool call did, in words.
func toolSummary(_ call: ToolCall, _ resolved: ResolvedTool) -> String {
    let args = JSONValue.parse(call.arguments)?.object ?? [:]
    if let sub = resolved.subagent { return "Asked \(agentName(sub))" }
    switch resolved.function {
    case HostFunction.query:
        var words = "Looked things up"
        if let q = args["q"]?.text {
            words = "Searched for “\(q)”"
        } else if let kind = args["kind"]?.text, args["id"]?.text != nil {
            words = "Looked up one \(kindDisplayName(kind).lowercasedFirst)"
        } else if !kinds(in: args).isEmpty {
            words = "Looked through your \(joinWords(kinds(in: args).map { kindDisplayPlural($0).lowercasedFirst }))"
        }
        if call.ok != false, let found = JSONValue.parse(call.output)?["records"]?.array {
            words += found.count == 1 ? ", found 1" : ", found \(found.count)"
        }
        return words
    case HostFunction.propose:
        return "Suggested a change"
    case HostFunction.write:
        let kind = args["kind"]?.text
        let what = kind.map { kindDisplayName($0).lowercasedFirst } ?? "record"
        switch args["op"]?.string {
        case "put", "create": return "Saved a \(what)"
        case "delete": return "Deleted a \(what)"
        default: return kind == nil ? "Made a change" : "Changed a \(what)"
        }
    case HostFunction.ask:
        return "Asked you some questions"
    default:
        if let description = resolved.description { return description }
        let fn = resolved.function ?? call.name
        let local = fn.split(separator: "/").last.map(String.init) ?? fn
        return "Used \(local.splittingWords().joined(separator: " ").lowercased())"
    }
}

/// A record a call answered with or changed.
struct RecordRef: Hashable, Sendable {
    var kind: String
    var id: String
    var op: String?
}

/// What came back from one settled call, for its expanded card.
enum ToolDetail: Hashable, Sendable {
    case failed(String?)
    case found([RecordRef], total: Int)
    case changed([RecordRef])
    case wrote(records: Int, kinds: [String], moreKinds: Int)
    case reply(agent: String, text: String)
    case output(String)
}

/// A failed call's reason, from the `{"error": …}` envelope.
func toolFailure(_ output: String?) -> String? {
    JSONValue.parse(output)?["error"]?.string
}

private func recordRef(_ value: JSONValue?) -> RecordRef? {
    guard let kind = value?["kind"]?.text, let id = value?["id"]?.text else { return nil }
    return RecordRef(kind: kind, id: id)
}

/// The details a settled call's card opens to. `skip` is the ids rendered
/// as their own cards (a suggestion, a batch of questions).
func toolDetails(_ call: ToolCall, _ resolved: ResolvedTool, skip: Set<String> = []) -> [ToolDetail] {
    guard let ok = call.ok else { return [] }
    let wrote = call.subagentWrites.map { ToolDetail.wrote(records: $0.records, kinds: $0.kinds, moreKinds: $0.moreKinds) }
    if !ok { return [.failed(toolFailure(call.output))] + (wrote.map { [$0] } ?? []) }

    let payload = JSONValue.parse(call.output)
    var out: [ToolDetail] = []
    if let records = payload?["records"]?.array {
        out.append(.found(Array(records.compactMap(recordRef).prefix(8)), total: records.count))
    } else if resolved.function == HostFunction.query, let object = payload?.object, object["record"] != nil {
        let one = recordRef(object["record"])
        out.append(.found(one.map { [$0] } ?? [], total: one == nil ? 0 : 1))
    }

    let stamped = (call.changes ?? []).filter { !skip.contains($0.id) }
    if !stamped.isEmpty {
        out.append(.changed(stamped.map { RecordRef(kind: $0.kind, id: $0.id, op: $0.op) }))
    } else if resolved.function == HostFunction.write, var written = recordRef(payload?["record"]) {
        let op = JSONValue.parse(call.arguments)?["op"]?.string
        written.op = op == "create" || op == "put" ? "put" : (op ?? "patch")
        out.append(.changed([written]))
    }
    if let wrote { out.append(wrote) }

    if let sub = resolved.subagent {
        if let text = payload?["reply"]?.text { out.append(.reply(agent: sub, text: text)) }
        return out
    }
    if !HostFunction.all.contains(resolved.function ?? "") {
        let raw: String
        if let returned = payload?["output"] {
            raw = returned.string?.trimmingCharacters(in: .whitespacesAndNewlines) ?? returned.jsonText
        } else {
            raw = out.isEmpty ? (call.output ?? "").trimmingCharacters(in: .whitespacesAndNewlines) : ""
        }
        if !raw.isEmpty, !["{}", "null", "[]"].contains(raw) { out.append(.output(raw)) }
    }
    return out
}

/// A sub-agent chain's writes in one sentence.
func wroteSentence(records: Int, kinds: [String], moreKinds: Int) -> String {
    if records == 0 { return "Wrote no records" }
    let noun = records == 1 ? "record" : "records"
    guard !kinds.isEmpty else { return "Wrote \(records) \(noun)" }
    let one = kinds.count + max(0, moreKinds) == 1
    var s = "Wrote \(records) \(noun) \(one ? "of" : "across") "
    s += kinds.map { kindDisplayName($0).lowercasedFirst }.joined(separator: ", ")
    if moreKinds > 0 { s += " and \(moreKinds) more \(moreKinds == 1 ? "kind" : "kinds")" }
    return s
}
