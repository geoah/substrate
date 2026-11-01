import SwiftUI

/// What the app will ask the owner for, before it opens the browser: the
/// access it needs and what it sets up on their server, in the words the
/// approval page uses, so nothing there is a surprise. The owner decides on
/// the page; this only says what they will find.
public struct ReviewChangesSheet: View {
    let appName: String
    let host: String
    let heading: ReviewHeading
    let changes: [Change]
    /// The page lists what the app holds now under Already allowed: a
    /// released app's client id, approved before.
    let alreadyAllowed: Bool
    /// The request carries details, whose access comes with setting them up.
    let setsUp: Bool
    let onContinue: () -> Void
    let onCancel: () -> Void

    public init(appName: String, host: String, changes: [Change], heading: ReviewHeading,
                onContinue: @escaping () -> Void, onCancel: @escaping () -> Void)
    {
        self.init(appName: appName, host: host, changes: changes, heading: heading, alreadyAllowed: false,
                  setsUp: changes.contains(where: \.isSetup), onContinue: onContinue, onCancel: onCancel)
    }

    init(appName: String, host: String, changes: [Change], heading: ReviewHeading, alreadyAllowed: Bool, setsUp: Bool,
         onContinue: @escaping () -> Void, onCancel: @escaping () -> Void)
    {
        self.appName = appName
        self.host = host
        self.heading = heading
        self.changes = changes
        self.alreadyAllowed = alreadyAllowed
        self.setsUp = setsUp
        self.onContinue = onContinue
        self.onCancel = onCancel
    }

    /// The owner's own Change Access for the request `manifest` sends now,
    /// `session` the access the app holds: the page's controls as the
    /// server will show them.
    public init(changingAccessFor manifest: AppManifest, session: Session, changes: [Change],
                onContinue: @escaping () -> Void, onCancel: @escaping () -> Void)
    {
        self.init(appName: manifest.appName, host: ServerAddress.display(session.server), changes: changes,
                  heading: .changeAccess, alreadyAllowed: manifest.showsAlreadyAllowed(session),
                  setsUp: !manifest.details.isEmpty, onContinue: onContinue, onCancel: onCancel)
    }

    /// Headed by what the changes are.
    public init(appName: String, host: String, changes: [Change], firstSignIn: Bool,
                onContinue: @escaping () -> Void, onCancel: @escaping () -> Void)
    {
        self.init(appName: appName, host: host, changes: changes, heading: ReviewHeading(changes: changes, firstSignIn: firstSignIn),
                  onContinue: onContinue, onCancel: onCancel)
    }

    public var body: some View {
        NavigationStack {
            Form {
                Section {
                    VStack(alignment: .leading, spacing: 6) {
                        Text(heading == .firstSignIn ? "Connect \(appName) to \(host)" : heading.title(appName: appName))
                            .font(.title2.weight(.semibold))
                            .accessibilityAddTraits(.isHeader)
                        Text(intro)
                            .foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    .padding(.vertical, 4)
                }

                ReviewChangesSections(appName: appName, host: host, heading: heading, changes: changes)

                Section {
                    Button(action: onContinue) {
                        Text("Continue in Browser").frame(maxWidth: .infinity, minHeight: 30)
                    }
                    .buttonStyle(.borderedProminent)
                    .buttonBorderShape(.capsule)
                    .controlSize(.large)
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets())
                } footer: {
                    Text(ReviewChangesSections.choosing(host: host, setsUp: setsUp))
                }
            }
            .formStyle(.grouped)
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Not Now", action: onCancel) }
            }
        }
    }

    var intro: String {
        let leftOut = changes.contains(where: \.leftOut)
        switch heading {
        case .firstSignIn:
            return "\(appName) will ask for this access. You see the same list in your browser, and approve it there."
        case .renew:
            return leftOut
                ? "\(appName) asks to keep the access you approved, for another while, and asks again for what you left out last time."
                : "Nothing new: \(appName) asks to keep the access you approved, for another while."
        case .changeAccess:
            let listed = alreadyAllowed
                ? "You choose on \(host): what \(appName) has now is listed there under Already allowed, where you can uncheck it."
                : "You choose on \(host), where you can uncheck any access \(appName) asks for."
            return listed + (setsUp ? " Access that comes with what it sets up, such as a webhook, has no checkbox: to refuse it, deny." : "")
        case .moreAccess, .serverSetup, .moreAccessAndServerSetup:
            return "\(appName) has changed since you approved it. What it already has keeps working until you decide."
        }
    }
}

/// A review's lists, as sections of a form: the access the app asks for,
/// what the owner left out last time and is asked for again, and what it
/// sets up on their server. A sign-in screen shows the same sections the
/// review sheet does.
public struct ReviewChangesSections: View {
    let appName: String
    let host: String
    let heading: ReviewHeading
    let changes: [Change]

    public init(appName: String, host: String, heading: ReviewHeading, changes: [Change]) {
        self.appName = appName
        self.host = host
        self.heading = heading
        self.changes = changes
    }

    private var access: [Change] { changes.filter { !$0.isSetup && !$0.leftOut } }
    private var leftOut: [Change] { changes.filter(\.leftOut) }
    private var setup: [Change] { changes.filter(\.isSetup) }

    public var body: some View {
        if !access.isEmpty {
            Section(heading == .firstSignIn ? "It will be able to" : "It asks to also") {
                ForEach(access) { ChangeRow($0) }
            }
        }

        if !leftOut.isEmpty {
            Section {
                ForEach(leftOut) { ChangeRow($0) }
            } header: {
                Text("You left these out last time")
            } footer: {
                Text("The page on \(host) asks for them again; uncheck them there to keep them out.")
            }
        }

        if !setup.isEmpty {
            Section {
                ForEach(setup) { ChangeRow($0) }
            } header: {
                Text("It will set up on your server")
            } footer: {
                Text(setup.contains(where: \.blocked)
                    ? "Some of this can't be set up yet: the page on \(host) shows why, and approving waits until it is fixed."
                    : "Setting up continues even if you close the browser. What it sets up stays if it stops part way or you later revoke \(appName).")
            }
        }
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
