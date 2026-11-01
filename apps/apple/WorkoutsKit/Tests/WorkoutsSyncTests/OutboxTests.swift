import Foundation
import SubstrateKit
import Testing
import WorkoutsCore
@testable import WorkoutsSync

/// The outbox's order and its answers to the server: the session before its
/// sets, a chain's head before its members, Finish under `ifVersion` raising
/// `revision`, a conflict read and sent again, a stale revision dropped, the
/// network waited out and a refusal held with its session.
@Suite("Outbox")
struct OutboxTests {
    let codec = RecordCodec()

    /// The recorded session, finished, and its 20 sets, four of them drops.
    func finished() throws -> SessionRecords {
        let rehome = Rehome(authority: recordedAuthority)
        func records(_ name: String) throws -> [FlatRecord] {
            try JSONDecoder().decode(WorkoutsAPI.Page.self, from: Fixtures.data(name)).records.map { try WorkoutsAPI.record($0, rehome: rehome) }
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
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        let records = try finished()
        await outbox.enqueue(OutboxPlan.finished(records, codec: codec))
        let state = await outbox.flush(api(server))
        #expect(state == .sent)
        #expect(await outbox.entries.isEmpty)
        let paths = server.methodsAndPaths
        #expect(paths.first == "PUT /api/v1/ada.example.com/training/session/\(records.session.id.rawValue)")
        #expect(paths.last == "PATCH /api/v1/ada.example.com/training/session/\(records.session.id.rawValue)")
        #expect(paths.dropFirst().dropLast().allSatisfy { $0.hasPrefix("PUT /api/v1/ada.example.com/training/set/") })
        // Every reference went out in the repository's spelling.
        for sent in server.sent where sent.method == "PUT" {
            let text = String(decoding: try JSONEncoder().encode(sent.body), as: UTF8.self)
            #expect(!text.contains("\"~/"))
        }
    }

    @Test("a conflict on Finish reads the record and sends the change again on top")
    func conflict() async throws {
        let server = ScriptedServer()
        server.answer("GET", "/api/v1/ada.example.com/training/session/s1", 200,
                      #"{"id":"s1","kind":"ada.example.com/training/session","properties":{},"version":5}"#)
        server.on { sent in
            guard sent.method == "PATCH" else { return nil }
            if case let .object(body)? = sent.body, body["ifVersion"] == .number(5) { return (200, record(6)) }
            return (409, #"{"error":{"code":"conflict","message":"version conflict: ifVersion 1, stored 5"}}"#)
        }
        server.on { $0.method == "PUT" ? (200, record(1)) : nil }
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        await outbox.enqueue([
            OutboxEntry(method: .put, kind: codec.kinds.session, recordID: "s1", properties: [:], session: "s1"),
            OutboxEntry(method: .patch, kind: codec.kinds.session, recordID: "s1", properties: ["notes": .string("x")], guarded: true, session: "s1"),
        ])
        #expect(await outbox.flush(api(server)) == .sent)
        // The create answered version 1, which somebody moved to 5: the
        // first patch is refused, the record read, and the patch sent again.
        #expect(server.methodsAndPaths == [
            "PUT /api/v1/ada.example.com/training/session/s1",
            "PATCH /api/v1/ada.example.com/training/session/s1",
            "GET /api/v1/ada.example.com/training/session/s1",
            "PATCH /api/v1/ada.example.com/training/session/s1",
        ])
    }

    @Test("a revision below the stored one is a newer write landed first: dropped")
    func staleRevision() async throws {
        let server = ScriptedServer()
        server.answer("PUT", "/api/v1/ada.example.com/training/set/a", 422,
                      #"{"error":{"code":"validation","message":"substrate: validation failed","problems":["props.revision: 1 is below the stored revision 2; a rewrite never lowers it, so this is a stale delivery"]}}"#)
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        await outbox.enqueue([OutboxEntry(method: .put, kind: codec.kinds.set, recordID: "a", properties: [:], session: "s")])
        #expect(await outbox.flush(api(server)) == .sent)
        #expect(await outbox.entries.isEmpty)
    }

    @Test("without the network nothing is lost, and the queue survives a relaunch")
    func offline() async throws {
        let server = ScriptedServer()
        server.on { _ in (-1, "") }
        let directory = temporaryDirectory()
        let outbox = Outbox(directory: directory, writingOptions: .atomic)
        await outbox.enqueue(OutboxPlan.finished(try finished(), codec: codec))
        let state = await outbox.flush(api(server))
        guard case .offline = state else { Issue.record("state \(state)"); return }
        let count = await outbox.entries.count
        #expect(count == 22)
        #expect(await Outbox(directory: directory, writingOptions: .atomic).entries.count == count)
    }

    @Test("a refusal holds the rest of its session and lets another session go")
    func refusalHoldsItsSession() async throws {
        let server = ScriptedServer()
        server.answer("PUT", "/api/v1/ada.example.com/training/session/bad", 422,
                      #"{"error":{"code":"validation","message":"substrate: validation failed","problems":["props.sport: expected one of strength"]}}"#)
        server.on { $0.method == "PUT" ? (200, record(1)) : nil }
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        await outbox.enqueue([
            OutboxEntry(method: .put, kind: codec.kinds.session, recordID: "bad", properties: [:], session: "bad"),
            OutboxEntry(method: .put, kind: codec.kinds.set, recordID: "bad-1", properties: [:], session: "bad"),
            OutboxEntry(method: .put, kind: codec.kinds.session, recordID: "good", properties: [:], session: "good"),
        ])
        let state = await outbox.flush(api(server))
        guard case let .refused(words) = state else { Issue.record("state \(state)"); return }
        #expect(words.contains("props.sport"))
        #expect(await outbox.entries.map(\.recordID) == ["bad", "bad-1"])
        #expect(!server.methodsAndPaths.contains("PUT /api/v1/ada.example.com/training/set/bad-1"))
        #expect(server.methodsAndPaths.contains("PUT /api/v1/ada.example.com/training/session/good"))
    }

    @Test("a refused token stops the run and asks to sign in, keeping everything")
    func signedOut() async throws {
        let server = ScriptedServer()
        server.on { _ in (401, #"{"error":{"code":"auth","message":"invalid token"}}"#) }
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        await outbox.enqueue([OutboxEntry(method: .put, kind: codec.kinds.set, recordID: "a", properties: [:])])
        #expect(await outbox.flush(api(server)) == .signedOut)
        #expect(await outbox.entries.count == 1)
    }

    @Test("a discard drops the session's queued writes; a session the server holds is purged")
    func discard() async throws {
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        let sink = OutboxSink<Int, Int>(outbox: outbox) {}
        await outbox.enqueue(OutboxPlan.finished(try finished(), codec: codec))
        let id = try finished().session.id
        await sink.discarded(id)
        #expect(await outbox.entries.isEmpty)

        let server = ScriptedServer()
        server.on { $0.method == "PUT" || $0.method == "PATCH" ? (200, record(3)) : nil }
        server.on { $0.method == "DELETE" ? (200, "{}") : nil }
        await outbox.enqueue(OutboxPlan.finished(try finished(), codec: codec))
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
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        let sink = OutboxSink<Int, Int>(outbox: outbox) {}
        let exercise = Exercise(id: ExerciseID(rawValue: "landmine-press"), name: "Landmine Press", measurement: .weightreps, equipment: .barbell)
        await sink.exerciseAdded(exercise)
        await sink.finished(try finished())
        let first = try #require(await outbox.entries.first)
        #expect(first.kind == codec.kinds.exercise && first.recordID == "landmine-press")
    }
}

@Suite("The package")
struct PackageTests {
    @Test("a repository without the package gets this build's manifests, under its authority, then reads current")
    func installsWhenMissing() async throws {
        let server = ScriptedServer()
        let manifest = try PackageManifest.bundled()
        let installed = Locked(false)
        server.on { sent in
            guard sent.method == "GET", sent.path == "/api/v1/substrate.reamde.dev/core/package/ada.example.com%2Ftraining" else { return nil }
            return installed.value ? (200, #"{"id":"ada.example.com/training","kind":"substrate.reamde.dev/core/package","properties":{"version":\#(manifest.version)},"version":1}"#)
                : (404, #"{"error":{"code":"not_found","message":"not found"}}"#)
        }
        server.on { sent in
            guard sent.method == "POST", sent.path == "/api/v1/vocabulary/apply" else { return nil }
            installed.value = true
            return (200, #"{"records":[]}"#)
        }
        let installer = PackageInstaller(api: api(server), manifest: manifest)
        #expect(try await installer.state() == .missing)
        #expect(try await installer.ensure() == .current(held: manifest.version))
        let apply = try #require(server.sent.first { $0.method == "POST" })
        guard case let .object(body)? = apply.body, case let .array(documents)? = body["documents"] else {
            Issue.record("no documents"); return
        }
        #expect(documents.count == manifest.documents.count)
    }

    @Test("an older version is upgraded; a newer one is left alone and stops the writes")
    func versions() async throws {
        #expect(PackageState.of(held: nil, shipped: 2) == .missing)
        #expect(PackageState.of(held: 1, shipped: 2) == .behind(held: 1))
        #expect(PackageState.of(held: 2, shipped: 2) == .current(held: 2))
        #expect(PackageState.of(held: 3, shipped: 2) == .newer(held: 3))
        #expect(PackageState.current(held: 2).writable)
        #expect(!PackageState.newer(held: 3).writable && !PackageState.behind(held: 1).writable)

        let server = ScriptedServer()
        server.answer("GET", "/api/v1/substrate.reamde.dev/core/package/ada.example.com%2Ftraining", 200,
                      #"{"id":"ada.example.com/training","kind":"substrate.reamde.dev/core/package","properties":{"version":99},"version":4}"#)
        let installer = PackageInstaller(api: api(server), manifest: try PackageManifest.bundled())
        #expect(try await installer.ensure() == .newer(held: 99))
        #expect(!server.sent.contains { $0.method == "POST" })
    }

    @Test("a token without the declare scope is told so, as needing approval")
    func needsApproval() async throws {
        let server = ScriptedServer()
        server.answer("GET", "/api/v1/substrate.reamde.dev/core/package/ada.example.com%2Ftraining", 404, #"{"error":{"code":"not_found","message":"x"}}"#)
        server.on { sent in
            sent.method == "POST" ? (403, #"{"error":{"code":"forbidden","message":"an app's token does not declare"}}"#) : nil
        }
        let installer = PackageInstaller(api: api(server), manifest: try PackageManifest.bundled())
        await #expect(throws: ServerError.self) { try await installer.ensure() }
    }
}

@Suite("Sign-in")
struct SignInTests {
    @Test("the request asks to keep the package, write its records, and set up health")
    func theRequest() {
        let manifest = WorkoutsManifest.manifest(clientID: WorkoutsManifest.localClientID)
        #expect(manifest.scopes.contains("declare:~/training"))
        #expect(manifest.scopes.contains("write:~/training/*"))
        #expect(manifest.details.contains { $0.type == "package" && $0.package == "substrate.reamde.dev/health" })
        #expect(manifest.requirement.scopeVerbs.contains("declare"))
        #expect(manifest.isLocal)
    }

    @Test("a server that cannot take the declare scope is too old, never a browser away")
    func tooOld() throws {
        let manifest = WorkoutsManifest.manifest(clientID: WorkoutsManifest.localClientID)
        let old = try JSONDecoder().decode(AuthorizationServerMetadata.self, from: Data(#"{"issuer":"http://localhost:8080","authorization_endpoint":"http://localhost:8080/oauth/authorize","token_endpoint":"http://localhost:8080/oauth/token","scopes_supported":["read:*","write:*","agents:*","decide:*"],"authorization_details_types_supported":["package","account","webhook"]}"#.utf8))
        let verdict = manifest.requirement.verdict(version: "v0.114.0", metadata: old)
        #expect(!verdict.isCompatible)
        let new = try JSONDecoder().decode(AuthorizationServerMetadata.self, from: Data(#"{"issuer":"http://localhost:8080","authorization_endpoint":"http://localhost:8080/oauth/authorize","token_endpoint":"http://localhost:8080/oauth/token","scopes_supported":["read:*","write:*","agents:*","decide:*","declare:~/<package>"],"authorization_details_types_supported":["package","account","webhook"]}"#.utf8))
        #expect(manifest.requirement.verdict(version: "dev", metadata: new).isCompatible)
    }

    @Test("the status says what the owner can do, in order of what blocks most")
    func status() {
        #expect(SyncStatus.derive(signedIn: false, package: nil, outbox: .idle, waiting: 3, known: 1) == .local)
        #expect(SyncStatus.derive(signedIn: true, package: nil, outbox: .idle, waiting: 0, known: 1) == .preparing)
        #expect(SyncStatus.derive(signedIn: true, package: .current(held: 1), outbox: .signedOut, waiting: 2, known: 1) == .signedOut)
        #expect(SyncStatus.derive(signedIn: true, package: .missing, outbox: .needsApproval, waiting: 0, known: 1) == .needsApproval)
        #expect(SyncStatus.derive(signedIn: true, package: .newer(held: 2), outbox: .sent, waiting: 5, known: 1) == .packageNewer(held: 2, known: 1))
        #expect(SyncStatus.derive(signedIn: true, package: .current(held: 1), outbox: .sent, waiting: 0, known: 1) == .ready(waiting: 0))
        #expect(SyncStatus.derive(signedIn: true, package: .current(held: 1), outbox: .offline("x"), waiting: 4, known: 1) == .offline(waiting: 4, reason: "x"))
        #expect(SyncStatus.packageNewer(held: 2, known: 1).sentence.contains("Update the app"))
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
