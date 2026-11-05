import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateScreens

/// The app's own settings rows, by id, with their defaults.
public enum TasksSettings {
    public static let remind = "remind"
    public static let remindHour = "remindHour"
    public static let order = "order"
    public static let showCompleted = "showCompleted"

    public static let hours = [7, 8, 9, 12, 18]
    public static let defaultHour = 9

    /// Off until the owner turns it on, which is when the system is asked
    /// whether the app may notify.
    public static func reminds(_ values: SettingsValues) -> Bool { values.toggle(remind, default: false) }

    public static func hour(_ values: SettingsValues) -> Int {
        Int(values.choice(remindHour, default: String(defaultHour))) ?? defaultHour
    }

    public static func order(_ values: SettingsValues) -> TaskOrder {
        TaskOrder(rawValue: values.choice(order, default: TaskOrder.manual.rawValue)) ?? .manual
    }

    public static func showsCompleted(_ values: SettingsValues) -> Bool { values.toggle(showCompleted, default: false) }

    /// The app's two sections: the reminders, then how lists are shown.
    public static func sections(
        _ values: SettingsValues, permission: NotificationPermission?, scheduled: Int, device: String,
        locale: Locale
    ) -> [SettingsSection] {
        var reminders = [
            SettingsRow(id: .app(remind), label: "Remind Me When Tasks Are Due", control: .toggle(reminds(values)))
        ]
        if reminds(values) {
            reminders.append(
                SettingsRow(
                    id: .app(remindHour), label: "Tasks Due on a Day", detail: "When the reminder comes",
                    control: .choice(
                        selected: String(hour(values)),
                        options: hours.map { SettingsOption(id: String($0), label: hourWords($0, locale: locale)) })))
            if permission == .off {
                reminders.append(
                    SettingsRow(
                        id: .app("notificationsOff"), label: "Notifications are off for Substrate Tasks",
                        detail: "Turn them on in the system's Settings to be reminded.", control: .note(.caution)))
            } else {
                reminders.append(
                    SettingsRow(
                        id: .app("scheduled"), label: "Reminders Set on \(titled(device))",
                        control: .value(String(scheduled), verbatim: false)))
            }
        }
        let lists = [
            SettingsRow(
                id: .app(order), label: "Sort Tasks By",
                control: .choice(
                    selected: order(values).rawValue,
                    options: [
                        SettingsOption(id: TaskOrder.manual.rawValue, label: "My Order"),
                        SettingsOption(id: TaskOrder.due.rawValue, label: "Due Date"),
                    ])),
            SettingsRow(id: .app(showCompleted), label: "Show Completed Tasks", control: .toggle(showsCompleted(values))),
        ]
        return [
            SettingsSection(
                id: .app("reminders"), title: "Reminders", rows: reminders,
                footer: reminds(values)
                    ? "A task due at a time reminds you then; a task due on a day, at the hour above."
                    : "Turned on, you are asked once whether Substrate Tasks may send notifications."),
            SettingsSection(id: .app("lists"), title: "Lists", rows: lists),
        ]
    }

    /// The device's words in a title-case label: "this phone" is "This
    /// Phone"; a word already carrying a capital ("iPhone") is kept as it
    /// is spelled.
    static func titled(_ device: String) -> String {
        device.split(separator: " ", omittingEmptySubsequences: false)
            .map { $0.contains(where: \.isUppercase) ? String($0) : String($0).capitalizedFirst }
            .joined(separator: " ")
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

    /// Settings as one screen: the app's sections, then the kit's.
    public static func screen(_ link: LinkPresentation<TasksPresentation>, _ app: TasksPresentation) -> SettingsPresentation {
        SettingsPresenter.screen(app.settings, link: link, storage: app.storage, about: app.about)
    }
}
