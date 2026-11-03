import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import SubstrateDevice

/// A scripted server for answers read line by line (the watch, a turn's
/// stream): each request takes the next answer queued, whose lines arrive
/// at once, and which then ends, fails, or stays open for the test to feed
/// with `send` and to cut with `drop`. With no answer queued a request
/// stays open with nothing on it. Every request is kept.
public final class ScriptedLines: HTTPTransport, @unchecked Sendable {
    public enum Ending: Sendable {
        case finish
        case fail(URLError.Code)
        case open
    }

    private struct Answer {
        var status: Int
        var lines: [String]
        var ending: Ending
    }

    private let lock = NSLock()
    private var answers: [Answer] = []
    private var kept: [URLRequest] = []
    private var open: AsyncThrowingStream<String, any Error>.Continuation?

    public init() {}

    public var requests: [URLRequest] { lock.withLock { kept } }

    /// Whether a request is open now, waiting for lines.
    public var isOpen: Bool { lock.withLock { open != nil } }

    public func answer(status: Int = 200, lines: [String], then ending: Ending = .finish) {
        lock.withLock { answers.append(Answer(status: status, lines: lines, ending: ending)) }
    }

    /// A line on the open answer.
    public func send(_ line: String) {
        _ = lock.withLock { open }?.yield(line)
    }

    /// Cuts the open answer as a lost connection does.
    public func drop() {
        let continuation = lock.withLock { () -> AsyncThrowingStream<String, any Error>.Continuation? in
            defer { open = nil }
            return open
        }
        continuation?.finish(throwing: URLError(.networkConnectionLost))
    }

    public func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let stream = try await lines(for: request)
        return (try await stream.body(limit: .max), stream.head)
    }

    public func lines(for request: URLRequest) async throws -> LineStream {
        guard let url = request.url else { throw URLError(.badURL) }
        let answer = lock.withLock { () -> Answer in
            kept.append(request)
            return answers.isEmpty ? Answer(status: 200, lines: [], ending: .open) : answers.removeFirst()
        }
        let (lines, continuation) = AsyncThrowingStream<String, any Error>.makeStream()
        for line in answer.lines { continuation.yield(line) }
        switch answer.ending {
        case .finish: continuation.finish()
        case let .fail(code): continuation.finish(throwing: URLError(code))
        case .open:
            lock.withLock {
                open?.finish()
                open = continuation
            }
        }
        let head = HTTPURLResponse(url: url, statusCode: answer.status, httpVersion: "HTTP/1.1",
                                   headerFields: ["Content-Type": "application/x-ndjson"])!
        return LineStream(head: head, lines: lines)
    }
}
