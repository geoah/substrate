import Foundation
import os
import SubstrateKit
import SubstrateLocal
import WorkoutsCore
import WorkoutsSync

extension SessionStore {
    /// The session, kept where every build keeps it: readable after the
    /// first unlock, so a Live Activity button or a background refresh can
    /// use it, and never synced off this device.
    nonisolated static let workouts = SessionStore(service: "dev.reamde.substrate.workouts")
}

/// The app's composition root: the kit's link with Workouts' root under it
/// on one runtime, the device services it runs on, and the three objects
/// the views read, each republishing its part of the one presentation tree
/// on the main actor and sending commands back. Made on first use: at
/// launch, or when a Live Activity intent launches the app in the
/// background, whose command then waits for the journal to be folded.
@MainActor
final class WorkoutsRoot {
    typealias Link = LinkController<WorkoutsApp>

    static let shared = WorkoutsRoot()

    let manifest = WorkoutsManifest.current
    let model: PresentationModel<Link>
    let app: AppModel
    let session: SessionModel
    let link: SubstrateLink
    /// `Application Support/Workouts/`.
    let directory: URL?
    private(set) var presentation: Link.Presentation
    private var waiters: [UUID: (check: (Link.Presentation) -> Bool, resume: CheckedContinuation<Void, Never>)] = [:]
    private let logger = Logger(subsystem: "dev.reamde.substrate.workouts", category: "root")

    /// `substrate-workouts@0.1.0`, the session record's adapter.
    static var adapter: String {
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0"
        return "substrate-workouts@\(version)"
    }

    /// What Settings → Substrate says, in Workouts' words.
    static let words = StatusWords(
        app: WorkoutsManifest.appName, device: "this iPhone", kept: "sessions",
        packageNames: [WorkoutsKinds.packageID: "Training", Coach.package: "Workout coach"],
        local: "Not signed in. Routines and exercises come with this app, and sessions stay on this iPhone.")

    private init() {
        let directory = AppModel.workoutsDirectory()
        self.directory = directory
        let settings = WorkoutsSettings.load()
        let accounts = WorkoutsAccounts(base: directory)
        var environment = AppEnvironment.apple()
        environment.client = ClientSource { SessionStore.workouts.load().map { SubstrateClient($0) } }
        environment.devices.notifications = SystemNotifications()
        environment.devices.activities = LiveActivities<WorkoutsActivityAttributes>(
            content: WorkoutsActivityAttributes.contentState, dismissedKey: "workouts.liveActivity.dismissed")
        environment.workouts = WorkoutsPlatform()
        environment.accounts = accounts
        let root = Link(app: WorkoutsApp(directory: directory, adapter: Self.adapter, settings: settings), manifest: manifest,
                        sessions: .workouts,
                        outboxes: { accounts.account(AccountRef(server: $0.server, repository: $0.repository))?.outbox },
                        packages: [WorkoutsKinds.packageID: WorkoutsManifest.trainingVersion, Coach.package: WorkoutsManifest.coachVersion],
                        words: Self.words)
        model = PresentationModel(root, environment: environment)
        presentation = model.presentation
        app = AppModel(directory: directory, settings: settings)
        session = SessionModel()
        link = SubstrateLink(manifest: manifest)
        app.root = self
        session.root = self
        link.root = self
        let runtime = model.runtime
        Task { [weak self] in
            for await next in await runtime.presentations() {
                self?.apply(next)
            }
        }
    }

    private func apply(_ next: Link.Presentation) {
        presentation = next
        link.apply(next)
        if let workouts = next.app {
            app.apply(workouts)
            if let screen = workouts.session { session.apply(screen) }
        }
        for (id, waiter) in waiters where waiter.check(next) {
            waiters[id] = nil
            waiter.resume.resume()
        }
    }

    /// Returns once the presentation tree holds what `check` asks: a change
    /// whose outcome a sheet waits on.
    func until(_ check: @escaping (Link.Presentation) -> Bool) async {
        if check(presentation) { return }
        await withCheckedContinuation { waiters[UUID()] = (check, $0) }
    }

    // MARK: Commands

    func send(_ command: Link.Command) { model.send(command) }
    func send(_ command: WorkoutsApp.Command) { model.send(command, to: Link.appPath) }
    func send(_ command: SessionCommand) { model.send(command, to: WorkoutsApp.sessionPath) }

    /// A command for the session from outside the views (a Live Activity
    /// intent), returning once it has been applied.
    func perform(_ command: SessionCommand) async {
        do {
            try await model.runtime.send(command, to: WorkoutsApp.sessionPath)
        } catch {
            logger.error("Session command not delivered: \(String(describing: error), privacy: .public)")
        }
    }

    /// A command whose outcome a sheet waits on: answered by its receipt.
    func outcome(_ make: (UUID) -> WorkoutsApp.Command) async throws {
        let receipt = UUID()
        send(make(receipt))
        await until { $0.app?.outcomes[receipt] != nil }
        if case let .some(.some(problem)) = presentation.app?.outcomes[receipt] { throw ChangeNotMade(reason: problem) }
    }
}

/// A change the app could not make, in its own words.
struct ChangeNotMade: LocalizedError {
    let reason: String
    var errorDescription: String? { reason }
}
