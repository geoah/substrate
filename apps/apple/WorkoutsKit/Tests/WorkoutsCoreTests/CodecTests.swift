import Foundation
@testable import WorkoutsCore
import Testing

/// The generated example records (`Fixtures/examples/*.json`) and
/// `Fixtures/kinds.json`, from `scripts/examples.py`. Shared by every test
/// file that needs the examples.
enum ExampleFixtures {
    static let files = ["exercises", "routines", "sessions", "coverage"]

    static func url(_ name: String, subdirectory: String = "Fixtures/examples") throws -> URL {
        try #require(Bundle.module.url(forResource: name, withExtension: "json", subdirectory: subdirectory), "missing fixture \(name)")
    }

    static func file(_ name: String) throws -> RecordFile {
        try JSONDecoder().decode(RecordFile.self, from: Data(contentsOf: url(name)))
    }

    static func records(_ name: String) throws -> [FlatRecord] {
        try file(name).records
    }

    static func allRecords() throws -> [FlatRecord] {
        try files.flatMap(records)
    }

    /// The finished and in-progress sessions of a fixture file with their sets.
    static func sessions(_ name: String) throws -> [SessionRecords] {
        try RecordCodec().sessionRecords(from: records(name))
    }

    static func kinds() throws -> JSONValue {
        try JSONValue(data: Data(contentsOf: url("kinds", subdirectory: "Fixtures")))
    }
}

/// A record's properties with decimals and quantity values compared as
/// numbers, durations as seconds and datetimes as instants (§3.4 rule 9).
func normalizedProperties(_ object: JSONObject, _ schema: [PropertySchema]) -> JSONValue {
    var out = JSONObject()
    for (key, value) in object {
        guard let property = schema.first(where: { $0.name == key }) else {
            out[key] = value
            continue
        }
        if property.repeated, case let .array(items) = value {
            out[key] = .array(items.map { normalizedValue($0, property) })
        } else {
            out[key] = normalizedValue(value, property)
        }
    }
    return .object(out)
}

private func normalizedValue(_ value: JSONValue, _ property: PropertySchema) -> JSONValue {
    switch (property.type, value) {
    case let (.decimal, .string(text)):
        DecimalText.parse(text).map(JSONValue.number) ?? value
    case let (.duration, .string(text)):
        ISODuration.parse(text).map { .number(Decimal($0)) } ?? value
    case let (.datetime, .string(text)):
        DateText.parse(text).map { .number(Decimal($0.timeIntervalSince1970)) } ?? value
    case let (.quantity, .object(fields)):
        if case let .string(text)? = fields["value"], let number = DecimalText.parse(text) {
            .object(["value": .number(number), "unit": fields["unit"] ?? .null])
        } else {
            value
        }
    case let (.object, .object(fields)):
        normalizedProperties(fields, property.fields)
    default:
        value
    }
}

private func roundTrip(_ codec: RecordCodec, _ record: FlatRecord) throws -> (FlatRecord, [String: JSONValue]) {
    switch try codec.decode(record) {
    case let .exercise(value): (codec.encode(value), value.extra)
    case let .routine(value): (codec.encode(value), value.extra)
    case let .session(value): (codec.encode(value), value.extra)
    case let .set(value): (codec.encode(value), value.extra)
    case let .setup(value): (codec.encode(value), value.extra)
    }
}

private func record(_ kind: String, _ id: String, _ properties: JSONObject) -> FlatRecord {
    FlatRecord(kind: kind, id: id, properties: properties)
}

private func record(_ kind: String, _ properties: JSONObject) -> FlatRecord {
    record(kind, "x", properties)
}

private let kinds = KindNames.v1

/// A minimal valid set record to vary one property at a time.
private func setProperties(_ changes: [String: JSONValue?] = [:]) -> JSONObject {
    var properties: JSONObject = [
        "at": .string("2026-10-27T13:42:10Z"),
        "session": .string("\(kinds.session)/01a17d8d-afc0-7a2b-bc4d-5e6f70819203"),
        "exercise": .string("\(kinds.exercise)/chest-press-machine"),
        "slot": .string("chest-press"),
        "order": .number(1),
    ]
    for (key, value) in changes {
        properties[key] = value
    }
    return properties
}

@Suite("Record codec: the examples")
struct CodecExampleTests {
    let codec = RecordCodec()

