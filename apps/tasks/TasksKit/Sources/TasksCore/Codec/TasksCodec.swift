import Foundation
import SubstrateCore

/// A task to the properties the REST API speaks, through its collection's
/// readings (the model reads them back, `TasksModel`): the body of a create
/// and a patch of what an edit changed. Writing names only what the app
/// means to set, so nothing it does not read is ever written over, and never
/// the status, which moves only by a decision's patch (`TasksModel.outcome`).
public enum TasksCodec {
    /// The property names the app's own kinds declare that it reads or
    /// writes, in declaration order; a test holds them to the closure the
    /// app carries.
    public static let taskProperties = ["name", "description", "url", "list", "position", "dueDay", "priority", "status", "completedAt"]
    public static let listProperties = ["name", "position"]

    /// A create's body: what the task holds, never its state, so a create
    /// sent again after the task was done never moves it back.
    public static func create(_ task: TaskItem, in collection: TaskCollection) -> JSONObject {
        var out = JSONObject()
        if let title = collection.title { out[title] = .string(task.name) }
        if let notes = collection.notes, let value = task.notes { out[notes] = .string(value) }
        if let link = collection.link, let value = task.url { out[link] = .string(value) }
        if let group = collection.group, case let .record(path)? = task.group { out[group.property] = .string(path) }
        if let order = collection.order, let position = task.position { out[order] = .string(DecimalText.format(position)) }
        for (key, value) in dueValues(task.due, in: collection) where value != .null { out[key] = value }
        if let priority = collection.priority, let value = task.priorityValue, value != "none" {
            out[priority] = .string(value)
        }
        return out
    }

    public static func create(_ list: TaskGroup) -> JSONObject {
        var out: JSONObject = ["name": .string(list.name)]
        if let position = list.position { out["position"] = .string(DecimalText.format(position)) }
        return out
    }

    /// What changed from `old` to `new`, as a patch of the readings' own
    /// properties: a value cleared is null.
    public static func patch(from old: TaskItem, to new: TaskItem, in collection: TaskCollection) -> JSONObject {
        var out = JSONObject()
        if let title = collection.title, old.name != new.name { out[title] = .string(new.name) }
        if let notes = collection.notes, old.notes != new.notes { out[notes] = new.notes.map(JSONValue.string) ?? .null }
        if let link = collection.link, old.url != new.url { out[link] = new.url.map(JSONValue.string) ?? .null }
        if let group = collection.group, old.group != new.group {
            if case let .record(path)? = new.group { out[group.property] = .string(path) } else { out[group.property] = .null }
        }
        if let order = collection.order, old.position != new.position {
            out[order] = new.position.map { .string(DecimalText.format($0)) } ?? .null
        }
        if old.due != new.due {
            for (key, value) in dueValues(new.due, in: collection) { out[key] = value }
        }
        if let priority = collection.priority, old.priorityValue != new.priorityValue {
            out[priority] = new.priorityValue.map(JSONValue.string) ?? .null
        }
        return out
    }

    public static func patch(from old: TaskGroup, to new: TaskGroup) -> JSONObject {
        var out = JSONObject()
        if old.name != new.name { out["name"] = .string(new.name) }
        if old.position != new.position { out["position"] = new.position.map { .string(DecimalText.format($0)) } ?? .null }
        return out
    }

    /// The due reading and the due day reading as a pair: a day alone
    /// clears the moment. A kind with no due day reading takes no day alone.
    static func dueValues(_ due: Due?, in collection: TaskCollection) -> [(String, JSONValue)] {
        let values: (day: JSONValue, at: JSONValue) =
            switch due {
            case nil: (.null, .null)
            case let .day(day)?: (.string(day.description), .null)
            case let .time(instant, day)?: (.string(day.description), .string(Timestamp.format(instant, precision: .seconds)))
            }
        var out: [(String, JSONValue)] = []
        if let dueDay = collection.dueDay { out.append((dueDay, values.day)) }
        if let at = collection.due { out.append((at, values.at)) }
        return out
    }

    /// A reference property's value as a write names it: the bare path.
    static func reference(_ path: String) -> JSONValue { .string(path) }
}
