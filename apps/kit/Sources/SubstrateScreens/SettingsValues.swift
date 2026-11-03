/// An app's own settings, by the id it gave each row (`SettingsRowID.app`):
/// the engine a settings screen's toggles and choices apply to, so the rows
/// and their values stay one value.
public struct SettingsValues: Codable, Hashable, Sendable {
    public var toggles: [String: Bool]
    public var choices: [String: String]

    public init(toggles: [String: Bool] = [:], choices: [String: String] = [:]) {
        self.toggles = toggles
        self.choices = choices
    }

    /// The values after a command; an open, an action, or a row that is
    /// not the app's own changes none.
    public func applying(_ command: SettingsCommand) -> SettingsValues {
        var next = self
        switch command {
        case let .toggle(.app(row), on): next.toggles[row] = on
        case let .choose(.app(row), option): next.choices[row] = option
        case .toggle, .choose, .open, .perform: break
        }
        return next
    }

    public func toggle(_ row: String, default value: Bool = false) -> Bool { toggles[row] ?? value }

    public func choice(_ row: String, default value: String) -> String { choices[row] ?? value }
}
