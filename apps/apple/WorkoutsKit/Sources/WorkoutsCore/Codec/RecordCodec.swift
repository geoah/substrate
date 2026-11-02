import Foundation

/// Converts the model to and from substrate's flat records (§3.4). Decoding
/// checks the kind and refuses what the kind would refuse; encoding writes
/// what substrate stores, in declaration order, and round-trips every record
/// it decodes.
public struct RecordCodec: Sendable {
    public let kinds: KindNames
    public let exerciseSchema: KindSchema
    public let routineSchema: KindSchema
    public let sessionSchema: KindSchema
    public let setSchema: KindSchema
    public let setupSchema: KindSchema

    public init(kinds: KindNames = .v1) {
        self.kinds = kinds
        exerciseSchema = Self.exerciseSchema(kinds)
        routineSchema = Self.routineSchema(kinds)
        sessionSchema = Self.sessionSchema(kinds)
        setSchema = Self.setSchema(kinds)
        setupSchema = Self.setupSchema(kinds)
    }

    /// The five kinds as this codec reads and writes them.
    public var schemas: [KindSchema] { [exerciseSchema, routineSchema, sessionSchema, setSchema, setupSchema] }

    /// The schema for a kind name, if the codec knows it.
    public func schema(for kind: String) -> KindSchema? { schemas.first { $0.kind == kind } }

    private func check(_ record: FlatRecord, is kind: String) throws(CodecError) {
        guard record.kind == kind else { throw .wrongKind(expected: kind, found: record.kind, id: record.id) }
    }
}

// MARK: - Any record

/// A decoded record of one of the five kinds.
public enum DecodedRecord: Hashable, Sendable {
    case exercise(Exercise)
    case routine(Routine)
    case session(TrainingSession)
    case set(PerformedSet)
    case setup(Setup)
}

public extension RecordCodec {
    /// Decodes a record of any of the five kinds.
    func decode(_ record: FlatRecord) throws(CodecError) -> DecodedRecord {
        switch record.kind {
        case kinds.exercise: try .exercise(decodeExercise(record))
        case kinds.routine: try .routine(decodeRoutine(record))
        case kinds.session: try .session(decodeSession(record))
        case kinds.set: try .set(decodeSet(record))
        case kinds.setup: try .setup(decodeSetup(record))
        default: throw .unsupported(kind: record.kind, id: record.id, property: "kind", value: record.kind)
        }
    }

    func encode(_ record: DecodedRecord) -> FlatRecord {
        switch record {
        case let .exercise(value): encode(value)
        case let .routine(value): encode(value)
        case let .session(value): encode(value)
        case let .set(value): encode(value)
        case let .setup(value): encode(value)
        }
    }

    /// One finished session as records: the session first, then its sets.
    func encode(_ records: SessionRecords) -> [FlatRecord] {
        [encode(records.session)] + records.sets.map(encode)
    }

    /// The sessions among `records` with their sets, in the order the sessions
    /// appear and each session's sets in the order they appear. Records of
    /// other kinds are passed over; a set whose session is not among them is
    /// left out.
    func sessionRecords(from records: [FlatRecord]) throws(CodecError) -> [SessionRecords] {
        var sessions: [TrainingSession] = []
        var sets: [SessionID: [PerformedSet]] = [:]
        for record in records {
            switch record.kind {
            case kinds.session: try sessions.append(decodeSession(record))
            case kinds.set:
                let set = try decodeSet(record)
                sets[set.session, default: []].append(set)
            default: continue
            }
        }
        return sessions.map { SessionRecords(session: $0, sets: sets[$0.id] ?? []) }
    }
}

// MARK: - Exercise

