import Foundation

/// The part of a kind's declaration the record views read: its names and
/// each property's type and words.
public struct KindDeclaration: Hashable, Sendable {
    public static let kind = "substrate.reamde.dev/core/kind"

    public var reference: String
    /// The declared label, which wins over the name built from words.
    public var label: String?
    public var description: String?
    public var properties: [String: PropertyDeclaration]

    public init?(record: Record) {
        guard record.kind == KindDeclaration.kind else { return nil }
        reference = record.id
        label = record["label"]?["singular"]?.text ?? record["label"]?.text
        description = record["description"]?.text
        var props: [String: PropertyDeclaration] = [:]
        for (name, value) in record["properties"]?.object ?? [:] {
            props[name] = PropertyDeclaration(value)
        }
        properties = props
    }

    public var displayName: String { label ?? kindDisplayName(reference) }
}

public struct PropertyDeclaration: Hashable, Sendable {
    public var type: String?
    public var description: String?
    /// An enum's values with their labels, where declared.
    public var values: [String: String]
    /// A reference's target kind.
    public var target: String?

    public init(_ value: JSONValue) {
        type = value["type"]?.text
        description = value["description"]?.text
        target = value["kind"]?.text
        var labels: [String: String] = [:]
        for item in value["values"]?.array ?? [] {
            if let v = item.text {
                labels[v] = v.splittingWords().map { $0.lowercased() }.joined(separator: " ").capitalizedFirst
            } else if let v = item["value"]?.text {
                labels[v] = item["label"]?.text ?? v.splittingWords().map { $0.lowercased() }.joined(separator: " ").capitalizedFirst
            }
        }
        values = labels
    }

    public func label(of value: String) -> String? { values[value] }
}
