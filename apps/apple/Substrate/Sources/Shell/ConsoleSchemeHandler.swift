import Foundation
import UniformTypeIdentifiers
import WebKit

/// Serves the console from the app bundle at `substrate://console/` and
/// forwards the substrate's own paths to the server, the way the server
/// itself serves both from one origin (and `vite.config.ts` forwards them in
/// dev). The console only ever asks for same-origin paths, so it runs here
/// unchanged and the server needs no CORS.
@MainActor
final class ConsoleSchemeHandler: NSObject, WKURLSchemeHandler {
    static let scheme = "substrate"
    static let start = URL(string: "\(scheme)://console/")!

    private let server: URL
    private let root: URL
    private let session: URLSession
    /// Tasks WebKit has not stopped. A scheme task must not be answered once
    /// it is stopped (WebKit raises), so every answer checks here first.
    private var live: [ObjectIdentifier: URLSessionDataTask] = [:]

    init(server: URL) {
        self.server = server
        root = Bundle.main.resourceURL!.appendingPathComponent("console", isDirectory: true)
        let config = URLSessionConfiguration.ephemeral
        config.requestCachePolicy = .reloadIgnoringLocalCacheData
        config.urlCache = nil
        config.httpShouldSetCookies = false
        // The changefeed and an agent's turn are bodies that stay open; the
        // request timeout is the longest silence between two chunks.
        config.timeoutIntervalForRequest = 3600
        config.timeoutIntervalForResource = 7 * 24 * 3600
        session = URLSession(configuration: config, delegate: nil, delegateQueue: .main)
    }

    func invalidate() {
        session.invalidateAndCancel()
    }

    // MARK: WKURLSchemeHandler

    func webView(_ webView: WKWebView, start task: any WKURLSchemeTask) {
        guard let url = task.request.url else { return }
        if Self.isSubstratePath(url.path, accept: task.request.value(forHTTPHeaderField: "Accept")) {
            forward(task, url: url)
        } else {
            serveAsset(task, url: url)
        }
    }

    func webView(_ webView: WKWebView, stop task: any WKURLSchemeTask) {
        live.removeValue(forKey: ObjectIdentifier(task))?.cancel()
    }

    /// The paths the server answers rather than the console. The auth door
    /// sits at the root beside the console's own `/login` and `/register`
    /// pages: a navigation asks those for HTML and gets the console, a call
    /// asks for JSON and reaches the door.
    nonisolated static func isSubstratePath(_ path: String, accept: String?) -> Bool {
        for prefix in ["/api/", "/.well-known/"] where path.hasPrefix(prefix) { return true }
        if path == "/healthz" { return true }
        let door = ["/login", "/register", "/password", "/totp", "/tokens"]
        if door.contains(where: { path == $0 || path.hasPrefix($0 + "/") }) {
            return !(accept ?? "").contains("text/html")
        }
        return false
    }

    // MARK: the bundled console

    private func serveAsset(_ task: any WKURLSchemeTask, url: URL) {
        let wanted = root.appendingPathComponent(String(url.path.drop(while: { $0 == "/" })))
            .standardizedFileURL
        var file = wanted
        var isDirectory: ObjCBool = false
        let inside = wanted.path.hasPrefix(root.standardizedFileURL.path)
        if !inside || !FileManager.default.fileExists(atPath: wanted.path, isDirectory: &isDirectory)
            || isDirectory.boolValue
        {
            // A built asset that is missing stays missing; every other path is
            // a console route, and the console routes it.
            if url.path.hasPrefix("/assets/") || !inside {
                answer(task, url: url, status: 404, type: "text/plain", body: Data("not found".utf8))
                return
            }
            file = root.appendingPathComponent("index.html")
        }
        guard let body = try? Data(contentsOf: file) else {
            answer(task, url: url, status: 500, type: "text/plain", body: Data("unreadable".utf8))
            return
        }
        answer(task, url: url, status: 200, type: Self.mimeType(of: file), body: body)
    }

