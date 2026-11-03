import Foundation

/// Any `Codable` to and from a `JSONValue`, as `JSONEncoder` and
/// `JSONDecoder` do with their default strategies, but with a `JSONValue`
/// (or `JSONObject`) anywhere inside taken whole: a `Decoder` hands a number
/// over only as a `Decimal` or a `Double`, so through one a body loses every
/// digit past 38 and every exponent past Decimal's, and an `Encoder` refuses
/// such a number. The bridge's messages are read with `JSONValue(data:)`,
/// written with `JSONValue.data(pretty:)`, and pass through this between,
/// never through Foundation's coders. A keyed container's keys are written
/// sorted, so a message is comparable byte for byte; a `JSONValue` object
/// keeps its own order.
///
/// Three types are written as the bridge's restricted model reads them
/// (`Bridged`), whatever their own coding: a `Date` as an RFC 3339 string
/// (`Timestamp`, milliseconds where it has a fraction), a `URL` as its
/// absolute string, a `UUID` as its string. The runtime encodes a
/// presentation and decodes a command through here too, so what crosses
/// to Android is the same whichever side wrote it.
public enum ValueCoding {
    /// `userInfo` reaches every `encode(to:)` (a `KeyedList` finds where
    /// it goes there).
    public static func encode(_ value: some Encodable, userInfo: [CodingUserInfoKey: any Sendable] = [:]) throws -> JSONValue {
        try ValueEncoder.box(value, codingPath: [], userInfo: userInfo).resolved()
    }

    public static func decode<T: Decodable>(_ type: T.Type, from value: JSONValue) throws -> T {
        try ValueDecoder(value: value, codingPath: []).unbox(type)
    }
}

extension ValueCoding {
    /// Whole seconds where the instant has no fraction, so a presenter's
    /// instant reads as it was written; milliseconds otherwise.
    static func timestamp(_ date: Date) -> String {
        let seconds = date.timeIntervalSince1970
        return Timestamp.format(date, precision: seconds == seconds.rounded(.down) ? .seconds : .milliseconds)
    }
}

// MARK: - Encoding

/// A value being written: a container's slot, filled once or by its container.
private final class Slot {
    let userInfo: [CodingUserInfoKey: any Sendable]

    init(userInfo: [CodingUserInfoKey: any Sendable]) {
        self.userInfo = userInfo
    }

    enum Content {
        case empty
        case value(JSONValue)
        case object([String: Slot])
        case array([Slot])
    }

    var content = Content.empty

    func resolved() throws -> JSONValue {
        switch content {
        // An encode(to:) that wrote nothing: JSONEncoder writes an empty object.
        case .empty: return .object(JSONObject())
        case let .value(value): return value
        case let .object(slots):
            return try .object(JSONObject(slots.keys.sorted().map { ($0, try slots[$0]!.resolved()) }))
        case let .array(slots):
            return try .array(slots.map { try $0.resolved() })
        }
    }

    func objectSlot(_ key: String) -> Slot {
        var slots: [String: Slot] = [:]
        if case let .object(existing) = content { slots = existing }
        let slot = slots[key] ?? Slot(userInfo: userInfo)
        slots[key] = slot
        content = .object(slots)
        return slot
    }

    func appendSlot() -> Slot {
        var slots: [Slot] = []
        if case let .array(existing) = content { slots = existing }
        let slot = Slot(userInfo: userInfo)
        slots.append(slot)
        content = .array(slots)
        return slot
    }
}

private struct AnyKey: CodingKey {
    var stringValue: String
    var intValue: Int?

    init(stringValue: String) {
        self.stringValue = stringValue
    }

    init(intValue: Int) {
        stringValue = "\(intValue)"
        self.intValue = intValue
    }

    static let `super` = AnyKey(stringValue: "super")
}

private struct ValueEncoder: Encoder {
    let slot: Slot
    let codingPath: [any CodingKey]
    var userInfo: [CodingUserInfoKey: Any] { slot.userInfo }

    func container<Key: CodingKey>(keyedBy _: Key.Type) -> KeyedEncodingContainer<Key> {
        if case .empty = slot.content { slot.content = .object([:]) }
        return KeyedEncodingContainer(KeyedEncoder<Key>(slot: slot, codingPath: codingPath))
    }

    func unkeyedContainer() -> any UnkeyedEncodingContainer {
        if case .empty = slot.content { slot.content = .array([]) }
        return UnkeyedEncoder(slot: slot, codingPath: codingPath)
    }

