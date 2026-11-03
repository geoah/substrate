import Foundation

/// A type a bridged value's field or payload has, in the restricted model.
public indirect enum BridgedType: Equatable, Sendable {
    case string
    case bool
    case int
    case double
    /// `JSONValue`: any JSON, carried as it is.
    case json
    /// A `Bridged` declaration, by its qualified Swift name (`BridgeFailure.Code`).
    case named(String)
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
        case let .named(name): name
        case let .optional(wrapped): "\(wrapped.spelling)?"
        case let .array(element): "[\(element.spelling)]"
        case let .dictionary(value): "[String: \(value.spelling)]"
        }
    }
}

public struct Field: Equatable, Sendable {
    public var name: String
    public var type: BridgedType
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

    public func declaration(_ name: String) -> Declaration? {
        declarations.first { $0.name == name }
    }

    public func children(of name: String?) -> [Declaration] {
        declarations.filter { $0.parent == name }
    }
}

/// A declaration outside the model, named with where it is.
public struct ModelError: Error, CustomStringConvertible, Equatable {
    public var location: String
    public var message: String

    public var description: String { "\(location): \(message)" }
}
