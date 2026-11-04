import Foundation

/// The part of a kind's declaration the record views and the grants read:
/// its names, each property's type and words, its purpose and the traits it
/// binds.
public struct KindDeclaration: Hashable, Sendable {
    public static let kind = "substrate.reamde.dev/core/kind"

    public var reference: String
    /// The declared label, which wins over the name built from words.
    public var label: String?
    public var description: String?
    public var properties: [String: PropertyDeclaration]
    /// The declared purpose; nil where the declaration names none, which
    /// reads as `primary` (decision 0133).
    public var purpose: String?
    /// The traits the kind binds, each by its full identity.
    public var traits: [String]

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
        purpose = record["purpose"]?.text
        traits = (record["traits"]?.array ?? []).compactMap { $0.text.flatMap(Self.boundTrait) }
    }

    public var displayName: String { label ?? kindDisplayName(reference) }

    /// The trait one `traits:` entry binds, by its full identity, as the
    /// server reads it (internal/vocabulary's TraitBindingRef): the entry
    /// before any `(`, since the variant and the remap say how the kind binds
    /// the trait, not which. Nil for an entry that does not parse or names
    /// the trait by a bare word, which no loaded declaration holds.
    public static func boundTrait(_ entry: String) -> String? {
        let whole = entry.trimmingCharacters(in: .whitespacesAndNewlines)
        let identity = whole.prefix { $0 != "(" }.trimmingCharacters(in: .whitespacesAndNewlines)
        let parts = identity.split(separator: "/", omittingEmptySubsequences: false)
        guard parts.count == 3, let first = parts[0].first, let package = parts[1].first, let name = parts[2].first,
            first.isASCII && (first.isLowercase || first.isNumber),
            parts[0].allSatisfy({ $0.isASCII && ($0.isLowercase || $0.isNumber || $0 == "." || $0 == "-") }),
            package.isASCII && package.isLowercase, parts[1].allSatisfy({ $0.isASCII && ($0.isLowercase || $0.isNumber) }),
            name.isASCII && name.isLowercase, parts[2].allSatisfy({ $0.isASCII && ($0.isLetter || $0.isNumber) })
        else { return nil }
        return identity
    }
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