    func singleValueContainer() -> any SingleValueEncodingContainer {
        SingleEncoder(slot: slot, codingPath: codingPath)
    }

    /// One value into a new slot, the types JSONEncoder treats apart treated
    /// as it does, and a `JSONValue` kept as it is.
    static func box(_ value: some Encodable, codingPath: [any CodingKey], userInfo: [CodingUserInfoKey: any Sendable]) throws -> Slot {
        let slot = Slot(userInfo: userInfo)
        switch value {
        case let json as JSONValue: slot.content = .value(json)
        case let object as JSONObject: slot.content = .value(.object(object))
        case let decimal as Decimal:
            guard !decimal.isNaN else {
                throw EncodingError.invalidValue(decimal, .init(codingPath: codingPath, debugDescription: "NaN is no JSON number"))
            }
            slot.content = .value(.number(JSONNumber(decimal)))
        case let url as URL: slot.content = .value(.string(url.absoluteString))
        case let date as Date: slot.content = .value(.string(ValueCoding.timestamp(date)))
        case let uuid as UUID: slot.content = .value(.string(uuid.uuidString))
        case let data as Data: slot.content = .value(.string(data.base64EncodedString()))
        default: try value.encode(to: ValueEncoder(slot: slot, codingPath: codingPath))
        }
        return slot
    }

    static func number(_ value: Double, _ codingPath: [any CodingKey]) throws -> JSONValue {
        guard let number = JSONNumber(exactly: value) else {
            throw EncodingError.invalidValue(value, .init(codingPath: codingPath, debugDescription: "\(value) is no JSON number"))
        }
        return .number(number)
    }

    static func number(_ value: some BinaryInteger) -> JSONValue {
        .number(JSONNumber(String(value))!)
    }
}

private struct KeyedEncoder<Key: CodingKey>: KeyedEncodingContainerProtocol {
    let slot: Slot
    let codingPath: [any CodingKey]

    private func set(_ value: JSONValue, _ key: Key) {
        slot.objectSlot(key.stringValue).content = .value(value)
    }

    mutating func encodeNil(forKey key: Key) throws { set(.null, key) }
    mutating func encode(_ value: Bool, forKey key: Key) throws { set(.bool(value), key) }
    mutating func encode(_ value: String, forKey key: Key) throws { set(.string(value), key) }
    mutating func encode(_ value: Double, forKey key: Key) throws { set(try ValueEncoder.number(value, codingPath + [key]), key) }
    mutating func encode(_ value: Float, forKey key: Key) throws {
        guard let number = JSONNumber("\(value)") else { throw EncodingError.invalidValue(value, .init(codingPath: codingPath + [key], debugDescription: "no JSON number")) }
        set(.number(number), key)
    }

    mutating func encode(_ value: Int, forKey key: Key) throws { set(ValueEncoder.number(value), key) }
    mutating func encode(_ value: Int8, forKey key: Key) throws { set(ValueEncoder.number(value), key) }
    mutating func encode(_ value: Int16, forKey key: Key) throws { set(ValueEncoder.number(value), key) }
    mutating func encode(_ value: Int32, forKey key: Key) throws { set(ValueEncoder.number(value), key) }
    mutating func encode(_ value: Int64, forKey key: Key) throws { set(ValueEncoder.number(value), key) }
    mutating func encode(_ value: UInt, forKey key: Key) throws { set(ValueEncoder.number(value), key) }
    mutating func encode(_ value: UInt8, forKey key: Key) throws { set(ValueEncoder.number(value), key) }
    mutating func encode(_ value: UInt16, forKey key: Key) throws { set(ValueEncoder.number(value), key) }
    mutating func encode(_ value: UInt32, forKey key: Key) throws { set(ValueEncoder.number(value), key) }
    mutating func encode(_ value: UInt64, forKey key: Key) throws { set(ValueEncoder.number(value), key) }

    mutating func encode(_ value: some Encodable, forKey key: Key) throws {
        let boxed = try ValueEncoder.box(value, codingPath: codingPath + [key], userInfo: slot.userInfo)
        slot.objectSlot(key.stringValue).content = boxed.content
    }