    @Test("every example record round-trips and the model covers every property it uses")
    func roundTripsEveryExample() throws {
        var count = 0
        for name in ExampleFixtures.files {
            for original in try ExampleFixtures.records(name) {
                count += 1
                let schema = try #require(codec.schema(for: original.kind), "\(original.kind)")
                let (encoded, extra) = try roundTrip(codec, original)
                #expect(extra.isEmpty, "\(name) \(original.id) left \(extra.keys.sorted()) in extra")
                #expect(encoded.kind == original.kind)
                #expect(encoded.id == original.id)
                #expect(
                    normalizedProperties(encoded.properties, schema.allProperties) == normalizedProperties(original.properties, schema.allProperties),
                    "\(name) \(original.id) did not round-trip"
                )
            }
        }
        #expect(count == 163)
    }

    @Test("the example files hold 36 exercises, 5 routines, 6 sessions, 111 sets and 5 setups")
    func countsByKind() throws {
        let records = try ExampleFixtures.allRecords()
        let counts = Dictionary(grouping: records, by: \.kind).mapValues(\.count)
        #expect(counts[kinds.exercise] == 36)
        #expect(counts[kinds.routine] == 5)
        #expect(counts[kinds.session] == 6)
        #expect(counts[kinds.set] == 111)
        #expect(counts[kinds.setup] == 5)
    }

    @Test("encoding writes properties in the kind file's order, trait properties first")
    func keyOrder() throws {
        for original in try ExampleFixtures.allRecords() {
            let schema = try #require(codec.schema(for: original.kind))
            let encoded = try roundTrip(codec, original).0
            let declared = schema.allProperties.map(\.name)
            #expect(encoded.properties.keys == declared.filter(encoded.properties.keys.contains), "\(original.id)")
        }
    }

    @Test("decoded models survive the journal's JSONEncoder round trip exactly")
    func modelCodable() throws {
        let encoder = JSONEncoder()
        let decoder = JSONDecoder()
        for original in try ExampleFixtures.allRecords() {
            let decoded = try codec.decode(original)
            switch decoded {
            case let .exercise(value): #expect(try decoder.decode(Exercise.self, from: encoder.encode(value)) == value)
            case let .routine(value): #expect(try decoder.decode(Routine.self, from: encoder.encode(value)) == value)
            case let .session(value): #expect(try decoder.decode(TrainingSession.self, from: encoder.encode(value)) == value)
            case let .set(value): #expect(try decoder.decode(PerformedSet.self, from: encoder.encode(value)) == value)
            case let .setup(value): #expect(try decoder.decode(Setup.self, from: encoder.encode(value)) == value)
            }
        }
    }

    @Test("sessions group with their sets in file order")
    func sessionRecords() throws {
        let sessions = try ExampleFixtures.sessions("sessions")
        #expect(sessions.map(\.session.id.rawValue) == [
            "01a16363-cf80-73ed-85fd-704f6ce38d73", "01a17831-8a38-7502-a232-81e9c175d65e", "01a17d8d-afc0-7a2b-bc4d-5e6f70819203",
        ])
        #expect(sessions.map(\.sets.count).reduce(0, +) == 65)
        for records in sessions {
            #expect(records.sets.allSatisfy { $0.session == records.session.id })
        }
        let encoded = codec.encode(sessions[2])
        #expect(encoded.first?.kind == kinds.session)
        #expect(encoded.count == sessions[2].sets.count + 1)
    }

    @Test("the Fully Body session's chest press set 2 and its drop decode field by field")
    func chestPressDrop() throws {
        let sets = try #require(ExampleFixtures.sessions("sessions").last).sets
        let head = try #require(sets.first { $0.slot == "chest-press" && $0.ordinal == 2 && $0.chainPosition == 1 })
        let drop = try #require(sets.first { $0.continues == head.id })
        #expect(head.structure == .drop)
        #expect(head.load == .kg(35))
        #expect(head.reps == 13)
        #expect(head.rpe == Decimal(string: "9.5"))
        #expect(head.loadMode == .external)
        #expect(head.target?.plannedSet == "s2")
        #expect(drop.chainPosition == 2)
        #expect(drop.reps == 8)
        #expect(drop.restBefore == 6)
        #expect(drop.restPlanned == 0)
    }

    @Test("the JSON writer's output parses back to the same value")
    func writerRoundTrip() throws {
        for name in ExampleFixtures.files {
            let value = try ExampleFixtures.file(name).json
            #expect(try JSONValue(data: value.data(pretty: true)) == value)
            #expect(try JSONValue(data: value.data(pretty: false)) == value)
        }
        let tricky: JSONValue = .object([
            "text": .string("quote \" backslash \\ newline \n tab \t bell \u{07} é ×"),
            "numbers": .array([.number(0), .number(-3), .number(Decimal(string: "83.9146")!)]),
            "empty": .object([:]), "none": .array([]), "null": .null, "flag": .bool(false),
        ])
        #expect(try JSONValue(data: tricky.data()) == tricky)
    }
}

