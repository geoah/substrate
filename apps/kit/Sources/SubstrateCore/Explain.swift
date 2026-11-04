import Foundation

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
