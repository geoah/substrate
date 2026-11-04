import SubstrateUI
import SwiftUI
import TasksCore

/// The sign-in while there is no session, the access-ended screen while the
/// token is refused, else the app.
public struct RootView: View {
    public init() {}

    public var body: some View {
        AppShellRoot<TasksApp, AppView>(tagline: "Your to-do list, kept in your substrate.", icon: Image("AppMark")) { link, app in
            AppView(link: link, app: app)
        }
        .environment(Tasks.shell)
    }
}
