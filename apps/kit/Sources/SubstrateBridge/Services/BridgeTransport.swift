import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import SubstrateCore
import SubstrateDevice

/// The transport on Android: OkHttp, through the bridge. OkHttp follows no
/// redirect on any path, a line stream's included, so a 3xx is the answer
/// itself, as `URLSessionTransport`'s is.
public struct BridgeTransport: HTTPTransport {
    public let channel: DeviceChannel
    /// Where a body past the inline limit is written for Kotlin to send:
    /// `BridgeEnvironment.spill`.
    public let folder: URL

    public init(channel: DeviceChannel, folder: URL) {
        self.channel = channel
        self.folder = folder
    }

    public func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let wire = try wireRequest(request)
        // Kotlin has sent the body by the time any answer comes, and a
        // cancelled call needs it no more.
        defer { Self.remove(wire.bodyFile) }
        let value = try await channel.call(.transportFetch(request: wire))
        return try Self.answer(value, for: request)
    }

    // Out of `data(for:)`, and not inlined into it: Swift 6.4.2's optimizer
    // crashes splitting that coroutine for Android with this inside it.
    @inline(never)
    static func answer(_ value: JSONValue, for request: URLRequest) throws -> (Data, HTTPURLResponse) {
        let outcome: TransportOutcome
        do {
            outcome = try BridgeCodec.decode(TransportOutcome.self, from: value)
        } catch {
            // Swift took the reply, so its file is Swift's to delete, read or
            // not: `TransportOutcome.response(response:)` is the case's name,
            // then its label's.
            remove(value["response"]?["response"]?["bodyFile"]?.string)
            throw error
        }
        switch outcome {
        case let .response(response):
            defer { remove(response.bodyFile) }
            return (try body(of: response), try httpResponse(response, for: request))
        case let .failed(kind, message):
            throw urlError(kind, message, url: request.url)
        }
    }

    /// `HTTPTransport`'s line stream, read from OkHttp as it arrives rather
    /// than whole: dropping the lines or cancelling their reader cancels the
    /// call on Kotlin's side.
    public func lines(for request: URLRequest) async throws -> LineStream {
        let (head, lines) = try await lines(for: request, capacity: 256)
        let reader = LineReader(lines.makeAsyncIterator())
        return LineStream(head: head, lines: AsyncThrowingStream { try await reader.next() })
    }

    /// A response read a line at a time (an ndjson stream): its head, then
    /// each line as it arrives. Dropping the lines, or cancelling the task
    /// reading them, cancels the call on Kotlin's side.
    public func lines(for request: URLRequest, capacity: Int = 256) async throws -> (HTTPURLResponse, TransportLines) {
        let wire = try wireRequest(request)
        // The body is sent before the head comes back.
        defer { Self.remove(wire.bodyFile) }
        var iterator = channel.chunks(.transportLines(request: wire), capacity: capacity).makeAsyncIterator()
        guard let first = try await iterator.next() else {
            throw URLError(.badServerResponse)
        }
        let head: HTTPURLResponse
        switch try BridgeCodec.decode(TransportChunk.self, from: first) {
        case let .head(response): head = try Self.httpResponse(response, for: request)
        case let .failed(kind, message): throw Self.urlError(kind, message, url: request.url)
        case .line: throw BridgeFailure(.malformed, "a line stream began with a line, not its head")
        }
        return (head, TransportLines(iterator: iterator))
    }

    private func wireRequest(_ request: URLRequest) throws -> TransportRequest {
        guard let url = request.url?.absoluteString else { throw URLError(.badURL) }
        let headers = (request.allHTTPHeaderFields ?? [:]).sorted { $0.key < $1.key }
            .map { TransportHeader(name: $0.key, value: $0.value) }
        var wire = TransportRequest(url: url, method: request.httpMethod ?? "GET", headers: headers,
                                    timeoutMillis: Int(request.timeoutInterval * 1000), credentials: Self.carriesCredentials(request))
        if let body = request.httpBody {
            if wire.credentials {
                guard body.count <= TransportRequest.credentialLimit else {
                    throw URLError(.dataLengthExceedsMaximum, userInfo: [NSLocalizedDescriptionKey:
                            "A request carrying credentials is \(body.count) bytes, more than \(TransportRequest.credentialLimit)."])
                }
                wire.body = body.base64EncodedString()
            } else if body.count <= TransportRequest.inlineLimit {
                wire.body = body.base64EncodedString()
            } else {
                try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
                let file = folder.appendingPathComponent("request-\(UUID().uuidString)")
                try body.write(to: file, options: .atomic)
                wire.bodyFile = file.path
            }
        }
        return wire
    }

    /// Whether a request's body or its answer may hold a credential, which
    /// then never goes to a file: an OAuth token or revocation exchange
    /// (a form body, RFC 6749 and 7009, or the endpoint by name), the
    /// password login and the registration, which answer a token, and a
    /// request authenticated by anything but the bearer token. A bearer
    /// header alone is not: it goes out in the message, never in a body,
    /// and an authenticated read's answer is the owner's records, which may
    /// be a blob's worth.
    public static func carriesCredentials(_ request: URLRequest) -> Bool {
        let type = request.value(forHTTPHeaderField: "Content-Type")?.lowercased() ?? ""
        if type.hasPrefix("application/x-www-form-urlencoded") { return true }
        if let authorization = request.value(forHTTPHeaderField: "Authorization"), !authorization.hasPrefix("Bearer ") { return true }
        let last = request.url?.lastPathComponent.lowercased() ?? ""
        return ["token", "revoke", "login", "register"].contains(last)
    }

    static func remove(_ path: String?) {
        guard let path else { return }
        try? FileManager.default.removeItem(atPath: path)
    }

    static func body(of response: TransportResponse) throws -> Data {
        if let file = response.bodyFile {
            return try Data(contentsOf: URL(fileURLWithPath: file))
        }
        guard let body = response.body else { return Data() }
        guard let data = Data(base64Encoded: body) else {
            throw BridgeFailure(.malformed, "a response body that is not base64")
        }
        return data
    }

    static func httpResponse(_ response: TransportResponse, for request: URLRequest) throws -> HTTPURLResponse {
        var fields: [String: String] = [:]
        for header in response.headers {
            fields[header.name] = fields[header.name].map { "\($0), \(header.value)" } ?? header.value
        }
        guard let url = URL(string: response.url) ?? request.url,
              let http = HTTPURLResponse(url: url, statusCode: response.status, httpVersion: "HTTP/1.1", headerFields: fields)
        else { throw URLError(.badServerResponse) }
        return http
    }

    static func urlError(_ kind: TransportFailureKind, _ message: String, url: URL?) -> URLError {
        let code: URLError.Code = switch kind {
        case .offline: .notConnectedToInternet
        case .timedOut: .timedOut
        case .cannotConnect: .cannotConnectToHost
        case .secureConnectionFailed: .secureConnectionFailed
        case .cancelled: .cancelled
        case .other: .unknown
        }
        var info: [String: Any] = [NSLocalizedDescriptionKey: message]
        if let url { info[NSURLErrorFailingURLErrorKey] = url }
        return URLError(code, userInfo: info)
    }
}

/// The rest of a line stream after its head, one line per `next`.
public struct TransportLines: AsyncSequence {
    public typealias Element = String

    fileprivate let iterator: AsyncThrowingStream<JSONValue, Error>.AsyncIterator

    public struct AsyncIterator: AsyncIteratorProtocol {
        fileprivate var iterator: AsyncThrowingStream<JSONValue, Error>.AsyncIterator

        public mutating func next() async throws -> String? {
            guard let value = try await iterator.next() else { return nil }
            switch try BridgeCodec.decode(TransportChunk.self, from: value) {
            case let .line(text): return text
            case let .failed(kind, message): throw BridgeTransport.urlError(kind, message, url: nil)
            case .head: throw BridgeFailure(.malformed, "a line stream sent its head twice")
            }
        }
    }

    public func makeAsyncIterator() -> AsyncIterator {
        AsyncIterator(iterator: iterator)
    }
}

/// The one reader of a line stream's iterator; released with the stream,
/// which cancels the call.
private final class LineReader: @unchecked Sendable {
    private var iterator: TransportLines.AsyncIterator

    init(_ iterator: TransportLines.AsyncIterator) {
        self.iterator = iterator
    }

    func next() async throws -> String? {
        try await iterator.next()
    }
}
