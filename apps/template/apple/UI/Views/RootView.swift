import SubstrateUI
import SwiftUI
import TemplateCore

/// The sign-in while there is no session, the access-ended screen while the
/// token is refused, else the app.
public struct RootView: View {
    public init() {}

    public var body: some View {
        AppShellRoot<TemplateApp, ItemsView>(tagline: "Your items, kept in your substrate.") { link, app in
            ItemsView(link: link, app: app)
        }
        .environment(Template.shell)
    }
}

/// Settings: the app's own sections, then the kit's status, repository and
/// about, as one presentation.
public struct SettingsView: View {
    public init() {}

    public var body: some View {
        AppSettingsView<TemplateApp>()
            .environment(Template.shell)
    }
}

public extension View {
    /// About's Check for Updates runs `action`: the updater the scene holds.
    func checkingForUpdates(_ action: @escaping @MainActor () -> Void) -> some View {
        environment(\.checkForUpdates, action)
    }
}
