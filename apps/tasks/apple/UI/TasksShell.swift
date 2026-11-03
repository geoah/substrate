import Foundation
import Observation
import SubstrateAPI
import SubstrateAuth
import SubstrateDevice
import SubstrateDeviceApple
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import TasksCore
#if canImport(UIKit)
import UIKit
#endif

/// The app's one link and what it runs over: the session in the Keychain,
/// the repository's folder (its outbox, its record cache, its settings), the
/// notifications service, and the runtime that runs `LinkController` over
/// `TasksApp` as one presentation tree. A sign-in to another repository
/// makes all of it again over that repository's folder; everything else
/// is a command.
@MainActor
@Observable
public final class TasksShell {
    public static let shared = TasksShell()

    let sessions = SessionStore(storage: KeychainStorage(service: "dev.reamde.substrate.tasks"))
    let manifest = TasksManifest.current
    /// Made at launch, before the app finishes launching, so a tap that
    /// launched it reaches the app. None for a shell that only draws.
    let notifications: SystemNotifications?
    private(set) var model: PresentationModel<LinkController<TasksApp>>
    /// A session is in the Keychain: the link's first presentation, before
    /// it has read it, is not a sign-in screen.
    private(set) var stored: Bool
    private var folderName: String?

    private init() {
        notifications = SystemNotifications()
        let session = sessions.load()
        stored = session != nil
        (model, folderName) = Self.make(session: session, sessions: sessions, manifest: manifest, notifications: notifications)
    }

    /// "this iPhone", "this iPad", "this Mac": the words the app says the
    /// device in.
    static var device: String {
        #if os(macOS)
        "this Mac"
        #else
        UIDevice.current.userInterfaceIdiom == .pad ? "this iPad" : "this iPhone"
        #endif
    }

    /// A shell with no session, no folder and no device services, for
    /// drawing the views from fixtures: its commands go nowhere.
    init(drawing: Void) {
        notifications = nil
        stored = true
        (model, folderName) = Self.make(session: nil, sessions: SessionStore(storage: KeychainStorage(service: "dev.reamde.substrate.tasks.drawing")),
                                        manifest: manifest, notifications: nil)
    }

    private static func make(session: Session?, sessions: SessionStore, manifest: AppManifest, notifications: SystemNotifications?)
        -> (PresentationModel<LinkController<TasksApp>>, String?)
    {
        var environment = AppEnvironment.apple()
        environment.client = ClientSource { sessions.load().map { SubstrateClient($0) } }
        environment.notifications = notifications
        var folder: RepositoryFolder?
        var name: String?
        if let session {
            name = RepositoryFolder.name(server: session.server, repository: session.repository)
            folder = try? RepositoryFolder(root: root.appendingPathComponent(name ?? "", isDirectory: true), writingOptions: protection)
        }
        environment.folder = folder
        let outbox = folder.map { Outbox(folder: $0) }
        let app = TasksApp(cache: folder.map { RecordCache(folder: $0) }, outbox: outbox, device: device, build: build)
        let link = LinkController(app: app, manifest: manifest, sessions: sessions, outbox: outbox, packages: TasksManifest.packages,
                                  words: StatusWords(app: TasksApp.appName, device: device, kept: "tasks",
                                                     packageNames: [TasksKinds.packageID: "Tasks"]))
        return (PresentationModel(link, environment: environment), name)
    }

    /// This build as `apple.sh release` stamped it, as the shared updater
    /// reads it: a local build is its short version and build number, on
    /// no channel.
    private static var build: BuildFacts {
        let info = Bundle.main.infoDictionary ?? [:]
        func value(_ key: String) -> String { (info[key] as? String)?.trimmingCharacters(in: .whitespaces) ?? "" }
        let stamped = value("SubstrateVersion")
        let channel = value("SubstrateChannel")
        return BuildFacts(version: stamped.isEmpty ? "\(value("CFBundleShortVersionString")) (\(value("CFBundleVersion")))" : stamped,
                          channel: channel.isEmpty ? nil : channel)
    }

    /// Where each repository's folder is: Application Support, which the
    /// system does not clear.
    private static var root: URL {
        let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? FileManager.default.temporaryDirectory
        return base.appendingPathComponent("SubstrateTasks", isDirectory: true)
    }

    private static var protection: Data.WritingOptions {
        #if os(iOS)
        // Readable after the first unlock, so a reminder's tap that opens
        // the app on a locked phone still reads its tasks.
        .completeFileProtectionUntilFirstUserAuthentication
        #else
        []
        #endif
    }

    // MARK: Commands

    public func send(_ command: TasksApp.Command) {
        model.send(command, to: LinkController<TasksApp>.appPath)
    }

    func send(_ command: TaskEditor.Command, to editor: ChildKey) {
        model.send(command, to: LinkController<TasksApp>.appPath + [editor])
    }

    public func send(_ command: LinkController<TasksApp>.Command) {
        model.send(command)
    }

    /// A settings row's command, to the controller that answers it.
    func settings(_ command: SettingsCommand) {
        switch TasksSettings.route(command) {
        case let .app(command): send(command)
        case let .link(command): send(command)
        case .changeAccess: Task { try? await changeAccess() }
        case .checkForUpdates, .signIn, .none: break
        }
    }

    // MARK: Signing in

    /// The browser sign-in on the server the connect screen found; the
    /// session replaces the one before, which is kept to be revoked.
    func signIn(_ probe: ServerProbe.Result) async throws {
        guard let oauth = OAuthClient(manifest: manifest, probe: probe) else { throw URLError(.badServerResponse) }
        let session = try await oauth.signIn(server: probe.origin, repository: probe.repository, keepingRejectedIn: sessions, browser: WebAuthenticator())
        try await sessions.save(session, replacing: sessions.load())
        adopt(session)
    }

    /// The whole request again on the same server: a change of access, a
    /// renewal, or signing in after the access ended.
    func changeAccess() async throws {
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
            send(.signedIn)
            return
        }
        (model, folderName) = Self.make(session: session, sessions: sessions, manifest: manifest, notifications: notifications)
    }

    /// The link signed out: the sign-in screen is the one to show.
    func refreshStored() {
        stored = sessions.load() != nil
    }
}
