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
    let onContinue: () -> Void
    let onCancel: () -> Void

    public init(appName: String, host: String, changes: [Change], firstSignIn: Bool,
                onContinue: @escaping () -> Void, onCancel: @escaping () -> Void)
    {
        self.appName = appName
        self.host = host
        heading = ReviewHeading(changes: changes, firstSignIn: firstSignIn)
        self.changes = changes
        self.onContinue = onContinue
        self.onCancel = onCancel
    }

    private var access: [Change] { changes.filter { !$0.isSetup } }
    private var setup: [Change] { changes.filter(\.isSetup) }

    public var body: some View {
        NavigationStack {
            Form {
                Section {
                    VStack(alignment: .leading, spacing: 6) {
                        Text(heading == .firstSignIn ? "Connect \(appName) to \(host)" : heading.title)
                            .font(.title2.weight(.semibold))
                        Text(intro)
                            .foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    .padding(.vertical, 4)
                }

                if !access.isEmpty {
                    Section(heading == .firstSignIn ? "It will be able to" : "It asks to also") {
                        ForEach(access) { ChangeRow($0) }
                    }
                }

                if !setup.isEmpty {
                    Section {
                        ForEach(setup) { ChangeRow($0) }
                    } header: {
                        Text("It will set up on your server")
                    } footer: {
                        Text(setup.contains(where: \.blocked)
                            ? "Some of this can't be done yet. The page on \(host) says what is in the way and where to fix it."
                            : "If you stop part way, what was installed stays.")
                    }
                }

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
                    Text("You approve this on \(host), signed in there. You can uncheck any access before you do, and change or revoke it later in Settings, under Apps.")
                }
            }
            .formStyle(.grouped)
            .navigationTitle(heading.title)
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Not Now", action: onCancel) }
            }
        }
    }

    private var intro: String {
        switch heading {
        case .firstSignIn:
            "\(appName) will ask for this access. You see the same list in your browser, and approve it there."
        case .renew:
            "Nothing new: \(appName) asks to keep the access you approved, for another while."
        case .moreAccess, .serverSetup, .moreAccessAndServerSetup:
            "\(appName) has changed since you approved it. What it already has keeps working until you decide."
        }
    }
}
