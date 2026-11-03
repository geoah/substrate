import Foundation
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting
import Testing
import WorkoutsCore
@testable import WorkoutsSync

/// Workouts' root under the link: what the link's snapshot starts, what a
/// sync step for another account changes (nothing), and what the session
/// hands the outbox.
@Suite("Workouts app under the link")
struct WorkoutsAppTests {
    static let server = URL(string: "http://127.0.0.1:8092")!
    static let account = AccountRef(server: server, repository: "ada")

    static func snapshot(_ repository: String = "ada", training: Int? = WorkoutsManifest.trainingVersion) -> LinkSnapshot {
        LinkSnapshot(phase: .signedIn, server: server, repository: repository, scopes: WorkoutsManifest.scopes,
                     packages: training.map { [WorkoutsKinds.packageID: $0] } ?? [:])
    }

    static func harness(directory: URL?, performsEffects: Bool = false) -> ControllerHarness<WorkoutsApp> {
        var environment = AppEnvironment.testing()
        environment.accounts = WorkoutsAccounts(base: directory)
        let app = WorkoutsApp(directory: directory, adapter: "substrate-workouts@test", settings: WorkoutsSettings())
        return ControllerHarness(app, environment: environment, performsEffects: performsEffects)
    }

    static func names(_ effects: [WorkoutsApp.Effect]) -> [String] {
        effects.map { String(String(describing: $0).prefix { $0 != "(" }) }
    }

    @Test("a snapshot signed in opens the account's folder and follows its outbox; a writable package then syncs it")
    func linkHandOff() async throws {
        let harness = Self.harness(directory: nil)
        _ = await harness.presentation
        harness.clearTrace()
        await harness.runtime.linkChanged(Self.snapshot(training: nil))
        #expect(Self.names(harness.effects) == ["stopSync", "openAccount", "followOutbox"])
        #expect(await harness.presentation.signedIn)
        #expect(await !harness.presentation.canWrite)
        harness.clearTrace()
        // The link read the package after its check: now it may write.
        await harness.runtime.linkChanged(Self.snapshot())
        #expect(Self.names(harness.effects) == ["sync"])
        #expect(await harness.presentation.canWrite)
    }

    @Test("a package behind this build writes nothing, and the presentation says why")
    func behind() async throws {
        let harness = Self.harness(directory: nil)
        await harness.runtime.linkChanged(Self.snapshot(training: WorkoutsManifest.trainingVersion - 1))
        #expect(!Self.names(harness.effects).contains("sync"))
        let reason = try #require(await harness.presentation.writeBlockedReason)
        #expect(reason.contains("version \(WorkoutsManifest.trainingVersion) or later"))
    }

    @Test("a sync step for an account no longer signed in changes nothing")
    func staleAccount() async throws {
        let harness = Self.harness(directory: nil)
        await harness.runtime.linkChanged(Self.snapshot("ada"))
        await harness.runtime.linkChanged(Self.snapshot("grace"))
        let before = await harness.state
        var catalog = Catalog()
        catalog.routines[RoutineID("ada-routine")] = try #require(try BundledCatalog().loadNow().routines.values.first)
        await harness.deliver(.catalogRead(Self.account, catalog, sample: .installed, sampleProblem: nil))
        #expect(await harness.state == before)
        // Signing out stops the run under its key.
        harness.clearTrace()
        await harness.runtime.linkChanged(LinkSnapshot(generation: 1))
        #expect(Self.names(harness.effects) == ["stopSync"])
        #expect(await harness.presentation.origin == .bundled)
    }

    @Test("a session finished under a writable package goes to the account's outbox as one batch, and joins History")
    func finishedHandOff() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("workouts-app-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let harness = Self.harness(directory: directory, performsEffects: true)
        await harness.runtime.linkChanged(Self.snapshot())
        try await harness.until(timeout: .seconds(60)) { await $0.presentation.historyLoaded }
        try await harness.until(timeout: .seconds(60)) { await $0.presentation.catalog != nil }
        try await harness.runtime.send(SessionCommand.start(.empty), to: [WorkoutsApp.sessionKey])
        try await harness.until(timeout: .seconds(60)) { await $0.presentation.session?.session != nil }
        let id = try #require(await harness.presentation.session?.session?.sessionID)
        let now = harness.clock.now
        try await harness.runtime.send(SessionCommand.finish(FinishRequest(start: now, end: now, policy: .drop, sessionRPE: nil, notes: nil)),
                                       to: [WorkoutsApp.sessionKey])
        try await harness.until(timeout: .seconds(60)) { await $0.presentation.history.contains { $0.id == id } }
        #expect(await harness.presentation.finished == 1)
        // The app's own outbox for the account: one actor per file.
        let outbox = try #require(harness.environment.accounts?.account(Self.account)?.outbox)
        try await harness.until(timeout: .seconds(60)) { _ in await outbox.entries.contains { $0.batch == id.rawValue } }
        let batch = await outbox.entries.filter { $0.batch == id.rawValue }
        #expect(batch.first?.method == .put)
        #expect(batch.last?.method == .patch)
        // The file every build reads, beside the account's other files.
        let file = directory.appendingPathComponent("substrate/127.0.0.1-8092-ada/outbox.json")
        #expect(FileManager.default.fileExists(atPath: file.path))
        let json = try #require(try JSONSerialization.jsonObject(with: Data(contentsOf: file)) as? [String: Any])
        let entries = try #require(json["entries"] as? [[String: Any]])
        #expect(entries.allSatisfy { $0["session"] as? String == id.rawValue })
    }

    @Test("a delete keeps its tombstone in the account's folder, removes the history file and answers its receipt")
    func deleteSession() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("workouts-app-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let records = SessionRecords(session: TrainingSession(id: SessionID("s1"), at: Date(timeIntervalSince1970: 1_793_000_000)), sets: [])
        try HistoryStore(directory: directory).write(HistoryEntry(records: records))
        let harness = Self.harness(directory: directory, performsEffects: true)
        await harness.runtime.linkChanged(Self.snapshot())
        try await harness.until(timeout: .seconds(60)) { await $0.presentation.history.count == 1 }
        let receipt = UUID()
        await harness.send(.deleteSession(SessionID("s1"), receipt: receipt))
        try await harness.until(timeout: .seconds(60)) { await $0.presentation.outcomes[receipt] != nil }
        #expect(await harness.presentation.outcomes[receipt] == .some(nil))
        #expect(await harness.presentation.history.isEmpty)
        #expect(await harness.presentation.hiddenSessions.contains("s1"))
        #expect(WorkoutsAccounts(base: directory).account(Self.account)?.tombstones.all == ["s1"])
    }
}