@Suite("Record codec: drift against the kind files")
struct CodecDriftTests {
    let codec = RecordCodec()

    @Test("the codec declares exactly the kind files' properties, fields, types and values")
    func drift() throws {
        guard case let .array(kindsJSON)? = try ExampleFixtures.kinds().objectValue?["kinds"] else {
            Issue.record("kinds.json has no kinds")
            return
        }
        #expect(kindsJSON.count == 5)
        for entry in kindsJSON {
            let object = try #require(entry.objectValue)
            let kind = try #require(object["kind"]?.stringValue)
            let schema = try #require(codec.schema(for: kind), "the codec does not know \(kind)")
            let traits = object["traits"]?.arrayValue?.compactMap(\.stringValue) ?? []
            #expect(schema.traits == traits, "\(kind) traits")
            let declared = try #require(object["properties"]?.arrayValue)
            compare(schema.properties, declared, path: kind)
        }
        #expect(codec.sessionSchema.traitProperties.map(\.name) == ["at", "endsAt"])
        #expect(codec.setSchema.traitProperties.map(\.name) == ["at"])
    }

    private func compare(_ swift: [PropertySchema], _ declared: [JSONValue], path: String) {
        let declaredNames = declared.compactMap { $0.objectValue?["name"]?.stringValue }
        #expect(swift.map(\.name) == declaredNames, "\(path): property names")
        for (property, entry) in zip(swift, declared) {
            guard let fields = entry.objectValue else { continue }
            let where_ = "\(path).\(property.name)"
            #expect(property.type.rawValue == fields["type"]?.stringValue, "\(where_) type")
            #expect(property.required == (fields["required"] == .bool(true)), "\(where_) required")
            #expect(property.repeated == (fields["repeated"] == .bool(true)), "\(where_) repeated")
            #expect(property.defaultValue == fields["default"]?.stringValue, "\(where_) default")
            #expect(property.values == (fields["values"]?.arrayValue?.compactMap(\.stringValue) ?? []), "\(where_) values")
            #expect(property.referenceKind == fields["kind"]?.stringValue, "\(where_) reference kind")
            compare(property.fields, fields["fields"]?.arrayValue ?? [], path: where_)
        }
    }

    @Test("a record with every declared property set decodes with nothing left over and round-trips")
    func everyProperty() throws {
        for schema in codec.schemas {
            let properties = sample(schema.allProperties)
            let original = record(schema.kind, "sample", properties)
            let (encoded, extra) = try roundTrip(codec, original)
            #expect(extra.isEmpty, "\(schema.kind) left \(extra.keys.sorted()) unread")
            #expect(normalizedProperties(encoded.properties, schema.allProperties) == normalizedProperties(original.properties, schema.allProperties), "\(schema.kind)")
        }
    }

    /// A value of every property, none of them a default (rule 4 would omit it).
    private func sample(_ schema: [PropertySchema]) -> JSONObject {
        var out = JSONObject()
        for property in schema {
            let value: JSONValue = switch property.type {
            case .string, .text: .string("text \(property.name)")
            case .timezone: .string("Europe/London")
            case .int: .number(3)
            case .decimal: .string("7.5")
            case .bool: .bool(true)
            case .duration: .string("PT1M30S")
            case .datetime: .string("2026-10-27T13:30:00Z")
            case .quantity: property.dimension == .length
                ? .object(["value": .string("40"), "unit": .string("m")])
                : .object(["value": .string("92.5"), "unit": .string("lb")])
            case .enumeration, .state: .string(property.values.last { $0 != property.defaultValue && $0 != "active" } ?? "")
            case .reference: .string(property.referenceKind == "any" ? "substrate.reamde.dev/core/person/owner" : "\(property.referenceKind!)/ref-\(property.name)")
            case .object: .object(sample(property.fields))
            }
            out[property.name] = property.repeated ? .array([value]) : value
        }
        return out
    }
}

