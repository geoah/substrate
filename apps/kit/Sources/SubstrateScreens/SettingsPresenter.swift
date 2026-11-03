import Foundation
import SubstrateLocal

/// The sections every app's settings have, as rows: the status and the
/// writes refused, the repository and its approved access, the storage and
/// about. Each fact is a row of its own, with an id that names it.
public enum SettingsPresenter {
    /// The repository's section, then its approved access as a section of
    /// its own, one row per scope.
    public static func repository(_ repository: RepositoryPresentation) -> [SettingsSection] {
        var rows = [
            SettingsRow(id: .repository(.server), label: repository.server.label, control: .value(repository.server.value, verbatim: false)),
            SettingsRow(id: .repository(.repository), label: repository.repository.label,
                        control: .value(repository.repository.value, verbatim: repository.repository.verbatim)),
        ]
        if let version = repository.serverVersion {
            rows.append(SettingsRow(id: .repository(.serverVersion), label: version.label, control: .value(version.value, verbatim: false)))
        }
        if let access = repository.access {
            rows.append(SettingsRow(id: .repository(.access), label: access.label, control: .value(access.value, verbatim: false)))
        }
        for (index, notice) in repository.unrevoked.enumerated() {
            rows.append(SettingsRow(id: .repository(.unrevoked(index)), label: notice.title ?? "", control: .problem(notice)))
        }
        rows.append(SettingsRow(id: .repository(.signOut), label: repository.signOut, control: .action(destructive: true, enabled: true)))
        var sections = [SettingsSection(id: .repository, title: repository.title, rows: rows, footer: repository.footer)]
        if let approved = repository.approved {
            var access = approved.scopes.map { SettingsRow(id: .repository(.scope($0.scope)), label: $0.words, control: .note(.positive)) }
            access.append(SettingsRow(id: .repository(.until), label: approved.until.label, control: .value(approved.until.value, verbatim: false)))
            if let changeAccess = repository.changeAccess {
                access.append(SettingsRow(id: .repository(.changeAccess), label: changeAccess, control: .action(destructive: false, enabled: true)))
            }
            sections.append(SettingsSection(id: .approvedAccess, title: approved.title, rows: access))
        }
        return sections
    }

    /// The status's section, then the writes the server refused as a
    /// section of their own, each with Retry and Discard.
    public static func status(_ status: StatusPresentation) -> [SettingsSection] {
        var rows = [SettingsRow(id: .status(.sentence), label: status.sentence, control: .note(status.tone))]
        if let problem = status.problem {
            rows.append(SettingsRow(id: .status(.problem), label: problem.title ?? "", control: .problem(problem)))
        }
        rows += status.packages.map {
            SettingsRow(id: .status(.package($0.id)), label: $0.row.label, control: .value($0.row.value, verbatim: false))
        }
        rows += status.actions.map { action in
            SettingsRow(id: .status(.action(action.action)), label: action.title, control: .action(destructive: false, enabled: !status.busy))
        }
        var sections = [SettingsSection(id: .status, title: status.title, rows: rows, footer: status.footer)]
        if let refused = status.refused {
            var writes: [SettingsRow] = []
            for write in refused.rows {
                writes.append(SettingsRow(id: .status(.refused(write.id)), label: write.title, detail: write.words,
                                          control: .value(write.recordID, verbatim: true)))
                writes.append(SettingsRow(id: .status(.action(write.retry.action)), label: write.retry.title,
                                          control: .action(destructive: false, enabled: !status.busy)))
                writes.append(SettingsRow(id: .status(.action(write.discard.action)), label: write.discard.title,
                                          control: .action(destructive: true, enabled: !status.busy)))
            }
            sections.append(SettingsSection(id: .refused, title: refused.title, rows: writes, footer: refused.footer))
        }
        return sections
    }

    /// Each area's size and its share of the budget, and a clear that never
    /// drops unsent writes: only the areas that can be read again clear.
    public static func storage(_ usage: [AreaUsage], budget: Int?) -> SettingsSection {
        var rows = usage.map { area in
            var value = byteWords(area.bytes)
            if area.area == .cache || area.area == .records, let budget, budget > 0 {
                value += " of \(byteWords(budget))"
            }
            return SettingsRow(id: .storage(.area(area.area)), label: areaWords(area.area), control: .value(value, verbatim: false))
        }
        let clearable = usage.contains { ($0.area == .cache || $0.area == .records) && $0.bytes > 0 }
        rows.append(SettingsRow(id: .storage(.clear), label: "Clear Saved Copies", control: .action(destructive: true, enabled: clearable)))
        return SettingsSection(id: .storage, title: "Storage", rows: rows,
                               footer: "Saved copies are read again from your substrate when needed. Changes waiting to be sent are never cleared.")
    }

    public static func about(_ about: AboutPresentation) -> SettingsSection {
        var rows = [SettingsRow(id: .about(.version), label: about.version.label, control: .value(about.version.value, verbatim: about.version.verbatim))]
        if let channel = about.channel {
            rows.append(SettingsRow(id: .about(.channel), label: channel.label, control: .value(channel.value, verbatim: false)))
        }
        if let check = about.check { rows.append(SettingsRow(id: .about(.check), label: check, control: .action(destructive: false, enabled: true))) }
        if let update = about.update {
            rows.append(SettingsRow(id: .about(.update), label: update.title, detail: update.message, control: .note(.informative)))
        }
        return SettingsSection(id: .about, title: about.title, rows: rows)
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
