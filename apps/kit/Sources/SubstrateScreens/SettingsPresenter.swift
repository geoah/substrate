import Foundation
import SubstrateLocal

/// The sections every app's settings have, as rows: the account, the
/// status, the storage and about.
public enum SettingsPresenter {
    public static func account(_ account: AccountPresentation) -> SettingsSection {
        var rows = account.rows.map { SettingsRow(id: "account.\($0.label)", label: $0.label, control: .value($0.value, verbatim: $0.verbatim)) }
        if let title = account.approvedTitle {
            rows.append(SettingsRow(id: "account.approved", label: title, detail: account.approved.joined(separator: "\n"), control: .note(.positive)))
        }
        for row in [account.until, account.access].compactMap({ $0 }) {
            rows.append(SettingsRow(id: "account.\(row.label)", label: row.label, control: .value(row.value, verbatim: false)))
        }
        if let changeAccess = account.changeAccess {
            rows.append(SettingsRow(id: "account.changeAccess", label: changeAccess, control: .action(destructive: false, enabled: true)))
        }
        for (index, notice) in account.unrevoked.enumerated() {
            rows.append(SettingsRow(id: "account.unrevoked.\(index)", label: notice.title ?? "", control: .problem(notice)))
        }
        rows.append(SettingsRow(id: "account.signOut", label: account.signOut, control: .action(destructive: true, enabled: true)))
        return SettingsSection(id: "account", title: account.title, rows: rows, footer: account.footer)
    }

    public static func status(_ status: StatusPresentation) -> SettingsSection {
        var rows = [SettingsRow(id: "status.sentence", label: status.sentence, control: .note(status.tone))]
        rows += status.rows.map { SettingsRow(id: "status.\($0.label)", label: $0.label, control: .value($0.value, verbatim: false)) }
        rows += status.actions.map { action in
            SettingsRow(id: "status.\(action.title)", label: action.title, control: .action(destructive: false, enabled: !status.busy))
        }
        return SettingsSection(id: "status", title: status.title, rows: rows, footer: status.footer)
    }

    /// Each area's size and its share of the budget, and a clear that never
    /// drops unsent writes: only the areas that can be read again clear.
    public static func storage(_ usage: [AreaUsage], budget: Int?) -> SettingsSection {
        var rows = usage.map { area in
            var value = byteWords(area.bytes)
            if area.area == .cache || area.area == .records, let budget, budget > 0 {
                value += " of \(byteWords(budget))"
            }
            return SettingsRow(id: "storage.\(area.area.rawValue)", label: areaWords(area.area), control: .value(value, verbatim: false))
        }
        let clearable = usage.contains { ($0.area == .cache || $0.area == .records) && $0.bytes > 0 }
        rows.append(SettingsRow(id: "storage.clear", label: "Clear Saved Copies", control: .action(destructive: true, enabled: clearable)))
        return SettingsSection(id: "storage", title: "Storage", rows: rows,
                               footer: "Saved copies are read again from your substrate when needed. Changes waiting to be sent are never cleared.")
    }

    public static func about(_ about: AboutPresentation) -> SettingsSection {
        var rows = about.rows.map { SettingsRow(id: "about.\($0.label)", label: $0.label, control: .value($0.value, verbatim: $0.verbatim)) }
        if let check = about.check { rows.append(SettingsRow(id: "about.check", label: check, control: .action(destructive: false, enabled: true))) }
        if let update = about.update {
            rows.append(SettingsRow(id: "about.update", label: update.title, detail: update.message, control: .note(.informative)))
        }
        return SettingsSection(id: "about", title: about.title, rows: rows)
    }

    static func areaWords(_ area: FolderArea) -> String {
        switch area {
        case .journals: "In progress"
        case .outbox: "Waiting to be sent"
        case .records: "Saved records"
        case .cache: "Saved copies"
        default: area.rawValue.capitalizedFirst
        }
    }

    /// A size as a person reads it, the same on every system: "0 bytes",
    /// "12 KB", "1.5 MB", decimal units as the systems' own settings use.
    public static func byteWords(_ bytes: Int) -> String {
        if bytes < 1000 { return bytes == 1 ? "1 byte" : "\(bytes) bytes" }
        let units = ["KB", "MB", "GB", "TB"]
        var value = Double(bytes) / 1000
        var unit = 0
        while value >= 1000, unit < units.count - 1 {
            value /= 1000
            unit += 1
        }
        let rounded = (value * 10).rounded() / 10
        let text = rounded >= 100 || rounded == rounded.rounded() ? String(Int(rounded.rounded())) : String(format: "%.1f", rounded)
        return "\(text) \(units[unit])"
    }
}
