import Foundation
import SubstrateKit

/// The server's own refusal: the error envelope's code, message and
/// problems. Its words are shown to the owner as they are.
struct ServerError: LocalizedError, Sendable, Equatable {
    let status: Int
    let code: String
    let text: String

    var errorDescription: String? { text }

    /// The envelope, or nil when the body is not one: then the answer may be
    /// a gateway's, and says nothing about whether the request took effect.
    init?(status: Int, envelope data: Data) {
        guard let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let error = object["error"] as? [String: Any],
              let message = error["message"] as? String
        else { return nil }
        self.status = status
        code = (error["code"] as? String) ?? ""
        let problems = (error["problems"] as? [String]) ?? []
        text = ([message] + problems).joined(separator: "\n")
    }

    init(status: Int, code: String = "", text: String) {
        self.status = status
        self.code = code
        self.text = text
    }

    /// A refusal the same request would meet again: a 4xx other than a
    /// conflict or a rate limit, which can clear on their own.
    var isPermanent: Bool { (400 ..< 500).contains(status) && status != 409 && status != 429 }
}

/// The connection failed, before or after the server answered, or an
/// answer came that was not the server's: whether the request took effect is
/// unknown, unlike a `ServerError`.
struct TransportError: LocalizedError, Sendable {
    let text: String
    var errorDescription: String? { text }
}

/// The server no longer accepts the token: revoked, expired, or the
/// repository was reset.
struct SessionExpired: LocalizedError, Sendable {
    var errorDescription: String? { "Your sign-in is no longer valid. Sign in again." }
}

#if os(macOS)
private let thisDevice = "This Mac"
#else
private let thisDevice = "This device"
#endif

/// An error in a sentence a person can act on. The system's network errors
/// say what failed but not what it means; the server's refusals are its own
/// words and are kept.
func explain(_ error: Error, server: URL? = nil) -> String {
    let host = server?.host() ?? "the server"
    let Host = host.prefix(1).uppercased() + host.dropFirst()
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
            return "\(Host) answered with something this app can't read."
        default: return error.localizedDescription
        }
    }
    // An approval that does not reach is changed in Settings, never by
    // signing in again, which approves nothing more on its own.
    if let error = error as? ServerError, error.status == 403 {
        return "Substrate Chat isn't approved to do that on \(host): \(error.text) Allow it with Settings → Change Access…"
    }
    if let error = error as? ServerError, error.status >= 500 {
        return "\(Host) ran into a problem (\(error.status)): \(error.text)"
    }
    if error is DecodingError { return "\(Host) answered with something this app can't read." }
    if error is CancellationError { return "Stopped." }
    return (error as? LocalizedError)?.errorDescription ?? error.localizedDescription
}

/// The network errors that mean the server could not be reached, as against
/// one it answered, refused or that was stopped.
func isUnreachable(_ error: Error) -> Bool {
    let codes: Set<URLError.Code> = [.timedOut, .cannotConnectToHost, .notConnectedToInternet, .networkConnectionLost,
                                     .cannotFindHost, .dnsLookupFailed, .dataNotAllowed, .internationalRoamingOff]
    return (error as? URLError).map { codes.contains($0.code) } ?? false
}

/// A read the view gave up on (it scrolled away, or was redrawn): nothing to
/// tell the owner.
func isCancellation(_ error: Error) -> Bool {
    error is CancellationError || (error as? URLError)?.code == .cancelled
}
