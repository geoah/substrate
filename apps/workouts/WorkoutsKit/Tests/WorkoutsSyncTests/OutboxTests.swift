import Foundation
import SubstrateLocal
import SubstrateAPI
import SubstrateAuth
import SubstrateDevice
import SubstrateScreens
import Testing
import WorkoutsCore
@testable import WorkoutsSync
import SubstrateCore

/// The outbox's order and its answers to the server: the session before its
/// sets, a chain's head before its members, Finish under `ifVersion` raising
/// `revision`, a conflict read and sent again, a stale revision dropped, the
/// network waited out and a refusal held with its session.
@Suite("Outbox", .timeLimit(.minutes(10)))
struct OutboxTests {
    let codec = RecordCodec()

    /// The recorded session, finished, and its 20 sets, four of them drops.
    func finished() throws -> SessionRecords {
        func records(_ name: String) throws -> [FlatRecord] {
            try JSONDecoder().decode(WorkoutsAPI.Page.self, from: Fixtures.data(name)).records.map { try WorkoutsAPI.record($0) }
        }
        var session = try #require(codec.sessionRecords(from: records("list-sessions") + records("list-sets")).first)
        // As the engine hands it over: sets in any order.
        session.sets.reverse()
        return session
    }

    @Test("a finished session is planned as its create, its sets heads first, then Finish under ifVersion")
    func thePlan() throws {
        let records = try finished()
        let plan = OutboxPlan.finished(records, codec: codec)
        #expect(plan.count == records.sets.count + 2)
        let create = try #require(plan.first)
        #expect(create.method == .put && create.kind == codec.kinds.session && !create.guarded)
        #expect(create.properties?["status"] == nil)
        let finish = try #require(plan.last)
        #expect(finish.method == .patch && finish.guarded)
        #expect(finish.properties?["status"] == .string("finished"))
        let revision = records.session.revision ?? 1
        #expect(create.properties?["revision"] == .number(Decimal(revision)))
        #expect(finish.properties?["revision"] == .number(Decimal(revision + 1)))
        var seen = Set<String>()
        for entry in plan.dropFirst().dropLast() {
            #expect(entry.kind == codec.kinds.set)
            if case let .object(head)? = entry.properties?["continues"], case let .string(ref)? = head["ref"] {
                #expect(seen.contains(String(ref.split(separator: "/").last!)), "a drop went before its head")
            }
            seen.insert(entry.recordID)
        }
        #expect(plan.allSatisfy { $0.session == records.session.id.rawValue })
    }

