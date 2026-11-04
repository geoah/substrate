import SubstrateAuth

/// The approval review (§5.6's Access), in the words `ReviewChangesSheet`
/// said it.
public enum ReviewPresenter {
    /// - Parameters:
    ///   - alreadyAllowed: the page lists what the app holds now under
    ///     Already allowed (a released app's client id, approved before).
    ///   - setsUp: the request carries details, whose access comes with
    ///     setting them up.
    public static func present(
        appName: String, host: String, changes: [Change], heading: ReviewHeading, alreadyAllowed: Bool,
        setsUp: Bool
    ) -> ReviewPresentation {
        ReviewPresentation(
            title: heading == .firstSignIn ? "Connect \(appName) to \(host)" : heading.title(appName: appName),
            intro: intro(appName: appName, host: host, heading: heading, changes: changes, alreadyAllowed: alreadyAllowed),
            sections: sections(appName: appName, host: host, heading: heading, changes: changes),
            continueTitle: "Continue in Browser", footer: choosing(host: host, setsUp: setsUp), cancelTitle: "Not Now")
    }

    static func intro(appName: String, host: String, heading: ReviewHeading, changes: [Change], alreadyAllowed: Bool) -> String {
        let leftOut = changes.contains(where: \.leftOut)
        switch heading {
        case .firstSignIn:
            return "\(appName) will ask for this access. You see the same list in your browser, and approve it there."
        case .renew:
            return leftOut
                ? "\(appName) asks to keep the access you approved, for another while, and asks again for what you left out last time."
                : "Nothing new: \(appName) asks to keep the access you approved, for another while."
        case .changeAccess:
            // The footer (`choosing`) says how set up is refused.
            return alreadyAllowed
                ? "You choose on \(host): what \(appName) has now is listed there under Already allowed, where you can uncheck it."
                : "You choose on \(host), where you can uncheck any access \(appName) asks for."
        case .moreAccess, .serverSetup, .moreAccessAndServerSetup:
            return "\(appName) has changed since you approved it. What it already has keeps working until you decide."
        }
    }

    /// The review's lists: the access the app asks for, what the owner left
    /// out last time and is asked for again, and what it sets up on their
    /// server. A sign-in screen shows the same sections the review does.
    public static func sections(appName: String, host: String, heading: ReviewHeading, changes: [Change]) -> [ReviewSection] {
        let access = changes.filter { !$0.isSetup && !$0.leftOut }
        let leftOut = changes.filter(\.leftOut)
        let setup = changes.filter(\.isSetup)
        var out: [ReviewSection] = []
        if !access.isEmpty {
            out.append(
                ReviewSection(
                    id: "access", title: heading == .firstSignIn ? "It will be able to" : "It asks to also",
                    rows: access.map(ChangeLine.init)))
        }
        if !leftOut.isEmpty {
            out.append(
                ReviewSection(
                    id: "leftOut", title: "You left these out last time", rows: leftOut.map(ChangeLine.init),
                    footer: "The page on \(host) asks for them again; uncheck them there to keep them out."))
        }
        if !setup.isEmpty {
            out.append(
                ReviewSection(
                    id: "setup", title: "It will set up on your server", rows: setup.map(ChangeLine.init),
                    footer: setup.contains(where: \.blocked)
                        ? "Some of this can't be set up yet: the page on \(host) shows why, and approving waits until it is fixed."
                        : "Setting up continues even if you close the browser. What it sets up stays if it stops part way or you later revoke \(appName)."))
        }
        return out
    }

    /// What the owner can do on the page and after it, naming only the
    /// controls there are: a checkbox per access, Deny for the set up,
    /// Change Access in the app, and revoking in the server's Settings.
    public static func choosing(host: String, setsUp: Bool) -> String {
        "You choose in your browser: you can uncheck any access with a checkbox there."
            + (setsUp ? " Setting up comes with the request; to refuse it, deny." : "")
            + " Later, Change Access in this app asks again; to revoke, use \(host)'s Settings, under Apps."
    }
}
