import Foundation
import SubstrateCore

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
        if let id = callableID(callable, kind: Kinds.agent) { return ResolvedTool(subagent: id) }
        if let id = callableID(callable, kind: Kinds.function) {
            let tool = tools.first { $0.name == name && $0.function == id }
            return ResolvedTool(function: id, description: tool?.description)
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

/// The function or agent a stamped callable names: the record path the
/// engine stamps a tool call and its row with
/// (`substrate.reamde.dev/core/function/<authority>/<package>/<name>`), read
/// as the declaration's own id. The console's rule (`callableRecord` in
/// web/console/src/lib/agent-chat.ts).
func callableID(_ callable: String, kind: String) -> String? {
    let prefix = "\(kind)/"
    guard callable.hasPrefix(prefix) else { return nil }
    let id = String(callable.dropFirst(prefix.count))
    return id.split(separator: "/", omittingEmptySubsequences: false).count == 3 ? id : nil
}

private func kinds(in args: [String: JSONValue]) -> [String] {
    args["filter"]?["kinds"]?.array?.compactMap(\.text) ?? []
}

/// A list argument, which the engine also takes encoded as a string.
private func list(_ value: JSONValue?) -> [JSONValue]? {
    if let encoded = value?.string { return JSONValue.parse(encoded)?.array }
    return value?.array
}

/// The kinds a `vocabulary` call's documents declare, by their last word.
private func declaredKinds(_ documents: JSONValue?) -> [String] {
    (list(documents) ?? []).compactMap { document in
        guard document["kind"]?.string == Kinds.kind, let id = document["metadata"]?["id"]?.text else { return nil }
        return id.split(separator: "/").last.map(String.init)
    }
}

/// A noun with its indefinite article, by how it sounds: "a book", "an
/// author", "a unit", "an hour". The console's rule (`withArticle` in
/// web/console/src/lib/agent-chat.ts).
func withArticle(_ noun: String) -> String {
    func starts(_ sound: Regex<Substring>) -> Bool { noun.prefixMatch(of: sound.ignoresCase()) != nil }
    // A silent h takes "an"; a vowel said as "you" or "wun" does not.
    let an =
        starts(/hour|honest|honou?r|heir/)
        || (starts(/[aeiou]/) && !starts(/uni|use|usu|uti|ure|uro|eu|ewe|one\b|once|url|uri\b|uuid|usb/))
    return "\(an ? "an" : "a") \(noun)"
}

/// The longest title a tool call's line quotes whole.
let lineTitle = 60

/// A title as the line quotes it: whole, or cut at a word and marked.
func quoted(_ title: String) -> String {
    let flat = title.split(whereSeparator: \.isWhitespace).joined(separator: " ")
    guard flat.count > lineTitle else { return "“\(flat)”" }
    var cut = flat.prefix(lineTitle)
    if let space = cut.lastIndex(of: " "), cut.distance(from: cut.startIndex, to: space) > lineTitle / 2 {
        cut = cut[..<space]
    }
    return "“\(cut.trimmingCharacters(in: .whitespaces))…”"
}

/// A record's heading from the envelope a tool answered with: the engine's
/// derived `title`, stored among the properties, or beside them.
func envelopeTitle(_ value: JSONValue?) -> String? {
    let held = value?["properties"]?["title"]
    return (held?.string != nil ? held : value?["title"])?.text
}

/// The one record a call worked on, when it worked on exactly one: what its
/// line names. A call that touched several lists them under the line
/// instead.
struct ToolSubject: Hashable, Sendable {
    var kind: String
    var id: String
    /// Its heading, when the answer carried it.
    var title: String?

    var reference: RecordReference { RecordReference(kind: kind, id: id) }
}

func toolSubject(_ call: ToolCall, _ resolved: ResolvedTool) -> ToolSubject? {
    guard call.ok != false else { return nil }
    let payload = JSONValue.parse(call.output)
    func one(_ value: JSONValue?) -> ToolSubject? {
        guard let kind = value?["kind"]?.text, let id = value?["id"]?.text else { return nil }
        return ToolSubject(kind: kind, id: id, title: envelopeTitle(value))
    }
    switch resolved.function {
    case HostFunction.write:
        return one(payload?["record"])
    case HostFunction.edit:
        return one(JSONValue.parse(call.arguments))
    case HostFunction.query:
        if let answer = payload?.object, answer["record"] != nil { return one(answer["record"]) }
        guard let found = payload?["records"]?.array, found.count == 1 else { return nil }
        return one(found[0])
    default:
        return nil
    }
}

/// A property name as a line says it: "the description", "the page title".
func fieldWords(_ name: String) -> String { "the \(propertyLabel(name).lowercased())" }

/// The fields a patch wrote, as a line says them before "of": "the
/// description", "the status and notes", "3 fields".
func fieldsWords(_ names: [String]) -> String? {
    switch names.count {
    case 0: nil
    case 1: fieldWords(names[0])
    case 2: "\(fieldWords(names[0])) and \(propertyLabel(names[1]).lowercased())"
    default: "\(names.count) fields"
    }
}

/// The property names a write's `input` set, in the order it set them.
func writtenFields(_ args: [String: JSONValue]) -> [String] {
    var input = args["input"]
    // The input may arrive as an encoded object.
    if let encoded = input?.string { input = JSONValue.parse(encoded) }
    return input?["properties"]?.objectValue?.keys ?? []
}

/// A call's line: its words with the record it worked on said by its kind,
/// and where a heading can name that record instead, the words around it.
struct ToolLine: Hashable, Sendable {
    var words: String
    /// What the line says before the quoted heading; nil where it names none.
    var before: String?
    var after = ""

    /// The line, naming the record by `title` where it can.
    func said(title: String?) -> String {
        guard let title, let before else { return words }
        return before + quoted(title) + after
    }
}

/// What a tool call did, in words. `title` is the heading of the one record
/// the call worked on (`toolSubject`), for an answer that did not carry it.
func toolSummary(_ call: ToolCall, _ resolved: ResolvedTool, title: String? = nil) -> String {
    let subject = toolSubject(call, resolved)
    return toolLine(call, resolved).said(title: subject?.title ?? (subject == nil ? nil : title))
}

func toolLine(_ call: ToolCall, _ resolved: ResolvedTool) -> ToolLine {
    let args = JSONValue.parse(call.arguments)?.object ?? [:]
    if let sub = resolved.subagent { return ToolLine(words: "Asked \(agentName(sub))") }
    switch resolved.function {
    case HostFunction.query:
        var words = "Looked things up"
        if let q = args["q"]?.text {
            words = "Searched for “\(q)”"
        } else if let kind = args["kind"]?.text, args["id"]?.text != nil {
            words = "Looked up \(withArticle(kindDisplayName(kind).lowercasedFirst))"
        } else if !kinds(in: args).isEmpty {
            words = "Looked through your \(joinWords(kinds(in: args).map { kindDisplayPlural($0).lowercasedFirst }))"
        }
        let payload = JSONValue.parse(call.output)
        var line = ToolLine(words: words)
        if call.ok != false, let found = payload?["records"]?.array {
            line.words += found.count == 1 ? ", found 1" : ", found \(found.count)"
            if found.count == 1 { line.before = "\(words), found " }
        }
        if payload?.object?["record"] != nil { line.before = "Looked up " }
        return line
    case HostFunction.propose:
        return ToolLine(words: "Suggested a change")
    case HostFunction.write:
        let kind = args["kind"]?.text
        let what = kind.map { kindDisplayName($0).lowercasedFirst } ?? "record"
        let op = args["op"]?.string
        // A put without an id is given one: it adds the record.
        if op == "create" || (op == "put" && args["id"]?.text == nil) {
            let to = kind.map { " to your \(kindDisplayPlural($0).lowercasedFirst)" } ?? ""
            return ToolLine(words: "Added \(withArticle(what))", before: "Added ", after: to)
        }
        if op == "put" { return ToolLine(words: "Saved \(withArticle(what))", before: "Saved ") }
        if op == "delete" { return ToolLine(words: "Deleted \(withArticle(what))", before: "Deleted ") }
        guard kind != nil else { return ToolLine(words: "Made a change") }
        let fields = fieldsWords(writtenFields(args)).map { "\($0) of " } ?? ""
        return ToolLine(words: "Changed \(fields)\(withArticle(what))", before: "Changed \(fields)")
    case HostFunction.ask:
        return ToolLine(words: "Asked you some questions")
    case HostFunction.wait:
        return ToolLine(words: "Waited for work running in the background")
    case HostFunction.edit:
        let what = args["kind"]?.text.map { kindDisplayName($0).lowercasedFirst } ?? "record"
        let field = args["property"]?.string.flatMap { $0.isEmpty ? nil : "\(fieldWords($0)) of " } ?? ""
        if args["op"]?.string == "view" {
            return ToolLine(words: "Read part of \(field)\(withArticle(what))", before: "Read part of \(field)")
        }
        let edits = list(args["edits"])?.count ?? 0
        let places = edits > 1 ? " in \(edits) places" : ""
        return ToolLine(words: "Edited \(field)\(withArticle(what))\(places)", before: "Edited \(field)", after: places)
    case HostFunction.vocabulary:
        let named = declaredKinds(args["documents"])
        let what = named.isEmpty ? "" : " (\(joinWords(named)))"
        return ToolLine(words: args["plan"]?.bool == true ? "Checked a change to your collections\(what)" : "Changed your collections\(what)")
    default:
        // A function whose answer has a shape the app knows is said from it.
        if resolved.function != nil, let shaped = toolResultSummary(call) { return ToolLine(words: shaped) }
        if let description = resolved.description { return ToolLine(words: description) }
        let fn = resolved.function ?? call.name
        let local = fn.split(separator: "/").last.map(String.init) ?? fn
        return ToolLine(words: "Used \(local.splittingWords().joined(separator: " ").lowercased())")
    }
}

/// What each stored op did, as the verb a change is said with.
let changeVerbs = ["put": "Saved", "patch": "Changed", "delete": "Deleted", "merge": "Merged", "split": "Split"]

/// A record a call answered with or changed.
struct RecordRef: Hashable, Sendable {
    var kind: String
    var id: String
    var op: String?
    /// Its heading, where the answer carried it.
    var title: String?
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
    return RecordRef(kind: kind, id: id, title: envelopeTitle(value))
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
    if !HostFunction.detailed.contains(resolved.function ?? "") {
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

/// The thread a sub-agent call's chain ran in: the engine's stamp, else the
/// reply's own word of it.
func childThread(_ call: ToolCall, _ resolved: ResolvedTool) -> String? {
    guard resolved.subagent != nil else { return nil }
    return call.subagentWrites?.thread ?? (call.ok == true ? JSONValue.parse(call.output)?["thread"]?.text : nil)
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
