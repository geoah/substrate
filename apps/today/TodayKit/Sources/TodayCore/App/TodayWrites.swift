import Foundation
import SubstrateCore
import SubstrateLocal

/// The writes the pane makes, each in the shape docs/records.md gives it,
/// through the kit's `RecordWrites`, so every one goes to the outbox and is
/// sent when the app reconnects. A create never names a state: a state
/// moves by a patch that follows the create in its batch. A record whose
/// id is fixed by what it is about (a log, the plan, a review mark, the
/// preference) is created when the device has not read it and changed when
/// it has, so two devices that write it meet at one record.
struct TodayWrites {
    let writes: RecordWrites
    let day: Day

    init(day: Day, mint: @escaping () -> UUID) {
        writes = RecordWrites(mint: mint)
        self.day = day
    }

    static func stamp(_ date: Date) -> JSONValue { .string(Timestamp.format(date, precision: .seconds)) }
    static func reference(_ path: String) -> JSONValue { .string(path) }

    func version(_ kind: String, _ id: String) -> Int? { day.versions["\(kind)/\(id)"] }

    /// A record under a fixed id: changed where the device holds it, else
    /// created with `created` and then changed.
    func upsert(_ kind: String, _ id: String, created: JSONObject, change: JSONObject) -> LocalWrite {
        if let version = version(kind, id) { return writes.change(kind, id, properties: change, version: version) }
        let create = writes.create(kind, id, properties: created)
        return LocalWrite(entries: create.entries + writes.change(kind, id, properties: change, version: nil).entries)
    }

    // MARK: Routines

    /// The log of one occurrence: its id is the occurrence's, so a tick
    /// made twice is one record.
    func log(_ occurrence: Occurrence, done: Bool) -> LocalWrite {
        let status: JSONObject = ["status": .string(done ? "done" : "skipped")]
        if let version = version(TodayKinds.log, occurrence.id) {
            return writes.change(TodayKinds.log, occurrence.id, properties: status, version: version)
        }
        let create = writes.create(
            TodayKinds.log, occurrence.id,
            properties: [
                "series": Self.reference(occurrence.series), "scheduledAt": Self.stamp(occurrence.slot), "at": Self.stamp(day.now),
            ])
        // `done` is the initial state: only a skip moves it.
        guard !done else { return create }
        return LocalWrite(entries: create.entries + writes.change(TodayKinds.log, occurrence.id, properties: status, version: nil).entries)
    }

    func unlog(_ id: String) -> LocalWrite { writes.delete(TodayKinds.log, id, version: version(TodayKinds.log, id)) }

    // MARK: Tasks

    func change(_ task: TaskItem, _ properties: JSONObject) -> LocalWrite {
        writes.change(TodayKinds.task, task.id, properties: properties, version: task.version)
    }

    /// The flag write: High, Medium, Low or none, never urgent, always as
    /// the owner's, with everything urgent left behind cleared.
    func flagProperties(_ flag: Flag?) -> JSONObject {
        var out: JSONObject = ["priority": .string(flag?.rawValue ?? "none")]
        guard day.discovery.taskMoves else { return out }
        out["prioritySetBy"] = "owner"
        for key in ["priorityReason", "priorityWas", "urgentCheckedAt", "urgentHeldAt", "urgentHeldReason"] { out[key] = .null }
        return out
    }

    func flag(_ task: TaskItem, _ flag: Flag?) -> LocalWrite { change(task, flagProperties(flag)) }

    /// "Not urgent": the flag it had comes back.
    func notUrgent(_ task: TaskItem) -> LocalWrite { flag(task, task.was) }

    /// "When": `notBefore`, and one more deferral when it moved later.
    func move(_ task: TaskItem, to moment: Date?) -> LocalWrite {
        guard day.discovery.taskMoves else { return LocalWrite(entries: []) }
        var out: JSONObject = ["notBefore": moment.map(Self.stamp) ?? .null]
        if let moment, moment > max(task.notBefore ?? .distantPast, day.now) { out["deferrals"] = .number(Decimal(task.deferrals + 1)) }
        return change(task, out)
    }

    func project(_ task: TaskItem, _ list: String?) -> LocalWrite {
        change(task, ["list": list.map { Self.reference("\(TodayKinds.list)/\($0)") } ?? .null])
    }

