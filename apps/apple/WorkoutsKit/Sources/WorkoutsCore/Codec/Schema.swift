import Foundation

/// A property's declared type, spelled as the kind files spell it.
public enum PropertyType: String, Sendable, Hashable {
    case string, text, int, decimal, quantity, duration, datetime, timezone, bool, object, reference, state
    case enumeration = "enum"
}

/// What a quantity measures, which decides its units (§3.4 rule 6).
public enum QuantityDimension: Sendable, Hashable {
    /// `kg` or `lb`, read as `Mass`.
    case mass
    /// `m`, read as `Length`.
    case length
}

/// One declared property or object field as the codec knows it. The record
/// codec writes properties in this order (§3.4 rule 8); the drift test holds
/// the list against the kind files (§10.4).
public struct PropertySchema: Sendable, Hashable {
    public var name: String
    public var type: PropertyType
    public var required = false
    public var repeated = false
    /// The kind's declared `default`, which is never encoded (§3.4 rule 4).
    public var defaultValue: String?
    /// An enum's values or a state's states, in declaration order.
    public var values: [String] = []
    /// A reference's declared kind, or "any".
    public var referenceKind: String?
    public var dimension: QuantityDimension?
    /// An object's fields.
    public var fields: [PropertySchema] = []
}

/// One kind as the codec knows it.
public struct KindSchema: Sendable, Hashable {
    public var kind: String
    public var traits: [String]
    /// The properties the traits add (`at`, `endsAt`), which the kind file
    /// does not list. Written first.
    public var traitProperties: [PropertySchema]
    public var properties: [PropertySchema]

    /// Every property in the order the encoder writes them.
    public var allProperties: [PropertySchema] { traitProperties + properties }

    /// The fields of the object property at `path`, such as `slots`, `sets`.
    func fields(_ path: String...) -> [PropertySchema] {
        var level = allProperties
        for name in path {
            guard let property = level.first(where: { $0.name == name }) else {
                preconditionFailure("\(kind) declares no object \(path.joined(separator: "."))")
            }
            level = property.fields
        }
        return level
    }
}

extension RecordCodec {
    static func exerciseSchema(_ kinds: KindNames) -> KindSchema {
        KindSchema(kind: kinds.exercise, traits: [], traitProperties: [], properties: [
            .string("name", required: true),
            .string("aliases", repeated: true),
            .enumeration("measurement", MeasurementType.self, required: true),
            .enumeration("equipment", Equipment.self),
            .enumeration("laterality", Laterality.self),
            .enumeration("loadBasis", LoadBasis.self),
            .int("implements"),
            .enumeration("primaryMuscles", Muscle.self, repeated: true),
            .enumeration("secondaryMuscles", Muscle.self, repeated: true),
            .enumeration("restClass", RestClass.self),
            .reference("variationOf", kind: kinds.exercise),
            .object("codes", repeated: true, fields: [
                .string("system", required: true),
                .string("code", required: true),
                .string("version"),
                .string("display"),
            ]),
            .quantity("loadBase", .mass),
            .quantity("loadStep", .mass),
            .quantity("loadMax", .mass),
            .decimal("bodyShare"),
            .int("defaultRepsMin"),
            .int("defaultRepsMax"),
            .decimal("defaultRPE"),
            .int("repGain"),
            .decimal("dropPercent"),
            .duration("timeStep"),
            .enumeration("myoStopRule", MyoStopRule.self),
            .int("myoMaxMiniSets"),
        ])
    }

    static func routineSchema(_ kinds: KindNames) -> KindSchema {
        KindSchema(kind: kinds.routine, traits: [], traitProperties: [], properties: [
            .string("name", required: true),
            .string("folder"),
            .int("position"),
            .text("notes"),
            .bool("deload"),
            .enumeration("activityType", ActivityType.self),
            .object("groups", repeated: true, fields: groupFields),
            .object("slots", repeated: true, fields: [
                .string("key", required: true),
                .reference("exercise", kind: kinds.exercise, required: true),
                .string("group"),
                .duration("setRest"),
                .duration("warmupRest"),
                .duration("transition"),
                .duration("sideSwitch"),
                .enumeration("sideOrder", SideOrder.self),
                .bool("sideMatch"),
                .text("notes"),
                .object("sets", repeated: true, fields: plannedSetFields),
            ]),
        ])
    }

