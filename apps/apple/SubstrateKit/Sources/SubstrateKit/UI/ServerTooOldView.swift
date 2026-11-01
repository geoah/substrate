import SwiftUI

/// A server that cannot do what the app needs: which release it runs, which
/// the app needs, what it cannot do yet, and who can change that.
public struct ServerTooOldView: View {
    let appName: String
    let host: String
    let running: String?
    let needed: String?
    let missing: [String]
    /// The package rows only a newer server sets up, each with the server's
    /// sentence.
    let blocked: [Change]
    var onTryAgain: (() -> Void)?
    var onChangeServer: (() -> Void)?

    public init(appName: String, host: String, running: String?, needed: String?, missing: [String], blocked: [Change] = [],
                onTryAgain: (() -> Void)? = nil, onChangeServer: (() -> Void)? = nil)
    {
        self.appName = appName
        self.host = host
        self.running = running
        self.needed = needed
        self.missing = missing
        self.blocked = blocked
        self.onTryAgain = onTryAgain
        self.onChangeServer = onChangeServer
    }

    public var body: some View {
        VStack(spacing: 18) {
            Image(systemName: "arrow.up.circle.fill")
                .font(.system(size: 44))
                .foregroundStyle(.orange)
                .symbolRenderingMode(.hierarchical)
                .accessibilityHidden(true)
            VStack(spacing: 8) {
                Text("\(host) needs an update")
                    .font(.title2.weight(.semibold))
                    .multilineTextAlignment(.center)
                Text(summary)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if !missing.isEmpty || !blocked.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    Text("It can't yet")
                        .font(.subheadline.weight(.semibold))
                    ForEach(missing, id: \.self) { line in
                        Label {
                            Text(line.capitalizedFirst).fixedSize(horizontal: false, vertical: true)
                        } icon: {
                            Image(systemName: "xmark.circle").foregroundStyle(.secondary)
                        }
                        .font(.subheadline)
                    }
                    ForEach(blocked) { ChangeRow($0).font(.subheadline) }
                }
                .modifier(KitCard())
            }
            Text("Whoever runs \(host) can update it. Once it is, try again.")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            if onTryAgain != nil || onChangeServer != nil {
                VStack(spacing: 10) {
                    if let onTryAgain {
                        Button(action: onTryAgain) {
                            Text("Try Again").frame(maxWidth: .infinity, minHeight: 30)
                        }
                        .buttonStyle(.borderedProminent)
                        .buttonBorderShape(.capsule)
                        .controlSize(.large)
                    }
                    if let onChangeServer {
                        Button("Use Another Server", action: onChangeServer)
                            .buttonStyle(.borderless)
                    }
                }
            }
        }
        .frame(maxWidth: 440)
    }

    private var summary: String {
        let release = running.flatMap(ServerVersion.init)?.isRelease == true
        let runs = "\(host) runs \(ServerVersion.display(running))" + (release ? "." : " that can't do it yet.")
        guard let needed else { return "\(appName) needs a newer Substrate. \(runs)" }
        return "\(appName) needs Substrate \(needed) or later. \(runs)"
    }
}
