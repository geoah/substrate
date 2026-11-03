#if canImport(SwiftUI)
import SwiftUI
import SubstrateScreens

/// The build, its channel and an update offered, as a section of a form.
public struct AboutScreen: View {
    let presentation: AboutPresentation
    let onCheck: () -> Void
    let onInstall: () -> Void

    public init(_ presentation: AboutPresentation, onCheck: @escaping () -> Void = {}, onInstall: @escaping () -> Void = {}) {
        self.presentation = presentation
        self.onCheck = onCheck
        self.onInstall = onInstall
    }

    public var body: some View {
        Section(presentation.title) {
            ForEach(presentation.rows, id: \.label) { row in
                // A pre-release's version is long: on its own line it wraps
                // whole rather than being squeezed beside the label.
                VStack(alignment: .leading, spacing: 4) {
                    Text(row.label).foregroundStyle(.secondary)
                    if row.verbatim {
                        Text(row.value).monospaced().textSelection(.enabled)
                    } else {
                        Text(row.value)
                    }
                }
            }
            if let update = presentation.update {
                VStack(alignment: .leading, spacing: 6) {
                    Text(update.title).font(.subheadline.weight(.semibold))
                    Text(update.message).font(.subheadline).foregroundStyle(.secondary)
                    Button(update.install, action: onInstall)
                        .buttonStyle(.borderedProminent)
                        .controlSize(.small)
                }
            }
            if let check = presentation.check {
                Button(check, action: onCheck)
            }
        }
    }
}
#endif
