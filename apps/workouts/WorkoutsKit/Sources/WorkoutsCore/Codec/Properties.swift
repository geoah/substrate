import Foundation

/// Reads one record's properties, or one object's fields, by declared type
/// (§3.4 rule 6). Every read marks its name as known; at every level what
/// is left over, the raw value of an unknown optional enum and a reference
/// read in its object form become the model's `extra` (rule 7), so an older
/// build writes back what a newer one added.
struct PropertyReader {
    let kind: String
    let id: String
    private let path: String
    private let object: JSONObject
    private var consumed: Set<String> = []
    private var unknownValues: [String: JSONValue] = [:]

    init(record: FlatRecord) {
        self.init(kind: record.kind, id: record.id, path: "", object: record.properties)
    }

    private init(kind: String, id: String, path: String, object: JSONObject) {
        self.kind = kind
        self.id = id
        self.path = path
        self.object = object
    }

    /// Unknown properties, unknown enum values and references read as
    /// objects, for the model's `extra`. A record's `title` is dropped: the
    /// server renders it (rule 3).
    var extra: [String: JSONValue] {
        var out = unknownValues
        for (key, value) in object where !consumed.contains(key) && !(path.isEmpty && key == "title") {
            out[key] = value
        }
        return out
    }

    // MARK: Errors

    func invalid(_ name: String, _ value: JSONValue) -> CodecError {
        .invalid(kind: kind, id: id, property: path + name, value: Self.render(value))
    }

    func unsupported(_ name: String, _ value: JSONValue?) -> CodecError {
        .unsupported(kind: kind, id: id, property: path + name, value: value.map(Self.render))
    }

    /// A string as its text, anything else as compact JSON.
    private static func render(_ value: JSONValue) -> String {
        if case let .string(text) = value { return text }
        return String(decoding: value.data(pretty: false), as: UTF8.self)
    }

    /// The value of `name`; JSON null reads as absent.
    private mutating func take(_ name: String) -> JSONValue? {
        consumed.insert(name)
        guard let value = object[name], value != .null else { return nil }
        return value
    }

    private mutating func list(_ name: String) throws(CodecError) -> [JSONValue] {
        guard let value = take(name) else { return [] }
        guard case let .array(items) = value else { throw invalid(name, value) }
        return items
    }

    // MARK: Scalars

    mutating func string(_ name: String) throws(CodecError) -> String? {
        guard let value = take(name) else { return nil }
        guard case let .string(text) = value else { throw invalid(name, value) }
        return text
    }

    mutating func requiredString(_ name: String) throws(CodecError) -> String {
        guard let text = try string(name) else { throw unsupported(name, nil) }
        return text
    }

    mutating func strings(_ name: String) throws(CodecError) -> [String] {
        var out: [String] = []
        for item in try list(name) {
            guard case let .string(text) = item else { throw invalid(name, item) }
            out.append(text)
        }
        return out
    }

    /// A JSON number with no fraction.
    mutating func int(_ name: String) throws(CodecError) -> Int? {
        guard let value = take(name) else { return nil }
        guard case let .number(number) = value, let integer = number.exactInt else { throw invalid(name, value) }
        return integer
    }

    mutating func requiredInt(_ name: String) throws(CodecError) -> Int {
        guard let integer = try int(name) else { throw unsupported(name, nil) }
        return integer
    }

    mutating func bool(_ name: String) throws(CodecError) -> Bool? {
        guard let value = take(name) else { return nil }
        guard case let .bool(flag) = value else { throw invalid(name, value) }
        return flag
    }

    /// A string of plain digits; a JSON number is refused, as substrate does.
    mutating func decimal(_ name: String) throws(CodecError) -> Decimal? {
        guard let value = take(name) else { return nil }
        guard case let .string(text) = value, let number = DecimalText.parse(text) else { throw invalid(name, value) }
        return number
    }

    mutating func duration(_ name: String) throws(CodecError) -> Seconds? {
        guard let value = take(name) else { return nil }
        guard case let .string(text) = value, let seconds = ISODuration.parse(text) else { throw invalid(name, value) }
        return seconds
    }

