import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateBridge
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import TasksCore

/// The app on Android as Apple's `AppShell` makes it (§5.2): the session in
/// the secure store, the signed-in repository's folder under
/// `noBackupFilesDir` (its outbox, its record cache, its settings), and one
/// runtime running `LinkController` over `TasksApp` as one presentation
/// tree. A sign-in to another repository is a new handle over that one's
/// folder; everything else is a command. A reminder's tap becomes the app's
/// command here, as the shell's delegate makes it there.
///
/// Each tree crosses as `TasksFrame`: the link's presentation, the Settings
/// screen made from it (which Apple's shell makes beside its views), and
/// whether a session was stored when the root opened, so the first frame,
/// drawn before the link has read the session, is not a sign-in screen.
/// Kotlin sends a `TasksShellCommand`: a command by its path, or a
/// Settings row's, routed here as the shell routes it.
final class TasksRoot: BridgeRoot {
    typealias Link = LinkController<TasksApp>

    private let started: Task<Runtime<Link>?, Never>
    private let following: Task<Void, Never>

    init(opening: TasksOpening, context: BridgeContext) {
        let started = Task<Runtime<Link>?, Never> {
            // The store answers through the bridge and blocks while it does,
            // so it is read here, on Swift's executors, never in `open`.
            let (runtime, stored) = Self.make(opening, context: context)
            Self.forward(runtime, stored: stored, to: context)
            return runtime
        }
        self.started = started
        following = Task {
            for await response in BridgeNotifications(channel: context.device).responses() {
                guard let command = Reminders.command(for: response) else { continue }
                try? await started.value?.send(command, to: Link.appPath)
            }
        }
    }

    private static func make(_ opening: TasksOpening, context: BridgeContext) -> (Runtime<Link>, Bool) {
        let sessions = SessionStore(storage: BridgeSecureStore(channel: context.device, store: TasksRoots.secureStore))
        var environment = context.appEnvironment(secureStore: TasksRoots.secureStore)
        let transport = environment.devices.transport
        environment.client = ClientSource {
            sessions.load().map { SubstrateClient(server: $0.server, token: $0.secret, transport: transport) }
        }
        let session = sessions.load()
        let folder = session.flatMap { session in
            try? RepositoryFolder(
                root: context.environment.data.appendingPathComponent(TasksRoots.folder, isDirectory: true)
                    .appendingPathComponent(RepositoryFolder.name(server: session.server, repository: session.repository), isDirectory: true))
        }
        environment.folder = folder
        let outbox = folder.map { Outbox(folder: $0) }
        let build = BuildFacts(version: opening.version, channel: opening.channel)
        let words = TasksApp.words(device: opening.device)
        let app = TasksApp(cache: folder.map { RecordCache(folder: $0) }, outbox: outbox, words: words, device: opening.device, build: build)
        let packages = [TasksKinds.packageID: TasksOpening.packageVersion(in: context.environment)]
        let link = Link(
            app: app, manifest: opening.manifest(in: context.environment), sessions: sessions, outbox: outbox, packages: packages,
            words: words)
        return (Runtime(link, environment: environment), session != nil)
    }

    /// Each frame's keyed lists first, whole then as deltas, then its tree
    /// inside a `TasksFrame`, as `RuntimeRoot` sends a bare one.
    private static func forward(_ runtime: Runtime<Link>, stored: Bool, to context: BridgeContext) {
        Task {
            let lists = ContextLists(context: context)
            for await frame in await runtime.encodedFrames() {
                for list in frame.lists { list.hand(to: lists) }
                do {
                    let tree = try JSONValue(data: frame.presentation.get())
                    let link = await runtime.presentation
                    let settings = try link.app.map { try ValueCoding.encode(TasksSettings.screen(link, $0)) } ?? .null
                    let wrapped: JSONValue = .object(JSONObject([("stored", .bool(stored)), ("link", tree), ("settings", settings)]))
                    context.present(encoded: wrapped.data(pretty: false))
                } catch {
                    context.fail(BridgeFailure(.swiftFailure, "a presentation did not encode: \(error)"))
                }
                if context.isClosed { break }
            }
        }
    }

    func receive(_ command: JSONValue) async throws {
        guard let runtime = await started.value else { return }
        switch try ValueCoding.decode(TasksShellCommand.self, from: command) {
        case let .route(route):
            try await runtime.send(encoded: ValueCoding.encode(route).data(pretty: false))
        case let .settings(row):
            // What only the UI can run (the browser, the updater, the
            // connect screen) Kotlin runs from the same row.
            switch SettingsRoute<TasksApp>.route(row, settings: { .settings($0) }, clearSaved: .clearSaved) {
            case let .app(command): try await runtime.send(command, to: Link.appPath)
            case let .link(command): await runtime.send(command)
            case .changeAccess, .checkForUpdates, .signIn, .none: break
            }
        }
    }

    func environmentChanged(_ environment: BridgeEnvironment) async {
        await started.value?.environmentChanged(locale: environment.locale, timeZone: environment.timeZone)
    }

    func close() async {
        following.cancel()
        await started.value?.close()
    }
}

/// What crosses for the `tasks` root: the link's presentation, with the
/// app's under it, and whether a session was stored when it opened.
public struct TasksFrame: Bridged, Equatable {
    public var stored: Bool
    public var link: LinkPresentation<TasksPresentation>
    /// The app's sections, then the link's status and repository, then
    /// storage and about (`TasksSettings.screen`); nil before the app's
    /// first presentation.
    public var settings: SettingsPresentation?
}

/// What Kotlin sends the `tasks` root.
public enum TasksShellCommand: Bridged, Equatable {
    /// A command for the controller at `path` (`[]` the link, `["app"]` the
    /// app's root, `["app", <editor>]` an editor).
    case route(command: RouteCommand)
    /// A Settings row's command, routed as `SettingsRoute` routes it.
    case settings(command: SettingsCommand)
}

/// A handle's context as what takes a frame's keyed lists.
private struct ContextLists: KeyedListReceiver {
    let context: BridgeContext

    func receive<Row>(list name: String, _ list: KeyedList<Row>) {
        context.publish(list: name, list)
    }
}
