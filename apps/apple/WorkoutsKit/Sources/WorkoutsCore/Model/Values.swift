import Foundation

/// The unit a load was entered in.
public enum MassUnit: String, Codable, Sendable, CaseIterable {
    case kg, lb
}

/// A load as entered: exact digits and the unit. Never a Double in storage.
public struct Mass: Codable, Hashable, Sendable {
    /// The digits as entered: "83.9146" stays 83.9146.
    public var value: Decimal
    public var unit: MassUnit

    /// One pound, exactly, by the international definition.
    public static let poundInKilograms = Decimal(string: "0.45359237")!

    public init(value: Decimal, unit: MassUnit) {
        self.value = value
        self.unit = unit
    }

    public static func kg(_ value: Decimal) -> Mass { Mass(value: value, unit: .kg) }
    public static func lb(_ value: Decimal) -> Mass { Mass(value: value, unit: .lb) }

    /// The same mass in another unit: an exact multiply or divide, no rounding.
    /// A pound-to-kilogram conversion is exact; the other way keeps Decimal's
    /// 38 digits, so callers round for display.
    public func converted(to target: MassUnit) -> Mass {
        switch (unit, target) {
        case (.kg, .kg), (.lb, .lb): self
        case (.lb, .kg): Mass(value: value * Self.poundInKilograms, unit: .kg)
        case (.kg, .lb): Mass(value: value / Self.poundInKilograms, unit: .lb)
        }
    }
}

/// A distance. Its quantity unit is always "m".
public struct Length: Codable, Hashable, Sendable {
    public var metres: Decimal

    public init(metres: Decimal) {
        self.metres = metres
    }
}

/// Every duration in the app is whole seconds.
public typealias Seconds = Int

/// A reference to a record of a kind the training package does not own
/// (`workout`, `source`, `about`, `origin`).
public struct RecordRef: Codable, Hashable, Sendable {
    public var kind: String
    public var id: String

    public init(kind: String, id: String) {
        self.kind = kind
        self.id = id
    }

    /// The reference as a record stores it: `<kind>/<id>`.
    public var path: String { "\(kind)/\(id)" }
}

/// How an effort value got onto a set (`set.rpeMethod`, `set.rirMethod`;
/// the package README, "Effort provenance").
public enum EffortMethod: String, Codable, Sendable, CaseIterable {
    /// The lifter chose the value, even one already shown.
    case entered
    /// The plan's value was shown pre-filled and the set logged with it untouched.
    case prefilled
    /// Copied from last time (a tap on PREVIOUS).
    case copied
    /// From another app, where how it was entered is unknown.
    case imported

    /// Whether a reader takes the value as a rating: `entered` and `imported`
    /// only. A `prefilled` or `copied` value reads as no rating.
    public var isRating: Bool { self == .entered || self == .imported }

    /// The same for an optional method: absent beside a value is a set written
    /// before the field existed, which V1 wrote only when tapped, so a rating.
    public static func isRating(_ method: EffortMethod?) -> Bool { method?.isRating ?? true }
}

/// Unknown fields of a nested object (a routine slot, a planned set, a
/// target), kept so an older build never erases what a newer one wrote
/// (§3.4 rule 7). Decodes as empty when its key is missing, so a journal
/// written before a type had one still loads.
@propertyWrapper
public struct UnknownFields: Codable, Hashable, Sendable {
    public var wrappedValue: [String: JSONValue]

    public init(wrappedValue: [String: JSONValue] = [:]) {
        self.wrappedValue = wrappedValue
    }

    public init(from decoder: any Decoder) throws {
        wrappedValue = try decoder.singleValueContainer().decode([String: JSONValue].self)
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(wrappedValue)
    }
}

extension KeyedDecodingContainer {
    /// A missing key decodes as no unknown fields.
    func decode(_ type: UnknownFields.Type, forKey key: Key) throws -> UnknownFields {
        try decodeIfPresent(type, forKey: key) ?? UnknownFields()
    }
}

public extension Date {
    /// The instant floored to a whole second (§3.2 "Time"): every instant the
    /// engine stores is, so durations are whole seconds and a record's `at`
    /// equals the instant in its UUIDv7.
    var flooredToSecond: Date {
        Date(timeIntervalSince1970: timeIntervalSince1970.rounded(.down))
    }
}
