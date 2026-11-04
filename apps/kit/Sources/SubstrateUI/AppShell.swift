#if canImport(SwiftUI)
import Foundation
import Observation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import SubstrateDeviceApple
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import SwiftUI

/// An app's one link and what it runs over on an Apple device (§5.2): the
/// session in the Keychain, the signed-in repository's folder (its outbox,
/// its record cache, its settings), the notifications service with the kit's
/// delegate, and the runtime that runs `LinkController` over the app's root
/// as one presentation tree. A sign-in to another repository makes all of it
/// again over that repository's folder; everything else is a command. The
/// app hands it a `Configuration`: what is the app's own.
///
/// Made once, at launch, before the app finishes launching, so a
/// notification's tap that launched it reaches the app's root.
@MainActor
@Observable
public final class AppShell<App: LinkedApp> {
    public typealias Link = LinkController<App>

    /// What the app's root is made over: one repository's folder and what
    /// the kit keeps in it, all nil while no repository is signed in, and
    /// the device's facts, which the shell fills.
    public struct Parts {
        public var folder: RepositoryFolder?
        public var cache: RecordCache?
        public var outbox: Outbox?
        /// The outbox of the repository a session is signed in to, for an
        /// app that keeps one per repository (Workouts' account folders):
        /// the link sends from it, and `outbox` is not read.
        public var outboxes: (@Sendable (SessionFacts) -> Outbox?)?
        /// "this iPhone", "this iPad", "this Mac".
        public internal(set) var device: String
        public internal(set) var build: BuildFacts
        /// The Status screen's words, made by `Configuration.words` in the
        /// device's: the link's, and the app's own linked records'.
        public internal(set) var words: StatusWords

        public init(
            folder: RepositoryFolder? = nil, cache: RecordCache? = nil, outbox: Outbox? = nil,
            outboxes: (@Sendable (SessionFacts) -> Outbox?)? = nil
        ) {
            self.folder = folder
            self.cache = cache
            self.outbox = outbox
            self.outboxes = outboxes
            device = ""
            build = BuildFacts(version: "")
            words = StatusWords(app: "", device: "", kept: "")
        }

        /// Each repository's folder in `Application Support/<folder>`, named
        /// for its server and repository (`RepositoryFolder.name`), with the
        /// kit's outbox and record cache in it: what an app made after the
        /// kit lays out. An app that kept its files elsewhere before the kit
        /// hands the shell its own `parts`, since a path on the device never
        /// moves.
        public static func repositoryFolders(in folder: String) -> (Session?) -> Parts {
            { session in
                guard let session else { return Parts() }
                let name = RepositoryFolder.name(server: session.server, repository: session.repository)
                let made = try? RepositoryFolder(
                    root: applicationSupport(folder).appendingPathComponent(name, isDirectory: true),
                    writingOptions: protection)
                return Parts(folder: made, cache: made.map { RecordCache(folder: $0) }, outbox: made.map { Outbox(folder: $0) })
            }
        }

        /// A folder in Application Support, which the system does not clear.
        public static func applicationSupport(_ folder: String) -> URL {
            let base =
                FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
                ?? FileManager.default.temporaryDirectory
            return base.appendingPathComponent(folder, isDirectory: true)
        }

        /// How a repository's files are written on this system.
        public static var protection: Data.WritingOptions {
            #if os(iOS)
            // Readable after the first unlock, so a notification's tap that
            // opens the app on a locked phone still reads its records.
            .completeFileProtectionUntilFirstUserAuthentication
            #else
            []
            #endif
        }
    }

    /// Everything an app's shell needs that is the app's own.
    public struct Configuration {
        public var manifest: AppManifest
        /// The Keychain service the session is kept under.
        public var keychainService: String
        /// What the app's root is made over for a session, nil while none
        /// is signed in: where its folder is, and the outbox and record cache
        /// in it. A sign-in to another repository asks again.
        public var parts: (Session?) -> Parts
        /// The app's packages, each with the version this build needs.
        public var packages: [String: Int]
        /// The Status screen's words, in the device's.
        public var words: (_ device: String) -> StatusWords
        /// The app's root over a repository's folder.
        public var app: (Parts) -> App
        /// The app's command when what the system may have changed is to be
        /// read again: it came to the front, or the time zone moved.
        public var refresh: App.Command?
        /// The app's command for a notification the owner answered, nil for
        /// one that is not the app's; nil keeps no notifications at all.
        public var respond: ((NotificationResponse) -> App.Command?)?
        /// The app's command for one of its own settings rows.
        public var settings: (SettingsCommand) -> App.Command?
        /// The app's command for Storage's Clear Saved Copies.
        public var clearSaved: App.Command?
        /// The Settings screen from the link's presentation and the app's.
        public var settingsScreen: (LinkPresentation<App.Presentation>, App.Presentation) -> SettingsPresentation

