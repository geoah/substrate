import Foundation
import SubstrateCore

/// Every collection the grant reads with a status, and the records of the
/// ones that are on, read through each one's readings
/// (apps/tasks/contract/README.md): the tasks by record path, the groups
/// they point at, and the owner's preference. The presenters, the
/// enrichment and the contract's tests all read the records through it.
public struct TasksModel: Equatable, Sendable {
    /// Every candidate: the app's own first, then by kind.
    public var collections: [TaskCollection]
    /// Every record of an enabled collection that is not deleted, by path.
    public var tasks: [String: TaskItem]
    /// Every record of an enabled collection's group kind, by path.
    public var groups: [String: TaskGroup]
    public var preference: TasksPreference
    /// Every declaration read, by kind.
    public var declarations: [String: TaskKindDeclaration]
    /// The device's calendar, in its time zone: a moment's day is its day.
    public var calendar: Calendar

    public init(
        collections: [TaskCollection] = [], tasks: [String: TaskItem] = [:], groups: [String: TaskGroup] = [:],
        preference: TasksPreference = TasksPreference(), declarations: [String: TaskKindDeclaration] = [:],
        calendar: Calendar = Calendar(identifier: .gregorian)
    ) {
        self.collections = collections
        self.tasks = tasks
        self.groups = groups
        self.preference = preference
        self.declarations = declarations
        self.calendar = calendar
    }

    /// The model of `records` (deleted ones are absent) under `grant`, each
    /// kind read by its declaration in `declarations`.
    public init(declarations: [TaskKindDeclaration], records: some Collection<Record>, grant: Grants, calendar: Calendar) {
        var byKind: [String: TaskKindDeclaration] = [:]
        for declaration in declarations where byKind[declaration.identity] == nil { byKind[declaration.identity] = declaration }
        let live = records.filter { $0.deletedAt == nil }
        var held: [String: Record] = [:]
        for record in live { held[record.reference.path] = record }

        var settings: [String: CollectionSetting] = [:]
        var preference: TasksPreference?
        for record in live.sorted(by: { $0.id < $1.id }) {
            if let setting = CollectionSetting(record) {
                // The record under the derived id wins over one a device
                // wrote under any other.
                if settings[setting.kind] == nil || record.id == DerivedIDs.collection(setting.kind) { settings[setting.kind] = setting }
            } else if record.kind == TasksKinds.preference, preference == nil || record.id == DerivedIDs.preference {
                preference = TasksPreference(record)
            }
        }
        let chosen = preference ?? TasksPreference()

        let collections = byKind.values.compactMap { TaskCollection($0, setting: settings[$0.identity], preference: chosen, grant: grant) }
            .sorted { ($0.isOwn ? 0 : 1, $0.kind) < ($1.isOwn ? 0 : 1, $1.kind) }
        let enabled = Dictionary(collections.filter(\.enabled).map { ($0.kind, $0) }) { a, _ in a }
        let groupKinds = Set(enabled.values.compactMap(\.group?.kind))

        var tasks: [String: TaskItem] = [:]
        var groups: [String: TaskGroup] = [:]
        for record in live {
            if let collection = enabled[record.kind] {
                let task = Self.item(record, in: collection, held: held, calendar: calendar)
                tasks[task.id] = task
            }
            if groupKinds.contains(record.kind) {
                let group = Self.group(record, declaration: byKind[record.kind])
                groups[group.id] = group
            }
        }
        self.init(collections: collections, tasks: tasks, groups: groups, preference: chosen, declarations: byKind, calendar: calendar)
    }

    // MARK: Reading a record