public extension RecordCodec {
    func decodeExercise(_ record: FlatRecord) throws(CodecError) -> Exercise {
        try check(record, is: kinds.exercise)
        var r = PropertyReader(record: record)
        var exercise = try Exercise(
            id: ExerciseID(record.id),
            name: r.requiredString("name"),
            aliases: r.strings("aliases"),
            measurement: r.requiredEnumeration("measurement"),
            equipment: r.enumeration("equipment"),
            // Decides side chains and the myo-rep stop: strict (rule 7).
            laterality: r.executionEnumeration("laterality"),
            loadBasis: r.enumeration("loadBasis"),
            implements: r.int("implements"),
            primaryMuscles: r.enumerations("primaryMuscles"),
            secondaryMuscles: r.enumerations("secondaryMuscles"),
            restClass: r.enumeration("restClass"),
            variationOf: r.reference("variationOf", kind: kinds.exercise).map(ExerciseID.init(rawValue:)),
            codes: r.objects("codes") { (c: inout PropertyReader) throws(CodecError) in
                var coding = try Coding(
                    system: c.requiredString("system"),
                    code: c.requiredString("code"),
                    version: c.string("version"),
                    display: c.string("display")
                )
                coding.extra = c.extra
                return coding
            },
            loadBase: r.mass("loadBase"),
            loadStep: r.mass("loadStep"),
            loadMax: r.mass("loadMax"),
            bodyShare: r.decimal("bodyShare"),
            defaultRepsMin: r.int("defaultRepsMin"),
            defaultRepsMax: r.int("defaultRepsMax"),
            defaultRPE: r.decimal("defaultRPE"),
            repGain: r.int("repGain"),
            dropPercent: r.decimal("dropPercent"),
            timeStep: r.duration("timeStep"),
            myoStopRule: r.executionEnumeration("myoStopRule"),
            myoMaxMiniSets: r.int("myoMaxMiniSets")
        )
        exercise.extra = r.extra
        return exercise
    }

    func encode(_ exercise: Exercise) -> FlatRecord {
        var w = PropertyWriter(schema: exerciseSchema.allProperties, extra: exercise.extra)
        w.string("name", exercise.name)
        w.strings("aliases", exercise.aliases)
        w.enumeration("measurement", exercise.measurement)
        w.enumeration("equipment", exercise.equipment)
        w.enumeration("laterality", exercise.laterality)
        w.enumeration("loadBasis", exercise.loadBasis)
        w.int("implements", exercise.implements)
        w.enumerations("primaryMuscles", exercise.primaryMuscles)
        w.enumerations("secondaryMuscles", exercise.secondaryMuscles)
        w.enumeration("restClass", exercise.restClass)
        w.reference("variationOf", kind: kinds.exercise, id: exercise.variationOf?.rawValue)
        let codingFields = exerciseSchema.fields("codes")
        w.objects("codes", exercise.codes.map { coding in
            var c = PropertyWriter(schema: codingFields, extra: coding.extra)
            c.string("system", coding.system)
            c.string("code", coding.code)
            c.string("version", coding.version)
            c.string("display", coding.display)
            return c.finish()
        })
        w.mass("loadBase", exercise.loadBase)
        w.mass("loadStep", exercise.loadStep)
        w.mass("loadMax", exercise.loadMax)
        w.decimal("bodyShare", exercise.bodyShare)
        w.int("defaultRepsMin", exercise.defaultRepsMin)
        w.int("defaultRepsMax", exercise.defaultRepsMax)
        w.decimal("defaultRPE", exercise.defaultRPE)
        w.int("repGain", exercise.repGain)
        w.decimal("dropPercent", exercise.dropPercent)
        w.duration("timeStep", exercise.timeStep)
        w.enumeration("myoStopRule", exercise.myoStopRule)
        w.int("myoMaxMiniSets", exercise.myoMaxMiniSets)
        return FlatRecord(kind: kinds.exercise, id: exercise.id.rawValue, properties: w.finish())
    }
}

// MARK: - Routine

public extension RecordCodec {
    func decodeRoutine(_ record: FlatRecord) throws(CodecError) -> Routine {
        try check(record, is: kinds.routine)
        var r = PropertyReader(record: record)
        let exerciseKind = kinds.exercise
        var routine = try Routine(
            id: RoutineID(record.id),
            version: record.version,
            name: r.requiredString("name"),
            folder: r.string("folder"),
            position: r.int("position"),
            notes: r.string("notes"),
            deload: r.bool("deload"),
            activityType: r.enumeration("activityType"),
            groups: r.objects("groups") { (g: inout PropertyReader) throws(CodecError) in
                try Self.decodeGroup(&g)
            },
            slots: r.objects("slots") { (s: inout PropertyReader) throws(CodecError) in
                var slot = try RoutineSlot(
                    key: s.requiredString("key"),
                    exercise: ExerciseID(s.requiredReference("exercise", kind: exerciseKind)),
                    group: s.string("group"),
                    setRest: s.duration("setRest"),
                    warmupRest: s.duration("warmupRest"),
                    transition: s.duration("transition"),
                    sideSwitch: s.duration("sideSwitch"),
                    sideOrder: s.executionEnumeration("sideOrder"),
                    sideMatch: s.bool("sideMatch"),
                    notes: s.string("notes"),
                    sets: s.objects("sets") { (p: inout PropertyReader) throws(CodecError) in
                        try Self.decodePlannedSet(&p, runs: true)
                    }
                )
                slot.extra = s.extra
                return slot
            }
        )
        routine.extra = r.extra
        return routine
    }

