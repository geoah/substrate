import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import SubstrateCore
import SubstrateDevice

/// A scripted server answering by rules: each request is answered by the
/// first rule whose method and path match, and every request is kept, in
/// order, its body read as JSON. A rule answering status -1 is the network
/// gone. Where `StubTransport` answers each route in turn, this one decides
/// from what was sent (an `ifVersion`, a query).
public final class ScriptedServer: HTTPTransport, @unchecked Sendable {
    public struct Sent: Sendable {
        public var method: String
        public var path: String
        public var query: String?
        public var body: JSONValue?
    }

    public typealias Rule = @Sendable (Sent) -> (Int, String)?

    private let lock = NSLock()
    private var rules: [Rule] = []
    private var kept: [Sent] = []
    private var holds: [String: HeldRequest] = [:]

    public init() {}

    public var sent: [Sent] { lock.withLock { kept } }

    public var methodsAndPaths: [String] { lock.withLock { kept.map { "\($0.method) \($0.path)" } } }

    public func on(_ rule: @escaping Rule) { lock.withLock { rules.append(rule) } }

    /// Answers `method path` with a status and a body, every time.
    public func answer(_ method: String, _ path: String, _ status: Int, _ body: String) {
        on { $0.method == method && $0.path == path ? (status, body) : nil }
    }

    /// Holds the next `method path` request on the wire until the hold is
    /// released, so a test can act while a write is in flight.
    public func hold(_ method: String, _ path: String) -> HeldRequest {
        let held = HeldRequest()
        lock.withLock { holds["\(method) \(path)"] = held }
        return held
    }

    public func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        guard let url = request.url else { throw URLError(.badURL) }
        var body: JSONValue?
        if let data = request.httpBody { body = try? JSONDecoder().decode(JSONValue.self, from: data) }
        let components = URLComponents(url: url, resolvingAgainstBaseURL: false)
        let sent = Sent(method: request.httpMethod ?? "GET", path: components?.percentEncodedPath ?? url.path,
                        query: components?.query, body: body)
        let held = lock.withLock { holds.removeValue(forKey: "\(sent.method) \(sent.path)") }
        if let held { await held.arriveAndWait() }
        let (status, text) = lock.withLock { () -> (Int, String) in
            kept.append(sent)
            for rule in rules {
                if let answer = rule(sent) { return answer }
            }
            return (500, #"{"error":{"code":"internal","message":"no rule"}}"#)
        }
        if status == -1 { throw URLError(.notConnectedToInternet) }
        let response = HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1",
                                       headerFields: ["Content-Type": "application/json"])!
        return (Data(text.utf8), response)
    }
}

/// One request held on the wire: `arrived()` returns once it is, and
/// `release()` lets it through.
public final class HeldRequest: @unchecked Sendable {
    private let lock = NSLock()
    private var arrival: CheckedContinuation<Void, Never>?
    private var hasArrived = false
    private var waiting: CheckedContinuation<Void, Never>?
    private var released = false

    func arriveAndWait() async {
        let arrival = lock.withLock { () -> CheckedContinuation<Void, Never>? in
            hasArrived = true
            defer { self.arrival = nil }
            return self.arrival
        }
        arrival?.resume()
        await withCheckedContinuation { continuation in
            let go = lock.withLock { () -> Bool in
                if released { return true }
                waiting = continuation
                return false
            }
            if go { continuation.resume() }
        }
    }

    public func arrived() async {
        await withCheckedContinuation { continuation in
            let go = lock.withLock { () -> Bool in
                if hasArrived { return true }
                arrival = continuation
                return false
            }
            if go { continuation.resume() }
        }
    }

    public func release() {
        let continuation = lock.withLock { () -> CheckedContinuation<Void, Never>? in
            released = true
            defer { waiting = nil }
            return waiting
        }
        continuation?.resume()
    }
}

/// A folder of its own under the temporary directory, for one test.
public func temporaryFolder(_ label: String = "kit") -> URL {
    let url = FileManager.default.temporaryDirectory.appendingPathComponent("\(label)-\(UUID().uuidString)", isDirectory: true)
    try? FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
    return url
}
