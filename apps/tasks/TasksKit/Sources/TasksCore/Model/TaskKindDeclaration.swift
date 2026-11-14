import Foundation
import SubstrateCore

/// One kind's declaration as the app reads it: from the lifecycle trait's
/// implementors (`KindInfo`), or from the closure this build carries for its
/// own kinds. The data map is the declaration's own, read conservatively
/// (docs/plans/tasks-sources.md, "What the app reads from a declaration").
public struct TaskKindDeclaration: Codable, Hashable, Sendable {
    public var identity: String
    public var singular: String?
    public var plural: String?
    public var definition: [String: JSONValue]

    public init(identity: String, singular: String? = nil, plural: String? = nil, definition: [String: JSONValue]) {
        self.identity = identity
        self.singular = singular ?? definition["label"]?["singular"]?.text
        self.plural = plural ?? definition["label"]?["plural"]?.text
        self.definition = definition
    }

    /// The kind's name in words, its declared label first.
    public var label: String { singular ?? kindDisplayName(identity) }
    public var pluralLabel: String { plural ?? kindDisplayPlural(identity) }

    /// The declared properties in declared order.
    public var propertyNames: [String] { definition["properties"]?.objectValue?.keys ?? [] }

    public func property(_ name: String) -> JSONValue? { definition["properties"]?[name] }

    public func type(of name: String) -> String? { property(name)?["type"]?.text }

    public var traitEntries: [String] { (definition["traits"]?.array ?? []).compactMap(\.text) }

    /// The kind as a grant is held to it: its purpose, absent read as
    /// `primary`, and the traits it binds.
    public var grantKind: GrantKind {
        GrantKind(
            identity, purpose: definition["purpose"]?.text ?? "primary",
            traits: traitEntries.compactMap(KindDeclaration.boundTrait))
    }

    public var status: KindStatus? { KindStatus(definition: definition) }

    /// The property `temporal(point: <name>)` names, whether the kind
    /// declares it or the trait supplies it.
    public var temporalPoint: String? {
        guard let entry = traitEntries.first(where: { KindDeclaration.boundTrait($0) == TasksKinds.temporal }) else { return nil }
        return KindDeclaration.renames(in: entry)["point"]
    }

    /// The property the title is edited through: the one `displayTemplate`
    /// names when it is exactly `{name}` and that property is a string.
    public var editableTitle: String? {
        guard let template = definition["displayTemplate"]?.text, template.hasPrefix("{"), template.hasSuffix("}") else { return nil }
        let name = String(template.dropFirst().dropLast())
        guard !name.isEmpty, !name.contains(where: { "{} ".contains($0) }), type(of: name) == "string" else { return nil }
        return name
    }

    /// A declared property's values in declared order: an enum's, or a
    /// machine's states.
    public func values(of name: String) -> [String] {
        let spec = property(name)
        let list = spec?["values"]?.array ?? spec?["states"]?.array ?? []
        return list.compactMap { $0.text ?? $0["value"]?.text }
    }

    /// A value's declared label, else the value in words.
    public func label(of value: String, in name: String) -> String {
        PropertyDeclaration(property(name) ?? .null).label(of: value)
            ?? value.splittingWords().map { $0.lowercased() }.joined(separator: " ")
            .capitalizedFirst
    }

    /// The properties a move into `state` stamps: each machine transition's
    /// `stamps` whose `to` is the state.
    public func stamps(into state: String, of name: String) -> [String] {
        var out: [String] = []
        for transition in property(name)?["transitions"]?.array ?? [] where transition["to"]?.text == state {
            for key in transition["stamps"]?.objectValue?.keys ?? [] where !out.contains(key) { out.append(key) }
        }
        return out
    }

    /// The properties the kind requires.
    public var required: [String] { propertyNames.filter { property($0)?["required"]?.bool == true } }

    /// Whether `name` is a single (not repeated) reference, and its target.
    public func singleReference(_ name: String) -> String? {
        guard type(of: name) == "reference", property(name)?["repeated"]?.bool != true else { return nil }
        return property(name)?["kind"]?.text
    }
}