    func encode(_ routine: Routine) -> FlatRecord {
        var w = PropertyWriter(schema: routineSchema.allProperties, extra: routine.extra)
        w.string("name", routine.name)
        w.string("folder", routine.folder)
        w.int("position", routine.position)
        w.string("notes", routine.notes)
        w.bool("deload", routine.deload)
        w.enumeration("activityType", routine.activityType)
        let groupFields = routineSchema.fields("groups")
        w.objects("groups", routine.groups.map { group in
            encodeGroup(key: group.key, groupKind: group.groupKind, pairing: group.pairing, rounds: group.rounds,
                        roundRest: group.roundRest, extra: group.extra, fields: groupFields)
        })
        let slotFields = routineSchema.fields("slots")
        let setFields = routineSchema.fields("slots", "sets")
        w.objects("slots", routine.slots.map { slot in
            var s = PropertyWriter(schema: slotFields, extra: slot.extra)
            s.string("key", slot.key)
            s.reference("exercise", kind: kinds.exercise, id: slot.exercise.rawValue)
            s.string("group", slot.group)
            s.duration("setRest", slot.setRest)
            s.duration("warmupRest", slot.warmupRest)
            s.duration("transition", slot.transition)
            s.duration("sideSwitch", slot.sideSwitch)
            s.enumeration("sideOrder", slot.sideOrder)
            s.bool("sideMatch", slot.sideMatch)
            s.string("notes", slot.notes)
            s.objects("sets", slot.sets.map { encodePlannedSet($0, fields: setFields) })
            return s.finish()
        })
        return FlatRecord(kind: kinds.routine, id: routine.id.rawValue, properties: w.finish(), version: routine.version)
    }
}

extension RecordCodec {
    /// A routine's planned set (`runs`), or a session slot's snapshot of
    /// one. A routine's execution enums are strict (§3.4 rule 7): an unknown
    /// value makes the routine unsupported, so it is never run as something
    /// else. A snapshot is never run, so it reads them as descriptions: an
    /// unknown value leaves the typed field at its default and is kept raw in
    /// `extra`, and the session stays readable.
    static func decodePlannedSet(_ p: inout PropertyReader, runs: Bool) throws(CodecError) -> PlannedSet {
        func execution<E: RawRepresentable>(_ name: String) throws(CodecError) -> E? where E.RawValue == String {
            try runs ? p.executionEnumeration(name) : p.enumeration(name)
        }
        var planned = try PlannedSet(
            key: p.requiredString("key"),
            role: execution("role") ?? .working,
            roleLabel: p.enumeration("roleLabel"),
            structure: execution("structure") ?? .straight,
            termination: execution("termination"),
            repsMin: p.int("repsMin"),
            repsMax: p.int("repsMax"),
            load: p.mass("load"),
            loadPercent: p.decimal("loadPercent"),
            loadReference: execution("loadReference"),
            assistance: p.mass("assistance"),
            rpeMin: p.decimal("rpeMin"),
            rpeMax: p.decimal("rpeMax"),
            rirMin: p.decimal("rirMin"),
            rirMax: p.decimal("rirMax"),
            durationMin: p.duration("durationMin"),
            durationMax: p.duration("durationMax"),
            timerMode: execution("timerMode"),
            distance: p.length("distance"),
            tempo: p.string("tempo"),
            miniSetReps: p.int("miniSetReps"),
            miniSets: p.int("miniSets"),
            myoStopRule: execution("myoStopRule"),
            totalReps: p.int("totalReps"),
            drops: p.int("drops"),
            dropPercent: p.decimal("dropPercent"),
            dropStep: p.mass("dropStep"),
            dropReps: p.int("dropReps"),
            intraSetPause: p.duration("intraSetPause"),
            rest: p.duration("rest")
        )
        planned.extra = p.extra
        return planned
    }

