#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateScreens

/// The Repository screen for an app that keeps its session itself rather
/// than in an `AppShell`: which server and repository, the server's
/// version, what the app was approved to do and until when, and the two
/// ways out of it. A password session says it reaches everything. A token
/// this device stopped using that its server has not revoked yet is said,
/// with where to revoke it by hand.
public struct AccountSection: View {
    let session: Session
    let manifest: AppManifest?
    let serverVersion: String?
    let unrevoked: [Session]
    let onChangeAccess: (() -> Void)?
    let onSignOut: () -> Void
    @Environment(\.locale) private var locale
    @Environment(\.timeZone) private var timeZone

    public init(
        session: Session, manifest: AppManifest? = nil, serverVersion: String? = nil, unrevoked: [Session] = [],
        onChangeAccess: (() -> Void)? = nil, onSignOut: @escaping () -> Void
    ) {
        self.session = session
        self.manifest = manifest
        self.serverVersion = serverVersion
        self.unrevoked = unrevoked
        self.onChangeAccess = onChangeAccess
        self.onSignOut = onSignOut
    }

    public var body: some View {
        RepositoryScreen(
            RepositoryPresenter.present(
                session: SessionFacts(session), manifest: manifest, serverVersion: serverVersion,
                unrevoked: unrevoked.map(SessionFacts.init), offersChangeAccess: onChangeAccess != nil,
                locale: locale, timeZone: timeZone),
            onChangeAccess: onChangeAccess, onSignOut: onSignOut)
    }
}
#endif