@Suite("Record codec: types")
struct CodecTypeTests {
    let codec = RecordCodec()

    @Test("a decimal refuses a JSON number")
    func decimalRefusesNumber() {
        #expect(throws: CodecError.invalid(kind: kinds.set, id: "x", property: "rpe", value: "9.5")) {
            try codec.decodeSet(record(kinds.set, setProperties(["rpe": .number(Decimal(string: "9.5")!)])))
        }
    }

    @Test("\"9.50\" decodes to 9.5 and encodes \"9.5\"")
    func decimalShortest() throws {
        let set = try codec.decodeSet(record(kinds.set, setProperties(["rpe": .string("9.50")])))
        #expect(set.rpe == Decimal(string: "9.5"))
        #expect(codec.encode(set).properties["rpe"] == .string("9.5"))
    }

    @Test("decimal text is strict and shortest", arguments: [
        ("9.50", "9.5"), ("100", "100"), ("0.000", "0"), ("-0", "0"), ("-0.5", "-0.5"), ("0.45359237", "0.45359237"),
        ("83.9146", "83.9146"), ("007", "7"),
    ])
    func decimalText(input: String, output: String) throws {
        let value = try #require(DecimalText.parse(input))
        #expect(DecimalText.format(value) == output)
    }

    @Test("decimal text refuses anything but digits", arguments: ["", "-", ".5", "5.", "+5", "1e3", "9.5abc", " 9", "9,5", "1.2.3", "NaN"])
    func decimalTextRefuses(input: String) {
        #expect(DecimalText.parse(input) == nil)
    }

    @Test("an int refuses a fraction and a string")
    func intRefuses() {
        #expect(throws: CodecError.self) { try codec.decodeSet(record(kinds.set, setProperties(["order": .number(Decimal(string: "1.5")!)]))) }
        #expect(throws: CodecError.self) { try codec.decodeSet(record(kinds.set, setProperties(["reps": .string("10")]))) }
    }

    @Test("durations decode ISO 8601 and encode substrate's spelling", arguments: [
        ("PT90S", 90, "PT1M30S"), ("PT36H", 129_600, "P1DT12H"), ("PT0S", 0, "PT0S"), ("P0D", 0, "PT0S"),
        ("PT1M30S", 90, "PT1M30S"), ("P1W", 604_800, "P7D"), ("P1DT1S", 86401, "P1DT1S"), ("PT1.5S", 2, "PT2S"),
        ("PT0.4S", 0, "PT0S"), ("PT2H", 7200, "PT2H"), ("PT1H0M5S", 3605, "PT1H5S"),
    ])
    func durations(input: String, seconds: Int, output: String) {
        #expect(ISODuration.parse(input) == seconds)
        #expect(ISODuration.format(seconds) == output)
    }

    @Test("durations refuse what is not P[nW][nD][T[nH][nM][nS]]", arguments: [
        "", "P", "PT", "P1M", "P1Y", "-PT5S", "PT-5S", "PT5", "T5S", "PT1S5M", "PT1.5M", "P1DT", "pt5s", "PT 5S", "PT5S ",
    ])
    func durationsRefuse(input: String) {
        #expect(ISODuration.parse(input) == nil)
    }

    @Test("a duration property decodes PT90S to 90 and encodes PT1M30S")
    func durationProperty() throws {
        let set = try codec.decodeSet(record(kinds.set, setProperties(["restBefore": .string("PT90S"), "duration": .string("PT36H")])))
        #expect(set.restBefore == 90)
        let encoded = codec.encode(set).properties
        #expect(encoded["restBefore"] == .string("PT1M30S"))
        #expect(encoded["duration"] == .string("P1DT12H"))
    }

    @Test("datetimes decode any offset to the instant and encode UTC with Z and whole seconds")
    func datetimes() throws {
        let set = try codec.decodeSet(record(kinds.set, setProperties([
            "at": .string("2026-10-27T14:30:00+01:00"), "startedAt": .string("2026-10-27T13:28:59.750Z"),
        ])))
        #expect(set.at == DateText.parse("2026-10-27T13:30:00Z"))
        #expect(set.startedAt?.timeIntervalSince1970 == 1_793_107_739.75)
        let encoded = codec.encode(set).properties
        #expect(encoded["at"] == .string("2026-10-27T13:30:00Z"))
        #expect(encoded["startedAt"] == .string("2026-10-27T13:28:59Z"))
    }

    @Test("datetime text", arguments: [
        ("2026-10-27T13:30:00Z", 1_793_107_800.0), ("2026-10-25T00:59:59-01:00", 1_792_893_599.0),
        ("1970-01-01T00:00:00Z", 0.0), ("2024-02-29T12:00:00+05:30", 1_709_188_200.0), ("2026-10-27t13:30:00z", 1_793_107_800.0),
    ])
    func datetimeText(input: String, seconds: Double) {
        #expect(DateText.parse(input)?.timeIntervalSince1970 == seconds)
    }

    @Test("datetime text refuses what is not RFC 3339", arguments: [
        "2026-10-27", "2026-10-27T13:30:00", "2026-10-27 13:30:00Z", "2026-13-01T00:00:00Z", "2025-02-29T00:00:00Z",
        "2026-10-27T24:00:00Z", "2026-10-27T13:30:00+0100", "2026-10-27T13:30:00.Z", "2026-10-27T13:30:00Zjunk",
    ])
    func datetimeRefuses(input: String) {
        #expect(DateText.parse(input) == nil)
    }

    @Test("datetime text formats the examples' instants")
    func datetimeFormat() {
        for text in ["2026-10-22T11:34:08Z", "2026-10-26T13:00:17Z", "2000-02-29T23:59:59Z", "1969-12-31T23:59:59Z"] {
            #expect(DateText.parse(text).map(DateText.format) == text)
        }
    }

    @Test("a reference refuses another kind")
    func referenceKind() {
        #expect(throws: CodecError.invalid(kind: kinds.set, id: "x", property: "session", value: "\(kinds.routine)/day-3")) {
            try codec.decodeSet(record(kinds.set, setProperties(["session": .string("\(kinds.routine)/day-3")])))
        }
        #expect(throws: CodecError.self) {
            try codec.decodeSet(record(kinds.set, setProperties(["exercise": .string("\(kinds.exercise)/")])))
        }
    }

    @Test("the workout reference keeps the / in its id; kind any splits at the third /")
    func recordRefs() throws {
        let workout = "\(kinds.healthWorkout)/applehealth/substrate-sync-5e2b9c41/F1DB07FB-4FAF-4334-922C-B0B3F5A970EA"
        let session = try codec.decodeSession(record(kinds.session, [
            "at": .string("2026-10-27T13:30:00Z"),
            "workout": .string(workout),
            "about": .string("substrate.reamde.dev/core/person/owner/extra"),
        ]))
        #expect(session.workout == RecordRef(kind: kinds.healthWorkout, id: "applehealth/substrate-sync-5e2b9c41/F1DB07FB-4FAF-4334-922C-B0B3F5A970EA"))
        #expect(session.about == RecordRef(kind: "substrate.reamde.dev/core/person", id: "owner/extra"))
        let encoded = codec.encode(session).properties
        #expect(encoded["workout"] == .string(workout))
        #expect(throws: CodecError.self) {
            try codec.decodeSession(record(kinds.session, ["at": .string("2026-10-27T13:30:00Z"), "workout": .string("\(kinds.session)/x")]))
        }
        #expect(throws: CodecError.self) {
            try codec.decodeSession(record(kinds.session, ["at": .string("2026-10-27T13:30:00Z"), "origin": .string("hevy/workout")]))
        }
    }

    @Test("a training reference's id may hold /")
    func referenceWithSlash() throws {
        let set = try codec.decodeSet(record(kinds.set, setProperties(["exercise": .string("\(kinds.exercise)/a/b")])))
        #expect(set.exercise == "a/b")
    }

    @Test("quantities take kg and lb for mass, m for length, and nothing else")
    func quantities() throws {
        let set = try codec.decodeSet(record(kinds.set, setProperties([
            "load": .object(["value": .string("190"), "unit": .string("lb")]),
            "distance": .object(["value": .string("40"), "unit": .string("m")]),
        ])))
        #expect(set.load == .lb(190))
        #expect(set.distance == Length(metres: 40))
        for bad: (String, JSONValue) in [
            ("load", .object(["value": .string("35"), "unit": .string("st")])),
            ("load", .object(["value": .number(35), "unit": .string("kg")])),
            ("load", .object(["value": .string("35"), "unit": .string("kg"), "extra": .bool(true)])),
            ("distance", .object(["value": .string("40"), "unit": .string("kg")])),
            ("load", .string("35 kg")),
        ] {
            #expect(throws: CodecError.self, "\(bad)") { try codec.decodeSet(record(kinds.set, setProperties([bad.0: bad.1]))) }
        }
    }

    @Test("mass converts exactly between kg and lb")
    func massConversion() {
        #expect(Mass.lb(10).converted(to: .kg) == .kg(Decimal(string: "4.5359237")!))
        #expect(Mass.kg(Decimal(string: "4.5359237")!).converted(to: .lb) == .lb(10))
        #expect(Mass.kg(35).converted(to: .kg) == .kg(35))
    }
}

