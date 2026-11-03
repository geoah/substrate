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
    /// the kit keeps in it, all nil while no repository is signed in.
    public struct Parts {
        public var folder: RepositoryFolder?
        public var cache: RecordCache?
        public var outbox: Outbox?
        /// "this iPhone", "this iPad", "this Mac".
        public var device: String
        public var build: BuildFacts
    }

    /// Everything an app's shell needs that is the app's own.
    public struct Configuration {
        public var manifest: AppManifest
        /// The Keychain service the session is kept under.
        public var keychainService: String
        /// The folder in Application Support each repository's folder is
        /// made in.
        public var folder: String
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

        public init(manifest: AppManifest, keychainService: String, folder: String, packages: [String: Int],
                    words: @escaping (_ device: String) -> StatusWords, app: @escaping (Parts) -> App, refresh: App.Command? = nil,
                    respond: ((NotificationResponse) -> App.Command?)? = nil, settings: @escaping (SettingsCommand) -> App.Command? = { _ in nil },
                    clearSaved: App.Command? = nil,
                    settingsScreen: @escaping (LinkPresentation<App.Presentation>, App.Presentation) -> SettingsPresentation)
        {
            self.manifest = manifest
            self.keychainService = keychainService
            self.folder = folder
            self.packages = packages
            self.words = words
            self.app = app
            self.refresh = refresh
            self.respond = respond
            self.settings = settings
            self.clearSaved = clearSaved
            self.settingsScreen = settingsScreen
        }
    }

    @ObservationIgnored public let configuration: Configuration
    @ObservationIgnored public let sessions: SessionStore
    public var manifest: AppManifest { configuration.manifest }
    /// None for a shell that only draws.
    @ObservationIgnored private let notifications: SystemNotifications?
    public private(set) var model: PresentationModel<Link>
    /// A session is in the Keychain: the link's first presentation, before
    /// it has read it, is not a sign-in screen.
    public private(set) var stored: Bool
    @ObservationIgnored private var folderName: String?
    @ObservationIgnored private var following: [Task<Void, Never>] = []

    public init(_ configuration: Configuration) {
        self.configuration = configuration
        sessions = SessionStore(storage: KeychainStorage(service: configuration.keychainService))
        notifications = configuration.respond == nil ? nil : SystemNotifications()
        if notifications != nil { SystemNotifications.installDelegate() }
        let session = sessions.load()
        stored = session != nil
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

    private static func make(_ configuration: Configuration, session: Session?, sessions: SessionStore,
                             notifications: SystemNotifications?) -> (PresentationModel<Link>, String?)
    {
        var environment = AppEnvironment.apple()
        environment.client = ClientSource { sessions.load().map { SubstrateClient($0) } }
        environment.devices.notifications = notifications
        var folder: RepositoryFolder?
        var name: String?
        if let session {
            name = RepositoryFolder.name(server: session.server, repository: session.repository)
            folder = try? RepositoryFolder(root: root(configuration.folder).appendingPathComponent(name ?? "", isDirectory: true),
                                           writingOptions: protection)
        }
        environment.folder = folder
        let outbox = folder.map { Outbox(folder: $0) }
        let parts = Parts(folder: folder, cache: folder.map { RecordCache(folder: $0) }, outbox: outbox, device: DeviceWords.current,
                          build: .bundle())
        let link = Link(app: configuration.app(parts), manifest: configuration.manifest, sessions: sessions, outbox: outbox,
                        packages: configuration.packages, words: configuration.words(DeviceWords.current))
        return (PresentationModel(link, environment: environment), name)
    }

    /// Where each repository's folder is: Application Support, which the
    /// system does not clear.
    private static func root(_ folder: String) -> URL {
        let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? FileManager.default.temporaryDirectory
        return base.appendingPathComponent(folder, isDirectory: true)
    }

    private static var protection: Data.WritingOptions {
        #if os(iOS)
        // Readable after the first unlock, so a notification's tap that
        // opens the app on a locked phone still reads its records.
        .completeFileProtectionUntilFirstUserAuthentication
        #else
        []
        #endif
    }

    /// The owner's answers to notifications, the one that launched the app
    /// included, and the time zone's moves, as the app's commands.
    private func follow() {
        if let notifications, let respond = configuration.respond {
            following.append(Task { [weak self] in
                for await response in notifications.responses() {
                    guard let command = respond(response) else { continue }
                    self?.send(command)
                }
            })
        }
        if let refresh = configuration.refresh {
            following.append(Task { [weak self] in
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
    /// is the scene's, which holds the updater.
    public func perform(_ command: SettingsCommand, checkForUpdates: () -> Void) {
        switch route(command) {
        case let .app(command): send(command)
        case let .link(command): send(command)
        case .changeAccess: Task { try? await changeAccess() }
        case .checkForUpdates: checkForUpdates()
        case .signIn, .none: break
        }
    }

    // MARK: Signing in

    /// The browser sign-in on the server the connect screen found; the
    /// session replaces the one before, which is kept to be revoked.
    public func signIn(_ probe: ServerProbe.Result) async throws {
        guard let oauth = OAuthClient(manifest: manifest, probe: probe) else { throw URLError(.badServerResponse) }
        let session = try await oauth.signIn(server: probe.origin, repository: probe.repository, keepingRejectedIn: sessions,
                                             browser: WebAuthenticator())
        try await sessions.save(session, replacing: sessions.load())
        adopt(session)
    }

    /// The whole request again on the same server: a change of access, a
    /// renewal, or signing in after the access ended.
    public func changeAccess() async throws {
        guard let session = sessions.load() else { return }
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

    /// The link signed out: the sign-in screen is the one to show.
    public func refreshStored() {
        stored = sessions.load() != nil
    }

    isolated deinit {
        for task in following { task.cancel() }
    }
}
#endif