    mutating func nestedContainer<NestedKey: CodingKey>(keyedBy _: NestedKey.Type, forKey key: Key) -> KeyedEncodingContainer<NestedKey> {
        let nested = slot.objectSlot(key.stringValue)
        nested.content = .object([:])
        return KeyedEncodingContainer(KeyedEncoder<NestedKey>(slot: nested, codingPath: codingPath + [key]))
    }

    mutating func nestedUnkeyedContainer(forKey key: Key) -> any UnkeyedEncodingContainer {
        let nested = slot.objectSlot(key.stringValue)
        nested.content = .array([])
        return UnkeyedEncoder(slot: nested, codingPath: codingPath + [key])
    }

    mutating func superEncoder() -> any Encoder {
        ValueEncoder(slot: slot.objectSlot(AnyKey.super.stringValue), codingPath: codingPath + [AnyKey.super])
    }

    mutating func superEncoder(forKey key: Key) -> any Encoder {
        ValueEncoder(slot: slot.objectSlot(key.stringValue), codingPath: codingPath + [key])
    }
}

private struct UnkeyedEncoder: UnkeyedEncodingContainer {
    let slot: Slot
    let codingPath: [any CodingKey]

    var count: Int {
        if case let .array(slots) = slot.content { return slots.count }
        return 0
    }

    private var nextKey: AnyKey { AnyKey(intValue: count) }

    private func append(_ value: JSONValue) {
        slot.appendSlot().content = .value(value)
    }

    mutating func encodeNil() throws { append(.null) }
    mutating func encode(_ value: Bool) throws { append(.bool(value)) }
    mutating func encode(_ value: String) throws { append(.string(value)) }
    mutating func encode(_ value: Double) throws { append(try ValueEncoder.number(value, codingPath + [nextKey])) }
    mutating func encode(_ value: Float) throws {
        guard let number = JSONNumber("\(value)") else { throw EncodingError.invalidValue(value, .init(codingPath: codingPath, debugDescription: "no JSON number")) }
        append(.number(number))
    }

    mutating func encode(_ value: Int) throws { append(ValueEncoder.number(value)) }
    mutating func encode(_ value: Int8) throws { append(ValueEncoder.number(value)) }
    mutating func encode(_ value: Int16) throws { append(ValueEncoder.number(value)) }
    mutating func encode(_ value: Int32) throws { append(ValueEncoder.number(value)) }
    mutating func encode(_ value: Int64) throws { append(ValueEncoder.number(value)) }
    mutating func encode(_ value: UInt) throws { append(ValueEncoder.number(value)) }
    mutating func encode(_ value: UInt8) throws { append(ValueEncoder.number(value)) }
    mutating func encode(_ value: UInt16) throws { append(ValueEncoder.number(value)) }
    mutating func encode(_ value: UInt32) throws { append(ValueEncoder.number(value)) }
    mutating func encode(_ value: UInt64) throws { append(ValueEncoder.number(value)) }

    mutating func encode(_ value: some Encodable) throws {
        let boxed = try ValueEncoder.box(value, codingPath: codingPath + [nextKey], userInfo: slot.userInfo)
        slot.appendSlot().content = boxed.content
    }

    mutating func nestedContainer<NestedKey: CodingKey>(keyedBy _: NestedKey.Type) -> KeyedEncodingContainer<NestedKey> {
        let key = nextKey
        let nested = slot.appendSlot()
        nested.content = .object([:])
        return KeyedEncodingContainer(KeyedEncoder<NestedKey>(slot: nested, codingPath: codingPath + [key]))
    }

    mutating func nestedUnkeyedContainer() -> any UnkeyedEncodingContainer {
        let key = nextKey
        let nested = slot.appendSlot()
        nested.content = .array([])
        return UnkeyedEncoder(slot: nested, codingPath: codingPath + [key])
    }

    mutating func superEncoder() -> any Encoder {
        let key = nextKey
        return ValueEncoder(slot: slot.appendSlot(), codingPath: codingPath + [key])
    }
}

private struct SingleEncoder: SingleValueEncodingContainer {
    let slot: Slot
    let codingPath: [any CodingKey]

    private func set(_ value: JSONValue) {
        slot.content = .value(value)
    }

    mutating func encodeNil() throws { set(.null) }
    mutating func encode(_ value: Bool) throws { set(.bool(value)) }
    mutating func encode(_ value: String) throws { set(.string(value)) }
    mutating func encode(_ value: Double) throws { set(try ValueEncoder.number(value, codingPath)) }
    mutating func encode(_ value: Float) throws {
        guard let number = JSONNumber("\(value)") else { throw EncodingError.invalidValue(value, .init(codingPath: codingPath, debugDescription: "no JSON number")) }
        set(.number(number))
    }

