import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import WorkoutsCore

@testable import WorkoutsSync

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// A scripted server: each request is answered by the first rule whose
/// method and path match, and every request is kept, in order.
final class ScriptedServer: HTTPTransport, @unchecked Sendable {
    struct Sent: Sendable {
        var method: String
        var path: String
        var query: String?
        var body: JSONValue?
    }

    typealias Rule = @Sendable (Sent) -> (Int, String)?

    private let lock = NSLock()
    private var rules: [Rule] = []
    private(set) var sent: [Sent] = []
    private var gates: [String: Gate] = [:]

    /// Holds the first `method path` request on the wire until the gate is
    /// released, so a test can change the outbox while a write is in flight.
    func hold(_ method: String, _ path: String) -> Gate {
        let gate = Gate()
        lock.withLock { gates["\(method) \(path)"] = gate }
        return gate
    }

    func on(_ rule: @escaping Rule) { lock.withLock { rules.append(rule) } }

    /// Answers `method path` with a status and a body, every time.
    func answer(_ method: String, _ path: String, _ status: Int, _ body: String) {
        on { $0.method == method && $0.path == path ? (status, body) : nil }
    }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let url = request.url!
        var body: JSONValue?
        if let data = request.httpBody { body = try? JSONDecoder().decode(JSONValue.self, from: data) }
        let sent = Sent(method: request.httpMethod ?? "GET", path: url.path(percentEncoded: true), query: url.query(percentEncoded: false), body: body)
        let gate = lock.withLock { gates.removeValue(forKey: "\(sent.method) \(sent.path)") }
        if let gate { await gate.arriveAndWait() }
        let (status, text) = lock.withLock { () -> (Int, String) in
            self.sent.append(sent)
            for rule in rules { if let answer = rule(sent) { return answer } }
            return (500, #"{"error":{"code":"internal","message":"no rule"}}"#)
        }
        if status == -1 { throw URLError(.notConnectedToInternet) }
        let response = HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"])!
        return (Data(text.utf8), response)
    }

    var methodsAndPaths: [String] { lock.withLock { sent.map { "\($0.method) \($0.path)" } } }
}
