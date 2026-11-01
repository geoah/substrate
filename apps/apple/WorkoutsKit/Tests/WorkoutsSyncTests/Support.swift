import Foundation
import SubstrateKit
import WorkoutsCore
@testable import WorkoutsSync

/// A scripted server: each request is answered by the first rule whose
/// method and path match, and every request is kept, in order.
final class ScriptedServer: HTTPTransport, @unchecked Sendable {
    struct Sent: Sendable {
        var method: String
        var path: String
        var query: String?
        var body: WorkoutsCore.JSONValue?
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
        var body: WorkoutsCore.JSONValue?
        if let data = request.httpBody { body = try? JSONDecoder().decode(WorkoutsCore.JSONValue.self, from: data) }
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

/// One request held on the wire: `arrived()` returns once it is, and
/// `release()` lets it through.
final class Gate: @unchecked Sendable {
    private let lock = NSLock()
    private var arrival: CheckedContinuation<Void, Never>?
    private var hasArrived = false
    private var release_: CheckedContinuation<Void, Never>?
    private var released = false

    func arriveAndWait() async {
        lock.withLock {
            hasArrived = true
            arrival?.resume()
            arrival = nil
        }
        await withCheckedContinuation { continuation in
            let go = lock.withLock { () -> Bool in
                if released { return true }
                release_ = continuation
                return false
            }
            if go { continuation.resume() }
        }
    }

    func arrived() async {
        await withCheckedContinuation { continuation in
            let go = lock.withLock { () -> Bool in
                if hasArrived { return true }
                arrival = continuation
                return false
            }
            if go { continuation.resume() }
        }
    }

    func release() {
        let continuation = lock.withLock { () -> CheckedContinuation<Void, Never>? in
            released = true
            defer { release_ = nil }
            return release_
        }
        continuation?.resume()
    }
}

enum Fixtures {
    static func data(_ name: String) throws -> Data {
        guard let url = Bundle.module.url(forResource: name, withExtension: "json", subdirectory: "Fixtures") else {
            throw CocoaError(.fileNoSuchFile)
        }
        return try Data(contentsOf: url)
    }
}

/// The repository the fixtures were recorded from: a throwaway server built
/// from this tree, its training package declared under this authority.
let recordedAuthority = "probe.example.com"

func api(_ server: ScriptedServer, authority: String = "ada.example.com") -> WorkoutsAPI {
    var client = SubstrateClient(server: URL(string: "http://localhost:8080")!, token: "substrate_tok_test", transport: server)
    client.pause = { _ in }
    return WorkoutsAPI(client: client, authority: authority)
}

func record(_ version: Int) -> String {
    #"{"id":"x","kind":"k","properties":{},"version":\#(version)}"#
}

func temporaryDirectory() -> URL {
    let url = FileManager.default.temporaryDirectory.appendingPathComponent("workouts-sync-\(UUID().uuidString)", isDirectory: true)
    try? FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
    return url
}