    func encodePlannedSet(_ planned: PlannedSet, fields: [PropertySchema]) -> JSONObject {
        var p = PropertyWriter(schema: fields, extra: planned.extra)
        p.string("key", planned.key)
        p.enumeration("role", planned.role, omitting: .working)
        p.enumeration("roleLabel", planned.roleLabel)
        p.enumeration("structure", planned.structure, omitting: .straight)
        p.enumeration("termination", planned.termination)
        p.int("repsMin", planned.repsMin)
        p.int("repsMax", planned.repsMax)
        p.mass("load", planned.load)
        p.decimal("loadPercent", planned.loadPercent)
        p.enumeration("loadReference", planned.loadReference)
        p.mass("assistance", planned.assistance)
        p.decimal("rpeMin", planned.rpeMin)
        p.decimal("rpeMax", planned.rpeMax)
        p.decimal("rirMin", planned.rirMin)
        p.decimal("rirMax", planned.rirMax)
        p.duration("durationMin", planned.durationMin)
        p.duration("durationMax", planned.durationMax)
        p.enumeration("timerMode", planned.timerMode)
        p.length("distance", planned.distance)
        p.string("tempo", planned.tempo)
        p.int("miniSetReps", planned.miniSetReps)
        p.int("miniSets", planned.miniSets)
        p.enumeration("myoStopRule", planned.myoStopRule)
        p.int("totalReps", planned.totalReps)
        p.int("drops", planned.drops)
        p.decimal("dropPercent", planned.dropPercent)
        p.mass("dropStep", planned.dropStep)
        p.int("dropReps", planned.dropReps)
        p.duration("intraSetPause", planned.intraSetPause)
        p.duration("rest", planned.rest)
        return p.finish()
    }

    /// A routine's or a session's group. `groupKind` is required.
    static func decodeGroup(_ g: inout PropertyReader) throws(CodecError) -> RoutineGroup {
        var group = try RoutineGroup(
            key: g.requiredString("key"),
            groupKind: g.requiredEnumeration("groupKind"),
            pairing: g.enumeration("pairing"),
            rounds: g.int("rounds"),
            roundRest: g.duration("roundRest")
        )
        group.extra = g.extra
        return group
    }

    func encodeGroup(key: String, groupKind: GroupKind, pairing: Pairing?, rounds: Int?, roundRest: Seconds?,
                     extra: [String: JSONValue], fields: [PropertySchema]) -> JSONObject
    {
        var g = PropertyWriter(schema: fields, extra: extra)
        g.string("key", key)
        g.enumeration("groupKind", groupKind)
        g.enumeration("pairing", pairing)
        g.int("rounds", rounds)
        g.duration("roundRest", roundRest)
        return g.finish()
    }
}

// MARK: - Training session

public extension RecordCodec {
    func decodeSession(_ record: FlatRecord) throws(CodecError) -> TrainingSession {
        try check(record, is: kinds.session)
        var r = PropertyReader(record: record)
        let exerciseKind = kinds.exercise
        var session = try TrainingSession(
            id: SessionID(record.id),
            at: r.requiredDatetime("at"),
            endsAt: r.datetime("endsAt"),
            name: r.string("name"),
            status: r.enumeration("status", default: .active),
            routine: r.reference("routine", kind: kinds.routine).map(RoutineID.init(rawValue:)),
            routineVersion: r.int("routineVersion"),
            deload: r.bool("deload"),
            sport: r.enumeration("sport"),
            sportSource: r.string("sportSource"),
            activityType: r.enumeration("activityType"),
            slots: r.objects("slots") { (s: inout PropertyReader) throws(CodecError) in
                var slot = try SessionSlot(
                    key: s.requiredString("key"),
                    exercise: ExerciseID(s.requiredReference("exercise", kind: exerciseKind)),
                    group: s.string("group"),
                    routineSlot: s.string("routineSlot"),
                    notes: s.string("notes"),
                    setRest: s.duration("setRest"),
                    warmupRest: s.duration("warmupRest"),
                    transition: s.duration("transition"),
                    sideSwitch: s.duration("sideSwitch"),
                    sets: s.objects("sets") { (p: inout PropertyReader) throws(CodecError) in
                        try Self.decodePlannedSet(&p, runs: false)
                    }
                )
                slot.extra = s.extra
                return slot
            },
            groups: r.objects("groups") { (g: inout PropertyReader) throws(CodecError) in
                let group = try Self.decodeGroup(&g)
                return SessionGroup(key: group.key, groupKind: group.groupKind, pairing: group.pairing, rounds: group.rounds,
                                    roundRest: group.roundRest, extra: group.extra)
            },
            pauses: r.objects("pauses") { (p: inout PropertyReader) throws(CodecError) in
                var pause = try Pause(start: p.requiredDatetime("start"), end: p.datetime("end"))
                pause.extra = p.extra
                return pause
            },
            utcOffsetStart: r.int("utcOffsetStart"),
            utcOffsetEnd: r.int("utcOffsetEnd"),
            timezone: r.string("timezone"),
            bodyMass: r.mass("bodyMass"),
            sessionRPE: r.decimal("sessionRPE"),
            notes: r.string("notes"),
            healthKitWorkout: r.string("healthKitWorkout"),
            workout: r.recordRef("workout", kind: kinds.healthWorkout),
            source: r.recordRef("source", kind: kinds.observationSource),
            about: r.recordRef("about", kind: nil),
            origin: r.recordRef("origin", kind: nil),
            revision: r.int("revision"),
            adapter: r.string("adapter")
        )
        session.extra = r.extra
        return session
    }

