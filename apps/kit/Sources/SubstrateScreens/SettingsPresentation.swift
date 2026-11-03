/// A settings screen as data (§5.6): sections of rows, each row a toggle, a
/// choice, a value, a link, an action or a note, which each system draws as
/// its own form.
public struct SettingsPresentation: Codable, Hashable, Sendable {
    public var title: String
    public var sections: [SettingsSection]

    public init(title: String = "Settings", sections: [SettingsSection]) {
        self.title = title
        self.sections = sections
    }
}

public struct SettingsSection: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var title: String?
    public var rows: [SettingsRow]
    public var footer: String?

    public init(id: String, title: String? = nil, rows: [SettingsRow], footer: String? = nil) {
        self.id = id
        self.title = title
        self.rows = rows
        self.footer = footer
    }
}

public struct SettingsRow: Codable, Hashable, Sendable, Identifiable {
    public enum Control: Codable, Hashable, Sendable {
        case toggle(Bool)
        /// The option chosen, by id, and every option.
        case choice(selected: String, options: [SettingsOption])
        /// A value to read, `verbatim` for one a person copies.
        case value(String, verbatim: Bool)
        /// A screen of its own, by id.
        case link
        /// A button; `destructive` for one that gives something up.
        case action(destructive: Bool, enabled: Bool)
        /// Words of their own, a status or a warning.
        case note(Tone)
        /// A problem in words, and what fixes it.
        case problem(ProblemPresentation)
    }

    public var id: String
    public var label: String
    public var detail: String?
    public var control: Control

    public init(id: String, label: String, detail: String? = nil, control: Control) {
        self.id = id
        self.label = label
        self.detail = detail
        self.control = control
    }
}

public struct SettingsOption: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var label: String

    public init(id: String, label: String) {
        self.id = id
        self.label = label
    }
}

/// What a settings row sends.
public enum SettingsCommand: Codable, Hashable, Sendable {
    case toggle(row: String, on: Bool)
    case choose(row: String, option: String)
    case open(row: String)
    case perform(row: String)
}