@Suite("Record codec: defaults, status and forward compatibility")
struct CodecRuleTests {
    let codec = RecordCodec()

    @Test("a set with role: working and structure: straight written out re-encodes without them")
    func declaredDefaults() throws {
        let set = try codec.decodeSet(record(kinds.set, setProperties(["role": .string("working"), "structure": .string("straight")])))
        #expect(set.role == .working)
        #expect(set.structure == .straight)
        let encoded = codec.encode(set).properties
        #expect(encoded["role"] == nil)
        #expect(encoded["structure"] == nil)
        let warmup = try codec.decodeSet(record(kinds.set, setProperties(["role": .string("warmup")])))
        #expect(codec.encode(warmup).properties["role"] == .string("warmup"))
    }

    @Test("a planned set's declared defaults are not encoded either")
    func plannedDefaults() throws {
        let routine = Routine(id: "r", name: "R", slots: [
            RoutineSlot(key: "a", exercise: "leg-press-machine", sets: [PlannedSet(key: "s1"), PlannedSet(key: "s2", role: .warmup, structure: .drop)]),
        ])
        let sets = try #require(codec.encode(routine).properties["slots"]?.arrayValue?.first?.objectValue?["sets"]?.arrayValue)
        #expect(sets[0] == .object(["key": .string("s1")]))
        #expect(sets[1] == .object(["key": .string("s2"), "role": .string("warmup"), "structure": .string("drop")]))
    }

