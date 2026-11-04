import Foundation
import SubstrateLocal
import SubstrateRuntime

/// The sections every app's settings have, as rows: the status and the
/// writes refused, the repository and its approved access, the storage and
/// about. Each fact is a row of its own, with an id that names it.
public enum SettingsPresenter {
    /// The repository's section, then its approved access as a section of
    /// its own, one row per scope.
    public static func repository(_ repository: RepositoryPresentation) -> [SettingsSection] {
        var rows = [
            SettingsRow(id: .repository(.server), label: repository.server.label, control: .value(repository.server.value, verbatim: false)),
            SettingsRow(
                id: .repository(.repository), label: repository.repository.label,
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
        rows.append(
            SettingsRow(
                id: .repository(.signOut), label: repository.signOut, control: .action(destructive: true, enabled: true),
                confirmation: repository.signOutConfirmation))
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
        // The package by its name, and under it its full reference: two
        // packages may share a name, never a reference.
        rows += status.packages.map {
            SettingsRow(id: .status(.package($0.id)), label: $0.row.label, detail: $0.id, control: .value($0.row.value, verbatim: false))
        }
        rows += status.actions.map { action in
            SettingsRow(id: .status(.action(action.action)), label: action.title, control: .action(destructive: false, enabled: !status.busy))
        }
        var sections = [SettingsSection(id: .status, title: status.title, rows: rows, footer: status.footer)]
        if let refused = status.refused {
            var writes: [SettingsRow] = []
            for write in refused.rows {
                writes.append(SettingsRow(id: .status(.refused(write.id)), label: write.label, detail: write.words, control: .note(.supporting)))
                writes.append(SettingsRow(id: .status(.refusedStatus(write.id)), label: write.status, control: .note(.caution)))
                writes.append(
                    SettingsRow(
                        id: .status(.refusedKind(write.id)), label: write.kind.label,
                        control: .value(write.kind.value, verbatim: write.kind.verbatim)))
                writes.append(
                    SettingsRow(
                        id: .status(.refusedRecord(write.id)), label: write.record.label,
                        control: .value(write.record.value, verbatim: write.record.verbatim)))
                writes.append(
                    SettingsRow(
                        id: .status(.action(write.retry.action)), label: write.retry.title,
                        control: .action(destructive: false, enabled: !status.busy)))
                writes.append(
                    SettingsRow(
                        id: .status(.action(write.discard.action)), label: write.discard.title,
                        control: .action(destructive: true, enabled: !status.busy)))
            }
            sections.append(SettingsSection(id: .refused, title: refused.title, rows: writes, footer: refused.footer))
        }
        return sections
    }

    /// Each area's size, then their total once, against the budget the
    /// saved copies share where the app keeps one, and a clear that never
    /// drops unsent writes: only the areas that can be read again clear.
    public static func storage(_ usage: [AreaUsage], budget: Int?) -> SettingsSection {
        var rows = usage.map { area in
            SettingsRow(id: .storage(.area(area.area)), label: areaWords(area.area), control: .value(byteWords(area.bytes), verbatim: false))
        }
        if !usage.isEmpty {
            var total = byteWords(usage.reduce(0) { $0 + $1.bytes })
            if let budget, budget > 0 { total += " of \(byteWords(budget))" }
            rows.append(SettingsRow(id: .storage(.total), label: "Total", control: .value(total, verbatim: false)))
        }
        let clearable = usage.contains { ($0.area == .cache || $0.area == .records) && $0.bytes > 0 }
        rows.append(SettingsRow(id: .storage(.clear), label: "Clear Saved Copies", control: .action(destructive: true, enabled: clearable)))
        return SettingsSection(
            id: .storage, title: "Storage", rows: rows,
            footer: "Saved copies are read again from your substrate when needed. Changes waiting to be sent are never cleared.")
    }

    /// An app's Settings as one screen: its own sections, then the link's
    /// status and repository, then storage and about, every app's in the
    /// same order.
    public static func screen<App>(
        _ sections: [SettingsSection], link: LinkPresentation<App>, storage: SettingsSection?,
        about: AboutPresentation
    ) -> SettingsPresentation {
        var all = sections
        all += status(link.status)
        if let repository = link.repository { all += self.repository(repository) }
        if let storage { all.append(storage) }
        all.append(self.about(about))
        return SettingsPresentation(sections: all)
    }

    public static func about(_ about: AboutPresentation) -> SettingsSection {
        var rows = [SettingsRow(id: .about(.version), label: about.version.label, control: .value(about.version.value, verbatim: false))]
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
