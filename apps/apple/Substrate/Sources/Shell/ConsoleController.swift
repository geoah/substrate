import SwiftUI
import WebKit

#if os(macOS)
import AppKit
#else
import UIKit
#endif

/// One window's console: the web view, pointed at one server, and everything
/// the page needs from outside it. Links leave for the system browser, a
/// download lands as a file, and the page reaches the shell through the
/// `substrateShell` message handler (`web/console/src/lib/shell.ts`).
@MainActor
final class ConsoleController: NSObject, ObservableObject {
    let server: URL
    let webView: WKWebView
    @Published private(set) var title = "Substrate"
    @Published private(set) var canGoBack = false
    @Published private(set) var canGoForward = false
    /// The server picker is up over this window's console.
    @Published var picking = false
    /// What the page's Check for updates asks for; the window wires it to the
    /// app's one updater.
    var checkForUpdates: (() -> Void)?

    private let schemeHandler: ConsoleSchemeHandler
    private var observations: [NSKeyValueObservation] = []
    /// Windows the page opened (`window.open`), held until they navigate
    /// somewhere the system browser should show instead.
    private var popups: [WKWebView] = []
    private var downloads: [WKDownload: URL] = [:]
    #if DEBUG
    private var driver: DebugDriver?
    #endif

    init(server: URL) {
        self.server = server
        schemeHandler = ConsoleSchemeHandler(server: server)

        let config = WKWebViewConfiguration()
        config.websiteDataStore = ServerStore.dataStore(for: server)
        config.setURLSchemeHandler(schemeHandler, forURLScheme: ConsoleSchemeHandler.scheme)
        #if os(iOS)
        config.allowsInlineMediaPlayback = true
        config.dataDetectorTypes = []
        #endif
        let content = WKUserContentController()
        content.addUserScript(WKUserScript(
            source: Self.shellScript(server: server), injectionTime: .atDocumentStart, forMainFrameOnly: true))
        config.userContentController = content

        webView = WKWebView(frame: .zero, configuration: config)
        webView.isInspectable = true
        #if os(macOS)
        webView.allowsMagnification = true
        webView.setValue(false, forKey: "drawsBackground")
        #else
        webView.isOpaque = false
        webView.backgroundColor = .systemBackground
        webView.scrollView.backgroundColor = .systemBackground
        webView.allowsBackForwardNavigationGestures = true
        #endif
        super.init()

        content.add(WeakMessageHandler(self), name: "substrateShell")
        webView.navigationDelegate = self
        webView.uiDelegate = self
        observations = [
            webView.observe(\.title) { [weak self] view, _ in
                MainActor.assumeIsolated { self?.title = view.title.flatMap { $0.isEmpty ? nil : $0 } ?? "Substrate" }
            },
            webView.observe(\.canGoBack) { [weak self] view, _ in
                MainActor.assumeIsolated { self?.canGoBack = view.canGoBack }
            },
            webView.observe(\.canGoForward) { [weak self] view, _ in
                MainActor.assumeIsolated { self?.canGoForward = view.canGoForward }
            },
        ]
        webView.load(URLRequest(url: ConsoleSchemeHandler.start))
        #if DEBUG
        driver = DebugDriver(webView: webView)
        #endif
    }

    func tearDown() {
        #if DEBUG
        driver?.stop()
        #endif
        observations.removeAll()
        webView.configuration.userContentController.removeScriptMessageHandler(forName: "substrateShell")
        webView.stopLoading()
        schemeHandler.invalidate()
    }

    /// What the page reads before any of its own script runs.
    private static func shellScript(server: URL) -> String {
        #if os(macOS)
        let platform = "macos"
        #else
        let platform = "ios"
        #endif
        let build = BuildInfo.current
        let info: [String: Any] = [
            "platform": platform,
            "server": server.absoluteString.trimmingCharacters(in: ["/"]),
            "version": build.version,
            "updates": build.updates != nil,
        ]
        let json = String(data: try! JSONSerialization.data(withJSONObject: info, options: [.sortedKeys]), encoding: .utf8)!
        return "window.substrateShell = Object.freeze(\(json));"
    }

    // MARK: commands

    func reload() { webView.reload() }
    func goBack() { webView.goBack() }
    func goForward() { webView.goForward() }
    func goHome() { webView.load(URLRequest(url: ConsoleSchemeHandler.start)) }

    #if os(macOS)
    func zoom(by step: Double?) {
        webView.pageZoom = step.map { min(3, max(0.5, webView.pageZoom + $0)) } ?? 1
    }
    #endif

    fileprivate func openExternally(_ url: URL) {
        #if os(macOS)
        NSWorkspace.shared.open(url)
        #else
        UIApplication.shared.open(url)
        #endif
    }

