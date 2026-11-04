import Foundation
import SubstrateCore
import SubstrateDevice

/// The owner's browser on Android, through the bridge: their default browser
/// on the sign-in page, and the callback taken by Kotlin. A local build's
/// loopback route listens on `127.0.0.1` alone; what comes back is checked
/// here (`OAuthClient`'s `state`, `iss` and PKCE), never in Kotlin. A
/// release's claimed route is refused by Kotlin until the app takes its
/// verified link, never answered through loopback instead (§5.4).
///
/// The sign-in leaves the app for the browser, so the screen waiting on it
/// offers its own Cancel (`opensBrowser`): cancelling the call stops Kotlin
/// listening.
public struct BridgeBrowserSignIn: BrowserSignIn {
    public static let opensBrowser = true

    public let channel: DeviceChannel

    public init(channel: DeviceChannel) {
        self.channel = channel
    }

    @MainActor
    public func prepare(_ route: SignInRoute) async throws -> SignInRedirect {
        let bridged: BrowserRoute = switch route {
        case let .claimed(url): .claimed(url: url.absoluteString)
        case let .loopback(appName, path): .loopback(appName: appName, path: path)
        }
        let channel = channel
        switch try await Self.cancellable({ try await channel.call(.browserPrepare(route: bridged), as: BrowserPrepared.self) }) {
        case let .ready(redirect): return SignInRedirect(route: route, uri: redirect)
        case let .failed(reason): throw TransportError(text: reason)
        }
    }

    @MainActor
    public func open(_ page: URL, for redirect: SignInRedirect) async throws -> SignInAnswer {
        let channel = channel
        switch try await Self.cancellable({
            try await channel.call(.browserOpen(page: page.absoluteString, redirect: redirect.uri), as: BrowserAnswer.self)
        }) {
        case let .callback(query): return .callback(query)
        case .cancelled: return .cancelled
        case .expired: return .expired
        case let .failed(reason): throw TransportError(text: reason)
        }
    }

    @MainActor
    public func finish(_ redirect: SignInRedirect) {
        let channel = channel
        Task { _ = try? await channel.call(.browserFinish(redirect: redirect.uri)) }
    }

    /// A call cancelled by its caller or by its handle's close is the
    /// sign-in's cancellation, which the connect screen says nothing about.
    private static func cancellable<Answer: Sendable>(_ call: @Sendable () async throws -> Answer) async throws -> Answer {
        do {
            return try await call()
        } catch let failure as BridgeFailure where failure.code == .cancelled {
            throw CancellationError()
        }
    }
}
