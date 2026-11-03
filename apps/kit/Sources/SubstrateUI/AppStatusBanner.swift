#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateScreens

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

    /// The actions there are closures for.
    public var offers: Set<AccessAction.Offer> {
        var offers = Set<AccessAction.Offer>()
        if review != nil { offers.insert(.review) }
        if signInAgain != nil { offers.insert(.signInAgain) }
        if moveToAppAccess != nil { offers.insert(.moveToAppAccess) }
        if retry != nil { offers.insert(.retry) }
        if openFix != nil { offers.insert(.openFix) }
        return offers
    }

    public func perform(_ action: AccessAction) {
        switch action {
        case .review: review?()
        case .signInAgain: signInAgain?()
        case .moveToAppAccess: moveToAppAccess?()
        case .retry: retry?()
        case let .openFix(path): openFix?(path)
        }
    }
}

/// Why an app's access ended, in one sentence every screen says it with.
public func accessEndedWords(appName: String, host: String) -> String {
    AccessPresenter.endedWords(appName: appName, host: host)
}

/// The Access screen's banner: one line per issue, with its action.
public struct AccessBannerView: View {
    let banner: AccessBanner
    let perform: (AccessAction) -> Void

    public init(_ banner: AccessBanner, perform: @escaping (AccessAction) -> Void) {
        self.banner = banner
        self.perform = perform
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            ForEach(Array(banner.lines.enumerated()), id: \.offset) { index, line in
                if index > 0 { Divider() }
                IssueLineView(line: line, perform: perform)
            }
        }
        .modifier(KitCard(tint: banner.tone.color))
    }
}

/// An app's status issues, one line each with its action: what needs the
/// owner, in words, and the one thing to press about it.
public struct AppStatusBanner: View {
    let issues: [AppIssue]
    let appName: String
    let host: String
    let actions: AppStatusActions
    @Environment(\.locale) private var locale
    @Environment(\.timeZone) private var timeZone

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
        if let banner = AccessPresenter.banner(issues: issues, appName: appName, host: host, offers: actions.offers, now: Date(),
                                               locale: locale, timeZone: timeZone)
        {
            AccessBannerView(banner, perform: actions.perform)
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

/// The access ended or was revoked, as its presentation words it.
public struct AccessEndedScreen: View {
    let presentation: AccessEndedPresentation
    let onSignInAgain: () -> Void

    public init(_ presentation: AccessEndedPresentation, onSignInAgain: @escaping () -> Void) {
        self.presentation = presentation
        self.onSignInAgain = onSignInAgain
    }

    public var body: some View {
        VStack(spacing: 20) {
            Image(systemName: presentation.symbol.systemName)
                .font(.system(size: 48))
                .foregroundStyle(.orange)
                .symbolRenderingMode(.hierarchical)
                .accessibilityHidden(true)
            Text(presentation.title)
                .font(.title2.weight(.semibold))
                .multilineTextAlignment(.center)
            Text(presentation.message)
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            Button(action: onSignInAgain) {
                Text(presentation.signInAgain).frame(minWidth: 160, minHeight: 30)
            }
            .buttonStyle(.borderedProminent)
            .buttonBorderShape(.capsule)
            .controlSize(.large)
        }
        .padding(32)
        .frame(maxWidth: 440)
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
        AccessEndedScreen(AccessPresenter.ended(appName: appName, host: host), onSignInAgain: onSignInAgain)
    }
}

/// One issue's line: its mark, title, words, changes and action.
struct IssueLineView: View {
    let line: IssueLine
    let perform: (AccessAction) -> Void

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Image(systemName: line.symbol.systemName)
                .foregroundStyle(line.tone.color)
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
                        ForEach(line.changes) { ChangeLineView($0) }
                        if let more = line.more {
                            Text(more).foregroundStyle(.secondary)
                        }
                    }
                    .font(.subheadline)
                }
                if let button = line.action {
                    Button(button.title) { perform(button.action) }
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                }
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .contain)
    }
}
#endif
