import Foundation

/// How the authorization redirect comes back to the app.
public enum SignInRoute: Hashable, Sendable {
    /// A release: the system hands the callback on this https URL to this
    /// app alone, because the host's association file names it.
    case claimed(URL)
    /// Any other build: a loopback redirect (RFC 8252) at `path` on a port
    /// picked when it listens. `appName` is what the page the browser lands
    /// on tells the person to go back to.
    case loopback(appName: String, path: String)
}

/// A route made ready: the redirect URI the authorization request names.
public struct SignInRedirect: Hashable, Sendable {
    public let route: SignInRoute
    public let uri: String

    public init(route: SignInRoute, uri: String) {
        self.route = route
        self.uri = uri
    }
}

/// What a browser sign-in came back with.
public enum SignInAnswer: Hashable, Sendable {
    /// The callback's query, unchecked: the caller holds it to its request.
    case callback([String: String])
    /// The person closed the sheet or the browser's page.
    case cancelled
    /// No callback came before the attempt's deadline, past which the
    /// server would no longer exchange its code.
    case expired
}

/// The person cancelled a sign-in.
public struct SignInCancelled: LocalizedError, Sendable {
    public init() {}
    public var errorDescription: String? { "Sign-in was cancelled." }
}

/// A sign-in no callback came back to in time.
public struct SignInExpired: LocalizedError, Sendable {
    public init() {}
    public var errorDescription: String? { "The sign-in waited too long for your browser. Start it again." }
}

/// The owner's browser on the server's sign-in and approval page, in two
/// steps, since a loopback redirect names a port known only once it
/// listens: `prepare` the route, build the page's URL on the redirect it
/// answers, then `open` it. `finish` lets go of what `prepare` took, whatever
/// `open` came to.
public protocol BrowserSignIn: Sendable {
    @MainActor func prepare(_ route: SignInRoute) async throws -> SignInRedirect
    @MainActor func open(_ page: URL, for redirect: SignInRedirect) async throws -> SignInAnswer
    @MainActor func finish(_ redirect: SignInRedirect)
}