    mutating func encode(_ value: Int) throws { set(ValueEncoder.number(value)) }
    mutating func encode(_ value: Int8) throws { set(ValueEncoder.number(value)) }
    mutating func encode(_ value: Int16) throws { set(ValueEncoder.number(value)) }
    mutating func encode(_ value: Int32) throws { set(ValueEncoder.number(value)) }
    mutating func encode(_ value: Int64) throws { set(ValueEncoder.number(value)) }
    mutating func encode(_ value: UInt) throws { set(ValueEncoder.number(value)) }
    mutating func encode(_ value: UInt8) throws { set(ValueEncoder.number(value)) }
    mutating func encode(_ value: UInt16) throws { set(ValueEncoder.number(value)) }
    mutating func encode(_ value: UInt32) throws { set(ValueEncoder.number(value)) }
    mutating func encode(_ value: UInt64) throws { set(ValueEncoder.number(value)) }

    mutating func encode(_ value: some Encodable) throws {
        slot.content = try ValueEncoder.box(value, codingPath: codingPath, userInfo: slot.userInfo).content
    }
}

// MARK: - Decoding

private struct ValueDecoder: Decoder {
    let value: JSONValue
    let codingPath: [any CodingKey]
    var userInfo: [CodingUserInfoKey: Any] { [:] }

    func container<Key: CodingKey>(keyedBy _: Key.Type) throws -> KeyedDecodingContainer<Key> {
        guard case let .object(object) = value else { throw mismatch([String: JSONValue].self) }
        return KeyedDecodingContainer(KeyedDecoder<Key>(object: object, codingPath: codingPath))
    }

    func unkeyedContainer() throws -> any UnkeyedDecodingContainer {
        guard case let .array(items) = value else { throw mismatch([JSONValue].self) }
        return UnkeyedDecoder(items: items, codingPath: codingPath)
    }

    func singleValueContainer() throws -> any SingleValueDecodingContainer {
        SingleDecoder(value: value, codingPath: codingPath)
    }

    func mismatch(_ type: Any.Type) -> DecodingError {
        if case .null = value {
            return .valueNotFound(type, .init(codingPath: codingPath, debugDescription: "null where a value was expected"))
        }
        return .typeMismatch(type, .init(codingPath: codingPath, debugDescription: "not a \(type)"))
    }

    func corrupted(_ what: String) -> DecodingError {
        .dataCorrupted(.init(codingPath: codingPath, debugDescription: what))
    }

    func unbox<T: Decodable>(_ type: T.Type) throws -> T {
        if type == JSONValue.self { return value as! T }
        if type == JSONObject.self {
            guard case let .object(object) = value else { throw mismatch(type) }
            return object as! T
        }
        if type == Decimal.self {
            guard case let .number(number) = value else { throw mismatch(type) }
            guard let decimal = number.decimal else { throw corrupted("\(number) does not fit a Decimal") }
            return decimal as! T
        }
        if type == URL.self {
            guard case let .string(text) = value else { throw mismatch(type) }
            guard let url = URL(string: text) else { throw corrupted("not a URL") }
            return url as! T
        }
        if type == Date.self {
            guard case let .string(text) = value else { throw mismatch(type) }
            guard let date = Timestamp.parse(text) else { throw corrupted("not an RFC 3339 instant") }
            return date as! T
        }
        if type == UUID.self {
            guard case let .string(text) = value else { throw mismatch(type) }
            guard let uuid = UUID(uuidString: text) else { throw corrupted("not a UUID") }
            return uuid as! T
        }
        if type == Data.self {
            guard case let .string(text) = value else { throw mismatch(type) }
            guard let data = Data(base64Encoded: text) else { throw corrupted("not base64") }
            return data as! T
        }
        return try T(from: self)
    }

    func bool() throws -> Bool {
        guard case let .bool(flag) = value else { throw mismatch(Bool.self) }
        return flag
    }

    func string() throws -> String {
        guard case let .string(text) = value else { throw mismatch(String.self) }
        return text
    }

    func double() throws -> Double {
        guard case let .number(number) = value else { throw mismatch(Double.self) }
        guard let double = number.double else { throw corrupted("\(number) does not fit a Double") }
        return double
    }

