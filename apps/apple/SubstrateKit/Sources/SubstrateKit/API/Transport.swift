import Foundation

/// What carries a request to the server and its answer back. The client is
/// written against this, so a test stands in for the server with a script
/// and an app picks its own session.
public protocol HTTPTransport: Sendable {
    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse)
}

/// The transport over a `URLSession`. A redirect is never followed: the
/// answer is the 3xx itself, which every caller refuses, since a request
/// carries a bearer token, a code and its verifier, or a token to revoke,
/// and goes to the server the owner chose and nowhere else.
public struct URLSessionTransport: HTTPTransport {
    public let session: URLSession

    public init(session: URLSession) {
        self.session = session
    }

    /// One session for every client that names none: no cache, since every
    /// answer is a repository's current state, and no waiting for a network
    /// that is not there, since every caller says so in its own words.
    public static let shared = URLSessionTransport(session: makeSession())

    /// A session whose every task refuses redirects, so a stream an app
    /// reads from it directly refuses them too.
    public static func makeSession(timeout: TimeInterval = 30, resource: TimeInterval? = nil,
                                   protocolClasses: [AnyClass]? = nil) -> URLSession
    {
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = timeout
        if let resource { config.timeoutIntervalForResource = resource }
        config.httpMaximumConnectionsPerHost = 6
        config.waitsForConnectivity = false
        config.requestCachePolicy = .reloadIgnoringLocalCacheData
        config.urlCache = nil
        if let protocolClasses { config.protocolClasses = protocolClasses }
        return URLSession(configuration: config, delegate: RefusingRedirects.shared, delegateQueue: nil)
    }

    /// The task's own delegate refuses a redirect whatever session it runs
    /// on, the caller's included.
    public func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let (data, response) = try await session.data(for: request, delegate: RefusingRedirects.shared)
        guard let http = response as? HTTPURLResponse else { throw URLError(.badServerResponse) }
        return (data, http)
    }
}

/// Answers every redirect with no request, so the task ends with the 3xx.
public final class RefusingRedirects: NSObject, URLSessionTaskDelegate, Sendable {
    public static let shared = RefusingRedirects()

    public func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                           newRequest request: URLRequest) async -> URLRequest?
    {
        nil
    }
}
