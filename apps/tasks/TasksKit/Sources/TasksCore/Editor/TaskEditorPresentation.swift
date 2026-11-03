import Foundation

/// The edit form: each field under the label the read view gives it, with
/// what it holds, and Save and Cancel.
public struct TaskEditorPresentation: Codable, Equatable, Sendable {
    public var title: String
    public var name: EditorText
    public var notes: EditorText
    public var link: EditorText
    public var list: EditorChoice
    public var due: EditorDue
    public var priority: EditorChoice
    public var save: String
    public var canSave: Bool
    public var cancel: String
    /// Why Save is off, once the owner has typed.
    public var problem: String?

    /// Each field's label, by the fact it edits: what the read view's
    /// labels are held to.
    public var labels: [TaskField: String] {
        [.name: name.label, .notes: notes.label, .link: link.label, .list: list.label, .due: due.label, .priority: priority.label]
    }
}

public struct EditorText: Codable, Hashable, Sendable {
    public var label: String
    public var value: String
    public var prompt: String
}

public struct EditorChoice: Codable, Hashable, Sendable {
    public var label: String
    /// The option chosen, by id ("" for none).
    public var selected: String
    public var options: [EditorOption]
}

public struct EditorOption: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var title: String
}

public struct EditorDue: Codable, Hashable, Sendable {
    public var label: String
    public var kind: DueKind
    public var kinds: [EditorOption]
    /// The day, or the day and time, the picker shows; nil with no due date.
    public var date: Date?
    /// "Day", or "Day and Time".
    public var dateLabel: String?
}
