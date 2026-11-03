#if canImport(SwiftUI)
import SwiftUI
import SubstrateScreens

/// The Repository screen as a section of a settings form, as its
/// presentation words it.
public struct RepositoryScreen: View {
    let presentation: RepositoryPresentation
    let onChangeAccess: (() -> Void)?
    let onSignOut: () -> Void

    public init(_ presentation: RepositoryPresentation, onChangeAccess: (() -> Void)? = nil, onSignOut: @escaping () -> Void) {
        self.presentation = presentation
        self.onChangeAccess = onChangeAccess
        self.onSignOut = onSignOut
    }

    public var body: some View {
        Section {
            ForEach(presentation.rows, id: \.label) { LabeledContent($0.label, value: $0.value) }
            if let approved = presentation.approved {
                VStack(alignment: .leading, spacing: 6) {
                    Text(approved.title)
                    ForEach(approved.scopes) { scope in
                        Label {
                            Text(scope.words).fixedSize(horizontal: false, vertical: true)
                        } icon: {
                            Image(systemName: DesignTokens.Control.check.systemName).foregroundStyle(ScreenTone.positive.color)
                        }
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    }
                }
                LabeledContent(approved.until.label, value: approved.until.value)
            }
            if let changeAccess = presentation.changeAccess, let onChangeAccess {
                Button(changeAccess, action: onChangeAccess)
            }
            if let access = presentation.access { LabeledContent(access.label, value: access.value) }
            UnrevokedNotices(presentation.unrevoked)
            ConfirmingButton(presentation.signOut, role: .destructive, confirmation: presentation.signOutConfirmation, action: onSignOut)
        } header: {
            Text(presentation.title)
        } footer: {
            Text(presentation.footer)
        }
    }
}
#endif
