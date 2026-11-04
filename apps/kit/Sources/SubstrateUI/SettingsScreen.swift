#if canImport(SwiftUI)
import SwiftUI
import SubstrateScreens

/// A settings presentation as a grouped form: each row drawn by its
/// control, each change a command.
public struct SettingsScreen: View {
    let presentation: SettingsPresentation
    let send: (SettingsCommand) -> Void

    public init(_ presentation: SettingsPresentation, send: @escaping (SettingsCommand) -> Void) {
        self.presentation = presentation
        self.send = send
    }

    public var body: some View {
        Form {
            ForEach(presentation.sections) { section in SettingsSectionView(section, send: send) }
        }
        .formStyle(.grouped)
    }
}

/// One section of a settings presentation, for a form that draws rows of
/// the app's own beside the kit's.
public struct SettingsSectionView: View {
    let section: SettingsSection
    let send: (SettingsCommand) -> Void

    public init(_ section: SettingsSection, send: @escaping (SettingsCommand) -> Void) {
        self.section = section
        self.send = send
    }

    public var body: some View {
        Section {
            ForEach(section.rows) { row in SettingsRowView(row: row, send: send) }
        } header: {
            if let title = section.title { Text(title) }
        } footer: {
            if let footer = section.footer { Text(footer) }
        }
    }
}

struct SettingsRowView: View {
    let row: SettingsRow
    let send: (SettingsCommand) -> Void

    var body: some View {
        switch row.control {
        case let .toggle(on):
            Toggle(row.label, isOn: Binding(get: { on }, set: { send(.toggle(row: row.id, on: $0)) }))
        case let .choice(selected, options):
            Picker(row.label, selection: Binding(get: { selected }, set: { send(.choose(row: row.id, option: $0)) })) {
                ForEach(options) { Text($0.label).tag($0.id) }
            }
        case let .value(value, verbatim):
            LabeledContent {
                if verbatim {
                    Text(value).monospaced().textSelection(.enabled)
                } else {
                    Text(value)
                }
            } label: {
                Text(row.label)
                if let detail = row.detail {
                    Text(detail)
                }
            }
        case .link:
            Button {
                send(.open(row: row.id))
            } label: {
                HStack {
                    Text(row.label).foregroundStyle(Color.primary)
                    Spacer()
                    Image(systemName: DesignTokens.Control.disclosure.systemName).font(.footnote.weight(.semibold)).foregroundStyle(.tertiary)
                }
            }
        case let .action(destructive, enabled):
            ConfirmingButton(row.label, role: destructive ? .destructive : nil, confirmation: row.confirmation) {
                send(.perform(row: row.id))
            }
            .disabled(!enabled)
        case let .note(tone):
            Label {
                VStack(alignment: .leading, spacing: 4) {
                    Text(row.label).foregroundStyle(tone == .quiet ? Color.secondary : Color.primary)
                        .fixedSize(horizontal: false, vertical: true)
                    if let detail = row.detail {
                        Text(detail).font(.footnote).foregroundStyle(.secondary)
                    }
                }
            } icon: {
                if let mark = Self.mark(tone) {
                    Image(systemName: mark).foregroundStyle(tone.color)
                }
            }
        case let .problem(problem):
            ProblemBox(problem)
        }
    }

    /// A note's mark by its tone: allowed or done, something to know,
    /// something to act on, something that stopped; none for quiet words.
    static func mark(_ tone: ScreenTone) -> String? {
        switch tone {
        case .positive: DesignTokens.Control.check.systemName
        case .informative: DesignTokens.Control.info.systemName
        case .caution, .critical: ScreenSymbol.warning.systemName
        case .quiet, .supporting: nil
        }
    }
}
#endif
