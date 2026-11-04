import Foundation
import SubstrateDevice

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// A scripted server: each route answers its responses in turn, the last one
/// again once they run out, and every request is kept for the test to read.
/// Every field is read and written under `lock`.
public final class StubTransport: HTTPTransport, @unchecked Sendable {
    public struct Answer: Sendable {
        public var status: Int
        public var headers: [String: String] = [:]
        public var body: Data = Data()
    }

    private let lock = NSLock()
    private var routes: [String: [Answer]] = [:]
    private var failures: [String: Error] = [:]
    private var sentRequests: [URLRequest] = []

    public init() {}

    /// Every request sent, in order.
    public var requests: [URLRequest] {
        lock.lock()
        defer { lock.unlock() }
        return sentRequests
    }

    @discardableResult
    public func on(_ method: String, _ path: String, status: Int = 200, headers: [String: String] = [:], json: String = "")
        -> StubTransport
    {
        lock.lock()
        defer { lock.unlock() }
        routes["\(method) \(path)", default: []].append(Answer(status: status, headers: headers, body: Data(json.utf8)))
        return self
    }

    public func fail(_ method: String, _ path: String, with error: Error) {
        lock.lock()
        defer { lock.unlock() }
        failures["\(method) \(path)"] = error
    }

    public func sent(_ method: String, _ path: String) -> [URLRequest] {
        lock.lock()
        defer { lock.unlock() }
        return sentRequests.filter { $0.httpMethod == method && $0.url.map(Self.path) == path }
    }

    public func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        guard let url = request.url else { throw URLError(.badURL) }
        let answer = try answer(for: request, url: url)
        let response = HTTPURLResponse(url: url, statusCode: answer.status, httpVersion: "HTTP/1.1", headerFields: answer.headers)!
        return (answer.body, response)
    }

    private func answer(for request: URLRequest, url: URL) throws -> Answer {
        let key = "\(request.httpMethod ?? "GET") \(Self.path(url))"
        lock.lock()
        defer { lock.unlock() }
        sentRequests.append(request)
        if let error = failures[key] { throw error }
        let answers = routes[key] ?? []
        if answers.count > 1 { routes[key]?.removeFirst() }
        return answers.first ?? Answer(status: 404, body: Data(#"{"error":{"code":"not_found","message":"no route \#(key)"}}"#.utf8))
    }

    /// The path as sent, percent-encoding kept.
    public static func path(_ url: URL) -> String {
        URLComponents(url: url, resolvingAgainstBaseURL: false)?.percentEncodedPath ?? url.path
    }
}