    @Test("the in-progress coverage session round-trips without a status; a finished one keeps it")
    func status() throws {
        let records = try ExampleFixtures.records("coverage").filter { $0.kind == kinds.session }
        let active = try #require(records.first { $0.properties["status"] == nil })
        let session = try codec.decodeSession(active)
        #expect(session.status == .active)
        #expect(session.endsAt == nil)
        #expect(codec.encode(session).properties["status"] == nil)
        let finished = try #require(records.first { $0.properties["status"] == .string("finished") })
        let done = try codec.decodeSession(finished)
        #expect(done.status == .finished)
        #expect(codec.encode(done).properties["status"] == .string("finished"))
    }

    @Test("a title is dropped on decode and never sent")
    func title() throws {
        let set = try codec.decodeSet(record(kinds.set, setProperties(["title": .string("Chest Press (Machine)")])))
        #expect(set.extra.isEmpty)
        #expect(codec.encode(set).properties["title"] == nil)
    }

    @Test("JSON null reads as absent")
    func null() throws {
        let set = try codec.decodeSet(record(kinds.set, setProperties(["rpe": .null, "notes": .null])))
        #expect(set.rpe == nil)
        #expect(set.extra.isEmpty)
    }

    @Test("an unknown property survives a round trip, after the declared ones")
    func unknownProperty() throws {
        let set = try codec.decodeSet(record(kinds.set, setProperties(["zeta": .string("later"), "barSpeed": .object(["value": .string("0.8")])])))
        #expect(set.extra == ["zeta": .string("later"), "barSpeed": .object(["value": .string("0.8")])])
        let encoded = codec.encode(set).properties
        #expect(encoded["zeta"] == .string("later"))
        #expect(Array(encoded.keys.suffix(2)) == ["barSpeed", "zeta"])
    }

