import Foundation

/// The server's own refusal: the error envelope's code, message and
/// problems, shown to the owner as they are.
public struct ServerError: LocalizedError, Sendable, Equatable {
    public let status: Int
    public let code: String
    public let message: String
    public let problems: [String]

    public var text: String { ([message] + problems).joined(separator: "\n") }
    public var errorDescription: String? { text }

    /// The envelope, or nil when the body is not one: then the answer may be
    /// a gateway's, and says nothing about whether the request took effect.
    public init?(status: Int, envelope data: Data) {
        guard let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let error = object["error"] as? [String: Any],
              let message = error["message"] as? String
        else { return nil }
        self.status = status
        code = (error["code"] as? String) ?? ""
        self.message = message
        problems = (error["problems"] as? [String]) ?? []
    }

    public init(status: Int, code: String = "", message: String, problems: [String] = []) {
        self.status = status
        self.code = code
        self.message = message
        self.problems = problems
    }

    /// A refusal the same request would meet again: a 4xx other than a
    /// conflict or a rate limit, which clear on their own.
    public var isPermanent: Bool { (400 ..< 500).contains(status) && status != 409 && status != 429 }
}

/// The connection failed, or an answer came that was not the server's (a
/// proxy's page, a body that is not the envelope): whether the request took
/// effect is unknown, unlike a `ServerError`.
public struct TransportError: LocalizedError, Sendable, Equatable {
    /// The HTTP status, when an answer came at all.
    public let status: Int?
    public let text: String
    public var errorDescription: String? { text }

    public init(status: Int? = nil, text: String) {
        self.status = status
        self.text = text
    }

    /// An answer that is not the envelope, in words: its body where it is
    /// short plain text, never a proxy's HTML page, which says nothing a
    /// person can use.
    public init(status: Int, body: Data) {
        let text = String(data: body, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let plain = !text.isEmpty && text.count < 300 && !text.hasPrefix("<") && !text.lowercased().contains("<html")
        self.init(status: status, text: plain ? "The server answered \(status): \(text)" : "The server answered \(status).")
    }
}

/// The server answered with a redirect, which is never followed: what a
/// request carries (a token, a code and its verifier) goes to the server the
/// owner chose alone. `location` is where it pointed, as the server said it.
public struct Redirected: LocalizedError, Sendable, Equatable {
    public let status: Int
    public let location: String?

    public init(status: Int, location: String?) {
        self.status = status
        self.location = location
    }

    /// The host it pointed at, where the location names one.
    public var target: String? { location.flatMap { URL(string: $0)?.host() } }

    public var errorDescription: String? {
        "The server answered with a redirect\(target.map { " to \($0)" } ?? "") (\(status)), which is never followed."
    }
}

/// The server no longer accepts the token: revoked, expired, or the
/// repository was reset. Signing in again is the way out.
public struct SessionExpired: LocalizedError, Sendable, Equatable {
    public let message: String?
    public init(message: String? = nil) { self.message = message }
    public var errorDescription: String? { "Your sign-in is no longer valid. Sign in again." }
}

/// The token is good but its approval does not reach what was asked: the
/// server answered 403 with `insufficient_scope`. The owner approves more on
/// the approval page; signing in again would not change what they approved.
public struct InsufficientScope: LocalizedError, Sendable, Equatable {
    public let message: String?
    public init(message: String? = nil) { self.message = message }
    public var errorDescription: String? { "This needs your approval first." }
}

/// The password door refused a sign-in: the name, the password or the code.
public struct LoginRefused: LocalizedError, Sendable, Equatable {
    public let server: ServerError?
    public init(server: ServerError?) { self.server = server }
    public var errorDescription: String? { server?.text ?? "The repository name, password or code was not accepted." }
}

#if os(macOS)
private let thisDevice = "This Mac"
#else
private let thisDevice = "This device"
#endif

/// An error in a sentence a person can act on. The system's network errors
/// say what failed but not what it means; the server's refusals are its own
/// words and are kept. An approval that does not reach far enough is "needs
/// your approval", never "sign in again": signing in again approves nothing
/// more.
public func explain(_ error: Error, app: String, server: URL? = nil) -> String {
    let host = server.map(ServerAddress.display) ?? "the server"
    let Host = host.capitalizedFirst
    if let error = error as? URLError {
        switch error.code {
        case .timedOut: return "\(Host) took too long to answer."
        case .notConnectedToInternet, .dataNotAllowed, .internationalRoamingOff: return "\(thisDevice) is offline."
        case .networkConnectionLost: return "The connection to \(host) dropped."
        case .cannotFindHost, .dnsLookupFailed: return "Couldn't find \(host). Check the address."
        case .cannotConnectToHost: return "\(Host) isn't answering."
        case .secureConnectionFailed, .serverCertificateUntrusted, .serverCertificateHasBadDate,
             .serverCertificateNotYetValid, .serverCertificateHasUnknownRoot, .clientCertificateRejected:
            return "Couldn't make a secure connection to \(host)."
        case .appTransportSecurityRequiresSecureConnection: return "\(Host) needs https:// to be reached from here."
        case .cancelled: return "Stopped."
        case .badServerResponse, .cannotParseResponse, .cannotDecodeContentData, .cannotDecodeRawData:
            return "\(Host) answered with something \(app) can't read."
        default: return error.localizedDescription
        }
    }
    if error is SessionExpired {
        return "\(Host) no longer accepts \(app)'s sign-in. Sign in again to carry on."
    }
    if let error = error as? InsufficientScope {
        let detail = error.message.map { " (\($0))" } ?? ""
        return "\(app) needs your approval to do that on \(host)\(detail). Review its access to approve it."
    }
    if let error = error as? ServerError, error.status >= 500 {
        return "\(Host) ran into a problem (\(error.status)): \(error.text)"
    }
    if let error = error as? Redirected {
        return "\(Host) answered with a redirect\(error.target.map { " to \($0)" } ?? ""), which \(app) never follows: "
            + "what it sends carries your sign-in, and goes to \(host) alone. Check the address, or ask whoever runs \(host)."
    }
    if error is DecodingError { return "\(Host) answered with something \(app) can't read." }
    if error is CancellationError { return "Stopped." }
    return (error as? LocalizedError)?.errorDescription ?? error.localizedDescription
}

/// The network errors that mean the server could not be reached, as against
/// one it answered, refused or that was stopped.
public func isUnreachable(_ error: Error) -> Bool {
    let codes: Set<URLError.Code> = [.timedOut, .cannotConnectToHost, .notConnectedToInternet, .networkConnectionLost,
                                     .cannotFindHost, .dnsLookupFailed, .dataNotAllowed, .internationalRoamingOff]
    return (error as? URLError).map { codes.contains($0.code) } ?? false
}

/// Work given up on (the view went away, the person pressed Stop): nothing
/// to tell anyone.
public func isCancellation(_ error: Error) -> Bool {
    error is CancellationError || (error as? URLError)?.code == .cancelled
}
