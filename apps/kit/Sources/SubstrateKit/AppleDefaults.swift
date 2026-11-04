import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import SubstrateDeviceApple

// The Apple device services as the defaults the single-module kit had, for
// the apps that import SubstrateKit: the portable libraries name no device.

#if canImport(dnssd)
public extension ServerLocator {
    /// A locator that reads `_substrate` records through DNS-SD.
    init(probe: ServerProbe = ServerProbe()) {
        self.init(probe: probe, resolver: DNSServiceTXTResolver())
    }
}
#endif

#if canImport(AuthenticationServices)
public extension OAuthClient {
    /// The whole flow through the system's browser sign-in
    /// (`WebAuthenticator`).
    @MainActor
    func signIn(
        server: URL, repository: String? = nil, keepingRejectedIn rejected: SessionStore? = nil,
        now: @escaping @Sendable () -> Date = { Date() }
    ) async throws -> Session {
        try await signIn(server: server, repository: repository, keepingRejectedIn: rejected, browser: WebAuthenticator(), now: now)
    }
}
#endif

#if canImport(Security)
public extension SessionStore {
    /// The session in the Keychain, under `service` and `account`:
    /// readable after the first unlock so background work (a sync, a
    /// refresh) can use it, never synced off the device by default, and
    /// shared with the app's other targets (a share sheet, a widget) through
    /// `accessGroup` where one is named.
    init(
        service: String, accessGroup: String? = nil, account: String = "session",
        accessibility: Accessibility = .afterFirstUnlockThisDeviceOnly, dataProtection: Bool = false
    ) {
        self.init(
            account: account,
            storage: KeychainStorage(
                service: service, accessGroup: accessGroup,
                accessibility: accessibility, dataProtection: dataProtection))
    }
}
#endif