    func status(_ task: TaskItem, _ status: TaskItem.Status) -> LocalWrite { change(task, ["status": .string(status.rawValue)]) }

    /// A suggested task's answer: a task, already done, or dismissed.
    func answer(_ task: TaskItem, _ answer: SuggestedAnswer) -> LocalWrite {
        switch answer {
        case .task:
            var out: JSONObject = ["status": "open"]
            // Taken by the owner, its flag is the owner's.
            if day.discovery.taskMoves { out["prioritySetBy"] = "owner" }
            return change(task, out)
        case .done: return status(task, .done)
        case .dismiss: return status(task, .abandoned)
        }
    }

    /// A task typed into the field: a task at once, with no flag.
    func add(_ name: String, id: String) -> LocalWrite { writes.create(TodayKinds.task, id, properties: ["name": .string(name)]) }

    // MARK: Projects

    func priority(_ list: ListItem, _ flag: Flag?) -> LocalWrite {
        guard day.discovery.listPriority else { return LocalWrite(entries: []) }
        return writes.change(TodayKinds.list, list.id, properties: ["priority": .string(flag?.rawValue ?? "none")], version: list.version)
    }

    // MARK: What the assistant suggests on a record that exists

    /// Accepting or rejecting a request, on the version the owner saw. An
    /// accepted flag is taken as the owner's: the adjusted diff says so.
    func decide(_ suggestion: Suggestion, accept: Bool) -> LocalWrite {
        var out: JSONObject = ["decision": .string(accept ? "accepted" : "rejected")]
        if accept, let flag = suggestion.flag, suggestion.target.hasPrefix(TodayKinds.task + "/") {
            out["adjustedDiff"] = .object(["properties": .object(flagProperties(flag))])
        }
        return writes.edit(TodayKinds.patchRequest, suggestion.id, properties: out, seen: suggestion.version)
    }

    // MARK: The plan, the marks and the preference

    func plan(_ change: JSONObject) -> LocalWrite {
        upsert(TodayKinds.plan, PlanRecord.id(day.today), created: ["day": .string(day.today.description)], change: change)
    }

    /// One overlap's choice, kept beside the others the plan holds.
    func overlaps(replacing overlap: DayEngine.Overlap, chosen: String, kept: [PlanRecord.Overlap]) -> JSONValue {
        let paths = overlap.meetings.map(\.path)
        var out = kept.filter { Set($0.events) != Set(paths) }
        out.append(PlanRecord.Overlap(events: paths, chosen: chosen, at: day.now))
        return Self.encode(out)
    }

    static func encode(_ overlaps: [PlanRecord.Overlap]) -> JSONValue {
        .array(
            overlaps.map { overlap in
                var value: JSONObject = ["events": .array(overlap.events.map(JSONValue.string))]
                if let chosen = overlap.chosen { value["chosen"] = .string(chosen) }
                if let at = overlap.at { value["at"] = stamp(at) }
                return .object(value)
            })
    }

    static func encode(_ sends: [PlanRecord.Send]) -> JSONValue {
        .array(
            sends.map { send in
                var value: JSONObject = ["id": .string(send.id), "text": .string(send.text), "ticked": .bool(send.ticked)]
                if let to = send.to { value["to"] = .string(to) }
                if let name = send.toName { value["toName"] = .string(name) }
                if let at = send.sentAt { value["sentAt"] = stamp(at) }
                return .object(value)
            })
    }

    func mark(_ key: String, through: Date) -> LocalWrite {
        var created: JSONObject = [:]
        if key != TodayKinds.unfiled { created["project"] = Self.reference("\(TodayKinds.list)/\(key)") }
        created["through"] = Self.stamp(through)
        if version(TodayKinds.reviewmark, key) == nil { return writes.create(TodayKinds.reviewmark, key, properties: created) }
        return writes.change(TodayKinds.reviewmark, key, properties: ["through": Self.stamp(through)], version: version(TodayKinds.reviewmark, key))
    }

    /// One of the assistant's standing instructions, changed on the version
    /// the owner saw: another hand's change in between is not written over.
    func instruction(_ instruction: Instruction, _ change: JSONObject) -> LocalWrite {
        writes.edit(TodayKinds.instruction, instruction.id, properties: change, seen: instruction.version)
    }

    func preference(_ change: JSONObject) -> LocalWrite {
        upsert(TodayKinds.preference, TodayKinds.preferenceID, created: [:], change: change)
    }
}
