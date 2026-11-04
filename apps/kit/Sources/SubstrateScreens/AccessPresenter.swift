import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore

/// The Access screen (§5.6): the status banner's issues and access ended,
/// in the words `AppStatusBanner` and `AccessEndedView` said them.
public enum AccessPresenter {
    /// How many changes a line lists before it counts the rest.
    public static let shownChanges = 3

    /// Why an app's access ended, in one sentence every screen says it with.
    public static func endedWords(appName: String, host: String) -> String {
        "\(host) no longer accepts \(appName): the time you approved it for is up, or it was revoked in Settings, under Apps."
    }

    public static func ended(appName: String, host: String) -> AccessEndedPresentation {
        AccessEndedPresentation(
            symbol: .accessEnded, title: "\(appName)'s access ended", message: endedWords(appName: appName, host: host),
            signInAgain: "Sign In Again")
    }

    /// The banner over the issues, nil when there are none. An action the
    /// app does not offer shows no button.
    public static func banner(
        issues: [AppIssue], appName: String, host: String, offers: Set<AccessAction.Offer>, now: Date,
        locale: Locale, timeZone: TimeZone
    ) -> AccessBanner? {
        guard !issues.isEmpty else { return nil }
        let urgent = issues.contains { issue in
            switch issue {
            case .serverTooOld, .accessEnded, .accessEndsSoon: true
            default: false
            }
        }
        let lines = issues.map {
            line(
                for: $0, in: issues, appName: appName, host: host, offers: offers, now: now, locale: locale,
                timeZone: timeZone)
        }
        return AccessBanner(tone: urgent || issues.approvalBlocked ? .caution : .informative, lines: lines)
    }

    static func line(
        for issue: AppIssue, in issues: [AppIssue], appName: String, host: String, offers: Set<AccessAction.Offer>,
        now: Date, locale: Locale, timeZone: TimeZone
    ) -> IssueLine {
        func offered(_ title: String, _ action: AccessAction) -> ActionButton<AccessAction>? {
            offers.contains(action.offer) ? ActionButton(title, action) : nil
        }
        switch issue {
        case let .serverTooOld(running, needed, missing, blocked):
            let cannot =
                missing.isEmpty
                ? blocked.isEmpty ? "" : "can't yet set up everything \(appName) needs"
                : "can't yet \(joinWords(missing))"
            let runs = running.map { "\(host) runs \(ServerVersion.display($0))" }
            var message: String
            if let needed {
                message = "\(appName) needs Substrate \(needed) or later" + (runs.map { "; \($0)." } ?? ".")
                if !cannot.isEmpty { message += " It \(cannot)." }
            } else {
                message =
                    runs.map { "\($0), which \(cannot.isEmpty ? "is too old for \(appName)" : cannot)." }
                    ?? "\(host) \(cannot.isEmpty ? "is too old for \(appName)" : cannot)."
            }
            return lineWithChanges(
                symbol: .serverUpdate, tone: .caution, title: "\(host) needs an update",
                message: message + " Whoever runs it can update it.", changes: blocked, action: nil)
        case let .needsApproval(changes) where changes.contains(where: \.blocked):
            // The approval page would refuse. A fix the server named is the
            // way on; without one, the page itself shows why.
            let count = changes.count == 1 ? "a change" : "\(changes.count) changes"
            if let fix = changes.lazy.compactMap({ $0.block?.fix }).first, offers.contains(.openFix) {
                return lineWithChanges(
                    symbol: .warning, tone: .caution, title: "Something on \(host) is in the way",
                    message: "\(appName) needs \(count) that can't be approved until what is in the way is fixed on \(host):",
                    changes: changes, action: ActionButton("Fix It on \(host)", .openFix(fix)))
            }
            return lineWithChanges(
                symbol: .warning, tone: .caution, title: "Something on \(host) is in the way",
                message: "\(appName) needs \(count) that can't be approved yet. The approval page on \(host) shows why:",
                changes: changes, action: offered("Review", .review))
        case let .needsApproval(changes) where issues.waitsForUpdate:
            return lineWithChanges(
                symbol: .approval, tone: .informative, title: "\(appName) needs your approval",
                message: "Once \(host) is updated, \(appName) will ask you to approve:", changes: changes, action: nil)
        case let .needsApproval(changes):
            return lineWithChanges(
                symbol: .approval, tone: .informative, title: "\(appName) needs your approval",
                message: changes.count == 1
                    ? "One change, which you approve on \(host):"
                    : "\(changes.count) changes, which you approve on \(host):",
                changes: changes, action: offered("Review", .review))
        case let .accessEndsSoon(date):
            return IssueLine(
                symbol: .clock, tone: .caution,
                title: "Access ends \(DateWords.day(date, now: now, locale: locale, timeZone: timeZone))",
                message: "\(appName) stops working with \(host) then, until you approve it again.",
                action: issues.approvalBlocked ? nil : offered("Renew", .review))
        case .accessEnded:
            return IssueLine(
                symbol: .ended, tone: .critical, title: "\(appName)'s access ended",
                message: endedWords(appName: appName, host: host), action: offered("Sign In Again", .signInAgain))
        case let .unreachable(message):
            return IssueLine(
                symbol: .offline, tone: .quiet, title: "Couldn't check with \(host)", message: message,
                action: offered("Try Again", .retry))
        case .legacyAccess:
            return IssueLine(
                symbol: .key, tone: .informative, title: "Move to app access",
                message: "\(appName) signed in with your password, so it can reach everything. \(host) can now give it only the access it needs.",
                action: offered("Move to App Access", .moveToAppAccess))
        }
    }

    private static func lineWithChanges(
        symbol: ScreenSymbol, tone: ScreenTone, title: String, message: String, changes: [Change],
        action: ActionButton<AccessAction>?
    ) -> IssueLine {
        IssueLine(
            symbol: symbol, tone: tone, title: title, message: message, changes: changes.prefix(shownChanges).map(ChangeLine.init),
            more: changes.count > shownChanges ? "and \(changes.count - shownChanges) more" : nil, action: action)
    }
}
