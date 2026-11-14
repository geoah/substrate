import SubstrateKit
import SwiftUI
import WebKit

#if os(macOS)
struct ConsoleWebView: NSViewRepresentable {
    let controller: ConsoleController
    func makeNSView(context: Context) -> WKWebView { controller.webView }
    func updateNSView(_ view: WKWebView, context: Context) {}
}
#else
struct ConsoleWebView: UIViewRepresentable {
    let controller: ConsoleController
    func makeUIView(context: Context) -> WKWebView { controller.webView }
    func updateUIView(_ view: WKWebView, context: Context) {}
}
#endif

/// A window: the connect screen until the app opens a server, the console
/// after, and the connect screen again over the console when the page or the
/// menu asks to change servers, and in its place when the sign-in ends.
struct RootView: View {
    @EnvironmentObject private var store: ServerStore
    @EnvironmentObject private var sessions: ConsoleSessions

    var body: some View {
        Group {
            if let server = store.current, sessions.opens(server) {
                let session = sessions.session(for: server)
                // A new sign-in is a new page: the session is read before any
                // of the page's script runs.
                ConsoleWindow(server: server, session: session).id("\(server.absoluteString) \(session?.tokenID ?? "page")")
            } else {
                ConsoleConnect(cancellable: false)
            }
        }
        .task { await sessions.revokePending() }
    }
}

private struct ConsoleWindow: View {
    // One controller per window for the window's life: SwiftUI rebuilds views
    // freely, and a rebuilt web view would drop the page. The autoclosure
    // runs once per identity, and RootView keys the identity on the server.
    @StateObject private var controller: ConsoleController

    init(server: URL, session: Session?) {
        _controller = StateObject(wrappedValue: ConsoleController(server: server, session: session))
    }

    var body: some View {
        ConsoleContent(controller: controller)
            .onDisappear { controller.tearDown() }
    }
}

private struct ConsoleContent: View {
    @ObservedObject var controller: ConsoleController
    @EnvironmentObject private var updates: Updater
    @EnvironmentObject private var sessions: ConsoleSessions

    var body: some View {
        ConsoleWebView(controller: controller)
            .focusedSceneObject(controller)
            #if os(macOS)
        .navigationTitle(controller.title)
        .navigationSubtitle(controller.server.host() ?? controller.server.absoluteString)
            #else
        .ignoresSafeArea(.container, edges: .bottom)
            #endif
            .sheet(isPresented: $controller.picking) {
                ConsoleConnect(cancellable: true)
                    // An alert from the window under a sheet never shows, so
                    // the sheet's own update check answers from it.
                    .updateOffer(updates)
                    #if os(macOS)
                .frame(width: 460, height: 640)
                    #endif
            }
            .onAppear {
                controller.checkForUpdates = { updates.checkNow() }
                let server = controller.server
                controller.signedOut = { [sessions] tokenID in
                    Task { await sessions.signedOut(of: server, tokenID: tokenID) }
                }
                controller.renamed = { [sessions, weak controller] tokenID, repository in
                    Task {
                        if let session = await sessions.renamed(on: server, tokenID: tokenID, repository: repository) {
                            controller?.updateSession(session)
                        }
                    }
                }
            }
    }
}
