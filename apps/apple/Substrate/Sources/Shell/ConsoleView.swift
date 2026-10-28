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

/// A window: the server picker until a server is chosen, the console after,
/// and the picker again over the console when the page or the menu asks.
struct RootView: View {
    @EnvironmentObject private var store: ServerStore

    var body: some View {
        if let server = store.current {
            ConsoleWindow(server: server).id(server)
        } else {
            ServerPicker(cancellable: false)
        }
    }
}

private struct ConsoleWindow: View {
    // One controller per window for the window's life: SwiftUI rebuilds views
    // freely, and a rebuilt web view would drop the page. The autoclosure
    // runs once per identity, and RootView keys the identity on the server.
    @StateObject private var controller: ConsoleController

    init(server: URL) {
        _controller = StateObject(wrappedValue: ConsoleController(server: server))
    }

    var body: some View {
        ConsoleContent(controller: controller)
            .onDisappear { controller.tearDown() }
    }
}

private struct ConsoleContent: View {
    @ObservedObject var controller: ConsoleController
    @EnvironmentObject private var updates: Updater

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
                ServerPicker(cancellable: true)
                    // An alert from the window under a sheet never shows, so
                    // the picker's own check answers from the sheet.
                    .updateOffer(updates)
                    #if os(macOS)
                    .frame(width: 460)
                    #endif
            }
            .onAppear {
                controller.checkForUpdates = { updates.checkNow() }
            }
    }
}
