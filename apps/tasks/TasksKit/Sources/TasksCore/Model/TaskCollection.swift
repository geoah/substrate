import Foundation
import SubstrateAuth
import SubstrateCore

/// The reference a collection's tasks are grouped by: the property, the
/// kind it points at, and the reference's label ("List", "Project").
public struct GroupReading: Codable, Hashable, Sendable {
    public var property: String
    public var kind: String

    public init(property: String, kind: String) {
        self.property = property
        self.kind = kind
    }

    /// "list", "project": the reference's label inside a sentence.
    public var label: String { propertyLabel(property).lowercased() }
}

/// What the app reads a property as, each by the property's name; nil is
/// none.
public struct TaskReadings: Codable, Hashable, Sendable {
    public var due: String?
    public var dueDay: String?
    public var priority: String?
    public var order: String?
    public var group: GroupReading?

    public init(due: String? = nil, dueDay: String? = nil, priority: String? = nil, order: String? = nil, group: GroupReading? = nil) {
        self.due = due
        self.dueDay = dueDay
        self.priority = priority
        self.order = order
        self.group = group
    }
}

/// The owner's choices about one collection: its
/// `substrate.reamde.dev/tasks/collection` record, read by its `kind`.
/// A reading's property is the record's where it holds one, `-` for none.
public struct CollectionSetting: Hashable, Sendable {
    public static let none = "-"

    public var kind: String
    public var enabled: Bool?
    public var group: String?
    public var due: String?
    public var dueDay: String?
    public var priority: String?
    public var order: String?
    public var recordID: String
    public var version: Int?

    public init?(_ record: Record) {
        guard record.kind == TasksKinds.collection, record.deletedAt == nil, let kind = record["kind"]?.text else { return nil }
        self.kind = kind
        enabled = record["enabled"]?.bool
        group = record["group"]?.text
        due = record["due"]?.text
        dueDay = record["dueDay"]?.text
        priority = record["priority"]?.text
        order = record["order"]?.text
        recordID = record.id
        version = record.version > 0 ? record.version : nil
    }

    /// The id every device names a kind's record by.
    public static func id(for kind: String) -> String { DerivedIDs.collection(kind) }
}

/// What the owner chose across every collection: the one
/// `substrate.reamde.dev/tasks/preference` record.
public struct TasksPreference: Hashable, Sendable {
    /// The kind new tasks are written to; nil is the app's own.
    public var newTasks: String?
    /// The pinned groups, by record path, in the owner's order.
    public var pins: [String]
    public var recordID: String?
    public var version: Int?

    public init(newTasks: String? = nil, pins: [String] = [], recordID: String? = nil, version: Int? = nil) {
        self.newTasks = newTasks
        self.pins = pins
        self.recordID = recordID
        self.version = version
    }

    public init(_ record: Record) {
        newTasks = record["newTasks"]?.text
        pins = (record["pins"]?.array ?? []).compactMap { RecordReference($0)?.path }
        recordID = record.id
        version = record.version > 0 ? record.version : nil
    }

    public static var id: String { DerivedIDs.preference }
}

/// A candidate: a kind that binds the lifecycle trait with a status the
/// app reads, and that the grant reads, with what the app reads from its
/// declaration and the owner's choices (the contract's "readings").
public struct TaskCollection: Hashable, Sendable {
    public var declaration: TaskKindDeclaration
    public var status: KindStatus
    public var setting: CollectionSetting?
    /// The collection is on: the app's own unless turned off, any other
    /// only once turned on.
    public var enabled: Bool
    /// The grant writes the kind; "View only" where it does not.
    public var writable: Bool
    /// New tasks may be written to it: it takes them, and it is where the
    /// preference sends them.
    public var creatable: Bool
    /// The grant writes it and the editor edits every property it requires:
    /// it may be where new tasks go.
    public var takesNew: Bool
    /// The property the title is edited through; nil where it is not.
    public var title: String?
    /// The readings in force, the owner's choice over the guess.
    public var readings: TaskReadings
    /// What the app would read with no choice of the owner's.
    public var guess: TaskReadings
    /// Every single reference the grant reads the target of, in declared
    /// order: what the collection may be grouped by.
    public var references: [GroupReading]
    /// Every single reference, read or not, in declared order.
    public var allReferences: [GroupReading]
    /// The reference the owner chose to group by, read or not: the group
    /// reading once the grant reads its kind, and until then a grouping out
    /// of reach, which the collection's page keeps chosen and asks to read.
    public var chosenGroup: GroupReading?

