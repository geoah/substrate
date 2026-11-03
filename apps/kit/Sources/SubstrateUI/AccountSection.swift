#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateScreens

/// The Account screen as a section of a settings form, as its presentation
/// words it.
public struct AccountScreen: View {
    let presentation: AccountPresentation
    let onChangeAccess: (() -> Void)?
    let onSignOut: () -> Void

    public init(_ presentation: AccountPresentation, onChangeAccess: (() -> Void)? = nil, onSignOut: @escaping () -> Void) {
        self.presentation = presentation
        self.onChangeAccess = onChangeAccess
        self.onSignOut = onSignOut
    }

    public var body: some View {
        Section {
            ForEach(presentation.rows, id: \.label) { LabeledContent($0.label, value: $0.value) }
            if let title = presentation.approvedTitle {
                VStack(alignment: .leading, spacing: 6) {
                    Text(title)
                    ForEach(presentation.approved, id: \.self) { words in
                        Label {
                            Text(words).fixedSize(horizontal: false, vertical: true)
                        } icon: {
                            Image(systemName: "checkmark").foregroundStyle(.green)
                        }
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    }
                }
            }
            if let until = presentation.until { LabeledContent(until.label, value: until.value) }
            if let changeAccess = presentation.changeAccess, let onChangeAccess {
                Button(changeAccess, action: onChangeAccess)
            }
            if let access = presentation.access { LabeledContent(access.label, value: access.value) }
            UnrevokedNotices(presentation.unrevoked)
            Button(presentation.signOut, role: .destructive, action: onSignOut)
        } header: {
            Text(presentation.title)
        } footer: {
            Text(presentation.footer)
        }
    }
}

/// The account, as a settings form shows it: which server and repository,
/// the server's version, what the app was approved to do and until when,
/// and the two ways out of it. A password session says it reaches
/// everything. A token this device stopped using that its server has not
/// revoked yet is said, with where to revoke it by hand.
public struct AccountSection: View {
    let session: Session
    let manifest: AppManifest?
    let serverVersion: String?
    let unrevoked: [Session]
    let onChangeAccess: (() -> Void)?
    let onSignOut: () -> Void
    @Environment(\.locale) private var locale
    @Environment(\.timeZone) private var timeZone

    public init(session: Session, manifest: AppManifest? = nil, serverVersion: String? = nil, unrevoked: [Session] = [],
                onChangeAccess: (() -> Void)? = nil, onSignOut: @escaping () -> Void)
    {
        self.session = session
        self.manifest = manifest
        self.serverVersion = serverVersion
        self.unrevoked = unrevoked
        self.onChangeAccess = onChangeAccess
        self.onSignOut = onSignOut
    }

    public var body: some View {
        AccountScreen(AccountPresenter.present(session: SessionFacts(session), manifest: manifest, serverVersion: serverVersion,
                                               unrevoked: unrevoked.map(SessionFacts.init), offersChangeAccess: onChangeAccess != nil,
                                               locale: locale, timeZone: timeZone),
                      onChangeAccess: onChangeAccess, onSignOut: onSignOut)
    }
}
#endif