    func encode(_ session: TrainingSession) -> FlatRecord {
        var w = PropertyWriter(schema: sessionSchema.allProperties, extra: session.extra)
        w.datetime("at", session.at)
        w.datetime("endsAt", session.endsAt)
        w.string("name", session.name)
        // A create never names `status`; only Finish moves it (rule 5).
        w.enumeration("status", session.status, omitting: .active)
        w.reference("routine", kind: kinds.routine, id: session.routine?.rawValue)
        w.int("routineVersion", session.routineVersion)
        w.bool("deload", session.deload)
        w.enumeration("sport", session.sport)
        w.string("sportSource", session.sportSource)
        w.enumeration("activityType", session.activityType)
        let slotFields = sessionSchema.fields("slots")
        let setFields = sessionSchema.fields("slots", "sets")
        w.objects("slots", session.slots.map { slot in
            var s = PropertyWriter(schema: slotFields, extra: slot.extra)
            s.string("key", slot.key)
            s.reference("exercise", kind: kinds.exercise, id: slot.exercise.rawValue)
            s.string("group", slot.group)
            s.string("routineSlot", slot.routineSlot)
            s.string("notes", slot.notes)
            s.duration("setRest", slot.setRest)
            s.duration("warmupRest", slot.warmupRest)
            s.duration("transition", slot.transition)
            s.duration("sideSwitch", slot.sideSwitch)
            s.objects("sets", slot.sets.map { encodePlannedSet($0, fields: setFields) })
            return s.finish()
        })
        let groupFields = sessionSchema.fields("groups")
        w.objects("groups", session.groups.map { group in
            encodeGroup(key: group.key, groupKind: group.groupKind, pairing: group.pairing, rounds: group.rounds,
                        roundRest: group.roundRest, extra: group.extra, fields: groupFields)
        })
        let pauseFields = sessionSchema.fields("pauses")
        w.objects("pauses", session.pauses.map { pause in
            var p = PropertyWriter(schema: pauseFields, extra: pause.extra)
            p.datetime("start", pause.start)
            p.datetime("end", pause.end)
            return p.finish()
        })
        w.int("utcOffsetStart", session.utcOffsetStart)
        w.int("utcOffsetEnd", session.utcOffsetEnd)
        w.string("timezone", session.timezone)
        w.mass("bodyMass", session.bodyMass)
        w.decimal("sessionRPE", session.sessionRPE)
        w.string("notes", session.notes)
        w.string("healthKitWorkout", session.healthKitWorkout)
        w.recordRef("workout", session.workout)
        w.recordRef("source", session.source)
        w.recordRef("about", session.about)
        w.recordRef("origin", session.origin)
        w.int("revision", session.revision)
        w.string("adapter", session.adapter)
        return FlatRecord(kind: kinds.session, id: session.id.rawValue, properties: w.finish())
    }
}

// MARK: - Set

