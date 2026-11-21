import SubstrateUI
import SwiftUI
import TodayCore

/// The sign-in while there is no session, the access-ended screen while the
/// token is refused, else the app.
public struct RootView: View {
    public init() {}

    public var body: some View {
        AppShellRoot<TodayApp, ItemsView>(tagline: "Your items, kept in your substrate.") { link, app in
            ItemsView(link: link, app: app)
        }
        .environment(Today.shell)
    }
}

/// Settings: the app's own sections, then the kit's status, repository and
/// about, as one presentation.
public struct SettingsView: View {
    public init() {}

    public var body: some View {
        AppSettingsView<TodayApp>()
            .environment(Today.shell)
    }
}
