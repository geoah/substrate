import Foundation
import SubstrateDevice
import SubstrateScreens

/// The app's own settings rows, by id, with their defaults, and how every
/// settings command finds the controller that answers it.
public enum TasksSettings {
    public static let remind = "remind"
    public static let remindHour = "remindHour"
    public static let order = "order"
    public static let showCompleted = "showCompleted"

    public static let hours = [7, 8, 9, 12, 18]
    public static let defaultHour = 9

    public static func reminds(_ values: SettingsValues) -> Bool { values.toggle(remind, default: true) }

    public static func hour(_ values: SettingsValues) -> Int {
        Int(values.choice(remindHour, default: String(defaultHour))) ?? defaultHour
    }

    public static func order(_ values: SettingsValues) -> TaskOrder {
        TaskOrder(rawValue: values.choice(order, default: TaskOrder.manual.rawValue)) ?? .manual
    }

    public static func showsCompleted(_ values: SettingsValues) -> Bool { values.toggle(showCompleted, default: false) }

    /// The app's two sections: the reminders, then how lists are shown.
    public static func sections(_ values: SettingsValues, permission: NotificationPermission?, scheduled: Int, device: String,
                                locale: Locale) -> [SettingsSection]
    {
        var reminders = [
            SettingsRow(id: .app(remind), label: "Remind Me When Tasks Are Due", control: .toggle(reminds(values))),
        ]
        if reminds(values) {
            reminders.append(SettingsRow(id: .app(remindHour), label: "Tasks Due on a Day", detail: "When the reminder comes",
                                         control: .choice(selected: String(hour(values)),
                                                          options: hours.map { SettingsOption(id: String($0), label: hourWords($0, locale: locale)) })))
            if permission == .denied {
                reminders.append(SettingsRow(id: .app("notificationsOff"), label: "Notifications are off for Substrate Tasks",
                                             detail: "Turn them on in the system's Settings to be reminded.", control: .note(.caution)))
            } else {
                reminders.append(SettingsRow(id: .app("scheduled"), label: "Reminders Set on \(device.capitalizedFirstWord)",
                                             control: .value(String(scheduled), verbatim: false)))
            }
        }
        let lists = [
            SettingsRow(id: .app(order), label: "Sort Tasks By",
                        control: .choice(selected: order(values).rawValue,
                                         options: [SettingsOption(id: TaskOrder.manual.rawValue, label: "My Order"),
                                                   SettingsOption(id: TaskOrder.due.rawValue, label: "Due Date")])),
            SettingsRow(id: .app(showCompleted), label: "Show Completed Tasks", control: .toggle(showsCompleted(values))),
        ]
        return [
            SettingsSection(id: .app("reminders"), title: "Reminders", rows: reminders,
                            footer: "A task due at a time reminds you then; a task due on a day, at the hour above."),
            SettingsSection(id: .app("lists"), title: "Lists", rows: lists),
        ]
    }

    /// "9:00 AM", or "09:00", as the locale writes an hour.
    static func hourWords(_ hour: Int, locale: Locale) -> String {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        let date = calendar.date(from: DateComponents(year: 2026, month: 1, day: 1, hour: hour)) ?? Date(timeIntervalSince1970: 0)
        let formatter = DateFormatter()
        formatter.locale = locale
        formatter.timeZone = calendar.timeZone
        formatter.setLocalizedDateFormatFromTemplate("jmm")
        return formatter.string(from: date)
    }

    /// Settings as one screen: the app's sections, then the link's status
    /// and repository, then storage and about.
    public static func screen(_ app: TasksPresentation, link: LinkPresentation<TasksPresentation>) -> SettingsPresentation {
        var sections = app.settings
        sections += SettingsPresenter.status(link.status)
        if let repository = link.repository { sections += SettingsPresenter.repository(repository) }
        sections.append(app.storage)
        sections.append(SettingsPresenter.about(app.about))
        return SettingsPresentation(sections: sections)
    }

    /// Where a settings command goes.
    public enum Route: Equatable, Sendable {
        case app(TasksApp.Command)
        case link(LinkController<TasksApp>.Command)
        /// The review of the app's access, which the UI opens.
        case changeAccess
        /// The updater's own check, which the UI runs.
        case checkForUpdates
        /// The connect screen, which the UI shows.
        case signIn
        case none
    }

    public static func route(_ command: SettingsCommand) -> Route {
        switch command {
        case .toggle(.app, _), .choose(.app, _): .app(.settings(command))
        case .perform(.storage(.clear)): .app(.clearSaved)
        case .perform(.status(.action(.sendNow))): .link(.sendNow)
        case let .perform(.status(.action(.retry(id)))): .link(.retry(id))
        case let .perform(.status(.action(.discard(id)))): .link(.discard(id))
        case .perform(.status(.action(.review))), .perform(.status(.action(.renew))), .perform(.repository(.changeAccess)): .changeAccess
        case .perform(.repository(.signOut)): .link(.signOut)
        case .perform(.status(.action(.signIn))): .signIn
        case .perform(.about(.check)): .checkForUpdates
        default: .none
        }
    }
}

extension String {
    /// "This iPhone" for "this iPhone".
    var capitalizedFirstWord: String { prefix(1).uppercased() + dropFirst() }
}
