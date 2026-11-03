import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateBridge
import SubstrateCore
import SubstrateScreens
import TasksCore

/// Substrate Tasks' roots, which kit-core's `Bridge.start` installs from
/// this library (`libSubstrateTasks.so`): `tasks`, the link over the app's
/// root, and `tasks.connect`, the sign-in while there is no session. Both
/// open with `TasksOpening`.
@_cdecl("substrate_bridge_roots")
public func substrateBridgeRoots(_ pointer: UnsafeMutableRawPointer) {
    TasksRoots.install(into: RootRegistry.from(pointer))
}

public enum TasksRoots {
    /// The secure store the session is kept in, one per app.
    static let secureStore = "session"
    /// The folder under `noBackupFilesDir` each repository's folder is made in.
    static let folder = "SubstrateTasks"

    public static func install(into registry: RootRegistry) {
        registry.register("tasks") { settings, context in
            TasksRoot(opening: try TasksOpening.read(settings), context: context)
        }
        registry.register("tasks.connect") { settings, context in
            let opening = try TasksOpening.read(settings)
            return (signIn(opening, context: context), context.appEnvironment(secureStore: secureStore))
        }
    }

    /// The connect screen against the build's manifest: the browser
    /// sign-in, where the owner approves what the app may do, and the
    /// session kept, replacing the one before, which is kept to be revoked.
    static func signIn(_ opening: TasksOpening, context: BridgeContext) -> SignInController {
        let manifest = opening.manifest(in: context.environment)
        let devices = context.devices(secureStore: secureStore)
        let transport = devices.transport
        let sessions = SessionStore(storage: BridgeSecureStore(channel: context.device, store: secureStore))
        let browser = BridgeBrowserSignIn(channel: context.device)
        let locator = ServerLocator(probe: ServerProbe(transport: transport), resolver: BridgeTXTResolver(channel: context.device))
        return SignInController(ConnectController(
            manifest: manifest, address: opening.address ?? "", locator: locator, opensBrowser: BridgeBrowserSignIn.opensBrowser
        ) { probe in
            guard let oauth = OAuthClient(manifest: manifest, probe: probe, transport: transport) else { throw URLError(.badServerResponse) }
            let session = try await oauth.signIn(server: probe.origin, repository: probe.repository, keepingRejectedIn: sessions,
                                                 browser: browser)
            // The store answers through the bridge and blocks while it does:
            // never on the main thread, which Kotlin's answers may need.
            let previous = await Task.detached { sessions.load() }.value
            try await sessions.save(session, replacing: previous, transport: transport)
        })
    }
}
