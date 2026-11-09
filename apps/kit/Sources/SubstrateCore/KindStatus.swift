import Foundation

/// A kind's status as core's `lifecycle` trait reads it (decisions 0226 and
/// 0247): the property it binds, renamed or not, its values in declared
/// order, which of them close a record (`done`, `dropped`), which are a
/// suggestion waiting for its owner (`proposed`), and, for a state machine,
/// the moves it allows. Every value in none of the lists is open, and a
/// record with no status is open.
public struct KindStatus: Hashable, Sendable {
    public static let lifecycle = "substrate.reamde.dev/core/lifecycle"

    public enum Standing: String, Hashable, Sendable, Bridged {
        case proposed, open, done, dropped
    }

    /// The kind's property the trait binds: `status`, or what the binding
    /// renames it to (`lifecycle(status: state)`).
    public var property: String
    public var values: [String]
    public var done: [String]
    public var dropped: [String]
    public var proposed: [String]
    /// A machine's moves as `from` to the states it may go to, in declared
    /// order; nil for an enum, which takes any of its values.
    public var moves: [String: [String]]?
    /// A machine's initial state, which a create that names none lands in.
    public var initial: String?

    public init(
        property: String, values: [String], done: [String] = [], dropped: [String] = [], proposed: [String] = [],
        moves: [String: [String]]? = nil, initial: String? = nil
    ) {
        self.property = property
        self.values = values
        self.done = done
        self.dropped = dropped
        self.proposed = proposed
        self.moves = moves
        self.initial = initial
    }

    /// The status of a kind that binds `lifecycle`, read off its declaration
    /// record; nil where it binds no such trait or its status is no enum or
    /// machine.
    public init?(declaration record: Record) {
        guard record.kind == KindDeclaration.kind else { return nil }
        self.init(definition: record.properties)
    }

    /// The same, read off a declaration's data map, as a trait's
    /// implementors route serves each kind's `definition`.
    public init?(definition: [String: JSONValue]) {
        let entries = (definition["traits"]?.array ?? []).compactMap(\.text)
        guard let entry = entries.first(where: { KindDeclaration.boundTrait($0) == Self.lifecycle }) else { return nil }
        let name = KindDeclaration.renames(in: entry)["status"] ?? "status"
        guard let spec = definition["properties"]?[name] else { return nil }
        let type = spec["type"]?.text
        let strings = { (key: String) in (spec[key]?.array ?? []).compactMap { $0.text ?? $0["value"]?.text } }
        switch type {
        case "state":
            values = strings("states")
            var moves: [String: [String]] = [:]
            for transition in spec["transitions"]?.array ?? [] {
                guard let from = transition["from"]?.text, let to = transition["to"]?.text else { continue }
                moves[from, default: []].append(to)
            }
            self.moves = moves
            initial = spec["initial"]?.text
        case "enum":
            values = strings("values")
            moves = nil
            initial = nil
        default:
            return nil
        }
        property = name
        done = strings("done")
        dropped = strings("dropped")
        proposed = strings("proposed")
    }

    public func standing(of value: String?) -> Standing {
        guard let value else { return .open }
        if done.contains(value) { return .done }
        if dropped.contains(value) { return .dropped }
        if proposed.contains(value) { return .proposed }
        return .open
    }

    /// The values a record standing at `value` may move to: a machine's
    /// declared moves, or every other value of an enum.
    public func moves(from value: String?) -> [String] {
        guard let moves else { return values.filter { $0 != value } }
        guard let value else { return [] }
        return moves[value] ?? []
    }

    /// The first move from `value` to a value standing `standing`: accepting
    /// a suggestion is its first move to an open value, dismissing it the
    /// first to a dropped one, completing a task the first to a done one.
    public func move(from value: String?, to standing: Standing) -> String? {
        moves(from: value).first { self.standing(of: $0) == standing }
    }
}

extension KindDeclaration {
    /// What a `traits:` entry's remap renames, contract name to the kind's
    /// own: `lifecycle(status: state)` is `["status": "state"]`, and
    /// `temporal(point: dueAt)` is `["point": "dueAt"]`, the variant before
    /// the colon. An entry without a colon renames nothing.
    public static func renames(in entry: String) -> [String: String] {
        guard let open = entry.firstIndex(of: "("), let close = entry.lastIndex(of: ")"), open < close else { return [:] }
        var out: [String: String] = [:]
        for part in entry[entry.index(after: open) ..< close].split(separator: ",") {
            let pair = part.split(separator: ":", maxSplits: 1).map { $0.trimmingCharacters(in: .whitespaces) }
            if pair.count == 2, !pair[0].isEmpty, !pair[1].isEmpty { out[pair[0]] = pair[1] }
        }
        return out
    }
}
