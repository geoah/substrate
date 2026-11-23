import SubstrateScreens
import SubstrateUI
import SwiftUI
import TodayCore

/// The sign-in while there is no session, the access-ended screen while the
/// token is refused, else the day.
public struct RootView: View {
    public init() {}

    public var body: some View {
        content.environment(Today.links)
    }

    @ViewBuilder private var content: some View {
        #if DEBUG
        if let scene = TodayDebug.demo {
            DayScreen(app: scene)
        } else {
            signedIn
        }
        #else
        signedIn
        #endif
    }

    private var signedIn: some View {
        AppShellRoot<TodayApp, DayRoot>(tagline: "Your day, in one pane, from your substrate.") { link, app in
            DayRoot(link: link, app: app)
        }
        .environment(Today.shell)
    }
}

/// The day under the shell: every command goes to the app's root, and what
/// stands in the way of the link is said above the pane.
struct DayRoot: View {
    @Environment(TodayShell.self) private var shell
    let link: LinkPresentation<TodayPresentation>
    let app: TodayPresentation
    @State private var settings = false
    #if os(macOS)
    @Environment(\.openSettings) private var openSettingsWindow
    #endif

    var body: some View {
        let shell = shell
        DayScreen(app: app, send: { shell.send($0) }, openSettings: settingsButton) {
            // The link's banner already says a server that is not answering: the records' own line would say it twice.
            LinkNotices<TodayApp>(link: link, lines: link.banner == nil ? app.notices : [], openStatus: showSettings)
        }
        .problem(app.problem, dismiss: { shell.send(.dismissProblem) })
        .task { shell.send(.looked) }
        #if os(iOS)
        .sheet(isPresented: $settings) {
            NavigationStack {
                SettingsView()
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) { Button("Done") { settings = false } }
                }
            }
        }
        #endif
    }
}

extension DayRoot {
    /// The Mac has Settings in its menu; an iPhone and an iPad reach it
    /// from a button under the day.
    private var settingsButton: (() -> Void)? {
        #if os(macOS)
        nil
        #else
        showSettings
        #endif
    }

    private var showSettings: () -> Void {
        #if os(macOS)
        let open = openSettingsWindow
        return { open() }
        #else
        return { settings = true }
        #endif
    }
}