    func float() throws -> Float {
        let double = try double()
        guard let float = Float(exactly: double) ?? Float(String(double)), float.isFinite else { throw corrupted("does not fit a Float") }
        return float
    }

    func integer<I: FixedWidthInteger>(_: I.Type) throws -> I {
        guard case let .number(number) = value else { throw mismatch(I.self) }
        if let int = number.exactInt, let fitted = I(exactly: int) { return fitted }
        // Past Int: only an unsigned type may hold it, read from its digits.
        if let fitted = I(number.description) { return fitted }
        throw corrupted("\(number) does not fit \(I.self)")
    }
}

private struct KeyedDecoder<Key: CodingKey>: KeyedDecodingContainerProtocol {
    let object: JSONObject
    let codingPath: [any CodingKey]

    var allKeys: [Key] { object.keys.compactMap { Key(stringValue: $0) } }

    func contains(_ key: Key) -> Bool { object[key.stringValue] != nil }

    private func child(_ key: Key) throws -> ValueDecoder {
        guard let value = object[key.stringValue] else {
            throw DecodingError.keyNotFound(key, .init(codingPath: codingPath, debugDescription: "no value for \(key.stringValue)"))
        }
        return ValueDecoder(value: value, codingPath: codingPath + [key])
    }

    func decodeNil(forKey key: Key) throws -> Bool {
        if case .null = try child(key).value { return true }
        return false
    }

    func decode(_: Bool.Type, forKey key: Key) throws -> Bool { try child(key).bool() }
    func decode(_: String.Type, forKey key: Key) throws -> String { try child(key).string() }
    func decode(_: Double.Type, forKey key: Key) throws -> Double { try child(key).double() }
    func decode(_: Float.Type, forKey key: Key) throws -> Float { try child(key).float() }
    func decode(_: Int.Type, forKey key: Key) throws -> Int { try child(key).integer(Int.self) }
    func decode(_: Int8.Type, forKey key: Key) throws -> Int8 { try child(key).integer(Int8.self) }
    func decode(_: Int16.Type, forKey key: Key) throws -> Int16 { try child(key).integer(Int16.self) }
    func decode(_: Int32.Type, forKey key: Key) throws -> Int32 { try child(key).integer(Int32.self) }
    func decode(_: Int64.Type, forKey key: Key) throws -> Int64 { try child(key).integer(Int64.self) }
    func decode(_: UInt.Type, forKey key: Key) throws -> UInt { try child(key).integer(UInt.self) }
    func decode(_: UInt8.Type, forKey key: Key) throws -> UInt8 { try child(key).integer(UInt8.self) }
    func decode(_: UInt16.Type, forKey key: Key) throws -> UInt16 { try child(key).integer(UInt16.self) }
    func decode(_: UInt32.Type, forKey key: Key) throws -> UInt32 { try child(key).integer(UInt32.self) }
    func decode(_: UInt64.Type, forKey key: Key) throws -> UInt64 { try child(key).integer(UInt64.self) }
    func decode<T: Decodable>(_ type: T.Type, forKey key: Key) throws -> T { try child(key).unbox(type) }

    func nestedContainer<NestedKey: CodingKey>(keyedBy type: NestedKey.Type, forKey key: Key) throws -> KeyedDecodingContainer<NestedKey> {
        try child(key).container(keyedBy: type)
    }

    func nestedUnkeyedContainer(forKey key: Key) throws -> any UnkeyedDecodingContainer {
        try child(key).unkeyedContainer()
    }

    func superDecoder() throws -> any Decoder {
        ValueDecoder(value: object[AnyKey.super.stringValue] ?? .null, codingPath: codingPath + [AnyKey.super])
    }

    func superDecoder(forKey key: Key) throws -> any Decoder {
        ValueDecoder(value: object[key.stringValue] ?? .null, codingPath: codingPath + [key])
    }
}

private struct UnkeyedDecoder: UnkeyedDecodingContainer {
    let items: [JSONValue]
    let codingPath: [any CodingKey]
    var currentIndex = 0

    init(items: [JSONValue], codingPath: [any CodingKey]) {
        self.items = items
        self.codingPath = codingPath
    }

    var count: Int? { items.count }
    var isAtEnd: Bool { currentIndex >= items.count }