    static func item(_ record: Record, in collection: TaskCollection, held: [String: Record], calendar: Calendar) -> TaskItem {
        let p = record.properties
        let text = { (name: String?) in name.flatMap { p[$0]?.text } }
        let instant = text(collection.due).flatMap(Timestamp.parse)
        let day = text(collection.dueDay).flatMap(CivilDay.init)
        let due: Due? = if let instant { .time(instant, day: day ?? CivilDay(instant, in: calendar)) } else { day.map(Due.day) }
        var group: TaskGroupLink?
        if let reading = collection.group {
            if let reference = RecordReference(p[reading.property]) {
                group = reference.kind == reading.kind && held[reference.path] != nil ? .record(reference.path) : .missing
            } else {
                group = TaskGroupLink.none
            }
        }
        let status = p[collection.status.property]?.text
        let priority = text(collection.priority)
        let completed: Date? = status.flatMap { value in
            collection.declaration.stamps(into: value, of: collection.status.property).lazy.compactMap { p[$0]?.text.flatMap(Timestamp.parse) }
                .first
        }
        return TaskItem(
            kind: record.kind, recordID: record.id, name: text(collection.title) ?? p["title"]?.text ?? record.id,
            notes: text(collection.notes), url: text(collection.link), group: group, position: decimal(collection.order.flatMap { p[$0] }),
            due: due, priorityValue: priority, priority: level(priority, in: collection), pressing: collection.pressing(priority),
            status: status, standing: collection.status.standing(of: status),
            actions: TaskActions(status: collection.status, from: status, writable: collection.writable),
            completedAt: completed ?? p["completedAt"]?.text.flatMap(Timestamp.parse),
            version: record.version > 0 ? record.version : nil, createdAt: Timestamp.parse(record.createdAt))
    }

    static func group(_ record: Record, declaration: TaskKindDeclaration?) -> TaskGroup {
        let status = declaration?.status
        let value = status.flatMap { record[$0.property]?.text }
        return TaskGroup(
            kind: record.kind, recordID: record.id, name: record["title"]?.text ?? record["name"]?.text ?? record.id,
            position: decimal(record["position"]), version: record.version > 0 ? record.version : nil,
            status: value.flatMap { value in status.map { declaration?.label(of: value, in: $0.property) ?? value } },
            standing: status.map { $0.standing(of: value) })
    }

    /// Where a priority value stands on the app's ladder: the app's own
    /// values as they are, any other by its place among its kind's.
    static func level(_ value: String?, in collection: TaskCollection) -> TaskPriority {
        guard let value, let index = collection.priorities.firstIndex(of: value) else { return .none }
        if let own = TaskPriority(rawValue: value) { return own }
        let n = collection.priorities.count
        if collection.pressing(value) { return index == n - 1 && n >= 4 ? .urgent : .high }
        return index == 0 ? .low : .medium
    }

    static func decimal(_ value: JSONValue?) -> Decimal? {
        switch value {
        case let .string(text)?: DecimalText.parse(text)
        case let .number(number)?: number.decimal
        default: nil
        }
    }

    // MARK: Asking it

    public func collection(_ kind: String) -> TaskCollection? { collections.first { $0.kind == kind } }

    public var enabled: [TaskCollection] { collections.filter(\.enabled) }

    /// The collection new tasks go to, where it takes them.
    public var newTasks: TaskCollection? { enabled.first(where: \.creatable) }

    /// Whether more than one collection is on: a row then names its
    /// collection.
    public var mixed: Bool { enabled.count > 1 }

    /// The kinds the data link reads: every enabled collection's, and the
    /// kinds they are grouped by.
    public var dataKinds: [String] {
        var out: [String] = []
        for collection in enabled {
            for kind in [collection.kind, collection.group?.kind].compactMap({ $0 }) where !out.contains(kind) { out.append(kind) }
        }
        return out
    }

    /// The app's own task lists, in the owner's order.
    public var arrangedLists: [TaskGroup] { groups.values.filter { $0.kind == TasksKinds.list }.arranged }

    /// Whether a selection shows `task`, open and closed alike; Needs you's
    /// are its open tasks and the suggestions' its proposed ones.
    public func holds(_ task: TaskItem, in selection: ListSelection) -> Bool {
        switch selection {
        case .needsYou: task.isOpen
        case .suggestions: task.isSuggestion
        case .noDate: task.isOpen && task.due == nil && !task.pressing
        case let .group(path): task.group == .record(path)
        case let .ungrouped(kind): task.kind == kind && task.group == TaskGroupLink.none
        case let .missing(kind): task.kind == kind && task.group == .missing
        case let .collection(kind): task.kind == kind
        }
    }

