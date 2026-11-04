import Foundation
import SubstrateDevice

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// A server scripted per test: each request is answered by the first route
/// whose method matches, whose path ends with the route's and whose query
/// holds its text, with a JSON body, an error envelope, or an ndjson stream
/// that can be held open part way until the test releases it. A read can be
/// held on the wire the same way.
final class ChatServer: HTTPTransport, @unchecked Sendable {
    enum Answer {
        case json(Int, String)
        case stream([String], holdAfter: Int? = nil)
    }

    struct Route {
        var method: String
        var path: String
        var query: String?
        var answer: Answer
    }

    private let lock = NSLock()
    private var routes: [Route] = []
    private var kept: [String] = []
    private var released = false
    private var heldPaths: [String: Bool] = [:]

    init(_ routes: [Route] = []) {
        self.routes = routes
    }

    func route(_ method: String, _ path: String, query: String? = nil, _ answer: Answer) {
        lock.withLock { routes.insert(Route(method: method, path: path, query: query, answer: answer), at: 0) }
    }

    /// Takes back the route `route(_:_:query:_:)` put first.
    func unroute() { lock.withLock { _ = routes.removeFirst() } }

    /// Every stream held part way goes on.
    func release() { lock.withLock { released = true } }

    /// The next requests ending with `path` wait until `letGo(_:)`, or
    /// until the request is cancelled.
    func hold(_ path: String) { lock.withLock { heldPaths[path] = false } }
    func letGo(_ path: String) { lock.withLock { heldPaths[path] = true } }

    func count(_ path: String, query: String? = nil) -> Int {
        lock.withLock { kept.count { $0.contains(path) && (query.map($0.contains) ?? true) } }
    }

    var requests: [String] { lock.withLock { kept } }

    private func answer(for request: URLRequest) async -> (Answer, URL) {
        let url = request.url!
        let components = URLComponents(url: url, resolvingAgainstBaseURL: false)
        let path = components?.percentEncodedPath ?? url.path
        let query = components?.query ?? ""
        let method = request.httpMethod ?? "GET"
        let route = lock.withLock { () -> Route? in
            kept.append("\(method) \(path)?\(query)")
            return routes.first { $0.method == method && path.hasSuffix($0.path) && ($0.query.map(query.contains) ?? true) }
        }
        while let held = lock.withLock({ heldPaths.first { path.hasSuffix($0.key) }?.value }), !held, !Task.isCancelled {
            try? await Task.sleep(nanoseconds: 5_000_000)
        }
        return (route?.answer ?? .json(404, #"{"error":{"code":"not_found","message":"no route"}}"#), url)
    }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let stream = try await lines(for: request)
        return (try await stream.body(limit: .max), stream.head)
    }

    func lines(for request: URLRequest) async throws -> LineStream {
        let (answer, url) = await answer(for: request)
        switch answer {
        case let .json(status, body):
            let head = HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"])!
            return LineStream(head: head, body: Data(body.utf8))
        case let .stream(lines, holdAfter):
            let head = HTTPURLResponse(
                url: url, statusCode: 200, httpVersion: "HTTP/1.1",
                headerFields: ["Content-Type": "application/x-ndjson"])!
            let stream = AsyncThrowingStream<String, any Error> { continuation in
                let task = Task {
                    for (index, line) in lines.enumerated() {
                        if let holdAfter, index == holdAfter {
                            while !lock.withLock({ released }) {
                                if Task.isCancelled { return continuation.finish() }
                                try? await Task.sleep(nanoseconds: 5_000_000)
                            }
                        }
                        continuation.yield(line)
                    }
                    continuation.finish()
                }
                continuation.onTermination = { _ in task.cancel() }
            }
            return LineStream(head: head, lines: stream)
        }
    }
}
