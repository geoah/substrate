#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateScreens

/// The review as its presentation words it: the heading and why, each
/// list, and the way on to the browser.
public struct ReviewScreen: View {
    let presentation: ReviewPresentation
    let onContinue: () -> Void
    let onCancel: () -> Void

    public init(_ presentation: ReviewPresentation, onContinue: @escaping () -> Void, onCancel: @escaping () -> Void) {
        self.presentation = presentation
        self.onContinue = onContinue
        self.onCancel = onCancel
    }

    public var body: some View {
        NavigationStack {
            Form {
                Section {
                    VStack(alignment: .leading, spacing: 6) {
                        Text(presentation.title)
                            .font(.title2.weight(.semibold))
                            .accessibilityAddTraits(.isHeader)
                        Text(presentation.intro)
                            .foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    .padding(.vertical, 4)
                }

                ReviewSectionsView(presentation.sections)

                Section {
                    Button(action: onContinue) {
                        Text(presentation.continueTitle).frame(maxWidth: .infinity, minHeight: 30)
                    }
                    .buttonStyle(.borderedProminent)
                    .buttonBorderShape(.capsule)
                    .controlSize(.large)
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets())
                } footer: {
                    Text(presentation.footer)
                }
            }
            .formStyle(.grouped)
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(presentation.cancelTitle, action: onCancel) }
            }
        }
    }
}

/// A review's lists, as sections of a form.
public struct ReviewSectionsView: View {
    let sections: [ReviewSection]

    public init(_ sections: [ReviewSection]) {
        self.sections = sections
    }

    public var body: some View {
        ForEach(sections) { section in
            Section {
                ForEach(section.rows) { ChangeLineView($0) }
            } header: {
                Text(section.title)
            } footer: {
                if let footer = section.footer { Text(footer) }
            }
        }
    }
}

/// What the app will ask the owner for, before it opens the browser: the
/// access it needs and what it sets up on their server, in the words the
/// approval page uses, so nothing there is a surprise. The owner decides on
/// the page; this only says what they will find.
public struct ReviewChangesSheet: View {
    let presentation: ReviewPresentation
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
        presentation = ReviewPresenter.present(appName: appName, host: host, changes: changes, heading: heading,
                                               alreadyAllowed: alreadyAllowed, setsUp: setsUp)
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
        ReviewScreen(presentation, onContinue: onContinue, onCancel: onCancel)
    }

    var intro: String { presentation.intro }
}

/// A review's lists, as sections of a form: the access the app asks for,
/// what the owner left out last time and is asked for again, and what it
/// sets up on their server. A sign-in screen shows the same sections the
/// review sheet does.
public struct ReviewChangesSections: View {
    let sections: [ReviewSection]

    public init(appName: String, host: String, heading: ReviewHeading, changes: [Change]) {
        sections = ReviewPresenter.sections(appName: appName, host: host, heading: heading, changes: changes)
    }

    public var body: some View {
        ReviewSectionsView(sections)
    }

    /// What the owner can do on the page and after it, naming only the
    /// controls there are.
    public static func choosing(host: String, setsUp: Bool) -> String {
        ReviewPresenter.choosing(host: host, setsUp: setsUp)
    }
}
#endif
