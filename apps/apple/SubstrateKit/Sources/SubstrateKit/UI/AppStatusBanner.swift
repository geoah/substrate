import SwiftUI

/// What the app does about each issue. An issue whose action is nil shows
/// no button.
public struct AppStatusActions {
    /// Opens the review of what changed, then the approval page.
    public var review: (() -> Void)?
    public var signInAgain: (() -> Void)?
    /// A password session's way to an app session.
    public var moveToAppAccess: (() -> Void)?
    public var retry: (() -> Void)?
    /// Opens the console page, by its root-relative path on the server,
    /// where what blocks an approval is fixed (`ServerAddress.page`).
    public var openFix: ((String) -> Void)?

    public init(review: (() -> Void)? = nil, signInAgain: (() -> Void)? = nil, moveToAppAccess: (() -> Void)? = nil,
                retry: (() -> Void)? = nil, openFix: ((String) -> Void)? = nil)
    {
        self.review = review
        self.signInAgain = signInAgain
        self.moveToAppAccess = moveToAppAccess
        self.retry = retry
        self.openFix = openFix
    }
}

/// Why an app's access ended, in one sentence every screen says it with.
public func accessEndedWords(appName: String, host: String) -> String {
    "\(host) no longer accepts \(appName): the time you approved it for is up, or it was revoked in Settings, under Apps."
}

/// An app's status issues, one line each with its action: what needs the
/// owner, in words, and the one thing to press about it.
public struct AppStatusBanner: View {
    let issues: [AppIssue]
    let appName: String
    let host: String
    let actions: AppStatusActions

    public init(issues: [AppIssue], appName: String, host: String, actions: AppStatusActions) {
        self.issues = issues
        self.appName = appName
        self.host = host
        self.actions = actions
    }

    public init(status: AppStatus, appName: String, host: String, actions: AppStatusActions) {
        self.init(issues: status.issues, appName: appName, host: host, actions: actions)
    }

    public var body: some View {
        if !issues.isEmpty {
            VStack(alignment: .leading, spacing: 14) {
                ForEach(Array(issues.enumerated()), id: \.offset) { index, issue in
                    if index > 0 { Divider() }
                    IssueLine(line: line(for: issue))
                }
            }
            .modifier(KitCard(tint: issues.contains(where: \.isUrgent) || issues.approvalBlocked ? .orange : .blue))
        }
    }

    private func line(for issue: AppIssue) -> IssueLine.Content {
        switch issue {
        case let .serverTooOld(running, needed, missing, blocked):
            let cannot = missing.isEmpty ? blocked.isEmpty ? "" : "can't yet set up everything \(appName) needs"
                : "can't yet \(joinWords(missing))"
            let runs = running.map { "\(host) runs \(ServerVersion.display($0))" }
            var message: String
            if let needed {
                message = "\(appName) needs Substrate \(needed) or later" + (runs.map { "; \($0)." } ?? ".")
                if !cannot.isEmpty { message += " It \(cannot)." }
            } else {
                message = runs.map { "\($0), which \(cannot.isEmpty ? "is too old for \(appName)" : cannot)." }
                    ?? "\(host) \(cannot.isEmpty ? "is too old for \(appName)" : cannot)."
            }
            return .init(symbol: "arrow.up.circle.fill", tint: .orange, title: "\(host) needs an update",
                         message: message + " Whoever runs it can update it.", changes: blocked, action: nil)
        case let .needsApproval(changes) where changes.contains(where: \.blocked):
            // The approval page would refuse. A fix the server named is the
            // way on; without one, the page itself shows why.
            let count = changes.count == 1 ? "a change" : "\(changes.count) changes"
            if let fix = changes.lazy.compactMap({ $0.block?.fix }).first, let openFix = actions.openFix {
                return .init(symbol: "exclamationmark.triangle.fill", tint: .orange, title: "Something on \(host) is in the way",
                             message: "\(appName) needs \(count) that can't be approved until what is in the way is fixed on \(host):",
                             changes: changes, action: ("Fix It on \(host)", { openFix(fix) }))
            }
            return .init(symbol: "exclamationmark.triangle.fill", tint: .orange, title: "Something on \(host) is in the way",
                         message: "\(appName) needs \(count) that can't be approved yet. The approval page on \(host) shows why:",
                         changes: changes, action: actions.review.map { ("Review", $0) })
        case let .needsApproval(changes) where issues.waitsForUpdate:
            return .init(symbol: "checkmark.shield.fill", tint: .blue, title: "\(appName) needs your approval",
                         message: "Once \(host) is updated, \(appName) will ask you to approve:", changes: changes, action: nil)
        case let .needsApproval(changes):
            return .init(symbol: "checkmark.shield.fill", tint: .blue, title: "\(appName) needs your approval",
                         message: changes.count == 1 ? "One change, which you approve on \(host):"
                             : "\(changes.count) changes, which you approve on \(host):",
                         changes: changes, action: actions.review.map { ("Review", $0) })
        case let .accessEndsSoon(date):
            return .init(symbol: "clock.fill", tint: .orange, title: "Access ends \(dayWords(date))",
                         message: "\(appName) stops working with \(host) then, until you approve it again.",
                         action: issues.approvalBlocked ? nil : actions.review.map { ("Renew", $0) })
        case .accessEnded:
            return .init(symbol: "xmark.circle.fill", tint: .red, title: "\(appName)'s access ended",
                         message: accessEndedWords(appName: appName, host: host),
                         action: actions.signInAgain.map { ("Sign In Again", $0) })
        case let .unreachable(message):
            return .init(symbol: "wifi.exclamationmark", tint: .secondary, title: "Couldn't check with \(host)",
                         message: message, action: actions.retry.map { ("Try Again", $0) })
        case .legacyAccess:
            return .init(symbol: "key.fill", tint: .blue, title: "Move to app access",
                         message: "\(appName) signed in with your password, so it can reach everything. \(host) can now give it only the access it needs.",
                         action: actions.moveToAppAccess.map { ("Move to App Access", $0) })
        }
    }
}