public extension RecordCodec {
    func decodeSet(_ record: FlatRecord) throws(CodecError) -> PerformedSet {
        try check(record, is: kinds.set)
        var r = PropertyReader(record: record)
        var set = try PerformedSet(
            id: SetID(record.id),
            at: r.requiredDatetime("at"),
            session: SessionID(r.requiredReference("session", kind: kinds.session)),
            exercise: ExerciseID(r.requiredReference("exercise", kind: kinds.exercise)),
            slot: r.requiredString("slot"),
            order: r.requiredInt("order"),
            ordinal: r.int("ordinal"),
            setup: r.reference("setup", kind: kinds.setup).map(SetupID.init(rawValue:)),
            // How a chain folds and counts: strict (rule 7).
            role: r.executionEnumeration("role", default: .working),
            roleLabel: r.enumeration("roleLabel"),
            structure: r.executionEnumeration("structure", default: .straight),
            continues: r.reference("continues", kind: kinds.set).map(SetID.init(rawValue:)),
            chainPosition: r.int("chainPosition"),
            side: r.enumeration("side"),
            loadMode: r.enumeration("loadMode"),
            load: r.mass("load"),
            assistance: r.mass("assistance"),
            resistanceLabel: r.string("resistanceLabel"),
            reps: r.int("reps"),
            duration: r.duration("duration"),
            distance: r.length("distance"),
            loadBasis: r.enumeration("loadBasis"),
            implements: r.int("implements"),
            bodyShare: r.decimal("bodyShare"),
            laterality: r.enumeration("laterality"),
            repsBasis: r.enumeration("repsBasis"),
            rpe: r.decimal("rpe"),
            rpeMethod: r.enumeration("rpeMethod"),
            rir: r.decimal("rir"),
            rirMethod: r.enumeration("rirMethod"),
            rpeScale: r.enumeration("rpeScale"),
            reachedFailure: r.bool("reachedFailure"),
            failedRepAttempted: r.bool("failedRepAttempted"),
            completionMethod: r.enumeration("completionMethod"),
            notes: r.string("notes"),
            sourceType: r.string("sourceType"),
            timeBasis: r.enumeration("timeBasis"),
            startedAt: r.datetime("startedAt"),
            restBefore: r.duration("restBefore"),
            restMethod: r.enumeration("restMethod"),
            restPlanned: r.duration("restPlanned"),
            restSkipped: r.bool("restSkipped"),
            target: r.object("target") { (t: inout PropertyReader) throws(CodecError) in
                var target = try SetTarget(
                    plannedSet: t.string("plannedSet"),
                    termination: t.enumeration("termination"),
                    repsMin: t.int("repsMin"),
                    repsMax: t.int("repsMax"),
                    load: t.mass("load"),
                    loadPercent: t.decimal("loadPercent"),
                    loadReference: t.enumeration("loadReference"),
                    referenceLoad: t.mass("referenceLoad"),
                    assistance: t.mass("assistance"),
                    rpeMin: t.decimal("rpeMin"),
                    rpeMax: t.decimal("rpeMax"),
                    rirMin: t.decimal("rirMin"),
                    rirMax: t.decimal("rirMax"),
                    durationMin: t.duration("durationMin"),
                    durationMax: t.duration("durationMax"),
                    timerMode: t.enumeration("timerMode"),
                    distance: t.length("distance"),
                    tempo: t.string("tempo"),
                    miniSetReps: t.int("miniSetReps"),
                    miniSets: t.int("miniSets"),
                    myoStopRule: t.enumeration("myoStopRule"),
                    totalReps: t.int("totalReps"),
                    drops: t.int("drops"),
                    dropPercent: t.decimal("dropPercent"),
                    dropStep: t.mass("dropStep"),
                    dropReps: t.int("dropReps"),
                    intraSetPause: t.duration("intraSetPause")
                )
                target.extra = t.extra
                return target
            },
            origin: r.recordRef("origin", kind: nil),
            revision: r.int("revision")
        )
        set.extra = r.extra
        return set
    }

