import Foundation
import SubstrateDevice
import SubstrateScreens

/// What this device reminds the owner of: each open task with a due date
/// still ahead, at its moment, or at the chosen hour of its day. The plan
/// is a value the presenter makes, so a test reads it, and the effects
/// schedule exactly it.
public enum Reminders {
    /// Every reminder's id starts with it, so the app cancels its own alone.
    public static let prefix = "tasks.reminder."
    /// The most a system keeps waiting for one app (iOS keeps 64); the
    /// soonest go first.
    public static let limit = 60

    public static func id(_ task: String) -> String { prefix + task }

    public static func plan(_ model: TasksModel, settings: SettingsValues, permission: NotificationPermission?, now: Date,
                            calendar: Calendar, locale: Locale) -> [NotificationRequest]
    {
        guard TasksSettings.reminds(settings), permission == .allowed else { return [] }
        var out: [NotificationRequest] = []
        for task in model.tasks.values where task.isOpen {
            guard let due = task.due else { continue }
            let at = due.instant ?? due.day.start(in: calendar, hour: TasksSettings.hour(settings))
            guard at > now else { continue }
            let list = task.list.flatMap { model.lists[$0]?.name }
            let when = due.instant.map { "Due at \(DueWords.time($0, calendar: calendar, locale: locale))" } ?? "Due today"
            out.append(NotificationRequest(id: id(task.id), title: task.name.isEmpty ? "Untitled task" : task.name,
                                           body: list.map { "\(when), in \($0)" } ?? when, at: at, payload: ["task": task.id]))
        }
        return Array(out.sorted { ($0.at, $0.id) < ($1.at, $1.id) }.prefix(limit))
    }
}
