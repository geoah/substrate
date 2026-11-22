import SubstrateScreens
import SubstrateUI
import SwiftUI
import TodayCore

/// Settings: how the assistant behaves (its standing instructions, each
/// with a switch and its text to edit), then the app's own rows, then the
/// kit's status, repository, storage and about, as one form.
public struct SettingsView: View {
    public init() {}

    public var body: some View {
        TodaySettingsScreen()
            .environment(Today.shell)
    }
}

struct TodaySettingsScreen: View {
    @Environment(TodayShell.self) private var shell
    @Environment(\.checkForUpdates) private var checkForUpdates
    @Environment(\.openURL) private var openURL
    @State private var editing: MossSettings.Row?

    var body: some View {
        if let screen = shell.settingsScreen {
            let shell = shell
            Form {
                if let moss = shell.presentation.app?.moss, !moss.rows.isEmpty || moss.empty != nil {
                    MossSettingsSection(moss: moss, send: { shell.send($0) }, edit: { editing = $0 })
                }
                ForEach(screen.sections) { section in
                    SettingsSectionView(section) { shell.perform($0, checkForUpdates: checkForUpdates, openURL: { openURL($0) }) }
                }
            }
            .formStyle(.grouped)
            .navigationTitle(screen.title)
            .sheet(item: $editing) { row in
                InstructionEditor(moss: shell.presentation.app?.moss ?? MossSettings(), row: row) { text in
                    shell.send(.rewriteInstruction(row.id, text: text))
                }
            }
        }
    }
}

/// The assistant's standing instructions: each in full, with who wrote it,
/// a switch and Edit. A switch and a saved edit take effect at once.
struct MossSettingsSection: View {
    var moss: MossSettings
    var send: (TodayCommand) -> Void
    var edit: (MossSettings.Row) -> Void

    var body: some View {
        Section {
            if let empty = moss.empty { Text(empty).foregroundStyle(.secondary) }
            ForEach(moss.rows) { row in
                VStack(alignment: .leading, spacing: 6) {
                    Toggle(isOn: Binding(get: { row.on }, set: { send(.instruction(row.id, on: $0)) })) {
                        Text(row.text)
                            .foregroundStyle(row.on ? Color.primary : Color.secondary)
                            .multilineTextAlignment(.leading)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    HStack {
                        Text(row.source).font(.footnote).foregroundStyle(.secondary)
                        Spacer()
                        Button(moss.edit) { edit(row) }
                            .buttonStyle(.bordered)
                            .controlSize(.small)
                            .accessibilityLabel("\(moss.edit): \(row.text)")
                    }
                }
                .padding(.vertical, 2)
            }
        } header: {
            Text(moss.title)
        } footer: {
            if let footer = moss.footer { Text(footer) }
        }
    }
}

/// One instruction's text, edited: nothing is written until Save.
struct InstructionEditor: View {
    var moss: MossSettings
    var row: MossSettings.Row
    var save: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var text: String

    init(moss: MossSettings, row: MossSettings.Row, save: @escaping (String) -> Void) {
        self.moss = moss
        self.row = row
        self.save = save
        _text = State(initialValue: row.text)
    }

    private var trimmed: String { text.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        NavigationStack {
            Form {
                TextEditor(text: $text)
                    .frame(minHeight: 220)
                    .accessibilityLabel(moss.editTitle)
            }
            .formStyle(.grouped)
            .navigationTitle(moss.editTitle)
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(moss.cancel) { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button(moss.save) {
                        save(trimmed)
                        dismiss()
                    }
                    .disabled(trimmed.isEmpty || trimmed == row.text)
                }
            }
        }
        #if os(macOS)
        .frame(minWidth: 460, minHeight: 320)
        #endif
    }
}