    @Test("a flush sends in order, and Finish asserts the version the create answered")
    func flushOrder() async throws {
        let server = ScriptedServer()
        server.on { $0.method == "PUT" ? (200, record(1)) : nil }
        server.on { sent in
            guard sent.method == "PATCH" else { return nil }
            guard case let .object(body)? = sent.body, body["ifVersion"] == .number(1) else { return (409, #"{"error":{"code":"conflict","message":"version conflict"}}"#) }
            return (200, record(2))
        }
        let outbox = try Outbox.workouts(directory: temporaryDirectory(), writingOptions: .atomic)
        let records = try finished()
        try await outbox.enqueue(OutboxPlan.finished(records, codec: codec))
        let state = await outbox.flush(api(server))
        #expect(state == .sent)
        #expect(await outbox.entries.isEmpty)
        let paths = server.methodsAndPaths
        #expect(paths.first == "PUT /api/v1/substrate.reamde.dev/training/session/\(records.session.id.rawValue)")
        #expect(paths.last == "PATCH /api/v1/substrate.reamde.dev/training/session/\(records.session.id.rawValue)")
        #expect(paths.dropFirst().dropLast().allSatisfy { $0.hasPrefix("PUT /api/v1/substrate.reamde.dev/training/set/") })
        // Every reference went out in the repository's spelling.
        for sent in server.sent where sent.method == "PUT" {
            let text = String(decoding: try JSONEncoder().encode(sent.body), as: UTF8.self)
            #expect(!text.contains("\"~/"))
        }
    }

    @Test("a conflict on Finish reads the record and sends the change again on top")
    func conflict() async throws {
        let server = ScriptedServer()
        server.answer("GET", "/api/v1/substrate.reamde.dev/training/session/s1", 200,
                      #"{"id":"s1","kind":"substrate.reamde.dev/training/session","properties":{},"version":5}"#)
        server.on { sent in
            guard sent.method == "PATCH" else { return nil }
            if case let .object(body)? = sent.body, body["ifVersion"] == .number(5) { return (200, record(6)) }
            return (409, #"{"error":{"code":"conflict","message":"version conflict: ifVersion 1, stored 5"}}"#)
        }
        server.on { $0.method == "PUT" ? (200, record(1)) : nil }
        let outbox = try Outbox.workouts(directory: temporaryDirectory(), writingOptions: .atomic)
        try await outbox.enqueue([
            OutboxEntry(method: .put, kind: codec.kinds.session, recordID: "s1", properties: [:], session: "s1"),
            OutboxEntry(method: .patch, kind: codec.kinds.session, recordID: "s1", properties: ["notes": .string("x")], guarded: true, session: "s1"),
        ])
        #expect(await outbox.flush(api(server)) == .sent)
        // The create answered version 1, which somebody moved to 5: the
        // first patch is refused, the record read, and the patch sent again.
        #expect(server.methodsAndPaths == [
            "PUT /api/v1/substrate.reamde.dev/training/session/s1",
            "PATCH /api/v1/substrate.reamde.dev/training/session/s1",
            "GET /api/v1/substrate.reamde.dev/training/session/s1",
            "PATCH /api/v1/substrate.reamde.dev/training/session/s1",
        ])
    }

    @Test("a revision below the stored one is a newer write landed first: dropped")
    func staleRevision() async throws {
        let server = ScriptedServer()
        server.answer("PUT", "/api/v1/substrate.reamde.dev/training/set/a", 422,
                      #"{"error":{"code":"validation","message":"substrate: validation failed","problems":["props.revision: 1 is below the stored revision 2; a rewrite never lowers it, so this is a stale delivery"]}}"#)
        let outbox = try Outbox.workouts(directory: temporaryDirectory(), writingOptions: .atomic)
        try await outbox.enqueue([OutboxEntry(method: .put, kind: codec.kinds.set, recordID: "a", properties: [:], session: "s")])
        #expect(await outbox.flush(api(server)) == .sent)
        #expect(await outbox.entries.isEmpty)
    }

    @Test("without the network nothing is lost, and the queue survives a relaunch")
    func offline() async throws {
        let server = ScriptedServer()
        server.on { _ in (-1, "") }
        let directory = temporaryDirectory()
        let outbox = try Outbox.workouts(directory: directory, writingOptions: .atomic)
        try await outbox.enqueue(OutboxPlan.finished(try finished(), codec: codec))
        let state = await outbox.flush(api(server))
        guard case .offline = state else { Issue.record("state \(state)"); return }
        let count = await outbox.entries.count
        #expect(count == 22)
        #expect(await (try! Outbox.workouts(directory: directory, writingOptions: .atomic)).entries.count == count)
    }

    @Test("a refusal parks the write, holds the rest of its session and lets another session go")
    func refusalHoldsItsSession() async throws {
        let server = ScriptedServer()
        server.answer("PUT", "/api/v1/substrate.reamde.dev/training/session/bad", 422,
                      #"{"error":{"code":"validation","message":"substrate: validation failed","problems":["props.sport: expected one of strength"]}}"#)
        server.on { $0.method == "PUT" ? (200, record(1)) : nil }
        let outbox = try Outbox.workouts(directory: temporaryDirectory(), writingOptions: .atomic)
        try await outbox.enqueue([
            OutboxEntry(method: .put, kind: codec.kinds.session, recordID: "bad", properties: [:], session: "bad"),
            OutboxEntry(method: .put, kind: codec.kinds.set, recordID: "bad-1", properties: [:], session: "bad"),
            OutboxEntry(method: .put, kind: codec.kinds.session, recordID: "good", properties: [:], session: "good"),
        ])
        let state = await outbox.flush(api(server))
        guard case let .refused(words) = state else { Issue.record("state \(state)"); return }
        #expect(words.contains("props.sport"))
        #expect(await outbox.entries.map(\.recordID) == ["bad", "bad-1"])
        #expect(await outbox.parked.map(\.recordID) == ["bad"])
        #expect(!server.methodsAndPaths.contains("PUT /api/v1/substrate.reamde.dev/training/set/bad-1"))
        #expect(server.methodsAndPaths.contains("PUT /api/v1/substrate.reamde.dev/training/session/good"))
        // Parked, it is not sent again on its own, and the state still says so.
        let before = server.sent.count
        guard case .refused = await outbox.flush(api(server)) else { Issue.record("a parked write stopped being reported"); return }
        #expect(server.sent.count == before)
    }

    @Test("a refused token stops the run and asks to sign in, keeping everything")
    func signedOut() async throws {
        let server = ScriptedServer()
        server.on { _ in (401, #"{"error":{"code":"auth","message":"invalid token"}}"#) }
        let outbox = try Outbox.workouts(directory: temporaryDirectory(), writingOptions: .atomic)
        try await outbox.enqueue([OutboxEntry(method: .put, kind: codec.kinds.set, recordID: "a", properties: [:])])
        #expect(await outbox.flush(api(server)) == .signedOut)
        #expect(await outbox.entries.count == 1)
    }

    @Test("a discard drops the session's queued writes; a session the server holds is purged")
    func discard() async throws {
        let outbox = try Outbox.workouts(directory: temporaryDirectory(), writingOptions: .atomic)
        let sink = OutboxSink<Int, Int>(outbox: outbox, onQueued: {})
        try await outbox.enqueue(OutboxPlan.finished(try finished(), codec: codec))
        let id = try finished().session.id
        await sink.discarded(id)
        #expect(await outbox.entries.isEmpty)

        let server = ScriptedServer()
        server.on { $0.method == "PUT" || $0.method == "PATCH" ? (200, record(3)) : nil }
        server.on { $0.method == "DELETE" ? (200, "{}") : nil }
        try await outbox.enqueue(OutboxPlan.finished(try finished(), codec: codec))
        await outbox.flush(api(server))
        await sink.deleted(id)
        #expect(await outbox.flush(api(server)) == .sent)
        let purge = try #require(server.sent.last)
        #expect(purge.method == "DELETE")
        #expect(purge.query?.contains("purge=true") == true)
        #expect(purge.query?.contains("ifVersion=3") == true)
    }

    @Test("an exercise added on the phone goes before anything names it")
    func exerciseFirst() async throws {
        let outbox = try Outbox.workouts(directory: temporaryDirectory(), writingOptions: .atomic)
        let sink = OutboxSink<Int, Int>(outbox: outbox, onQueued: {})
        let exercise = Exercise(id: ExerciseID(rawValue: "landmine-press"), name: "Landmine Press", measurement: .weightreps, equipment: .barbell)
        await sink.exerciseAdded(exercise)
        await sink.finished(try finished())
        let first = try #require(await outbox.entries.first)
        #expect(first.kind == codec.kinds.exercise && first.recordID == "landmine-press")
    }
}

@Suite("The package", .timeLimit(.minutes(10)))
struct PackageTests {
    @Test("a repository without the package, or whose row the token cannot see, is missing; nothing is applied")
    func missing() async throws {
        let server = ScriptedServer()
        server.answer("GET", packagePath, 404, notFoundBody)
        let check = PackageCheck(api: api(server), manifest: try PackageManifest.bundled())
        #expect(try await check.state() == .missing)
        let asked = Locked(0)
        #expect(try await check.ensure { asked.value += 1 } == .missing)
        #expect(asked.value == 0)
        #expect(!server.sent.contains { $0.method != "GET" })
    }

    @Test("the version held is the publisher's shippedVersion, and the row's own version where none was stamped")
    func heldVersion() async throws {
        let manifest = try PackageManifest.bundled()
        let stamped = ScriptedServer()
        stamped.answer("GET", packagePath, 200, packageRow(manifest.package, shipped: manifest.version))
        #expect(try await PackageCheck(api: api(stamped), manifest: manifest).state() == .current(held: manifest.version))
        let bare = ScriptedServer()
        bare.answer("GET", packagePath, 200, #"{"id":"substrate.reamde.dev/training","kind":"substrate.reamde.dev/core/package","properties":{"version":\#(manifest.version + 1)},"version":4}"#)
        #expect(try await PackageCheck(api: api(bare), manifest: manifest).state() == .current(held: manifest.version + 1))
    }

    @Test("a newer version is current: additive, so this build keeps writing; an older one is behind and not written to")
    func versions() throws {
        #expect(PackageState.of(held: nil, needed: 2) == .missing)
        #expect(PackageState.of(held: 1, needed: 2) == .behind(held: 1))
        #expect(PackageState.of(held: 2, needed: 2) == .current(held: 2))
        #expect(PackageState.of(held: 3, needed: 2) == .current(held: 3))
        #expect(PackageState.current(held: 3).writable && !PackageState.behind(held: 1).writable && !PackageState.missing.writable)
    }

    @Test("a repository behind the build asks the server once, through the set up check, and reads the row again")
    func behindAsksTheServer() async throws {
        let manifest = try PackageManifest.bundled()
        let server = ScriptedServer()
        let upgraded = Locked(false)
        server.on { sent in
            guard sent.method == "GET", sent.path == packagePath else { return nil }
            return (200, packageRow(manifest.package, shipped: upgraded.value ? manifest.version : manifest.version - 1))
        }
        let check = PackageCheck(api: api(server), manifest: manifest)
        #expect(try await check.state() == .behind(held: manifest.version - 1))
        // The server applied the kinds-only upgrade when the check named the floor.
        let applied = try await check.ensure { upgraded.value = true }
        #expect(applied == .current(held: manifest.version))
        // One that waits for the owner's review stays behind, and nothing is written.
        let waiting = ScriptedServer()
        waiting.answer("GET", packagePath, 200, packageRow(manifest.package, shipped: manifest.version - 1))
        let asked = Locked(0)
        let still = try await PackageCheck(api: api(waiting), manifest: manifest).ensure { asked.value += 1 }
        #expect(still == .behind(held: manifest.version - 1) && asked.value == 1)
        #expect(!waiting.sent.contains { $0.method != "GET" })
    }
}

@Suite("Sign-in", .timeLimit(.minutes(10)))
struct SignInTests {
    @Test("the request asks for the published packages at this build's versions, the writes, the reads and the coach")
    func theRequest() throws {
        let manifest = WorkoutsManifest.manifest(clientID: WorkoutsManifest.localClientID)
        #expect(manifest.scopes.first == "write:substrate.reamde.dev/training/*")
        #expect(manifest.scopes.contains("agents:substrate.reamde.dev/trainingcoach/*"))
        #expect(manifest.scopes.contains("read:providers.substrate.reamde.dev/applehealth/account"))
        #expect(!manifest.scopes.contains { $0.hasPrefix("declare:") || $0.contains("~") })
        let training = try PackageManifest.bundled()
        let coach = try PackageManifest.coach()
        #expect(manifest.details.map { "\($0.package ?? "")@\($0.version ?? 0)" } == [
            "substrate.reamde.dev/health@1", "\(training.package)@\(training.version)", "\(coach.package)@\(coach.version)",
        ])
        #expect(training.version >= 4 && coach.version >= 2)
        #expect(manifest.details.allSatisfy { $0.type == AuthorizationDetail.Types.package && !$0.optional })
        #expect(manifest.requirement.scopeVerbs == ["write", "agents", "read"])
        #expect(manifest.requirement.detailTypes == [AuthorizationDetail.Types.package])
        #expect(manifest.isLocal)
        #expect(!manifest.softwareVersion.isEmpty)
        #expect(WorkoutsManifest.manifest(clientID: WorkoutsManifest.localClientID, softwareVersion: "0.120.0 (1)").softwareVersion == "0.120.0 (1)")
    }

    @Test("Approved access names the Health account as the approval page does, Apple Health in its own capitals")
    func approvedAccessWords() throws {
        let manifest = WorkoutsManifest.manifest(clientID: WorkoutsManifest.localClientID)
        let session = SessionFacts(server: URL(string: "http://localhost:8080")!, repository: "ada", scopes: manifest.scopes)
        let repository = RepositoryPresenter.present(session: session, manifest: manifest, serverVersion: nil, unrevoked: [],
                                                     offersChangeAccess: true, locale: Locale(identifier: "en_GB"), timeZone: .gmt)
        let words = try #require(repository.approved).scopes.map(\.words)
        #expect(words.contains("See your Apple Health accounts, and search them by meaning, which spends your embedding budget"))
        #expect(!words.contains { $0.contains("apple Health") })
    }

    @Test("a server without agents, or without package details, is too old, never a browser away")
    func tooOld() throws {
        let manifest = WorkoutsManifest.manifest(clientID: WorkoutsManifest.localClientID)
        let noAgents = try JSONDecoder().decode(AuthorizationServerMetadata.self, from: Data(#"{"issuer":"http://localhost:8080","authorization_endpoint":"http://localhost:8080/oauth/authorize","token_endpoint":"http://localhost:8080/oauth/token","scopes_supported":["read:*","write:*"],"authorization_details_types_supported":["package","account","webhook"]}"#.utf8))
        #expect(!manifest.requirement.verdict(version: "v0.114.0", metadata: noAgents).isCompatible)
        let noDetails = try JSONDecoder().decode(AuthorizationServerMetadata.self, from: Data(#"{"issuer":"http://localhost:8080","authorization_endpoint":"http://localhost:8080/oauth/authorize","token_endpoint":"http://localhost:8080/oauth/token","scopes_supported":["read:*","write:*","agents:*","decide:*"]}"#.utf8))
        #expect(!manifest.requirement.verdict(version: "dev", metadata: noDetails).isCompatible)
        let fine = try JSONDecoder().decode(AuthorizationServerMetadata.self, from: Data(#"{"issuer":"http://localhost:8080","authorization_endpoint":"http://localhost:8080/oauth/authorize","token_endpoint":"http://localhost:8080/oauth/token","scopes_supported":["read:*","write:*","agents:*","decide:*"],"authorization_details_types_supported":["package","account","webhook"]}"#.utf8))
        #expect(manifest.requirement.verdict(version: "dev", metadata: fine).isCompatible)
        #expect(manifest.door(for: ServerProbe.Result(origin: URL(string: "http://localhost:8080")!, discovery: Discovery(server: .init(version: "dev")),
                                                      metadata: noAgents, verdict: manifest.requirement.verdict(version: "dev", metadata: noAgents))) == .tooOld)
    }
}

final class Locked<Value>: @unchecked Sendable {
    private let lock = NSLock()
    private var stored: Value
    init(_ value: Value) { stored = value }
    var value: Value {
        get { lock.withLock { stored } }
        set { lock.withLock { stored = newValue } }
    }
}