        public init(
            manifest: AppManifest, keychainService: String, parts: @escaping (Session?) -> Parts, packages: [String: Int],
            words: @escaping (_ device: String) -> StatusWords, app: @escaping (Parts) -> App, refresh: App.Command? = nil,
            respond: ((NotificationResponse) -> App.Command?)? = nil, settings: @escaping (SettingsCommand) -> App.Command? = { _ in nil },
            clearSaved: App.Command? = nil,
            settingsScreen: @escaping (LinkPresentation<App.Presentation>, App.Presentation) -> SettingsPresentation
        ) {
            self.manifest = manifest
            self.keychainService = keychainService
            self.parts = parts
            self.packages = packages
            self.words = words
            self.app = app
            self.refresh = refresh
            self.respond = respond
            self.settings = settings
            self.clearSaved = clearSaved
            self.settingsScreen = settingsScreen
        }

        /// Each repository's folder in `Application Support/<folder>`
        /// (`Parts.repositoryFolders(in:)`).
        public init(
            manifest: AppManifest, keychainService: String, folder: String, packages: [String: Int],
            words: @escaping (_ device: String) -> StatusWords, app: @escaping (Parts) -> App, refresh: App.Command? = nil,
            respond: ((NotificationResponse) -> App.Command?)? = nil, settings: @escaping (SettingsCommand) -> App.Command? = { _ in nil },
            clearSaved: App.Command? = nil,
            settingsScreen: @escaping (LinkPresentation<App.Presentation>, App.Presentation) -> SettingsPresentation
        ) {
            self.init(
                manifest: manifest, keychainService: keychainService, parts: Parts.repositoryFolders(in: folder), packages: packages,
                words: words, app: app, refresh: refresh, respond: respond, settings: settings, clearSaved: clearSaved,
                settingsScreen: settingsScreen)
        }
    }

    @ObservationIgnored public let configuration: Configuration
    @ObservationIgnored public let sessions: SessionStore
    public var manifest: AppManifest { configuration.manifest }
    /// None for a shell that only draws.
    @ObservationIgnored private let notifications: SystemNotifications?
    public private(set) var model: PresentationModel<Link>
    /// A session is in the Keychain, or something the Keychain could not
    /// read is: the link's first presentation, before it has read it, is
    /// not a sign-in screen, which would save a new session over it.
    public private(set) var stored: Bool
    @ObservationIgnored private var folderName: String?
    @ObservationIgnored private var following: [Task<Void, Never>] = []

    public init(_ configuration: Configuration) {
        self.configuration = configuration
        sessions = SessionStore(storage: KeychainStorage(service: configuration.keychainService))
        notifications = configuration.respond == nil ? nil : SystemNotifications()
        if notifications != nil { SystemNotifications.installDelegate() }
        let read = sessions.read()
        stored = read != .missing
        let session: Session? = if case let .found(session) = read { session } else { nil }
        (model, folderName) = Self.make(configuration, session: session, sessions: sessions, notifications: notifications)
        follow()
    }

    /// A shell with no session, no folder and no device services, for
    /// drawing the app's views from its fixtures: its commands go nowhere.
    public init(drawing configuration: Configuration) {
        self.configuration = configuration
        sessions = SessionStore(storage: KeychainStorage(service: configuration.keychainService + ".drawing"))
        notifications = nil
        stored = true
        (model, folderName) = Self.make(configuration, session: nil, sessions: sessions, notifications: nil)
    }

    private static func make(
        _ configuration: Configuration, session: Session?, sessions: SessionStore,
        notifications: SystemNotifications?
    ) -> (PresentationModel<Link>, String?) {
        var environment = AppEnvironment.apple()
        environment.client = ClientSource { await sessions.load().map { SubstrateClient($0) } }
        environment.devices.notifications = notifications
        let parts = parts(configuration, session: session, device: DeviceWords.current, build: .bundle())
        environment.folder = parts.folder
        let name = session.map { RepositoryFolder.name(server: $0.server, repository: $0.repository) }
        return (PresentationModel(link(configuration, parts: parts, sessions: sessions), environment: environment), name)
    }

    /// The app's parts for a session, with the device's facts filled.
    static func parts(_ configuration: Configuration, session: Session?, device: String, build: BuildFacts) -> Parts {
        var parts = configuration.parts(session)
        parts.device = device
        parts.build = build
        parts.words = configuration.words(device)
        return parts
    }

