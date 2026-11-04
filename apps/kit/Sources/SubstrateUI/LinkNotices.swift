#if canImport(SwiftUI)
import SubstrateCore
import SubstrateRuntime
import SubstrateScreens
import SwiftUI

/// What stands in the way, above an app's column: the link's Access banner,
/// then each notice line (the records not being read, a change the owner
/// must answer), each its own row with its mark. Every action goes to the
/// shell, which runs the browser, a check again or the console's page.
public struct LinkNotices<App: LinkedApp>: View {
    @Environment(AppShell<App>.self) private var shell
    @Environment(\.openURL) private var openURL
    let link: LinkPresentation<App.Presentation>
    let lines: [NoticeLine]
    let openStatus: (() -> Void)?

    /// - Parameters:
    ///   - lines: the app's own notices; the link's (a refused write) come
    ///     after them.
    ///   - openStatus: where the link's notice takes the owner: the Status
    ///     section of Settings.
    public init(link: LinkPresentation<App.Presentation>, lines: [NoticeLine], openStatus: (() -> Void)? = nil) {
        self.link = link
        self.lines = lines
        self.openStatus = openStatus
    }

    public var body: some View {
        VStack(spacing: 8) {
            if let banner = link.banner {
                AccessBannerView(banner) { action in
                    switch action {
                    case .review, .signInAgain, .moveToAppAccess: Task { try? await shell.changeAccess() }
                    case .retry: shell.send(LinkController<App>.Command.refresh(force: true))
                    case let .openFix(path):
                        if let server = link.snapshot.server, let url = ServerAddress.page(server, path: path) { openURL(url) }
                    }
                }
            }
            ForEach(lines, id: \.self) { NoticeLineView(line: $0) }
            if let notice = link.status.notice {
                if let openStatus {
                    Button(action: openStatus) { NoticeLineView(line: notice, opens: true) }
                        .buttonStyle(.plain)
                } else {
                    NoticeLineView(line: notice)
                }
            }
        }
    }
}

/// One notice: its mark in its tone, then its words.
public struct NoticeLineView: View {
    let line: NoticeLine
    let opens: Bool

    public init(line: NoticeLine, opens: Bool = false) {
        self.line = line
        self.opens = opens
    }

    public var body: some View {
        HStack(spacing: 6) {
            Label(line.text, systemImage: line.symbol.systemName)
                .foregroundStyle(line.tone.color)
            if opens {
                Spacer(minLength: 0)
                Image(systemName: "chevron.right").foregroundStyle(.tertiary)
            }
        }
        .font(.footnote)
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(.rect)
    }
}
#endif
