import Foundation

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// What carries a request to the server and its answer back. The client is
/// written against this, so a test stands in for the server with a script
/// and an app picks its own session.
public protocol HTTPTransport: Sendable {
    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse)
    /// The answer's head, then its body line by line as it arrives, for an
    /// answer that does not end (the watch, an agent turn). Cancelling the
    /// lines' reader cancels the request. A redirect is the head itself, as
    /// it is for `data(for:)`.
    func lines(for request: URLRequest) async throws -> LineStream
}

public extension HTTPTransport {
    /// The whole answer, then its lines: a transport with no stream of its
    /// own (a scripted server) answers this way.
    func lines(for request: URLRequest) async throws -> LineStream {
        let (data, head) = try await data(for: request)
        return LineStream(head: head, body: data)
    }
}

/// An answer read line by line: its head, then each line as it arrives,
/// without its line break.
public struct LineStream: Sendable {
    public var head: HTTPURLResponse
    public var lines: AsyncThrowingStream<String, any Error>

    public init(head: HTTPURLResponse, lines: AsyncThrowingStream<String, any Error>) {
        self.head = head
        self.lines = lines
    }

    /// The lines of a body read whole.
    public init(head: HTTPURLResponse, body: Data) {
        self.head = head
        let text = String(decoding: body, as: UTF8.self)
        let lines = text.split(omittingEmptySubsequences: false, whereSeparator: \.isNewline).map(String.init)
        self.lines = AsyncThrowingStream { continuation in
            for line in lines where !line.isEmpty { continuation.yield(line) }
            continuation.finish()
        }
    }

    /// The body read on, up to `limit` bytes, for a refusal's envelope.
    public func body(limit: Int) async throws -> Data {
        var data = Data()
        for try await line in lines {
            data.append(contentsOf: line.utf8)
            data.append(0x0A)
            if data.count > limit { break }
        }
        return data
    }
}

/// The transport over a `URLSession`. A redirect is never followed: the
/// answer is the 3xx itself, which every caller refuses, since a request
/// carries a bearer token, a code and its verifier, or a token to revoke,
/// and goes to the server the owner chose and nowhere else.
public struct URLSessionTransport: HTTPTransport {
    public let session: URLSession
    #if !canImport(Darwin)
    /// The open-source URLSession asks only the session's own delegate about
    /// a redirect, never a task's, so a caller's session is run as one made
    /// from its configuration with a delegate that refuses.
    private let refusing: URLSession
    #endif

    public init(session: URLSession) {
        self.session = session
        #if !canImport(Darwin)
        refusing =
            session.delegate is RefusingRedirects
            ? session
            : URLSession(configuration: session.configuration, delegate: RefusingRedirects.shared, delegateQueue: nil)
        #endif
    }

    /// One session for every client that names none: no cache, since every
    /// answer is a repository's current state, and no waiting for a network
    /// that is not there, since every caller says so in its own words.
    public static let shared = URLSessionTransport(session: makeSession())

    /// A session whose every task refuses redirects, so a stream an app
    /// reads from it directly refuses them too.
    public static func makeSession(
        timeout: TimeInterval = 30, resource: TimeInterval? = nil,
        protocolClasses: [AnyClass]? = nil
    ) -> URLSession {
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = timeout
        if let resource { config.timeoutIntervalForResource = resource }
        config.httpMaximumConnectionsPerHost = 6
        #if canImport(Darwin)
        config.waitsForConnectivity = false
        #endif
        config.requestCachePolicy = .reloadIgnoringLocalCacheData
        config.urlCache = nil
        if let protocolClasses { config.protocolClasses = protocolClasses }
        return URLSession(configuration: config, delegate: RefusingRedirects.shared, delegateQueue: nil)
    }

    /// The task's own delegate refuses a redirect whatever session it runs
    /// on, the caller's included.
    public func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        #if canImport(Darwin)
        let (data, response) = try await session.data(for: request, delegate: RefusingRedirects.shared)
        #else
        let (data, response) = try await refusing.data(for: request)
        #endif
        guard let http = response as? HTTPURLResponse else { throw URLError(.badServerResponse) }
        return (data, http)
    }

    /// The task's own delegate refuses a redirect here too. The open-source
    /// URLSession has no byte stream, so off Apple systems the lines arrive
    /// when the answer ends; Android's transport streams them (§5.4).
    public func lines(for request: URLRequest) async throws -> LineStream {
        #if canImport(Darwin)
        let (bytes, response) = try await session.bytes(for: request, delegate: RefusingRedirects.shared)
        guard let http = response as? HTTPURLResponse else { throw URLError(.badServerResponse) }
        let lines = AsyncThrowingStream<String, any Error> { continuation in
            let task = Task {
                do {
                    for try await line in bytes.lines { continuation.yield(line) }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
        return LineStream(head: http, lines: lines)
        #else
        let (data, http) = try await data(for: request)
        return LineStream(head: http, body: data)
        #endif
    }
}

/// Answers every redirect with no request, so the task ends with the 3xx.
public final class RefusingRedirects: NSObject, URLSessionTaskDelegate, Sendable {
    public static let shared = RefusingRedirects()

    #if canImport(Darwin)
    public func urlSession(
        _ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
        newRequest request: URLRequest
    ) async -> URLRequest? {
        nil
    }
    #else
    // The open-source URLSession asks with a completion handler alone.
    public func urlSession(
        _ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
        newRequest request: URLRequest, completionHandler: @escaping @Sendable (URLRequest?) -> Void
    ) {
        completionHandler(nil)
    }
    #endif
}