    static func sessionSchema(_ kinds: KindNames) -> KindSchema {
        KindSchema(
            kind: kinds.session,
            traits: ["substrate.reamde.dev/core/temporal(range)", "substrate.reamde.dev/core/revised"],
            traitProperties: [.datetime("at", required: true), .datetime("endsAt")],
            properties: [
                .string("name"),
                .state("status", SessionStatus.self),
                .reference("routine", kind: kinds.routine),
                .int("routineVersion"),
                .bool("deload"),
                .enumeration("sport", Sport.self),
                .string("sportSource"),
                .enumeration("activityType", ActivityType.self),
                .object("slots", repeated: true, fields: [
                    .string("key", required: true),
                    .reference("exercise", kind: kinds.exercise, required: true),
                    .string("group"),
                    .string("routineSlot"),
                    .text("notes"),
                    .duration("setRest"),
                    .duration("warmupRest"),
                    .duration("transition"),
                    .duration("sideSwitch"),
                    .object("sets", repeated: true, fields: plannedSetFields),
                ]),
                .object("groups", repeated: true, fields: groupFields),
                .object("pauses", repeated: true, fields: [
                    .datetime("start", required: true),
                    .datetime("end"),
                ]),
                .int("utcOffsetStart"),
                .int("utcOffsetEnd"),
                .timezone("timezone"),
                .quantity("bodyMass", .mass),
                .decimal("sessionRPE"),
                .text("notes"),
                .string("healthKitWorkout"),
                .reference("workout", kind: kinds.healthWorkout),
                .reference("source", kind: kinds.observationSource),
                .reference("about", kind: "any"),
                .reference("origin", kind: "any"),
                .int("revision"),
                .string("adapter"),
            ]
        )
    }

    static func setSchema(_ kinds: KindNames) -> KindSchema {
        KindSchema(
            kind: kinds.set,
            traits: ["substrate.reamde.dev/core/temporal(point)", "substrate.reamde.dev/core/revised"],
            traitProperties: [.datetime("at", required: true)],
            properties: [
                .reference("session", kind: kinds.session, required: true),
                .reference("exercise", kind: kinds.exercise, required: true),
                .string("slot", required: true),
                .int("order", required: true),
                .int("ordinal"),
                .reference("setup", kind: kinds.setup),
                .enumeration("role", Role.self, default: Role.working),
                .enumeration("roleLabel", RoleLabel.self),
                .enumeration("structure", Structure.self, default: Structure.straight),
                .reference("continues", kind: kinds.set),
                .int("chainPosition"),
                .enumeration("side", Side.self),
                .enumeration("loadMode", LoadMode.self),
                .quantity("load", .mass),
                .quantity("assistance", .mass),
                .string("resistanceLabel"),
                .int("reps"),
                .duration("duration"),
                .quantity("distance", .length),
                .enumeration("loadBasis", LoadBasis.self),
                .int("implements"),
                .decimal("bodyShare"),
                .enumeration("laterality", Laterality.self),
                .enumeration("repsBasis", RepsBasis.self),
                .decimal("rpe"),
                .enumeration("rpeMethod", EffortMethod.self),
                .decimal("rir"),
                .enumeration("rirMethod", EffortMethod.self),
                .enumeration("rpeScale", RPEScale.self),
                .bool("reachedFailure"),
                .bool("failedRepAttempted"),
                .enumeration("completionMethod", CompletionMethod.self),
                .text("notes"),
                .string("sourceType"),
                .enumeration("timeBasis", TimeBasis.self),
                .datetime("startedAt"),
                .duration("restBefore"),
                .enumeration("restMethod", RestMethod.self),
                .duration("restPlanned"),
                .bool("restSkipped"),
                .object("target", fields: [
                    .string("plannedSet"),
                    .enumeration("termination", Termination.self),
                    .int("repsMin"),
                    .int("repsMax"),
                    .quantity("load", .mass),
                    .decimal("loadPercent"),
                    .enumeration("loadReference", LoadReference.self),
                    .quantity("referenceLoad", .mass),
                    .quantity("assistance", .mass),
                    .decimal("rpeMin"),
                    .decimal("rpeMax"),
                    .decimal("rirMin"),
                    .decimal("rirMax"),
                    .duration("durationMin"),
                    .duration("durationMax"),
                    .enumeration("timerMode", TimerMode.self),
                    .quantity("distance", .length),
                    .string("tempo"),
                    .int("miniSetReps"),
                    .int("miniSets"),
                    .enumeration("myoStopRule", MyoStopRule.self),
                    .int("totalReps"),
                    .int("drops"),
                    .decimal("dropPercent"),
                    .quantity("dropStep", .mass),
                    .int("dropReps"),
                    .duration("intraSetPause"),
                ]),
                .reference("origin", kind: "any"),
                .int("revision"),
            ]
        )
    }