    /// The link over the app's root, sending from the parts' outbox, or
    /// from the signed-in repository's where the app keeps one for each.
    static func link(_ configuration: Configuration, parts: Parts, sessions: SessionStore) -> Link {
        let app = configuration.app(parts)
        if let outboxes = parts.outboxes {
            return Link(
                app: app, manifest: configuration.manifest, sessions: sessions, outboxes: outboxes, packages: configuration.packages,
                words: parts.words)
        }
        return Link(
            app: app, manifest: configuration.manifest, sessions: sessions, outbox: parts.outbox, packages: configuration.packages,
            words: parts.words)
    }

    /// The owner's answers to notifications, the one that launched the app
    /// included, and the time zone's moves, as the app's commands.
    private func follow() {
        if let notifications, let respond = configuration.respond {
            following.append(
                Task { [weak self] in
                    for await response in notifications.responses() {
                        guard let command = respond(response) else { continue }
                        self?.send(command)
                    }
                })
        }
        if let refresh = configuration.refresh {
            following.append(
                Task { [weak self] in
                    for await _ in NotificationCenter.default.notifications(named: .NSSystemTimeZoneDidChange) {
                        self?.send(refresh)
                    }
                })
        }
    }

    // MARK: Commands

    public var presentation: Link.Presentation { model.presentation }

    public func send(_ command: App.Command) {
        model.send(command, to: Link.appPath)
    }

    /// A command for the controller the app's root keeps under `key`.
    public func send<Command: Codable & Sendable>(_ command: Command, to key: ChildKey) {
        model.send(command, to: Link.appPath + [key])
    }

    public func send(_ command: Link.Command) {
        model.send(command)
    }

    /// The app came to the front: what the system may have changed is read
    /// again, and the access is checked where it is due.
    public func cameForward() {
        if let refresh = configuration.refresh { send(refresh) }
        send(Link.Command.refresh(force: false))
    }

    // MARK: Settings

    /// The Settings screen as it stands, nil before the app's first
    /// presentation.
    public var settingsScreen: SettingsPresentation? {
        presentation.app.map { configuration.settingsScreen(presentation, $0) }
    }

    public func route(_ command: SettingsCommand) -> SettingsRoute<App> {
        SettingsRoute.route(command, settings: configuration.settings, clearSaved: configuration.clearSaved)
    }

    /// A settings row's command, to whatever answers it; the updater's check
    /// is the scene's, which holds the updater, and a page is the browser's.
    public func perform(_ command: SettingsCommand, checkForUpdates: () -> Void, openURL: (URL) -> Void) {
        switch route(command) {
        case let .app(command): send(command)
        case let .link(command): send(command)
        case .changeAccess: Task { try? await changeAccess() }
        case .checkForUpdates: checkForUpdates()
        case .privacyPolicy:
            if let url = BuildFacts.bundle().privacyPolicy.flatMap(URL.init(string:)) { openURL(url) }
        case .signIn, .none: break
        }
    }

    // MARK: Signing in

    /// The browser sign-in on the server the connect screen found; the
    /// session replaces the one before, which is kept to be revoked.
    public func signIn(_ probe: ServerProbe.Result) async throws {
        guard let oauth = OAuthClient(manifest: manifest, probe: probe) else { throw URLError(.badServerResponse) }
        let session = try await oauth.signIn(
            server: probe.origin, repository: probe.repository, keepingRejectedIn: sessions,
            browser: WebAuthenticator())
        try await sessions.save(session, replacing: sessions.load())
        adopt(session)
    }

    /// The whole request again on the same server: a change of access, a
    /// renewal, or signing in after the access ended.
    public func changeAccess() async throws {
        guard let session = await sessions.load() else { return }
        let probe = try await ServerProbe().probe(session.server, requirement: manifest.requirement, app: manifest.appName)
        try await signIn(probe)
    }

    /// The link reads the session again over the same folder, or the app is
    /// made again over the new repository's.
    private func adopt(_ session: Session) {
        stored = true
        let name = RepositoryFolder.name(server: session.server, repository: session.repository)
        guard name != folderName else {
            send(Link.Command.signedIn)
            return
        }
        (model, folderName) = Self.make(configuration, session: session, sessions: sessions, notifications: notifications)
    }

    /// The link's phase moved: signed out, the sign-in screen is the one to
    /// show; a session read at last that the launch could not read has the
    /// app made again over its repository's folder.
    public func refreshStored() {
        let read = sessions.read()
        stored = read != .missing
        if case let .found(session) = read, RepositoryFolder.name(server: session.server, repository: session.repository) != folderName {
            adopt(session)
        }
    }

    isolated deinit {
        for task in following { task.cancel() }
    }
}
#endif
