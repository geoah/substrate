#if canImport(SwiftUI)
import SwiftUI
import SubstrateScreens

/// The Status screen as sections of a settings form: the sentence, why
/// where there is a reason, each package, what to do about it, and the
/// writes the server refused.
public struct StatusScreen: View {
    let presentation: StatusPresentation
    let perform: (StatusAction) -> Void

    public init(_ presentation: StatusPresentation, perform: @escaping (StatusAction) -> Void) {
        self.presentation = presentation
        self.perform = perform
    }

    public var body: some View {
        Section {
            Label {
                Text(presentation.sentence)
                    .fixedSize(horizontal: false, vertical: true)
            } icon: {
                Image(systemName: presentation.symbol.systemName)
                    .foregroundStyle(presentation.tone == .quiet ? Color.primary : presentation.tone.color)
            }
            if let problem = presentation.problem { ProblemBox(problem) }
            // The package by its name, and under it its full reference: two
            // packages may share a name, never a reference.
            ForEach(presentation.packages) { line in
                LabeledContent {
                    Text(line.row.value)
                } label: {
                    Text(line.row.label)
                    Text(line.id).monospaced()
                }
            }
            ForEach(presentation.actions, id: \.self) { button in
                Button {
                    perform(button.action)
                } label: {
                    HStack {
                        Text(button.title)
                        if button.action == .sendNow, presentation.busy {
                            Spacer()
                            ProgressView()
                        }
                    }
                }
                .disabled(button.action == .sendNow && presentation.busy)
            }
        } header: {
            Text(presentation.title)
        } footer: {
            if let footer = presentation.footer { Text(footer) }
        }
        if let refused = presentation.refused {
            Section {
                ForEach(refused.rows) { row in
                    VStack(alignment: .leading, spacing: 6) {
                        Text(row.label)
                            .font(.subheadline.weight(.semibold))
                        Label(row.status, systemImage: ScreenSymbol.warning.systemName)
                            .font(.footnote)
                            .foregroundStyle(ScreenTone.caution.color)
                        ForEach([row.kind, row.record], id: \.self) { fact in
                            LabeledContent(fact.label) {
                                Text(fact.value).font(.caption.monospaced()).textSelection(.enabled)
                            }
                            .font(.caption)
                        }
                        if let words = row.words {
                            Text(words)
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        HStack(spacing: 16) {
                            Button(row.retry.title) { perform(row.retry.action) }
                            Button(row.discard.title, role: .destructive) { perform(row.discard.action) }
                        }
                        .buttonStyle(.borderless)
                        .font(.footnote.weight(.semibold))
                    }
                    .padding(.vertical, 2)
                }
            } header: {
                Text(refused.title)
            } footer: {
                Text(refused.footer)
            }
        }
    }
}
#endif