    @Test("an unknown optional enum value reads as nil and survives a round trip")
    func unknownOptionalEnum() throws {
        let set = try codec.decodeSet(record(kinds.set, setProperties(["side": .string("both"), "loadMode": .string("external")])))
        #expect(set.side == nil)
        #expect(set.extra == ["side": .string("both")])
        let encoded = codec.encode(set).properties
        #expect(encoded["side"] == .string("both"))
        #expect(encoded.keys.firstIndex(of: "side")! < encoded.keys.firstIndex(of: "loadMode")!)
    }

    @Test("an unknown structure is unsupported, never a straight set; an unknown status reads as the default and survives")
    func unknownDefaultedEnum() throws {
        #expect(throws: CodecError.unsupported(kind: kinds.set, id: "x", property: "structure", value: "giantdrop")) {
            try codec.decodeSet(record(kinds.set, setProperties(["structure": .string("giantdrop")])))
        }
        let session = try codec.decodeSession(record(kinds.session, ["at": .string("2026-10-27T13:30:00Z"), "status": .string("abandoned")]))
        #expect(session.status == .active)
        #expect(codec.encode(session).properties["status"] == .string("abandoned"))
    }

    @Test("an unknown member of a repeated enum survives while the known ones are unchanged")
    func unknownRepeatedEnum() throws {
        var exercise = try codec.decodeExercise(record(kinds.exercise, [
            "name": .string("Pec Deck"), "measurement": .string("weightreps"),
            "primaryMuscles": .array([.string("chest"), .string("pecminor")]),
        ]))
        #expect(exercise.primaryMuscles == [.chest])
        #expect(codec.encode(exercise).properties["primaryMuscles"] == .array([.string("chest"), .string("pecminor")]))
        exercise.primaryMuscles = [.chest, .shoulders]
        #expect(codec.encode(exercise).properties["primaryMuscles"] == .array([.string("chest"), .string("shoulders")]))
    }

    @Test("an unknown measurement makes the exercise undecodable")
    func unknownMeasurement() {
        #expect(throws: CodecError.unsupported(kind: kinds.exercise, id: "rower", property: "measurement", value: "strokes")) {
            try codec.decodeExercise(record(kinds.exercise, "rower", ["name": .string("Rower"), "measurement": .string("strokes")]))
        }
    }

    @Test("a missing required property or unknown required enum makes the record undecodable")
    func requiredProperties() {
        #expect(throws: CodecError.unsupported(kind: kinds.exercise, id: "x", property: "name", value: nil)) {
            try codec.decodeExercise(record(kinds.exercise, ["measurement": .string("weightreps")]))
        }
        #expect(throws: CodecError.unsupported(kind: kinds.routine, id: "x", property: "groups[0].groupKind", value: "dropset")) {
            try codec.decodeRoutine(record(kinds.routine, ["name": .string("R"), "groups": .array([.object(["key": .string("a"), "groupKind": .string("dropset")])])]))
        }
        #expect(throws: CodecError.unsupported(kind: kinds.routine, id: "x", property: "slots[0].exercise", value: nil)) {
            try codec.decodeRoutine(record(kinds.routine, ["name": .string("R"), "slots": .array([.object(["key": .string("a")])])]))
        }
        #expect(throws: CodecError.unsupported(kind: kinds.session, id: "x", property: "at", value: nil)) {
            try codec.decodeSession(record(kinds.session, [:]))
        }
        #expect(throws: CodecError.unsupported(kind: kinds.set, id: "x", property: "slot", value: nil)) {
            try codec.decodeSet(record(kinds.set, setProperties(["slot": nil])))
        }
    }

    @Test("a record of another kind is refused")
    func wrongKind() {
        #expect(throws: CodecError.wrongKind(expected: kinds.set, found: kinds.session, id: "x")) {
            try codec.decodeSet(record(kinds.session, setProperties()))
        }
        #expect(throws: CodecError.self) { try codec.decode(record("mneme.rxtx.dev/health/routine", [:])) }
    }

    @Test("a routine keeps the record's server version")
    func routineVersion() throws {
        var flat = try #require(ExampleFixtures.records("routines").first)
        #expect(try codec.decodeRoutine(flat).version == nil)
        #expect(try codec.decodeRoutine(flat).resolvedVersion == 1)
        flat.version = 4
        let routine = try codec.decodeRoutine(flat)
        #expect(routine.version == 4)
        #expect(codec.encode(routine).version == 4)
        let parsed = try FlatRecord(json: JSONValue(data: Data(#"{"kind": "k", "id": "i", "properties": {}, "version": 2, "createdAt": "x"}"#.utf8)))
        #expect(parsed.version == 2)
        #expect(parsed.json.objectValue?["createdAt"] == nil)
    }

    @Test("the kind names come from one place and can be replaced")
    func kindNames() throws {
        var names = KindNames.v1
        names.exercise = "example.dev/catalogue/exercise"
        let codec = RecordCodec(kinds: names)
        let exercise = try codec.decodeExercise(record(names.exercise, [
            "name": .string("Squat"), "measurement": .string("weightreps"), "variationOf": .string("example.dev/catalogue/exercise/lunge"),
        ]))
        #expect(exercise.variationOf == "lunge")
        #expect(codec.encode(exercise).kind == names.exercise)
        #expect(throws: CodecError.self) { try RecordCodec().decodeExercise(codec.encode(exercise)) }
    }
}

@Suite("Model helpers for the session record")
struct SessionRecordHelperTests {
    @Test("the name and the offsets match every session the app wrote in the examples")
    func namesAndOffsets() throws {
        let codec = RecordCodec()
        let routines = try (ExampleFixtures.records("routines") + ExampleFixtures.records("coverage"))
            .filter { $0.kind == codec.kinds.routine }
            .map(codec.decodeRoutine)
        var checked = 0
        for records in try ExampleFixtures.sessions("sessions") + ExampleFixtures.sessions("coverage") {
            let session = records.session
            guard session.adapter?.hasPrefix("substrate-workouts@") == true else { continue }
            let zone = try #require(session.timezone.flatMap(TimeZone.init(identifier:)))
            let routineName = session.routine.flatMap { id in routines.first { $0.id == id }?.name }
            #expect(TrainingSession.name(routine: routineName, start: session.at, timeZone: zone) == session.name)
            #expect(TrainingSession.utcOffset(in: zone, at: session.at) == session.utcOffsetStart)
            if let end = session.endsAt {
                #expect(TrainingSession.utcOffset(in: zone, at: end) == session.utcOffsetEnd)
            }
            checked += 1
        }
        #expect(checked == 5)
    }

    @Test("an instant floors to its second")
    func floor() {
        #expect(Date(timeIntervalSince1970: 1_793_107_800.999).flooredToSecond == Date(timeIntervalSince1970: 1_793_107_800))
        #expect(Date(timeIntervalSince1970: 1_793_107_800).flooredToSecond == Date(timeIntervalSince1970: 1_793_107_800))
    }

    @Test("a measurement says the load mode a set records")
    func loadModes() {
        #expect(MeasurementType.weightreps.loadMode == .external)
        #expect(MeasurementType.weightduration.loadMode == .external)
        #expect(MeasurementType.shortdistanceweight.loadMode == .external)
        #expect(MeasurementType.bodyweightreps.loadMode == .bodyweight)
        #expect(MeasurementType.weightedbodyweightreps.loadMode == .bodyweightplus)
        #expect(MeasurementType.bodyweightassistedreps.loadMode == .assisted)
        #expect(MeasurementType.repsonly.loadMode == nil)
        #expect(MeasurementType.duration.loadMode == nil)
        #expect(MeasurementType.distanceduration.loadMode == nil)
    }

    @Test("every example set's load mode is its exercise's measurement's")
    func exampleLoadModes() throws {
        let codec = RecordCodec()
        let exercises = try (ExampleFixtures.records("exercises") + ExampleFixtures.records("coverage"))
            .filter { $0.kind == codec.kinds.exercise }
            .map(codec.decodeExercise)
        let byID = Dictionary(uniqueKeysWithValues: exercises.map { ($0.id, $0) })
        for records in try ExampleFixtures.sessions("sessions") + ExampleFixtures.sessions("coverage") {
            for set in records.sets {
                let exercise = try #require(byID[set.exercise], "\(set.exercise)")
                #expect(set.loadMode == exercise.measurement.loadMode, "\(set.id)")
            }
        }
    }
}