    mutating func datetime(_ name: String) throws(CodecError) -> Date? {
        guard let value = take(name) else { return nil }
        guard case let .string(text) = value, let date = DateText.parse(text) else { throw invalid(name, value) }
        return date
    }

    mutating func requiredDatetime(_ name: String) throws(CodecError) -> Date {
        guard let date = try datetime(name) else { throw unsupported(name, nil) }
        return date
    }

    // MARK: Quantities

    private mutating func quantity(_ name: String, units: [String]) throws(CodecError) -> (Decimal, String)? {
        guard let value = take(name) else { return nil }
        guard case let .object(fields) = value, fields.count == 2,
              case let .string(text)? = fields["value"], let number = DecimalText.parse(text),
              case let .string(unit)? = fields["unit"], units.contains(unit)
        else { throw invalid(name, value) }
        return (number, unit)
    }

    mutating func mass(_ name: String) throws(CodecError) -> Mass? {
        guard let (value, unit) = try quantity(name, units: ["kg", "lb"]) else { return nil }
        return Mass(value: value, unit: MassUnit(rawValue: unit)!)
    }

    mutating func length(_ name: String) throws(CodecError) -> Length? {
        guard let (value, _) = try quantity(name, units: ["m"]) else { return nil }
        return Length(metres: value)
    }

    // MARK: Enums

    /// An optional enum. An unknown value reads as nil and is kept raw.
    mutating func enumeration<E: RawRepresentable>(_ name: String) throws(CodecError) -> E? where E.RawValue == String {
        guard let value = take(name) else { return nil }
        guard case let .string(text) = value else { throw invalid(name, value) }
        guard let known = E(rawValue: text) else {
            unknownValues[name] = value
            return nil
        }
        return known
    }

    /// An enum that decides how the engine runs a set (`role`, `structure`,
    /// `termination`, `timerMode`, `myoStopRule`, `loadReference`,
    /// `sideOrder`): an unknown value makes the record unsupported rather
    /// than read as a default, so a plan this build cannot run is never run
    /// as a straight set (rule 7).
    mutating func executionEnumeration<E: RawRepresentable>(_ name: String) throws(CodecError) -> E? where E.RawValue == String {
        guard let value = take(name) else { return nil }
        guard case let .string(text) = value else { throw invalid(name, value) }
        guard let known = E(rawValue: text) else { throw unsupported(name, value) }
        return known
    }

    /// The same for an enum with a declared default: absent reads as the default.
    mutating func executionEnumeration<E: RawRepresentable>(_ name: String, default fallback: E) throws(CodecError) -> E where E.RawValue == String {
        try executionEnumeration(name) ?? fallback
    }

    /// A required enum: missing or unknown makes the record undecodable.
    mutating func requiredEnumeration<E: RawRepresentable>(_ name: String) throws(CodecError) -> E where E.RawValue == String {
        guard let value = take(name) else { throw unsupported(name, nil) }
        guard case let .string(text) = value else { throw invalid(name, value) }
        guard let known = E(rawValue: text) else { throw unsupported(name, value) }
        return known
    }

    /// An enum with a declared default (`role`, `structure`) or an initial
    /// state (`status`): absent reads as the default; an unknown value too,
    /// kept raw.
    mutating func enumeration<E: RawRepresentable>(_ name: String, default fallback: E) throws(CodecError) -> E where E.RawValue == String {
        try enumeration(name) ?? fallback
    }

    /// A repeated enum. Unknown members are left out of the typed list; the
    /// raw list is kept whole and written back while the known part is unchanged.
    mutating func enumerations<E: RawRepresentable>(_ name: String) throws(CodecError) -> [E] where E.RawValue == String {
        let items = try list(name)
        var out: [E] = []
        for item in items {
            guard case let .string(text) = item else { throw invalid(name, item) }
            if let known = E(rawValue: text) { out.append(known) }
        }
        if out.count < items.count { unknownValues[name] = .array(items) }
        return out
    }

    // MARK: References

