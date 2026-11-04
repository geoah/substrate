import Foundation
import SubstrateCore
import SubstrateLocal

/// A settings screen as data (§5.6): sections of rows, each row a toggle, a
/// choice, a value, a link, an action or a note, which each system draws as
/// its own form.
public struct SettingsPresentation: Bridged, Hashable, Sendable {
    public var title: String
    public var sections: [SettingsSection]

    public init(title: String = "Settings", sections: [SettingsSection]) {
        self.title = title
        self.sections = sections
    }
}

public struct SettingsSection: Bridged, Hashable, Sendable, Identifiable {
    public var id: SettingsSectionID
    public var title: String?
    public var rows: [SettingsRow]
    public var footer: String?

    public init(id: SettingsSectionID, title: String? = nil, rows: [SettingsRow], footer: String? = nil) {
        self.id = id
        self.title = title
        self.rows = rows
        self.footer = footer
    }
}

public struct SettingsRow: Bridged, Hashable, Sendable, Identifiable {
    public enum Control: Bridged, Hashable, Sendable {
        case toggle(Bool)
        /// The option chosen, by id, and every option.
        case choice(selected: String, options: [SettingsOption])
        /// A value to read, `verbatim` for one a person copies.
        case value(String, verbatim: Bool)
        /// A screen of its own, by id.
        case link
        /// A button; `destructive` for one that gives something up.
        case action(destructive: Bool, enabled: Bool)
        /// Words of their own, a status or a warning; a positive one is
        /// marked as allowed or done.
        case note(ScreenTone)
        /// A problem in words, and what fixes it.
        case problem(ProblemPresentation)
    }

    public var id: SettingsRowID
    public var label: String
    public var detail: String?
    public var control: Control
    /// The question an action asks before it is sent, by its own button
    /// (Sign Out); nil sends it at once.
    public var confirmation: ConfirmationPresentation<SettingsRowID>?

    public init(
        id: SettingsRowID, label: String, detail: String? = nil, control: Control,
        confirmation: ConfirmationPresentation<SettingsRowID>? = nil
    ) {
        self.id = id
        self.label = label
        self.detail = detail
        self.control = control
        self.confirmation = confirmation
    }
}

public struct SettingsOption: Bridged, Hashable, Sendable, Identifiable {
    public var id: String
    public var label: String

    public init(id: String, label: String) {
        self.id = id
        self.label = label
    }
}

/// What a settings row sends, by the row's id.
public enum SettingsCommand: Bridged, Hashable, Sendable {
    case toggle(row: SettingsRowID, on: Bool)
    case choose(row: SettingsRowID, option: String)
    case open(row: SettingsRowID)
    case perform(row: SettingsRowID)
}

/// A settings section, by what it holds: the app's own by the id it gave
/// it, and the ones every app has by name.
public enum SettingsSectionID: Bridged, Hashable, Sendable {
    case app(String)
    case status
    /// The writes the server refused, each with Retry and Discard.
    case refused
    case repository
    /// One row per approved scope, until when, and Change Access.
    case approvedAccess
    case storage
    case about
}

/// A settings row, by what it is, never by its words: an app dispatches a
/// command on it, and the Android bridge carries it as is.
public enum SettingsRowID: Bridged, Hashable, Sendable {
    /// One of the app's own rows, by the id it gave it.
    case app(String)
    case status(StatusRow)
    case repository(RepositoryRow)
    case storage(StorageRow)
    case about(AboutRow)

    public enum StatusRow: Bridged, Hashable, Sendable {
        case sentence
        /// Why, in the server's or the system's words: a change not kept, a
        /// write refused.
        case problem
        /// A package the app needs, by its id.
        case package(String)
        case action(StatusAction)
        /// A refused write, by its id.
        case refused(UUID)
        /// What the server refused of it.
        case refusedStatus(UUID)
        /// The kind it was of, by its full reference.
        case refusedKind(UUID)
        /// Its record's id.
        case refusedRecord(UUID)
    }

    public enum RepositoryRow: Bridged, Hashable, Sendable {
        case server
        case repository
        case serverVersion
        /// An approved scope, as the token carries it.
        case scope(String)
        case until
        case access
        case changeAccess
        /// A token this device stopped using that its server has not
        /// revoked yet, by its place in the list.
        case unrevoked(Int)
        case signOut
    }

    public enum StorageRow: Bridged, Hashable, Sendable {
        case area(FolderArea)
        /// Every area's size together, against the budget where there is one.
        case total
        case clear
    }

    public enum AboutRow: Bridged, Hashable, Sendable {
        case version
        case channel
        case check
        case update
    }
}
