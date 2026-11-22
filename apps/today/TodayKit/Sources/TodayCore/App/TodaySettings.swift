import Foundation
import SubstrateCore
import SubstrateScreens

/// The app's part of Settings: how the assistant behaves (its standing
/// instructions) and when it prepares, from the one `today/preference`.
public enum TodaySettings {
    static let prepares = "assistantPrepares"
    static let prepareAt = "prepareAt"
    static let urgentWait = "urgentWait"

    /// Half past five to nine, by the half hour: when the morning is
    /// prepared. The function that prepares runs every half hour.
    static let hours = ["05:30", "06:00", "06:30", "07:00", "07:30", "08:00", "08:30", "09:00"]
    /// How long an urgent mark nobody checked waits, in seconds.
    static let waits: [(seconds: Int, label: String)] = [(60, "One minute"), (120, "Two minutes"), (300, "Five minutes"), (600, "Ten minutes")]

    /// The assistant's standing instructions, oldest first.
    static func moss(_ day: Day) -> MossSettings {
        guard day.discovery.present.contains(TodayKinds.instruction) else { return MossSettings() }
        let rows = day.instructions.map { instruction in
            MossSettings.Row(
                id: instruction.id, text: instruction.text, on: instruction.active,
                source: instruction.source == "owner" ? "Yours" : instruction.source == "agent" ? "Written by Moss" : "Came with a package")
        }
        return MossSettings(
            rows: rows, empty: rows.isEmpty ? "Moss has no standing instructions yet." : nil,
            footer: "Moss reads these at the start of everything it does, here and in every other app. One that is off is not read.")
    }

    /// The preference's rows, where the repository holds the kind.
    static func sections(_ day: Day) -> [SettingsSection] {
        guard day.discovery.present.contains(TodayKinds.preference) else { return [] }
        let preference = day.preference
        var rows = [SettingsRow(id: .app(prepares), label: "Moss prepares the day", control: .toggle(preference.assistantPrepares))]
        if preference.assistantPrepares {
            var hours = hours
            if !hours.contains(preference.prepareAt) { hours = (hours + [preference.prepareAt]).sorted() }
            rows.append(
                SettingsRow(
                    id: .app(prepareAt), label: "The morning is prepared at",
                    control: .choice(selected: preference.prepareAt, options: hours.map { SettingsOption(id: $0, label: $0) })))
        }
        let seconds = Int(preference.urgentWait)
        var waits = waits
        if !waits.contains(where: { $0.seconds == seconds }) { waits.append((seconds, "\(seconds) seconds")) }
        rows.append(
            SettingsRow(
                id: .app(urgentWait), label: "An urgent mark nobody checked waits",
                control: .choice(
                    selected: String(seconds), options: waits.sorted { $0.seconds < $1.seconds }.map { SettingsOption(id: String($0.seconds), label: $0.label) }
                )))
        return [
            SettingsSection(
                id: .app("prepare"), title: "Preparing the day", rows: rows,
                footer: preference.assistantPrepares
                    ? "Each morning Moss writes its advice on your suggested tasks and what to know before the day's meetings, and suggests flags. It changes nothing you chose."
                    : "Moss prepares nothing: no advice on suggested tasks, nothing before meetings, and no check of an urgent mark.")
        ]
    }

    /// What a row's command writes to the preference, as its properties.
    static func change(_ command: SettingsCommand) -> JSONObject? {
        switch command {
        case let .toggle(.app(row), on) where row == prepares: [prepares: .bool(on)]
        case let .choose(.app(row), option) where row == prepareAt: [prepareAt: .string(option)]
        case let .choose(.app(row), option) where row == urgentWait: Int(option).map { [urgentWait: .string("PT\($0)S")] }
        default: nil
        }
    }
}
