import Foundation
import SubstrateCore

/// The two kinds to and from the properties the REST API speaks, by the
/// package's declarations (apps/tasks/package): what a read answers, the
/// body of a create, and a patch of what an edit changed. Reading is
/// lenient (a value the app does not know reads as the kind's default), so a
/// newer build's record never hides a task; writing names only what the
/// app means to set, so nothing it does not know is ever written over.
public enum TasksCodec {
    /// The property names each kind declares that the app reads or writes,
    /// in declaration order; a test holds them to the closure the app carries.
    public static let taskProperties = ["name", "description", "url", "list", "position", "dueDay", "priority", "status", "completedAt"]
    /// The temporal trait's point, a property the trait binds rather than
    /// the kind declares.
    public static let taskTraitProperties = ["dueAt"]
    public static let listProperties = ["name", "position"]

    // MARK: Reading

    /// A task from its record; `calendar` names the day of a moment that
    /// came without one.
    public static func task(_ record: Record, in calendar: Calendar) -> TaskItem {
        let p = record.properties
        let dueAt = p["dueAt"]?.text.flatMap(Timestamp.parse)
        let dueDay = p["dueDay"]?.text.flatMap(CivilDay.init)
        let due: Due? = if let dueAt { .time(dueAt, day: dueDay ?? CivilDay(dueAt, in: calendar)) } else { dueDay.map(Due.day) }
        return TaskItem(
            id: record.id, name: p["name"]?.text ?? "", notes: nonEmpty(p["description"]?.text), url: nonEmpty(p["url"]?.text),
            list: listID(p["list"]), position: decimal(p["position"]), due: due,
            priority: p["priority"]?.text.flatMap(TaskPriority.init(rawValue:)) ?? .none,
            status: p["status"]?.text.flatMap(TaskStatus.init(rawValue:)) ?? .open,
            completedAt: p["completedAt"]?.text.flatMap(Timestamp.parse), version: record.version > 0 ? record.version : nil,
            createdAt: Timestamp.parse(record.createdAt))
    }

    public static func list(_ record: Record) -> TaskList {
        TaskList(id: record.id, name: record.properties["name"]?.text ?? "", position: decimal(record.properties["position"]),
                 version: record.version > 0 ? record.version : nil)
    }

    /// A list's id from a task's `list`: `{ref: <kind>/<id>}` as served, or
    /// the bare path as written.
    static func listID(_ value: JSONValue?) -> String? {
        guard let reference = RecordReference(value), reference.kind == TasksKinds.list else { return nil }
        return reference.recordID
    }

    static func decimal(_ value: JSONValue?) -> Decimal? {
        switch value {
        case let .string(text)?: DecimalText.parse(text)
        case let .number(number)?: number.decimal
        default: nil
        }
    }

    static func nonEmpty(_ text: String?) -> String? {
        guard let text, !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
        return text
    }

    // MARK: Writing

    /// A create's body: what the task holds, never its state, which moves
    /// only by patch, so a create sent again after the task was done never
    /// moves it back.
    public static func create(_ task: TaskItem) -> JSONObject {
        var out = JSONObject()
        out["name"] = .string(task.name)
        if let notes = task.notes { out["description"] = .string(notes) }
        if let url = task.url { out["url"] = .string(url) }
        if let list = task.list { out["list"] = listValue(list) }
        if let position = task.position { out["position"] = .string(DecimalText.format(position)) }
        for (key, value) in dueValues(task.due) where value != .null { out[key] = value }
        if task.priority != .none { out["priority"] = .string(task.priority.rawValue) }
        return out
    }

    public static func create(_ list: TaskList) -> JSONObject {
        var out: JSONObject = ["name": .string(list.name)]
        if let position = list.position { out["position"] = .string(DecimalText.format(position)) }
        return out
    }

    /// What changed from `old` to `new`, as a patch: a value cleared is
    /// null, a state moved names its stamp where reopening must clear it.
    public static func patch(from old: TaskItem, to new: TaskItem) -> JSONObject {
        var out = JSONObject()
        if old.name != new.name { out["name"] = .string(new.name) }
        if old.notes != new.notes { out["description"] = new.notes.map(JSONValue.string) ?? .null }
        if old.url != new.url { out["url"] = new.url.map(JSONValue.string) ?? .null }
        if old.list != new.list { out["list"] = new.list.map(listValue) ?? .null }
        if old.position != new.position { out["position"] = new.position.map { .string(DecimalText.format($0)) } ?? .null }
        if old.due != new.due {
            for (key, value) in dueValues(new.due) { out[key] = value }
        }
        if old.priority != new.priority { out["priority"] = .string(new.priority.rawValue) }
        if old.status != new.status {
            out["status"] = .string(new.status.rawValue)
            // The move to done stamps completedAt; nothing removes it on the
            // way back, so a reopen clears it in the same write.
            if new.status == .open { out["completedAt"] = .null }
        }
        return out
    }

    public static func patch(from old: TaskList, to new: TaskList) -> JSONObject {
        var out = JSONObject()
        if old.name != new.name { out["name"] = .string(new.name) }
        if old.position != new.position { out["position"] = new.position.map { .string(DecimalText.format($0)) } ?? .null }
        return out
    }

    static func listValue(_ id: String) -> JSONValue { .string("\(TasksKinds.list)/\(id)") }

    /// `dueDay` and `dueAt` as a pair: a day alone clears the moment.
    static func dueValues(_ due: Due?) -> [(String, JSONValue)] {
        switch due {
        case nil: [("dueDay", .null), ("dueAt", .null)]
        case let .day(day)?: [("dueDay", .string(day.description)), ("dueAt", .null)]
        case let .time(instant, day)?: [("dueDay", .string(day.description)), ("dueAt", .string(Timestamp.format(instant, precision: .seconds)))]
        }
    }
}