    public var kind: String { declaration.identity }
    public var viewOnly: Bool { !writable }
    public var isOwn: Bool { kind == TasksKinds.task }
    public var group: GroupReading? { readings.group }
    public var due: String? { readings.due }
    public var dueDay: String? { readings.dueDay }
    public var priority: String? { readings.priority }
    public var order: String? { readings.order }

    /// "Tasks", "Chores": what a sentence calls the collection.
    public var label: String { declaration.pluralLabel }

    /// The priority's values, lowest first.
    public var priorities: [String] { priority.map(declaration.values(of:)) ?? [] }

    /// Whether a priority value is pressing: of four or more values the top
    /// two, of two or three the top one, of one none.
    public func pressing(_ value: String?) -> Bool {
        guard let value, let index = priorities.firstIndex(of: value) else { return false }
        let n = priorities.count
        let top = n >= 4 ? 2 : n >= 2 ? 1 : 0
        return index >= n - top
    }

    public func rank(_ value: String?) -> Int { value.flatMap { priorities.firstIndex(of: $0) } ?? -1 }

    /// The property a record's description is read from, under its label.
    public var notes: String? {
        ["description", "notes", "body"].first { ["markdown", "string"].contains(declaration.type(of: $0) ?? "") }
    }

    /// The property a link goes in, where the kind keeps one.
    public var link: String? { declaration.type(of: "url") == "url" ? "url" : nil }

    /// The readings of `declaration` under `grant`, with the owner's
    /// `setting` and the preference's choice of where new tasks go.
    public init?(_ declaration: TaskKindDeclaration, setting: CollectionSetting?, preference: TasksPreference, grant: Grants) {
        guard let status = declaration.status, grant.reads(declaration.grantKind) else { return nil }
        self.declaration = declaration
        self.status = status
        self.setting = setting
        let own = declaration.identity == TasksKinds.task
        enabled = own ? setting?.enabled != false : setting?.enabled == true
        writable = grant.writes(declaration.grantKind)
        title = declaration.editableTitle

        let reads = { (kind: String) in grant.reads(kind) }
        allReferences = declaration.propertyNames.compactMap { name in
            declaration.singleReference(name).map { GroupReading(property: name, kind: $0) }
        }
        references = allReferences.filter { reads($0.kind) }

        let declared = { (name: String, type: String) in declaration.type(of: name) == type ? name : nil }
        var guess = TaskReadings()
        if let point = declaration.temporalPoint, declaration.type(of: point).map({ $0 == "datetime" }) ?? true {
            guess.due = point
        } else {
            guess.due = declared("dueAt", "datetime")
        }
        guess.dueDay = declared("dueDay", "date")
        guess.priority = declared("priority", "enum")
        guess.order = declared("position", "decimal")
        let named = references.filter { ["list", "project", "area"].contains($0.property) }
        guess.group = named.count == 1 ? named[0] : nil
        self.guess = guess

        // A property the owner chose that the declaration no longer holds as
        // the reading needs it (renamed, retyped, made repeated) is no
        // choice: the guess stands, and nothing is written under the old name.
        func chosen(_ value: String?, _ guess: String?, type: String) -> String? {
            guard let value else { return guess }
            if value == CollectionSetting.none { return nil }
            guard declaration.type(of: value) == type, declaration.property(value)?["repeated"]?.bool != true else { return guess }
            return value
        }
        var readings = TaskReadings(
            due: chosen(setting?.due, guess.due, type: "datetime"), dueDay: chosen(setting?.dueDay, guess.dueDay, type: "date"),
            priority: chosen(setting?.priority, guess.priority, type: "enum"), order: chosen(setting?.order, guess.order, type: "decimal"),
            group: guess.group)
        chosenGroup = guess.group
        if let group = setting?.group {
            if group == CollectionSetting.none {
                readings.group = nil
                chosenGroup = nil
            } else if let reference = allReferences.first(where: { $0.property == group }) {
                readings.group = references.contains(reference) ? reference : nil
                chosenGroup = reference
            }
        }
        self.readings = readings

        let edited = Set([title, readings.due, readings.dueDay, readings.priority, readings.order, readings.group?.property].compactMap { $0 })
        takesNew = writable && declaration.required.allSatisfy { edited.contains($0) }
        creatable = takesNew && declaration.identity == (preference.newTasks ?? TasksKinds.task)
    }
}

/// The ids the settings records are kept under, derived from what each
/// describes so every device names the same record.
public enum DerivedIDs {
    public static func collection(_ kind: String) -> String { DerivedID.uuid("collection:\(kind)") }
    public static var preference: String { DerivedID.uuid("preference") }
}
