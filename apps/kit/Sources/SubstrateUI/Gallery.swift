#if canImport(SwiftUI)
import SwiftUI
import SubstrateScreens

/// Every shared screen's renderer, by screen: what the gallery draws each
/// fixture with (§7.4). A screen added to `ScreenKind` without a renderer
/// here fails the gallery's check, so a presentation never ships without
/// its view.
public enum Gallery {
    public static let renderers: [ScreenKind: @MainActor (FixturePresentation) -> AnyView?] = [
        .connect: { presentation in
            guard case let .connect(connect) = presentation else { return nil }
            return AnyView(ConnectScreen(connect, text: .constant(connect.field.text), send: { _ in },
                                         header: { ConnectHeader(appName: ScreenFixtures.manifest.appName, tagline: "Your tasks, in your substrate.") },
                                         passwordDoor: { _ in PasswordDoorStandIn() }))
        },
        .serverTooOld: { presentation in
            guard case let .serverTooOld(tooOld) = presentation else { return nil }
            return AnyView(ScrollView { ServerTooOldScreen(tooOld, onTryAgain: {}, onChangeServer: {}).padding(24) })
        },
        .accessBanner: { presentation in
            guard case let .accessBanner(banner) = presentation else { return nil }
            return AnyView(ScrollView { AccessBannerView(banner, perform: { _ in }).padding(20) })
        },
        .accessEnded: { presentation in
            guard case let .accessEnded(ended) = presentation else { return nil }
            return AnyView(AccessEndedScreen(ended, onSignInAgain: {}))
        },
        .review: { presentation in
            guard case let .review(review) = presentation else { return nil }
            return AnyView(ReviewScreen(review, onContinue: {}, onCancel: {}))
        },
        .repository: { presentation in
            guard case let .repository(repository) = presentation else { return nil }
            return AnyView(Form { RepositoryScreen(repository, onChangeAccess: {}, onSignOut: {}) }.formStyle(.grouped))
        },
        .status: { presentation in
            guard case let .status(status) = presentation else { return nil }
            return AnyView(Form { StatusScreen(status, perform: { _ in }) }.formStyle(.grouped))
        },
        .settings: { presentation in
            guard case let .settings(settings) = presentation else { return nil }
            return AnyView(SettingsScreen(settings, send: { _ in }))
        },
        .problem: { presentation in
            guard case let .problem(problem) = presentation else { return nil }
            return AnyView(ScrollView { ProblemView(problem).modifier(KitCard(tint: problem.tone.color)).padding(20) })
        },
        .about: { presentation in
            guard case let .about(about) = presentation else { return nil }
            return AnyView(Form { AboutScreen(about) }.formStyle(.grouped))
        },
    ]

    /// Where an app's own password form stands: the kit draws the door,
    /// the app the form.
    struct PasswordDoorStandIn: View {
        var body: some View {
            VStack(alignment: .leading, spacing: 10) {
                Text("The app's own password form").font(.subheadline.weight(.semibold))
                Text("Password").foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                    .padding(.horizontal, 12)
                    .background(.fill.tertiary, in: .rect(cornerRadius: 10))
            }
            .modifier(KitCard(tint: .secondary))
        }
    }

    /// The fixture drawn by its screen's renderer; nil when there is none.
    @MainActor
    public static func view(for fixture: ScreenFixture) -> AnyView? {
        renderers[fixture.screen]?(fixture.presentation)
    }
}
#endif