    fileprivate func received(_ message: WKScriptMessage) {
        guard let body = message.body as? [String: Any], let type = body["type"] as? String else { return }
        switch type {
        case "changeServer": picking = true
        case "checkForUpdates": checkForUpdates?()
        default: break
        }
    }

    fileprivate static func belongsInside(_ url: URL) -> Bool {
        ["substrate", "about", "blob", "data"].contains(url.scheme?.lowercased() ?? "")
    }
}

// MARK: navigation

extension ConsoleController: WKNavigationDelegate {
    func webView(
        _ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
        preferences: WKWebpagePreferences
    ) async -> (WKNavigationActionPolicy, WKWebpagePreferences) {
        if action.shouldPerformDownload { return (.download, preferences) }
        guard let url = action.request.url else { return (.cancel, preferences) }
        if webView !== self.webView {
            // A window the page opened: whatever it is sent to is the system
            // browser's to show (the OAuth consent page, above all).
            if Self.belongsInside(url) { return (.allow, preferences) }
            openExternally(url)
            popups.removeAll { $0 === webView }
            return (.cancel, preferences)
        }
        if Self.belongsInside(url) { return (.allow, preferences) }
        openExternally(url)
        return (.cancel, preferences)
    }

    func webView(
        _ webView: WKWebView, decidePolicyFor response: WKNavigationResponse
    ) async -> WKNavigationResponsePolicy {
        response.canShowMIMEType ? .allow : .download
    }

    func webView(_ webView: WKWebView, navigationAction: WKNavigationAction, didBecome download: WKDownload) {
        download.delegate = self
    }

    func webView(_ webView: WKWebView, navigationResponse: WKNavigationResponse, didBecome download: WKDownload) {
        download.delegate = self
    }

    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        webView.reload()
    }
}

// MARK: windows the page opens

extension ConsoleController: WKUIDelegate {
    func webView(
        _ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
        for action: WKNavigationAction, windowFeatures: WKWindowFeatures
    ) -> WKWebView? {
        if let url = action.request.url, !Self.belongsInside(url) {
            openExternally(url)
            return nil
        }
        // `window.open("about:blank")` from a click, navigated once its
        // address is known: the page gets a real window to hold, and the
        // address goes to the browser when it arrives.
        let popup = WKWebView(frame: .zero, configuration: configuration)
        popup.navigationDelegate = self
        popup.uiDelegate = self
        popups.append(popup)
        return popup
    }

    func webViewDidClose(_ webView: WKWebView) {
        popups.removeAll { $0 === webView }
    }
}

// MARK: downloads

extension ConsoleController: WKDownloadDelegate {
    func download(
        _ download: WKDownload, decideDestinationUsing response: URLResponse, suggestedFilename: String
    ) async -> URL? {
        #if os(macOS)
        let folder = FileManager.default.urls(for: .downloadsDirectory, in: .userDomainMask)[0]
        #else
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        #endif
        let destination = Self.unused(folder.appendingPathComponent(suggestedFilename))
        downloads[download] = destination
        return destination
    }

    func downloadDidFinish(_ download: WKDownload) {
        guard let file = downloads.removeValue(forKey: download) else { return }
        #if os(macOS)
        NSWorkspace.shared.activateFileViewerSelecting([file])
        #else
        let share = UIActivityViewController(activityItems: [file], applicationActivities: nil)
        share.popoverPresentationController?.sourceView = webView
        share.popoverPresentationController?.sourceRect = CGRect(
            x: webView.bounds.midX, y: webView.bounds.midY, width: 0, height: 0)
        var top = webView.window?.rootViewController
        while let presented = top?.presentedViewController { top = presented }
        top?.present(share, animated: true)
        #endif
    }

    func download(_ download: WKDownload, didFailWithError error: any Error, resumeData: Data?) {
        downloads.removeValue(forKey: download)
    }

    private static func unused(_ file: URL) -> URL {
        var candidate = file
        var n = 1
        let stem = file.deletingPathExtension().lastPathComponent
        let ext = file.pathExtension
        while FileManager.default.fileExists(atPath: candidate.path) {
            n += 1
            let name = ext.isEmpty ? "\(stem) \(n)" : "\(stem) \(n).\(ext)"
            candidate = file.deletingLastPathComponent().appendingPathComponent(name)
        }
        return candidate
    }
}

/// The content controller holds its handlers strongly; this keeps it from
/// holding the controller that owns the web view that owns it.
private final class WeakMessageHandler: NSObject, WKScriptMessageHandler {
    weak var target: ConsoleController?
    init(_ target: ConsoleController) { self.target = target }

    func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
        MainActor.assumeIsolated { target?.received(message) }
    }
}