    /// A reference's path, `"<kind>/<id>"`: from the object substrate serves,
    /// `{"ref": "<kind>/<id>", …}`, or from the bare path a write may send.
    /// An object is kept raw, so it is written back as it was read, link
    /// fields and all, while it still points at the same record (rule 7).
    private mutating func referencePath(_ name: String) throws(CodecError) -> (path: String, raw: JSONValue)? {
        guard let value = take(name) else { return nil }
        switch value {
        case let .string(text):
            return (text, value)
        case let .object(fields):
            guard case let .string(text)? = fields["ref"] else { throw invalid(name, value) }
            unknownValues[name] = value
            return (text, value)
        default:
            throw invalid(name, value)
        }
    }

    /// The id of a reference to `kind`: the path less `"<kind>/"`. The id may
    /// hold `/`. A reference to another kind is refused.
    mutating func reference(_ name: String, kind referenced: String) throws(CodecError) -> String? {
        guard let (text, raw) = try referencePath(name) else { return nil }
        let prefix = referenced + "/"
        guard text.hasPrefix(prefix), text.count > prefix.count else { throw invalid(name, raw) }
        return String(text.dropFirst(prefix.count))
    }

    mutating func requiredReference(_ name: String, kind referenced: String) throws(CodecError) -> String {
        guard let id = try reference(name, kind: referenced) else { throw unsupported(name, nil) }
        return id
    }

    /// A reference to a kind the package does not own. With a declared kind
    /// it splits at that kind; for `kind: any`, at the third `/`
    /// (`authority/package/name/id`).
    mutating func recordRef(_ name: String, kind declared: String?) throws(CodecError) -> RecordRef? {
        guard let (text, raw) = try referencePath(name) else { return nil }
        if let declared {
            let prefix = declared + "/"
            guard text.hasPrefix(prefix), text.count > prefix.count else { throw invalid(name, raw) }
            return RecordRef(kind: declared, id: String(text.dropFirst(prefix.count)))
        }
        let slashes = text.indices.filter { text[$0] == "/" }
        guard slashes.count >= 3 else { throw invalid(name, raw) }
        let kindPart = text[..<slashes[2]]
        let idPart = text[text.index(after: slashes[2])...]
        guard !idPart.isEmpty, !kindPart.split(separator: "/", omittingEmptySubsequences: false).contains(where: \.isEmpty) else {
            throw invalid(name, raw)
        }
        return RecordRef(kind: String(kindPart), id: String(idPart))
    }

    // MARK: Objects

    mutating func object<T>(_ name: String, _ read: (inout PropertyReader) throws(CodecError) -> T) throws(CodecError) -> T? {
        guard let value = take(name) else { return nil }
        guard case let .object(fields) = value else { throw invalid(name, value) }
        var nested = PropertyReader(kind: kind, id: id, path: path + name + ".", object: fields)
        return try read(&nested)
    }

    mutating func objects<T>(_ name: String, _ read: (inout PropertyReader) throws(CodecError) -> T) throws(CodecError) -> [T] {
        var out: [T] = []
        for (index, item) in try list(name).enumerated() {
            guard case let .object(fields) = item else { throw invalid(name, item) }
            var nested = PropertyReader(kind: kind, id: id, path: "\(path)\(name)[\(index)].", object: fields)
            try out.append(read(&nested))
        }
        return out
    }
}

/// Writes one record's properties, or one object's fields, by declared type,
/// in declaration order (§3.4 rule 8). Nil and empty lists are left out
/// (rule 3). A property the model holds no value for falls back to the raw
/// value `extra` kept for it (an unknown enum value), except a reference,
/// whose typed value is the whole truth; unknown properties follow the
/// declared ones, sorted.
struct PropertyWriter {
    private let schema: [PropertySchema]
    private let extra: [String: JSONValue]
    private var values: [String: JSONValue] = [:]
    /// Written as absent with no fallback to `extra`.
    private var writtenAbsent: Set<String> = []

    init(schema: [PropertySchema], extra: [String: JSONValue] = [:]) {
        self.schema = schema
        self.extra = extra
    }