    public func tasks(in selection: ListSelection) -> [TaskItem] { tasks.values.filter { holds($0, in: selection) } }

    /// The highest position among the app's own tasks of a list (nil: in
    /// none).
    public func lastPosition(inList list: String?) -> Decimal? {
        tasks.values.filter { $0.kind == TasksKinds.task && $0.list == list }.compactMap(\.position).max()
    }

    /// The name of the group a task is in; nil where it is in none, or its
    /// group is out of reach.
    public func groupName(_ task: TaskItem) -> String? {
        guard case let .record(path)? = task.group else { return nil }
        return groups[path]?.name
    }

    /// Every suggestion, newest first, ties by path.
    public var suggestions: [TaskItem] {
        tasks.values.filter(\.isSuggestion).sorted { a, b in
            let (ca, cb) = (a.createdAt ?? .distantPast, b.createdAt ?? .distantPast)
            return ca != cb ? ca > cb : a.id < b.id
        }
    }

    /// One collection's groups holding open tasks: each group in the group
    /// kind's order where it has one (a decimal named `position`), else by
    /// title; then its tasks in no group, then those whose group is gone,
    /// each with its open count, and only where that is above 0.
    public func groupRows(of collection: TaskCollection) -> [(group: GroupRow, open: Int)] {
        guard collection.group != nil else { return [] }
        var counts: [String: Int] = [:]
        var none = 0
        var missing = 0
        for task in tasks.values where task.kind == collection.kind && task.isOpen {
            switch task.group {
            case let .record(path)?: counts[path, default: 0] += 1
            case .none?: none += 1
            case .missing?: missing += 1
            case nil: break
            }
        }
        let held = counts.keys.compactMap { groups[$0] }
        let named = ordered(held, of: collection.group?.kind ?? "")
        var out = named.map { (group: GroupRow.record($0.id), open: counts[$0.id] ?? 0) }
        if none > 0 { out.append((.none, none)) }
        if missing > 0 { out.append((.missing, missing)) }
        return out
    }

    /// Groups of `kind` in its order reading where it has one, a decimal
    /// named `position` (those holding none after), else by title.
    public func ordered(_ groups: [TaskGroup], of kind: String) -> [TaskGroup] {
        let byTitle = { (a: TaskGroup, b: TaskGroup) in (a.name.lowercased(), a.id) < (b.name.lowercased(), b.id) }
        guard declarations[kind]?.type(of: "position") == "decimal" else { return groups.sorted(by: byTitle) }
        return groups.sorted { a, b in
            switch (a.position, b.position) {
            case let (x?, y?) where x != y: return x < y
            case (.some, nil): return true
            case (nil, .some): return false
            default: return byTitle(a, b)
            }
        }
    }

    /// The pins that show, in the owner's order: a group record held and
    /// not deleted, of the group kind of an enabled collection.
    public var pins: [String] {
        let kinds = Set(enabled.compactMap(\.group?.kind))
        return preference.pins.filter { path in groups[path].map { kinds.contains($0.kind) } ?? false }
    }

    /// The pins a write of the preference keeps: those whose record is held,
    /// and those of a kind not read now, hidden because their collection is
    /// off, which nothing says is gone.
    public func pinsKept(held: [String: Record], reading kinds: [String]) -> [String] {
        preference.pins.filter { path in
            if let record = held[path] { return record.deletedAt == nil }
            return RecordReference(path: path).map { !kinds.contains($0.kind) } ?? false
        }
    }
}

/// A row of a collection's groups: a group, its tasks in no group, or
/// those whose group is gone or out of reach.
public enum GroupRow: Hashable, Sendable {
    case record(String)
    case none
    case missing
}

public extension [TaskGroup] {
    /// By `position`, groups with none last, then by name.
    var arranged: [TaskGroup] {
        sorted { a, b in
            switch (a.position, b.position) {
            case let (x?, y?) where x != y: return x < y
            case (.some, nil): return true
            case (nil, .some): return false
            default: return (a.name.lowercased(), a.id) < (b.name.lowercased(), b.id)
            }
        }
    }
}