    static func setupSchema(_ kinds: KindNames) -> KindSchema {
        KindSchema(kind: kinds.setup, traits: [], traitProperties: [], properties: [
            .string("name", required: true),
            .reference("exercise", kind: kinds.exercise, required: true),
            .string("gym"),
            .quantity("loadBase", .mass),
            .quantity("loadStep", .mass),
            .quantity("loadMax", .mass),
            .text("notes"),
        ])
    }

    /// A routine slot's planned sets and a session slot's snapshot of them
    /// share their fields.
    private static var plannedSetFields: [PropertySchema] {
        [
            .string("key", required: true),
            .enumeration("role", Role.self, default: Role.working),
            .enumeration("roleLabel", RoleLabel.self),
            .enumeration("structure", Structure.self, default: Structure.straight),
            .enumeration("termination", Termination.self),
            .int("repsMin"),
            .int("repsMax"),
            .quantity("load", .mass),
            .decimal("loadPercent"),
            .enumeration("loadReference", LoadReference.self),
            .quantity("assistance", .mass),
            .decimal("rpeMin"),
            .decimal("rpeMax"),
            .decimal("rirMin"),
            .decimal("rirMax"),
            .duration("durationMin"),
            .duration("durationMax"),
            .enumeration("timerMode", TimerMode.self),
            .quantity("distance", .length),
            .string("tempo"),
            .int("miniSetReps"),
            .int("miniSets"),
            .enumeration("myoStopRule", MyoStopRule.self),
            .int("totalReps"),
            .int("drops"),
            .decimal("dropPercent"),
            .quantity("dropStep", .mass),
            .int("dropReps"),
            .duration("intraSetPause"),
            .duration("rest"),
        ]
    }

    /// A routine's and a session's groups share their fields.
    private static var groupFields: [PropertySchema] {
        [
            .string("key", required: true),
            .enumeration("groupKind", GroupKind.self, required: true),
            .enumeration("pairing", Pairing.self),
            .int("rounds"),
            .duration("roundRest"),
        ]
    }
}

extension PropertySchema {
    static func string(_ name: String, required: Bool = false, repeated: Bool = false) -> Self {
        PropertySchema(name: name, type: .string, required: required, repeated: repeated)
    }

    static func text(_ name: String) -> Self { PropertySchema(name: name, type: .text) }
    static func int(_ name: String, required: Bool = false) -> Self { PropertySchema(name: name, type: .int, required: required) }
    static func decimal(_ name: String) -> Self { PropertySchema(name: name, type: .decimal) }
    static func duration(_ name: String) -> Self { PropertySchema(name: name, type: .duration) }
    static func bool(_ name: String) -> Self { PropertySchema(name: name, type: .bool) }
    static func timezone(_ name: String) -> Self { PropertySchema(name: name, type: .timezone) }

    static func datetime(_ name: String, required: Bool = false) -> Self {
        PropertySchema(name: name, type: .datetime, required: required)
    }

    static func quantity(_ name: String, _ dimension: QuantityDimension) -> Self {
        PropertySchema(name: name, type: .quantity, dimension: dimension)
    }

    static func enumeration<E: CaseIterable & RawRepresentable>(
        _ name: String, _: E.Type, required: Bool = false, repeated: Bool = false, default defaultValue: E? = nil
    ) -> Self where E.RawValue == String {
        PropertySchema(
            name: name, type: .enumeration, required: required, repeated: repeated,
            defaultValue: defaultValue?.rawValue, values: E.allCases.map(\.rawValue)
        )
    }

    static func state<E: CaseIterable & RawRepresentable>(_ name: String, _: E.Type) -> Self where E.RawValue == String {
        PropertySchema(name: name, type: .state, values: E.allCases.map(\.rawValue))
    }

    static func reference(_ name: String, kind: String, required: Bool = false) -> Self {
        PropertySchema(name: name, type: .reference, required: required, referenceKind: kind)
    }

    static func object(_ name: String, repeated: Bool = false, fields: [PropertySchema]) -> Self {
        PropertySchema(name: name, type: .object, repeated: repeated, fields: fields)
    }
}