    private mutating func next(_ type: Any.Type) throws -> ValueDecoder {
        let key = AnyKey(intValue: currentIndex)
        guard !isAtEnd else {
            throw DecodingError.valueNotFound(type, .init(codingPath: codingPath + [key], debugDescription: "the array ended"))
        }
        defer { currentIndex += 1 }
        return ValueDecoder(value: items[currentIndex], codingPath: codingPath + [key])
    }

    mutating func decodeNil() throws -> Bool {
        guard !isAtEnd else { throw DecodingError.valueNotFound(JSONValue.self, .init(codingPath: codingPath, debugDescription: "the array ended")) }
        if case .null = items[currentIndex] {
            currentIndex += 1
            return true
        }
        return false
    }

    mutating func decode(_: Bool.Type) throws -> Bool { try next(Bool.self).bool() }
    mutating func decode(_: String.Type) throws -> String { try next(String.self).string() }
    mutating func decode(_: Double.Type) throws -> Double { try next(Double.self).double() }
    mutating func decode(_: Float.Type) throws -> Float { try next(Float.self).float() }
    mutating func decode(_: Int.Type) throws -> Int { try next(Int.self).integer(Int.self) }
    mutating func decode(_: Int8.Type) throws -> Int8 { try next(Int8.self).integer(Int8.self) }
    mutating func decode(_: Int16.Type) throws -> Int16 { try next(Int16.self).integer(Int16.self) }
    mutating func decode(_: Int32.Type) throws -> Int32 { try next(Int32.self).integer(Int32.self) }
    mutating func decode(_: Int64.Type) throws -> Int64 { try next(Int64.self).integer(Int64.self) }
    mutating func decode(_: UInt.Type) throws -> UInt { try next(UInt.self).integer(UInt.self) }
    mutating func decode(_: UInt8.Type) throws -> UInt8 { try next(UInt8.self).integer(UInt8.self) }
    mutating func decode(_: UInt16.Type) throws -> UInt16 { try next(UInt16.self).integer(UInt16.self) }
    mutating func decode(_: UInt32.Type) throws -> UInt32 { try next(UInt32.self).integer(UInt32.self) }
    mutating func decode(_: UInt64.Type) throws -> UInt64 { try next(UInt64.self).integer(UInt64.self) }
    mutating func decode<T: Decodable>(_ type: T.Type) throws -> T { try next(type).unbox(type) }

    mutating func nestedContainer<NestedKey: CodingKey>(keyedBy type: NestedKey.Type) throws -> KeyedDecodingContainer<NestedKey> {
        try next(type).container(keyedBy: type)
    }

    mutating func nestedUnkeyedContainer() throws -> any UnkeyedDecodingContainer {
        try next([JSONValue].self).unkeyedContainer()
    }

    mutating func superDecoder() throws -> any Decoder {
        try next(JSONValue.self)
    }
}

private struct SingleDecoder: SingleValueDecodingContainer {
    let value: JSONValue
    let codingPath: [any CodingKey]

    private var decoder: ValueDecoder { ValueDecoder(value: value, codingPath: codingPath) }

    func decodeNil() -> Bool {
        if case .null = value { return true }
        return false
    }

    func decode(_: Bool.Type) throws -> Bool { try decoder.bool() }
    func decode(_: String.Type) throws -> String { try decoder.string() }
    func decode(_: Double.Type) throws -> Double { try decoder.double() }
    func decode(_: Float.Type) throws -> Float { try decoder.float() }
    func decode(_: Int.Type) throws -> Int { try decoder.integer(Int.self) }
    func decode(_: Int8.Type) throws -> Int8 { try decoder.integer(Int8.self) }
    func decode(_: Int16.Type) throws -> Int16 { try decoder.integer(Int16.self) }
    func decode(_: Int32.Type) throws -> Int32 { try decoder.integer(Int32.self) }
    func decode(_: Int64.Type) throws -> Int64 { try decoder.integer(Int64.self) }
    func decode(_: UInt.Type) throws -> UInt { try decoder.integer(UInt.self) }
    func decode(_: UInt8.Type) throws -> UInt8 { try decoder.integer(UInt8.self) }
    func decode(_: UInt16.Type) throws -> UInt16 { try decoder.integer(UInt16.self) }
    func decode(_: UInt32.Type) throws -> UInt32 { try decoder.integer(UInt32.self) }
    func decode(_: UInt64.Type) throws -> UInt64 { try decoder.integer(UInt64.self) }
    func decode<T: Decodable>(_ type: T.Type) throws -> T { try decoder.unbox(type) }
}
