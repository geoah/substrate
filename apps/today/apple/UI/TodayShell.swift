import Foundation
import SubstrateAuth
import SubstrateDevice
import SubstrateDeviceApple
import SubstrateScreens
import SubstrateUI
import TodayCore

/// The app's shell: the kit's `AppShell` over `TodayApp`.
public typealias TodayShell = AppShell<TodayApp>

/// What Substrate Today hands its shell: its manifest (`full`, alone, as
/// Substrate Chat asks), where it keeps its session and folders (the kit's
/// layout: one folder per repository in `Application Support/SubstrateToday`),
/// its root, how a routine reminder's answer reaches it, and its Settings.
/// The app target reaches the shell through the calls here alone: the kit's
/// types are linked into this framework, never into the app, which would
/// hold a second copy of them.
public enum Today {
    @MainActor static let shell = TodayShell(configuration)

    /// Makes the shell, which installs the kit's notification delegate:
    /// called from the app's initialiser, before the app finishes
    /// launching, so the tap that launched it is not lost.
    @MainActor public static func start() {
        #if DEBUG
        // A fixture scene draws with no shell at all, and a session handed
        // to the build is written before the shell reads the Keychain.
        if TodayDebug.demo != nil { return }
        TodayDebug.writeSession()
        #endif
        _ = shell
    }

    /// The app came to the front.
    @MainActor public static func cameForward() {
        #if DEBUG
        if TodayDebug.demo != nil { return }
        #endif
        shell.cameForward()
        shell.send(.looked)
    }

    /// The updater found a newer build, or none.
    @MainActor public static func updateOffered(_ version: String?) {
        #if DEBUG
        if TodayDebug.demo != nil { return }
        #endif
        shell.send(.offered(version))
    }

    static var configuration: TodayShell.Configuration {
        var service = "dev.reamde.substrate.today"
        var folder = "SubstrateToday"
        #if DEBUG
        if TodayDebug.session != nil {
            service = TodayDebug.keychainService
            folder = TodayDebug.folder
        }
        #endif
        return TodayShell.Configuration(
            manifest: TodayManifest.current, keychainService: service, folder: folder, packages: TodayManifest.packages,
            words: TodayApp.words(device:),
            app: { TodayApp(cache: $0.cache, outbox: $0.outbox, words: $0.words, build: $0.build, folder: $0.folder) },
            refresh: .foreground, respond: TodayReminders.command(for:), clearSaved: .clearSaved,
            settingsScreen: TodayPresenter.settings)
    }
}

#if DEBUG
/// Two hooks for a debug build, since nothing in the kit offers them. Both
/// compile out of a release build.
///
/// - `SUBSTRATE_TODAY_DEMO=<scene>` (or the launch argument
///   `-SUBSTRATE_TODAY_DEMO <scene>`) draws a named fixture scene with no
///   server and no session: its commands go nowhere, but lines open in
///   place and pages open from their fixtures.
/// - `SUBSTRATE_TODAY_SESSION="<server> <repository> <token>"` opens a real
///   server with a token already held, without the approval page. The
///   session is kept under a Keychain service and a folder of its own, so
///   it shares nothing with a signed-in build. A fourth word, `app`, keeps
///   it as an app session with `full`; without it the session reads as a
///   password one, which the status check reports as older access.
enum TodayDebug {
    static let keychainService = "dev.reamde.substrate.today.debug"
    static let folder = "SubstrateToday-Debug"

    static func value(_ name: String) -> String? {
        if let value = ProcessInfo.processInfo.environment[name], !value.isEmpty { return value }
        if let value = UserDefaults.standard.string(forKey: name), !value.isEmpty { return value }
        return nil
    }

    /// The fixture scene to draw, when one is named and exists.
    static let demo: TodayPresentation? = {
        guard let name = value("SUBSTRATE_TODAY_DEMO") else { return nil }
        let all = TodayFixtures.all
        return (all.first { $0.name == name } ?? all.first { $0.name == "today-" + name } ?? all.first)?.app
    }()

    /// The session handed to the build, when one is.
    static let session: Session? = {
        let words = (value("SUBSTRATE_TODAY_SESSION") ?? "").split(separator: " ").map(String.init)
        guard words.count >= 3, let server = URL(string: words[0]) else { return nil }
        if words.count > 3, words[3] == "app" {
            return Session(
                server: server, repository: words[1], secret: words[2], clientID: TodayManifest.localClientID,
                scopes: TodayManifest.scopes, requestedScopes: TodayManifest.scopes)
        }
        return Session(server: server, repository: words[1], secret: words[2])
    }()

    /// Writes the session where the debug shell reads it, replacing the
    /// one a run before left.
    static func writeSession() {
        guard let session, let data = try? JSONEncoder().encode(session) else { return }
        try? KeychainStorage(service: keychainService).write(data, account: "session")
    }
}
#endif
