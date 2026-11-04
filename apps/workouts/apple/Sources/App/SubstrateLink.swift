import Foundation
import Observation
import SubstrateKit
import SubstrateLocal
import WorkoutsCore
import WorkoutsSync

/// The app's substrate as the views read it, republished from the kit's
/// link and Workouts' root on the runtime: where the repository stands (the
/// Status, Repository and Access screens' presentations), and what the
/// routine pages need of the sync. Its commands go to the link or to the
/// app; the sign-in alone runs here, since the browser sheet is the
/// device's, and its session reaches the link through the session store.
@MainActor
@Observable
final class SubstrateLink {
    private(set) var link: LinkPresentation<WorkoutsPresentation>?
    private(set) var workouts: WorkoutsPresentation?
    /// The browser sheet is open for a change of access.
    private(set) var authorizing = false
    /// Why a change of access did not happen.
    private(set) var accessProblem: String?

    let manifest: AppManifest
    @ObservationIgnored weak var root: WorkoutsRoot?

    init(manifest: AppManifest) {
        self.manifest = manifest
    }

    func apply(_ next: LinkPresentation<WorkoutsPresentation>) {
        var mine = next
        mine.app = nil
        if mine != link { link = mine }
        if next.app != workouts { workouts = next.app }
    }

    // MARK: What the views read

    var isSignedIn: Bool { link?.snapshot.phase != .signedOut && link?.snapshot.server != nil }
    var host: String? { link?.snapshot.server.map { ServerAddress.display($0) } }
    var status: StatusPresentation? { link?.status }
    var repository: RepositoryPresentation? { link?.repository }
    var signOutProblem: ProblemPresentation? { link?.signOutProblem }
    var canWrite: Bool { workouts?.canWrite ?? false }
    var writeBlockedReason: String? { workouts?.writeBlockedReason }
    var canDeleteRoutines: Bool { workouts?.canDeleteRoutines ?? false }
    var sampleState: SampleInstall.State? { workouts?.sampleState }
    var sampleProblem: String? { workouts?.sampleProblem }
    var addingSample: Bool { workouts?.addingSample ?? false }
    var draft: RoutineEditor? { workouts?.draft }
    var waitingRoutines: Set<RoutineID> { workouts?.waitingRoutines ?? [] }
    var syncProblem: String? { workouts?.problem }

    func conflict(for routine: RoutineID) -> OutboxEntry? { workouts?.conflict(for: routine) }

    static let conflictFirst = WorkoutsPresentation.conflictFirst

    // MARK: Signing in

    /// The app authorization flow on a server the sign-in screen probed: the
    /// server's own sign-in and approval page in the system sheet, the code
    /// exchanged with its PKCE verifier. Approving installs the health
    /// catalogue, the training package and the coach. A session this
    /// replaces is revoked like any replaced one.
    func signIn(_ probe: ServerProbe.Result) async throws {
        guard let oauth = OAuthClient(manifest: manifest, probe: probe) else { throw URLError(.badServerResponse) }
        let current = await SessionStore.workouts.load()
        let session = try await oauth.signIn(server: probe.origin, repository: probe.repository, keepingRejectedIn: .workouts)
        try await SessionStore.workouts.save(session, replacing: current)
        root?.send(.signedIn)
    }

    /// The flow again with the whole request: Change Access, a renewal, a
    /// scope the owner left out. The old access works until the new one is
    /// kept.
    func reauthorize() async {
        guard let old = await SessionStore.workouts.load(), !authorizing else { return }
        authorizing = true
        accessProblem = nil
        defer { authorizing = false }
        do {
            let probe = try await ServerProbe().probe(old.server, requirement: manifest.requirement, app: manifest.appName)
            guard manifest.door(for: probe) == .app, let oauth = OAuthClient(manifest: manifest, probe: probe) else {
                accessProblem = "\(ServerAddress.display(old.server)) can\u{2019}t give \(manifest.appName) access of its own: it needs an update first."
                return
            }
            try await SessionStore.workouts.save(try await oauth.signIn(server: old.server), replacing: old)
            root?.send(.signedIn)
        } catch let error where error is WebAuthenticator.Cancelled || error is CancellationError {
            // The sheet was closed: the access stays as it was.
        } catch {
            accessProblem = explain(error, app: manifest.appName, server: old.server)
        }
    }

    /// Forgets the session on this phone and revokes the token. What still
    /// waits in the outbox stays with that repository and goes when it
    /// signs in again; the app returns to its own routines.
    func signOut() { root?.send(.signOut) }

    // MARK: The link's commands

    /// The status check, and the app's sync: at launch, on return to the
    /// foreground and after a sign-in.
    func refresh() {
        root?.send(.refresh(force: false))
        root?.send(.sync)
    }

    func sync() { root?.send(.sync) }

    /// A sync once the running one ends: a change made while one runs (an
    /// import) was not in the history it read.
    func syncAfterRunning() async {
        await root?.until { $0.app?.flushing == false }
        root?.send(.sync)
    }

    func perform(_ action: StatusAction) {
        switch action {
        case .sendNow: root?.send(.sendNow)
        case .review, .renew: Task { await reauthorize() }
        case .signIn: break
        case let .retry(id): root?.send(.retry(id))
        case let .discard(id): root?.send(.discard(id))
        case .readAgain: root?.send(.readAgain)
        }
    }

    // MARK: Routines

    func addSampleRoutine() { root?.send(.addSampleRoutine) }

    func deleteRoutine(_ id: RoutineID) { root?.send(.deleteRoutine(id)) }

    func keepDraft(_ editor: RoutineEditor) { root?.send(.keepDraft(editor)) }

    func dropDraft() { root?.send(.dropDraft) }

    /// Saves an edited or duplicated routine through the outbox. An edit the
    /// outbox cannot keep on disk changes nothing and throws, and the draft
    /// stays.
    func saveRoutine(_ edited: RoutineEditor) async throws {
        try await root?.outcome { .saveRoutine(edited, receipt: $0) }
    }

    func resolve(_ entry: OutboxEntry, keepMine: Bool) { root?.send(.resolve(entry.id, keepMine: keepMine)) }

    func exerciseAdded(_ exercise: Exercise) { root?.send(.exerciseAdded(exercise)) }

    /// What an import (Hevy's) writes beside the sessions: routines after
    /// every new exercise they name, and body measurements.
    func importRecords(routines: [Routine], measurements: [FlatRecord], exercises: [Exercise]) async throws {
        try await root?.outcome {
            .importRecords(routines: routines, measurements: measurements.map(RecordWrite.init), exercises: exercises, receipt: $0)
        }
    }
}