    func encode(_ set: PerformedSet) -> FlatRecord {
        var w = PropertyWriter(schema: setSchema.allProperties, extra: set.extra)
        w.datetime("at", set.at)
        w.reference("session", kind: kinds.session, id: set.session.rawValue)
        w.reference("exercise", kind: kinds.exercise, id: set.exercise.rawValue)
        w.string("slot", set.slot)
        w.int("order", set.order)
        w.int("ordinal", set.ordinal)
        w.reference("setup", kind: kinds.setup, id: set.setup?.rawValue)
        w.enumeration("role", set.role, omitting: .working)
        w.enumeration("roleLabel", set.roleLabel)
        w.enumeration("structure", set.structure, omitting: .straight)
        w.reference("continues", kind: kinds.set, id: set.continues?.rawValue)
        w.int("chainPosition", set.chainPosition)
        w.enumeration("side", set.side)
        w.enumeration("loadMode", set.loadMode)
        w.mass("load", set.load)
        w.mass("assistance", set.assistance)
        w.string("resistanceLabel", set.resistanceLabel)
        w.int("reps", set.reps)
        w.duration("duration", set.duration)
        w.length("distance", set.distance)
        w.enumeration("loadBasis", set.loadBasis)
        w.int("implements", set.implements)
        w.decimal("bodyShare", set.bodyShare)
        w.enumeration("laterality", set.laterality)
        w.enumeration("repsBasis", set.repsBasis)
        // A method goes with its value: with none, not even one kept raw.
        w.decimal("rpe", set.rpe)
        w.enumeration("rpeMethod", set.rpe == nil ? nil : set.rpeMethod, keepingRaw: set.rpe != nil)
        w.decimal("rir", set.rir)
        w.enumeration("rirMethod", set.rir == nil ? nil : set.rirMethod, keepingRaw: set.rir != nil)
        w.enumeration("rpeScale", set.rpeScale)
        w.bool("reachedFailure", set.reachedFailure)
        w.bool("failedRepAttempted", set.failedRepAttempted)
        w.enumeration("completionMethod", set.completionMethod)
        w.string("notes", set.notes)
        w.string("sourceType", set.sourceType)
        w.enumeration("timeBasis", set.timeBasis)
        w.datetime("startedAt", set.startedAt)
        w.duration("restBefore", set.restBefore)
        w.enumeration("restMethod", set.restMethod)
        w.duration("restPlanned", set.restPlanned)
        w.bool("restSkipped", set.restSkipped)
        w.object("target", set.target.map { target in
            var t = PropertyWriter(schema: setSchema.fields("target"), extra: target.extra)
            t.string("plannedSet", target.plannedSet)
            t.enumeration("termination", target.termination)
            t.int("repsMin", target.repsMin)
            t.int("repsMax", target.repsMax)
            t.mass("load", target.load)
            t.decimal("loadPercent", target.loadPercent)
            t.enumeration("loadReference", target.loadReference)
            t.mass("referenceLoad", target.referenceLoad)
            t.mass("assistance", target.assistance)
            t.decimal("rpeMin", target.rpeMin)
            t.decimal("rpeMax", target.rpeMax)
            t.decimal("rirMin", target.rirMin)
            t.decimal("rirMax", target.rirMax)
            t.duration("durationMin", target.durationMin)
            t.duration("durationMax", target.durationMax)
            t.enumeration("timerMode", target.timerMode)
            t.length("distance", target.distance)
            t.string("tempo", target.tempo)
            t.int("miniSetReps", target.miniSetReps)
            t.int("miniSets", target.miniSets)
            t.enumeration("myoStopRule", target.myoStopRule)
            t.int("totalReps", target.totalReps)
            t.int("drops", target.drops)
            t.decimal("dropPercent", target.dropPercent)
            t.mass("dropStep", target.dropStep)
            t.int("dropReps", target.dropReps)
            t.duration("intraSetPause", target.intraSetPause)
            return t.finish()
        })
        w.recordRef("origin", set.origin)
        w.int("revision", set.revision)
        return FlatRecord(kind: kinds.set, id: set.id.rawValue, properties: w.finish())
    }
}

// MARK: - Setup

public extension RecordCodec {
    func decodeSetup(_ record: FlatRecord) throws(CodecError) -> Setup {
        try check(record, is: kinds.setup)
        var r = PropertyReader(record: record)
        var setup = try Setup(
            id: SetupID(record.id),
            name: r.requiredString("name"),
            exercise: ExerciseID(r.requiredReference("exercise", kind: kinds.exercise)),
            gym: r.string("gym"),
            loadBase: r.mass("loadBase"),
            loadStep: r.mass("loadStep"),
            loadMax: r.mass("loadMax"),
            notes: r.string("notes")
        )
        setup.extra = r.extra
        return setup
    }

    func encode(_ setup: Setup) -> FlatRecord {
        var w = PropertyWriter(schema: setupSchema.allProperties, extra: setup.extra)
        w.string("name", setup.name)
        w.reference("exercise", kind: kinds.exercise, id: setup.exercise.rawValue)
        w.string("gym", setup.gym)
        w.mass("loadBase", setup.loadBase)
        w.mass("loadStep", setup.loadStep)
        w.mass("loadMax", setup.loadMax)
        w.string("notes", setup.notes)
        return FlatRecord(kind: kinds.setup, id: setup.id.rawValue, properties: w.finish())
    }
}
