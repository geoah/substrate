/// The key a parent keeps a child under: an id the app minted when it made
/// the child (Chat's conversation UUID), never a server's, which arrives
/// later as an event.
public struct ChildKey: RawRepresentable, Hashable, Codable, CodingKeyRepresentable, Sendable, Comparable,
    CustomStringConvertible
{
    public var rawValue: String

    public init(rawValue: String) {
        self.rawValue = rawValue
    }

    public init(_ rawValue: String) {
        self.rawValue = rawValue
    }

    public init(from decoder: any Decoder) throws {
        rawValue = try decoder.singleValueContainer().decode(String.self)
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(rawValue)
    }

    // A dictionary keyed by children encodes as a JSON object, which is how
    // the Android bridge reads a presentation's children.
    public var codingKey: any CodingKey { Key(stringValue: rawValue) }

    public init?<T: CodingKey>(codingKey: T) {
        rawValue = codingKey.stringValue
    }

    private struct Key: CodingKey {
        var stringValue: String
        var intValue: Int? { nil }
        init(stringValue: String) { self.stringValue = stringValue }
        init?(intValue: Int) { nil }
    }

    public static func < (lhs: ChildKey, rhs: ChildKey) -> Bool { lhs.rawValue < rhs.rawValue }

    public var description: String { rawValue }
}