    private func answer(_ task: any WKURLSchemeTask, url: URL, status: Int, type: String, body: Data) {
        let response = HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: [
            "Content-Type": type,
            "Content-Length": String(body.count),
            "Cache-Control": "no-cache",
        ])!
        task.didReceive(response)
        task.didReceive(body)
        task.didFinish()
    }

    private static func mimeType(of file: URL) -> String {
        switch file.pathExtension.lowercased() {
        case "html": return "text/html; charset=utf-8"
        case "js", "mjs": return "text/javascript; charset=utf-8"
        case "css": return "text/css; charset=utf-8"
        case "json": return "application/json"
        case "svg": return "image/svg+xml"
        case "wasm": return "application/wasm"
        case "woff2": return "font/woff2"
        default:
            return UTType(filenameExtension: file.pathExtension)?.preferredMIMEType ?? "application/octet-stream"
        }
    }

    // MARK: the substrate

    private func forward(_ task: any WKURLSchemeTask, url: URL) {
        var target = URLComponents(url: server, resolvingAgainstBaseURL: false)!
        let asked = URLComponents(url: url, resolvingAgainstBaseURL: false)
        // Percent-encoded as asked: a declaration id's `/` travels as `%2F`
        // and the API decodes it exactly once.
        target.percentEncodedPath = asked?.percentEncodedPath ?? url.path
        target.percentEncodedQuery = asked?.percentEncodedQuery
        guard let targetURL = target.url else {
            answer(task, url: url, status: 400, type: "text/plain", body: Data("bad path".utf8))
            return
        }

        var upstream = URLRequest(url: targetURL)
        upstream.httpMethod = task.request.httpMethod ?? "GET"
        for (name, value) in task.request.allHTTPHeaderFields ?? [:] {
            // The bundle's origin means nothing to the server.
            if ["origin", "referer", "host"].contains(name.lowercased()) { continue }
            upstream.setValue(value, forHTTPHeaderField: name)
        }
        upstream.httpBody = task.request.httpBody ?? task.request.httpBodyStream.map(Self.drain)

        let relay = Relay(handler: self, task: task)
        let dataTask = session.dataTask(with: upstream)
        dataTask.delegate = relay
        live[ObjectIdentifier(task)] = dataTask
        dataTask.resume()
    }

    private static func drain(_ stream: InputStream) -> Data {
        var data = Data()
        stream.open()
        defer { stream.close() }
        var buffer = [UInt8](repeating: 0, count: 64 * 1024)
        while stream.hasBytesAvailable {
            let read = stream.read(&buffer, maxLength: buffer.count)
            if read <= 0 { break }
            data.append(buffer, count: read)
        }
        return data
    }

    fileprivate func isLive(_ task: any WKURLSchemeTask) -> Bool {
        live[ObjectIdentifier(task)] != nil
    }

    fileprivate func finished(_ task: any WKURLSchemeTask) {
        live.removeValue(forKey: ObjectIdentifier(task))
    }
}

/// One forwarded request's answer, chunk by chunk, so a streamed body (the
/// changefeed, an agent's turn) reaches the page as it arrives. The session
/// delivers on the main queue, where the handler's bookkeeping lives.
private final class Relay: NSObject, URLSessionDataDelegate, @unchecked Sendable {
    private weak var handler: ConsoleSchemeHandler?
    private let task: any WKURLSchemeTask

    init(handler: ConsoleSchemeHandler, task: any WKURLSchemeTask) {
        self.handler = handler
        self.task = task
    }

    private func ifLive(_ body: @MainActor (ConsoleSchemeHandler) -> Void) {
        MainActor.assumeIsolated {
            guard let handler, handler.isLive(task) else { return }
            body(handler)
        }
    }

    func urlSession(
        _ session: URLSession, dataTask: URLSessionDataTask, didReceive response: URLResponse,
        completionHandler: @escaping (URLSession.ResponseDisposition) -> Void
    ) {
        ifLive { _ in
            let http = response as? HTTPURLResponse
            var headers: [String: String] = [:]
            for (name, value) in http?.allHeaderFields ?? [:] {
                guard let name = name as? String, let value = value as? String else { continue }
                // URLSession has already decoded the body, so its encoding and
                // length no longer describe what the page receives.
                if ["content-encoding", "content-length", "transfer-encoding"].contains(name.lowercased()) {
                    continue
                }
                headers[name] = value
            }
            let answer = HTTPURLResponse(
                url: task.request.url!, statusCode: http?.statusCode ?? 502,
                httpVersion: "HTTP/1.1", headerFields: headers)!
            task.didReceive(answer)
        }
        completionHandler(.allow)
    }

    func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data) {
        ifLive { _ in task.didReceive(data) }
    }

    func urlSession(_ session: URLSession, task sessionTask: URLSessionTask, didCompleteWithError error: (any Error)?) {
        ifLive { handler in
            handler.finished(task)
            if let error {
                task.didFailWithError(error)
            } else {
                task.didFinish()
            }
        }
    }
}
