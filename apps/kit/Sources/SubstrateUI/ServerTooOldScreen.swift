#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateScreens

/// A server that cannot do what the app needs, as its presentation words it.
public struct ServerTooOldScreen: View {
    let presentation: ServerTooOldPresentation
    var onTryAgain: (() -> Void)?
    var onChangeServer: (() -> Void)?

    public init(_ presentation: ServerTooOldPresentation, onTryAgain: (() -> Void)? = nil, onChangeServer: (() -> Void)? = nil) {
        self.presentation = presentation
        self.onTryAgain = onTryAgain
        self.onChangeServer = onChangeServer
    }

    public var body: some View {
        VStack(spacing: 18) {
            Image(systemName: presentation.symbol.systemName)
                .font(.system(size: 44))
                .foregroundStyle(.orange)
                .symbolRenderingMode(.hierarchical)
                .accessibilityHidden(true)
            VStack(spacing: 8) {
                Text(presentation.title)
                    .font(.title2.weight(.semibold))
                    .multilineTextAlignment(.center)
                Text(presentation.summary)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if let cannot = presentation.cannotTitle {
                VStack(alignment: .leading, spacing: 8) {
                    Text(cannot)
                        .font(.subheadline.weight(.semibold))
                    ForEach(presentation.missing) { ChangeLineView($0).font(.subheadline) }
                }
                .modifier(KitCard())
            }
            Text(presentation.note)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            let tryAgain = presentation.tryAgain.flatMap { title in onTryAgain.map { (title, $0) } }
            let changeServer = presentation.changeServer.flatMap { title in onChangeServer.map { (title, $0) } }
            if tryAgain != nil || changeServer != nil {
                VStack(spacing: 10) {
                    if let (title, action) = tryAgain {
                        Button(action: action) {
                            Text(title).frame(maxWidth: .infinity, minHeight: 30)
                        }
                        .buttonStyle(.borderedProminent)
                        .buttonBorderShape(.capsule)
                        .controlSize(.large)
                    }
                    if let (title, action) = changeServer {
                        Button(title, action: action)
                            .buttonStyle(.borderless)
                    }
                }
            }
        }
        .frame(maxWidth: 440)
    }
}

/// A server that cannot do what the app needs: which release it runs, which
/// the app needs, what it cannot do yet, and who can change that.
public struct ServerTooOldView: View {
    let presentation: ServerTooOldPresentation
    var onTryAgain: (() -> Void)?
    var onChangeServer: (() -> Void)?

    /// - Parameter blocked: the package rows only a newer server sets up,
    ///   each with the server's sentence.
    public init(appName: String, host: String, running: String?, needed: String?, missing: [String], blocked: [Change] = [],
                onTryAgain: (() -> Void)? = nil, onChangeServer: (() -> Void)? = nil)
    {
        presentation = .present(appName: appName, host: host, running: running, needed: needed, missing: missing, blocked: blocked,
                                tryAgain: onTryAgain != nil, changeServer: onChangeServer != nil)
        self.onTryAgain = onTryAgain
        self.onChangeServer = onChangeServer
    }

    public var body: some View {
        ServerTooOldScreen(presentation, onTryAgain: onTryAgain, onChangeServer: onChangeServer)
    }
}
#endif