/// One access ending soon, on its own: the date and the way to renew.
public struct AccessEndingBanner: View {
    let appName: String
    let host: String
    let date: Date
    let onRenew: (() -> Void)?

    public init(appName: String, host: String, date: Date, onRenew: (() -> Void)?) {
        self.appName = appName
        self.host = host
        self.date = date
        self.onRenew = onRenew
    }

    public var body: some View {
        AppStatusBanner(issues: [.accessEndsSoon(date)], appName: appName, host: host,
                        actions: AppStatusActions(review: onRenew))
    }
}

/// The access ended or was revoked: what happened, and the one way out.
public struct AccessEndedView: View {
    let appName: String
    let host: String
    let onSignInAgain: () -> Void

    public init(appName: String, host: String, onSignInAgain: @escaping () -> Void) {
        self.appName = appName
        self.host = host
        self.onSignInAgain = onSignInAgain
    }

    public var body: some View {
        VStack(spacing: 20) {
            Image(systemName: "person.crop.circle.badge.exclamationmark")
                .font(.system(size: 48))
                .foregroundStyle(.orange)
                .symbolRenderingMode(.hierarchical)
                .accessibilityHidden(true)
            Text("\(appName)'s access ended")
                .font(.title2.weight(.semibold))
                .multilineTextAlignment(.center)
            Text(accessEndedWords(appName: appName, host: host))
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            Button(action: onSignInAgain) {
                Text("Sign In Again").frame(minWidth: 160, minHeight: 30)
            }
            .buttonStyle(.borderedProminent)
            .buttonBorderShape(.capsule)
            .controlSize(.large)
        }
        .padding(32)
        .frame(maxWidth: 440)
    }
}

/// One issue's line: its symbol, title, words and action.
struct IssueLine: View {
    struct Content {
        var symbol: String
        var tint: Color
        var title: String
        var message: String
        /// The changes an approval makes, the first few listed.
        var changes: [Change] = []
        var action: (String, () -> Void)?
    }

    /// How many changes a line lists before it counts the rest.
    static let shownChanges = 3

    let line: Content

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Image(systemName: line.symbol)
                .foregroundStyle(line.tint)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 8) {
                VStack(alignment: .leading, spacing: 3) {
                    Text(line.title).font(.subheadline.weight(.semibold))
                    Text(line.message)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                if !line.changes.isEmpty {
                    VStack(alignment: .leading, spacing: 6) {
                        ForEach(line.changes.prefix(Self.shownChanges)) { ChangeRow($0) }
                        if line.changes.count > Self.shownChanges {
                            Text("and \(line.changes.count - Self.shownChanges) more")
                                .foregroundStyle(.secondary)
                        }
                    }
                    .font(.subheadline)
                }
                if let (title, action) = line.action {
                    Button(title, action: action)
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                }
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .contain)
    }
}

extension AppIssue {
    /// Whether the app stops working, or will, until the owner acts.
    var isUrgent: Bool {
        switch self {
        case .serverTooOld, .accessEnded, .accessEndsSoon: true
        default: false
        }
    }
}
