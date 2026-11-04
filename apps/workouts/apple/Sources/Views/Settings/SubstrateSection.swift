import SubstrateKit
import SwiftUI
import WorkoutsSync

/// Settings → Substrate, as the kit's shared screens draw it: the Status
/// screen (where the app stands, each package, what waits, the writes the
/// server refused with Retry and Discard) and, signed in, the Repository
/// screen (the account and its two ways out). Signed out, what signing in
/// sets up.
struct SubstrateSection: View {
    @Environment(SubstrateLink.self) private var link
    @State private var signingIn = false

    var body: some View {
        Group { content }
            .signInSheet(isPresented: $signingIn)
    }

    @ViewBuilder
    private var content: some View {
        if let status = link.status {
            StatusScreen(status) { action in
                if action == .signIn { signingIn = true } else { link.perform(action) }
            }
        }
        if let problem = link.syncProblem, link.isSignedIn {
            Section { ProblemView(ProblemPresentation(title: "The training package could not be checked", message: problem, tone: .caution)) }
        }
        if let problem = link.accessProblem {
            Section { ProblemView(ProblemPresentation(title: "Access not changed", message: problem, tone: .caution)) }
        }
        if let repository = link.repository {
            RepositoryScreen(repository, onChangeAccess: { Task { await link.reauthorize() } }, onSignOut: link.signOut)
            if let problem = link.signOutProblem {
                Section { ProblemView(problem) { _ in link.signOut() } }
            }
        } else {
            Section {
                UnrevokedNotices(appName: link.manifest.appName, unrevoked: (try? SessionStore.workouts.pendingRevocations()) ?? [])
            } footer: {
                Text(
                    "Signing in asks your substrate to install the Training package and the workout coach and to let \(link.manifest.appName) write its sessions and sets. Sessions logged before you sign in are sent then."
                )
            }
        }
    }
}

/// The kit's connect screen against the app's manifest: the address, then
/// the server's own sign-in and approval page in the system sheet.
struct SignInSheet: View {
    @Environment(SubstrateLink.self) private var link
    @Environment(\.dismiss) private var dismiss
    @AppStorage("signin.server") private var address = ""

    var body: some View {
        NavigationStack {
            ConnectView(manifest: link.manifest, address: $address, unrevoked: (try? SessionStore.workouts.pendingRevocations()) ?? []) {
                ConnectHeader(
                    appName: link.manifest.appName, tagline: "Your training sessions, in your substrate.",
                    icon: Image("AppMark"))
            } onSignIn: { probe in
                try await link.signIn(probe)
                dismiss()
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
        }
    }
}

extension View {
    /// The sign-in sheet, wherever the app offers it.
    func signInSheet(isPresented: Binding<Bool>) -> some View {
        sheet(isPresented: isPresented) { SignInSheet() }
    }
}
