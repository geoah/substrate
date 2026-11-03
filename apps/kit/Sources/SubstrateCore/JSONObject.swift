import Foundation

/// A JSON object whose keys keep the order they were set in. Equality and
/// hashing ignore the order, as JSON does.
public struct JSONObject: Hashable, Sendable, Sequence, ExpressibleByDictionaryLiteral {
    public typealias Key = String
    public typealias Value = JSONValue

    public private(set) var keys: [String] = []
    private var storage: [String: JSONValue] = [:]

    public init() {}

    /// The pairs in order; a repeated key keeps its first place and last value.
    public init(_ pairs: [(String, JSONValue)]) {
        for (key, value) in pairs {
            self[key] = value
        }
    }

    /// A dictionary's members, in key order: a dictionary has no order of
    /// its own, and this gives it one that is the same on every run.
    public init(_ dictionary: [String: JSONValue]) {
        self.init(dictionary.sorted { $0.key < $1.key }.map { ($0.key, $0.value) })
    }

    public init(dictionaryLiteral elements: (String, JSONValue)...) {
        self.init(elements)
    }

    /// Setting a new key appends it; setting nil removes it.
    public subscript(key: String) -> JSONValue? {
        get { storage[key] }
        set {
            if let newValue {
                if storage.updateValue(newValue, forKey: key) == nil { keys.append(key) }
            } else if storage.removeValue(forKey: key) != nil {
                keys.removeAll { $0 == key }
            }
        }
    }

    public var count: Int { keys.count }
    public var isEmpty: Bool { keys.isEmpty }
    public var dictionary: [String: JSONValue] { storage }

    public func makeIterator() -> some IteratorProtocol<(key: String, value: JSONValue)> {
        keys.lazy.map { (key: $0, value: storage[$0]!) }.makeIterator()
    }

    public static func == (lhs: JSONObject, rhs: JSONObject) -> Bool { lhs.storage == rhs.storage }
    public func hash(into hasher: inout Hasher) { hasher.combine(storage) }
}

extension JSONObject: Codable {
    private struct Name: CodingKey {
        var stringValue: String
        var intValue: Int? { nil }
        init(stringValue: String) { self.stringValue = stringValue }
        init?(intValue _: Int) { nil }
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: Name.self)
        self.init()
        for key in container.allKeys.sorted(by: { $0.stringValue < $1.stringValue }) {
            self[key.stringValue] = try container.decode(JSONValue.self, forKey: key)
        }
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: Name.self)
        for (key, value) in self {
            try container.encode(value, forKey: Name(stringValue: key))
        }
    }
}
