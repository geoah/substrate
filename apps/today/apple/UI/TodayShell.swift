import Foundation
import SubstrateAuth
import SubstrateDevice
import SubstrateDeviceApple
import SubstrateScreens
import SubstrateUI
import TodayCore
import TodayGlance

#if os(iOS)
import WidgetKit
#endif

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
        TodayDebug.setZone()
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

    /// The links the app was opened with, which the day's screen follows.
    @MainActor static let links = OpenedLinks()

    /// The app was opened with a link (`TodayLinks`): a tap on a widget, or
    /// on one of its lines. The day's screen opens the page it names; the
    /// day's own link, and one this build does not know, open the day.
    @MainActor public static func open(_ url: URL) {
        links.open(TodayLinks.route(url))
    }

    /// Where the root hands the widgets' day: the Keychain item the widget
    /// extension reads, on an iPhone and an iPad. A Mac has no widget of
    /// the app, and writes none.
    static var glance: (@Sendable (GlanceTimeline) async -> Void)? {
        #if os(iOS)
        return { await TodayGlanceWriter.shared.write($0) }
        #else
        return nil
        #endif
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
            app: { TodayApp(cache: $0.cache, outbox: $0.outbox, words: $0.words, build: $0.build, folder: $0.folder, glance: glance) },
            refresh: .foreground, respond: TodayReminders.command(for:), settings: { .settings($0) }, clearSaved: .clearSaved,
            settingsScreen: TodayPresenter.settings)
    }
}

/// The link the app was opened with, until the day's screen follows it:
/// the screen watches `waiting` and takes the link once. Navigation stays
/// the screen's own; this only hands it where to go.
@MainActor @Observable final class OpenedLinks {
    struct Link: Equatable {
        /// One more for every link, so the same link opened twice is two.
        var number: Int
        /// The page it names; nil is the day.
        var route: TodayRoute?
    }

    private(set) var waiting: Link?
    private var count = 0

    func open(_ route: TodayRoute?) {
        count += 1
        waiting = Link(number: count, route: route)
    }

    /// The link that waits, which then waits no more.
    func take() -> Link? {
        defer { waiting = nil }
        return waiting
    }
}

#if os(iOS)
/// What writes the widgets' day into the Keychain item the widget
/// extension reads (`GlanceKeychain`). One write at a time, in the order
/// they were asked for, so the day that signing out leaves is never written
/// over by one made before it.
actor TodayGlanceWriter {
    static let shared = TodayGlanceWriter()

    /// Writes the timeline and has the widgets read it again, unless the
    /// item already says the same from now on: a change the widgets would
    /// not show costs them no reload. A write whose work was cancelled
    /// while it waited its turn is not made.
    func write(_ timeline: GlanceTimeline) {
        guard !Task.isCancelled else { return }
        if let kept = GlanceKeychain.timeline(), kept.says(sameAs: timeline, from: timeline.writtenAt) { return }
        guard let data = try? GlanceKeychain.pack(timeline), GlanceKeychain.write(data) else { return }
        WidgetCenter.shared.reloadAllTimelines()
    }
}
#endif

#if DEBUG
/// Three hooks for a debug build, since nothing in the kit offers them. All
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
/// - `SUBSTRATE_TODAY_ZONE=<IANA zone>` reads the day in that time zone, so
///   a day seeded with `seed.py --timezone` can be tried at another hour
///   than this machine's: the morning, late in the evening.
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

    /// The process's time zone, set before the shell is made, so the first
    /// day read is already in it. The kit's environment follows the
    /// system's zone, not the process's default one, and the system's is
    /// read from `TZ` where that is set.
    static func setZone() {
        guard let zone = value("SUBSTRATE_TODAY_ZONE").flatMap(TimeZone.init(identifier:)) else { return }
        setenv("TZ", zone.identifier, 1)
        NSTimeZone.resetSystemTimeZone()
        NSTimeZone.default = zone
    }

    /// Writes the session where the debug shell reads it, replacing the
    /// one a run before left.
    static func writeSession() {
        guard let session, let data = try? JSONEncoder().encode(session) else { return }
        try? KeychainStorage(service: keychainService).write(data, account: "session")
    }
}
#endif
