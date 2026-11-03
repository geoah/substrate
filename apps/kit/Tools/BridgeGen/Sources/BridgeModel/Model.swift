import Foundation

/// A type a bridged value's field or payload has, in the restricted model.
public indirect enum BridgedType: Equatable, Sendable {
    case string
    case bool
    case int
    case double
    /// `JSONValue`: any JSON, carried as it is.
    case json
    /// An instant, as `ValueCoding` writes it: an RFC 3339 string.
    case date
    /// A `URL`, as its absolute string.
    case url
    /// A `UUID`, as its string.
    case uuid
    /// A type outside the model that crosses as the JSON its own coding
    /// writes, for Kotlin to carry and never read (`bridge.json`'s `opaque`).
    case opaque(String)
    /// A `Bridged` declaration, by its qualified Swift name (`BridgeFailure.Code`).
    case named(String)
    /// A generic `Bridged` struct with its arguments (`ActionButton<AccessAction>`).
    case generic(String, [BridgedType])
    /// One of the enclosing generic struct's parameters.
    case parameter(String)
    /// SubstrateRuntime's `KeyedList` of a bridged row: `{revision, rows}`,
    /// which a presentation crosses as a reference to the list apart.
    case keyedList(BridgedType)
    case optional(BridgedType)
    case array(BridgedType)
    /// `[String: T]`.
    case dictionary(BridgedType)

    /// The spelling the golden writes: Swift's, with `JSON` for `JSONValue`.
    public var spelling: String {
        switch self {
        case .string: "String"
        case .bool: "Bool"
        case .int: "Int"
        case .double: "Double"
        case .json: "JSON"
        case .date: "Date"
        case .url: "URL"
        case .uuid: "UUID"
        case let .opaque(name): "JSON(\(name))"
        case let .named(name): name
        case let .generic(name, arguments): "\(name)<\(arguments.map(\.spelling).joined(separator: ", "))>"
        case let .parameter(name): name
        case let .keyedList(row): "KeyedList<\(row.spelling)>"
        case let .optional(wrapped): "\(wrapped.spelling)?"
        case let .array(element): "[\(element.spelling)]"
        case let .dictionary(value): "[String: \(value.spelling)]"
        }
    }
}

public struct Field: Equatable, Sendable {
    /// The key Swift's synthesized coding writes: the label, or `_0`, `_1`
    /// for a payload's unlabelled values by their place.
    public var name: String
    public var type: BridgedType

    public var isUnlabelled: Bool { name.first == "_" && name.dropFirst().allSatisfy(\.isNumber) }
}

public struct Case: Equatable, Sendable {
    public var name: String
    /// A `String`-raw enum's value; nil for a payload enum's case.
    public var rawValue: String?
    public var fields: [Field]
}

public enum DeclarationKind: String, Equatable, Sendable {
    case `struct`
    case stringEnum
    case `enum`
}

/// One `Bridged` declaration.
public struct Declaration: Equatable, Sendable {
    /// Qualified by the declarations it is nested in: `BridgeFailure.Code`.
    public var name: String
    public var module: String
    public var kind: DeclarationKind
    public var fields: [Field]
    public var cases: [Case]
    /// A generic struct's type parameters, in order.
    public var parameters: [String] = []
    /// Read from another configuration's sources (`uses`): known here, and
    /// generated there.
    public var external = false

    /// The declaration it is nested in, if any.
    public var parent: String? {
        guard let dot = name.lastIndex(of: ".") else { return nil }
        return String(name[..<dot])
    }

    public var simpleName: String {
        guard let dot = name.lastIndex(of: ".") else { return name }
        return String(name[name.index(after: dot)...])
    }
}

/// Every declaration the sources hold, sorted by name.
public struct Model: Equatable, Sendable {
    public var declarations: [Declaration]

    /// The declarations this configuration generates.
    public var own: Model { Model(declarations: declarations.filter { !$0.external }) }

    public func declaration(_ name: String) -> Declaration? {
        declarations.first { $0.name == name }
    }

    public func children(of name: String?) -> [Declaration] {
        declarations.filter { $0.parent == name }
    }

    /// The names bridged values are nested in that are not bridged
    /// themselves (a controller holding its `Command`), directly under
    /// `scope`: Kotlin keeps them as namespaces.
    public func namespaces(in scope: String?) -> [String] {
        var found = Set<String>()
        for declaration in declarations {
            var parent = declaration.parent
            while let name = parent {
                let above = name.lastIndex(of: ".").map { String(name[..<$0]) }
                if self.declaration(name) == nil, above == scope { found.insert(name) }
                parent = above
            }
        }
        return found.sorted()
    }
}

/// A declaration outside the model, named with where it is.
public struct ModelError: Error, CustomStringConvertible, Equatable {
    public var location: String
    public var message: String

    public var description: String { "\(location): \(message)" }
}