    func finish() -> JSONObject {
        var out = JSONObject()
        var declared = Set<String>()
        for property in schema {
            declared.insert(property.name)
            if let value = values[property.name] ?? (writtenAbsent.contains(property.name) ? nil : extra[property.name]) {
                out[property.name] = value
            }
        }
        assert(Set(values.keys).isSubset(of: declared), "wrote an undeclared property: \(values.keys.sorted())")
        for key in extra.keys.sorted() where !declared.contains(key) {
            out[key] = extra[key]
        }
        return out
    }

    private mutating func put(_ name: String, _ value: JSONValue?) {
        values[name] = value
    }

    mutating func string(_ name: String, _ value: String?) { put(name, value.map(JSONValue.string)) }

    mutating func strings(_ name: String, _ value: [String]) {
        put(name, value.isEmpty ? nil : .array(value.map(JSONValue.string)))
    }

    mutating func int(_ name: String, _ value: Int?) { put(name, value.map { .number(Decimal($0)) }) }
    mutating func bool(_ name: String, _ value: Bool?) { put(name, value.map(JSONValue.bool)) }
    mutating func decimal(_ name: String, _ value: Decimal?) { put(name, value.map { .string(DecimalText.format($0)) }) }
    mutating func duration(_ name: String, _ value: Seconds?) { put(name, value.map { .string(ISODuration.format($0)) }) }
    mutating func datetime(_ name: String, _ value: Date?) { put(name, value.map { .string(DateText.format($0)) }) }

    mutating func mass(_ name: String, _ value: Mass?) {
        put(name, value.map { Self.quantity($0.value, $0.unit.rawValue) })
    }

    mutating func length(_ name: String, _ value: Length?) {
        put(name, value.map { Self.quantity($0.metres, "m") })
    }

    private static func quantity(_ value: Decimal, _ unit: String) -> JSONValue {
        .object(["value": .string(DecimalText.format(value)), "unit": .string(unit)])
    }

    mutating func enumeration<E: RawRepresentable>(_ name: String, _ value: E?) where E.RawValue == String {
        put(name, value.map { .string($0.rawValue) })
    }

    /// The same, but with `keepingRaw` false a nil leaves the property out
    /// even when `extra` kept a raw value for it: a set's `rpeMethod` once
    /// its `rpe` is cleared.
    mutating func enumeration<E: RawRepresentable>(_ name: String, _ value: E?, keepingRaw: Bool) where E.RawValue == String {
        if value == nil, !keepingRaw { writtenAbsent.insert(name) }
        put(name, value.map { .string($0.rawValue) })
    }

    /// Left out when it is the declared default (rule 4) or, for `status`,
    /// the initial state (rule 5).
    mutating func enumeration<E: RawRepresentable & Equatable>(_ name: String, _ value: E, omitting omitted: E) where E.RawValue == String {
        put(name, value == omitted ? nil : .string(value.rawValue))
    }

    mutating func enumerations<E: RawRepresentable>(_ name: String, _ value: [E]) where E.RawValue == String {
        let typed = value.map { JSONValue.string($0.rawValue) }
        // The raw list holds unknown members; write it back while its known part is what the model holds.
        if case let .array(raw)? = extra[name],
           raw.filter({ $0.stringValue.flatMap(E.init(rawValue:)) != nil }) == typed
        {
            put(name, nil)
            return
        }
        put(name, typed.isEmpty ? nil : .array(typed))
    }

    mutating func reference(_ name: String, kind: String, id: String?) {
        referencePath(name, id.map { "\(kind)/\($0)" })
    }

    mutating func recordRef(_ name: String, _ value: RecordRef?) { referencePath(name, value?.path) }

    /// The bare path, or the object it was read as while it still points at
    /// the same record (rule 7).
    private mutating func referencePath(_ name: String, _ path: String?) {
        guard let path else {
            writtenAbsent.insert(name)
            put(name, nil)
            return
        }
        if case let .object(fields)? = extra[name], fields["ref"] == .string(path) {
            put(name, extra[name])
        } else {
            put(name, .string(path))
        }
    }

    mutating func object(_ name: String, _ value: JSONObject?) {
        put(name, value.flatMap { $0.isEmpty ? nil : .object($0) })
    }

    mutating func objects(_ name: String, _ value: [JSONObject]) {
        put(name, value.isEmpty ? nil : .array(value.map(JSONValue.object)))
    }
}
