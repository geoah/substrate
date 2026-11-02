import Foundation
import SubstrateKit
import Testing
import WorkoutsCore
@testable import WorkoutsSync

/// The app's whole path against a real substrate, skipped unless
/// `WORKOUTS_E2E_SERVER` names one and `WORKOUTS_E2E_OWNER_TOKEN` holds a
/// full-access token of a repository with no training package: the
/// authorization request, approved by the owner through the approval API the
/// console's page calls (which installs the health catalogue), the code
/// exchanged with its PKCE verifier, then the app's own package declared,
/// the owner's edit to it refused and the package taken over, after which
/// the app applies nothing and its records still go: the sample routine
/// written on the first run, deleted through the outbox and not written
/// again, a finished session and its sets written through the outbox, and
/// all of it read back with the owner's token.
///
///     WORKOUTS_E2E_SERVER=http://localhost:18087 WORKOUTS_E2E_OWNER_TOKEN=… swift test --filter LiveServer
@Suite("Live server", .enabled(if: ProcessInfo.processInfo.environment["WORKOUTS_E2E_SERVER"] != nil))
struct LiveServerTests {
    let server = URL(string: ProcessInfo.processInfo.environment["WORKOUTS_E2E_SERVER"] ?? "http://localhost")!
    let ownerToken = ProcessInfo.processInfo.environment["WORKOUTS_E2E_OWNER_TOKEN"] ?? ""
    let redirect = "http://127.0.0.1:53111/oauth/callback"

    @Test("sign in, approve, install the package, write a session, read it back")
    func theWholePath() async throws {
        let manifest = WorkoutsManifest.manifest(clientID: WorkoutsManifest.localClientID)
        let probe = try await ServerProbe().probe(server, requirement: manifest.requirement, app: manifest.appName)
        #expect(manifest.door(for: probe) == .app, "the server cannot take the request: \(probe.verdict)")
        let metadata = try #require(probe.metadata)
        let oauth = OAuthClient(manifest: manifest, metadata: metadata)
        let pkce = try PKCE()
        let state = try PKCE.state()

        // The browser's first stop parks the request and sends it to the
        // console's approval page.
        let authorize = try oauth.authorizeURL(redirect: redirect, pkce: pkce, state: state)
        var request = URLRequest(url: authorize)
        request.httpMethod = "GET"
        let (_, parked) = try await URLSessionTransport.shared.data(for: request)
        let location = try #require(parked.value(forHTTPHeaderField: "Location"))
        let id = try #require(location.split(separator: "/").last.map { String($0.split(separator: "?").first ?? $0) })

        // The owner reads the request as the page does, and approves every
        // row with the set up it plans.
        let owner = SubstrateClient(server: server, token: ownerToken)
        let view = try await owner.send("GET", "api/v1/authorizations/\(id)").data
        let page = try JSONDecoder().decode(WorkoutsCore.JSONValue.self, from: view)
        guard case let .object(fields) = page, case let .array(scopes)? = fields["scopes"] else { Issue.record("no scopes"); return }
        let approved = scopes.compactMap { scope -> WorkoutsCore.JSONValue? in
            guard case let .object(row) = scope else { return nil }
            return row["scope"]
        }
        #expect(approved.contains(.string("declare:\(KindNames.packageID(authority: try repository(owner)))")))
        var approval: JSONObject = ["scopes": .array(approved), "lifetimeDays": .number(7)]
        if let hash = fields["planHash"] { approval["planHash"] = hash }
        let encoder = JSONEncoder()
        encoder.outputFormatting = .withoutEscapingSlashes
        var answer = try await owner.send("POST", "api/v1/authorizations/\(id)",
                                          body: .json(encoder.encode(WorkoutsCore.JSONValue.object(approval))),
                                          timeout: SubstrateClient.Timeout.serverWork).data
        var next = redirectOf(answer)
        // The set up runs before the code exists; the page reads the request
        // until it is approved.
        for _ in 0 ..< 120 where next == nil {
            try await Task.sleep(for: .milliseconds(500))
            answer = try await owner.send("GET", "api/v1/authorizations/\(id)").data
            next = redirectOf(answer)
        }
        let back = try #require(next.flatMap(URLComponents.init(string:)))
        let query = Dictionary(uniqueKeysWithValues: (back.queryItems ?? []).map { ($0.name, $0.value ?? "") })
        let code = try oauth.callback(query, state: state)
        let session = try await oauth.session(exchanging: code, redirect: redirect, pkce: pkce, server: server)
        #expect(session.isApp)
        #expect(session.scopes?.contains("declare:\(session.repository)/training") == true)

        // The approval installed health; the app now keeps its own package.
        let api = WorkoutsAPI(client: SubstrateClient(session), authority: session.repository)
        let baseline = temporaryDirectory()
        let installer = PackageInstaller(api: api, manifest: try PackageManifest.bundled(), baselineDirectory: baseline)
        #expect(try await installer.state() == .missing)
        let installed = try await installer.ensure()
        #expect(installed == .current(held: installer.manifest.version))
        #expect(try await installer.ensure() == installed)
        #expect(PackageBaseline.load(from: baseline)?.kinds.count == 6)

        // The next build's upgrade passes the server's additions-only guard
        // over the real stored package.
        let shipped = installer.manifest.version
        let upgrade = PackageInstaller(api: api, manifest: bumped(installer.manifest, to: shipped + 1), baselineDirectory: baseline)
        #expect(try await upgrade.ensure() == .current(held: shipped + 1))

        // The owner's edit to the app's kinds is refused: the package is the
        // app's (decision 0205). The owner takes it over instead, and the
        // app's next check applies nothing and keeps writing records, which
        // the rest of this path does.
        let ownerAPI = WorkoutsAPI(client: owner, authority: session.repository)
        var setKind = try #require(upgrade.manifest.documents(for: api.rehome).first { document in
            guard case let .object(object) = document, case let .object(metadata)? = object["metadata"] else { return false }
            return metadata["id"] == .string("\(session.repository)/training/set")
        })
        if case var .object(object) = setKind, case var .object(data)? = object["data"], case var .object(properties)? = data["properties"] {
            let note: JSONObject = ["type": .string("string"), "description": .string("the owner's own")]
            properties["ownersNote"] = WorkoutsCore.JSONValue.object(note)
            data["properties"] = WorkoutsCore.JSONValue.object(properties)
            object["data"] = WorkoutsCore.JSONValue.object(data)
            setKind = WorkoutsCore.JSONValue.object(object)
        }
        do {
            try await ownerAPI.applyVocabulary([setKind])
            Issue.record("the owner's edit to the app's kind landed")
        } catch let error as ServerError {
            #expect(error.status == 403 && error.message.contains("managed by"))
        }
        let packageID = KindNames.packageID(authority: session.repository)
        let takeover: JSONObject = ["package": .string(packageID), "confirm": .string(packageID)]
        _ = try await owner.send("POST", "api/v1/vocabulary/takeover", body: .json(encoder.encode(WorkoutsCore.JSONValue.object(takeover))))
        let third = PackageInstaller(api: api, manifest: bumped(installer.manifest, to: shipped + 2), baselineDirectory: baseline,
                                     clientID: manifest.clientID)
        #expect(try await third.ensure() == .managedElsewhere(held: shipped + 1, by: .owner(takenOverFrom: manifest.clientID)))
        try await ownerAPI.applyVocabulary([setKind])

        // The first run writes the sample routine and its exercises.
        let directory = temporaryDirectory()
        let remote = RemoteCatalog(directory: directory, writingOptions: .atomic)
        let sample = SampleInstall(directory: directory, writingOptions: .atomic)
        let bundled = try BundledCatalog().loadNow()
        #expect(try await remote.refresh(api).routines.isEmpty)
        #expect(try await sample.firstRun(api, remote: remote, bundled: bundled))
        let catalog = try await remote.refresh(api)
        #expect(catalog.routines.keys.map(\.rawValue) == [SampleRoutine.id.rawValue])
        #expect(catalog.exercises.count == 13)

        // Deleted through the outbox: gone from the server, its exercises stay.
        let outbox = Outbox(directory: directory, writingOptions: .atomic)
        let codec = RecordCodec()
        try await outbox.delete(routine: SampleRoutine.id, codec: codec)
        #expect(await outbox.flush(api) == .sent)
        #expect(try await ownerAPI.get(codec.kinds.routine, SampleRoutine.id.rawValue) == nil)
        let afterDelete = try await remote.refresh(api)
        #expect(afterDelete.routines.isEmpty && afterDelete.exercises.count == 13)
        #expect(try await !sample.firstRun(api, remote: remote, bundled: bundled))
        #expect(sample.offersSample(afterDelete))

        // A second phone, and this one reinstalled: neither writes it again.
        for _ in 0 ..< 2 {
            let other = temporaryDirectory()
            let fresh = SampleInstall(directory: other, writingOptions: .atomic)
            #expect(try await !fresh.firstRun(api, remote: RemoteCatalog(directory: other, writingOptions: .atomic), bundled: bundled))
            #expect(fresh.state == .installed)
        }
        #expect(try await ownerAPI.get(codec.kinds.routine, SampleRoutine.id.rawValue) == nil)

        // Added back, then a session logged signed out on the sample whose
        // exercises the repository lacks (the owner purged two): the outbox
        // writes them before the session, and the server's mustExist takes it.
        try await sample.addBack(api, remote: remote, outbox: outbox, bundled: bundled)
        #expect(try await ownerAPI.get(codec.kinds.routine, SampleRoutine.id.rawValue) != nil)

        // Edited on the phone: renamed, a group's members reordered, an
        // exercise taken out of its group, a set's reps changed, a set added.
        // A guarded PATCH of what changed; read back, the server holds the
        // edit and nothing else moved.
        let before = try #require(try await ownerAPI.get(codec.kinds.routine, SampleRoutine.id.rawValue))
        let read = try #require(try await remote.refresh(api).routines[SampleRoutine.id])
        var editor = RoutineEditor(read)
        editor.rename("Sample, edited on the phone")
        let group = try #require(editor.routine.groups.first?.key)
        let members = editor.members(of: group)
        editor.moveMembers(of: group, from: [0], to: members.count)
        let firstSlot = try #require(editor.routine.slots.first { $0.sets.contains { $0.repsMax != nil } })
        let firstSet = try #require(firstSlot.sets.first { $0.repsMax != nil })
        editor.updateSet(firstSlot.key, firstSet.key) { $0.repsMax = ($0.repsMax ?? 0) + 2 }
        editor.addSet(to: firstSlot.key)
        try await outbox.edit(routine: SampleRoutine.id, patch: editor.patch(codec: codec), baseVersion: editor.baseVersion, codec: codec)
        #expect(await outbox.flush(api) == .sent)
        let edited = try #require(try await ownerAPI.get(codec.kinds.routine, SampleRoutine.id.rawValue))
        #expect(edited.version == (before.version ?? 0) + 1)
        var expected = editor.routine
        expected.version = edited.version
        #expect(try codec.decodeRoutine(edited) == expected, "the server's routine is not the phone's edit")
        #expect(edited.properties["notes"] == before.properties["notes"])

        // The coach changes the notes meanwhile; the phone's next edit was
        // made on the version before. It is parked as a conflict, the coach's
        // change stands, and Keep My Edit sends it over without a version:
        // the name is the phone's, the notes the coach's.
        var stale = RoutineEditor(try codec.decodeRoutine(edited))
        stale.rename("Renamed against a stale version")
        let coachNotes = "The coach's notes."
        try await ownerAPI.write("PATCH", codec.kinds.routine, SampleRoutine.id.rawValue, properties: ["notes": .string(coachNotes)],
                                 ifVersion: edited.version)
        try await outbox.edit(routine: SampleRoutine.id, patch: stale.patch(codec: codec), baseVersion: stale.baseVersion, codec: codec)
        guard case .refused = await outbox.flush(api) else { Issue.record("a stale edit was not refused"); return }
        let conflict = try #require(await outbox.parked.first { $0.conflict })
        let coached = try #require(try await ownerAPI.get(codec.kinds.routine, SampleRoutine.id.rawValue))
        #expect(coached.properties["name"] == .string("Sample, edited on the phone"))
        #expect(coached.properties["notes"] == .string(coachNotes))
        try await outbox.resolve(conflict.id, keepMine: true)
        #expect(await outbox.flush(api) == .sent)
        let kept = try #require(try await ownerAPI.get(codec.kinds.routine, SampleRoutine.id.rawValue))
        #expect(kept.properties["name"] == .string("Renamed against a stale version"))
        #expect(kept.properties["notes"] == .string(coachNotes))

        // Duplicated: a new routine, whole.
        let copy = RoutineEditor.duplicate(try codec.decodeRoutine(kept), taken: [SampleRoutine.id])
        try await outbox.enqueue([OutboxPlan.routineCreated(copy.routine, codec: codec)])
        #expect(await outbox.flush(api) == .sent)
        let duplicated = try #require(try await ownerAPI.get(codec.kinds.routine, copy.routine.id.rawValue))
        #expect(try codec.decodeRoutine(duplicated).slots == copy.routine.slots)
        print("live: routine edited at version \(edited.version ?? 0), conflict kept over the coach's notes, duplicated as \(copy.routine.id)")
        for id in ["bulgarian-split-squat-dumbbell", "triceps-pushdown"] {
            try await owner.send("DELETE", SubstrateClient.recordPath("\(session.repository)/training/exercise", id), query: [("purge", "true")])
        }
        let rehomed = Rehome(authority: recordedAuthority)
        func recorded(_ name: String) throws -> [FlatRecord] {
            try JSONDecoder().decode(WorkoutsAPI.Page.self, from: Fixtures.data(name)).records.map { try WorkoutsAPI.record($0, rehome: rehomed) }
        }
        var signedOut = try #require(codec.sessionRecords(from: recorded("list-sessions") + recorded("list-sets")).first)
        let onto: [ExerciseID: ExerciseID] = ["romanian-deadlift-dumbbell": "bulgarian-split-squat-dumbbell", "seated-row-machine": "triceps-pushdown"]
        signedOut.session.id = SessionID(UUIDv7.make(at: signedOut.session.at))
        for index in signedOut.session.slots.indices {
            signedOut.session.slots[index].exercise = onto[signedOut.session.slots[index].exercise] ?? signedOut.session.slots[index].exercise
        }
        var renamed: [SetID: SetID] = [:]
        for set in signedOut.sets { renamed[set.id] = SetID(UUIDv7.make(at: set.at)) }
        for index in signedOut.sets.indices {
            signedOut.sets[index].exercise = onto[signedOut.sets[index].exercise] ?? signedOut.sets[index].exercise
            signedOut.sets[index].session = signedOut.session.id
            signedOut.sets[index].id = renamed[signedOut.sets[index].id]!
            signedOut.sets[index].continues = signedOut.sets[index].continues.map { renamed[$0] ?? $0 }
        }
        let held = Set(try await api.list([codec.kinds.exercise]).map(\.id))
        let missingFirst = OutboxPlan.exercisesFor([signedOut], server: held, known: bundled.exercises, codec: codec)
        #expect(Set(missingFirst.map(\.recordID)) == ["bulgarian-split-squat-dumbbell", "triceps-pushdown"])
        try await outbox.enqueue(missingFirst + OutboxPlan.finished(signedOut, codec: codec))
        let signedOutSent = await outbox.flush(api)
        let signedOutParked = await outbox.parked.map(\.lastError)
        #expect(signedOutSent == .sent, "refused: \(signedOutParked)")
        #expect(try await ownerAPI.get(codec.kinds.session, signedOut.session.id.rawValue) != nil)

        // The recorded session's exercises the sample does not carry, as the
        // phone adds them.
        let missing = [("romanian-deadlift-dumbbell", "Romanian Deadlift (Dumbbell)"), ("seated-row-machine", "Seated Row (Machine)")]
        try await outbox.enqueue(missing.map { OutboxPlan.exerciseAdded(Exercise(id: ExerciseID($0.0), name: $0.1, measurement: .weightreps), codec: codec) })
        #expect(await outbox.flush(api) == .sent)

        // A finished session and its 20 sets, four of them drops, through
        // the outbox.
        let rehome = Rehome(authority: recordedAuthority)
        func records(_ name: String) throws -> [FlatRecord] {
            try JSONDecoder().decode(WorkoutsAPI.Page.self, from: Fixtures.data(name)).records.map { try WorkoutsAPI.record($0, rehome: rehome) }
        }
        let finished = try #require(codec.sessionRecords(from: records("list-sessions") + records("list-sets")).first)
        try await outbox.enqueue(OutboxPlan.finished(finished, codec: codec))
        #expect(await outbox.flush(api) == .sent)

        // The next exchange finds nothing to do: every record is at the
        // version the outbox saw it written at. Then the owner deletes the
        // session on the server, and the phone reads it as theirs to delete.
        var snapshot = try await ServerHistory(api: api, codec: codec).snapshot()
        let quiet = HistoryExchange.plan(server: snapshot, local: [finished, signedOut], queued: [], versions: await outbox.versions, tombstones: [], codec: codec)
        #expect(quiet.isEmpty, "a session the phone just wrote read as changed: \(quiet)")
        try await owner.send("DELETE", SubstrateClient.recordPath("\(session.repository)/training/session", finished.session.id.rawValue), query: [("purge", "true")])
        snapshot = try await ServerHistory(api: api, codec: codec).snapshot()
        let deleted = HistoryExchange.plan(server: snapshot, local: [finished, signedOut], queued: [], versions: await outbox.versions, tombstones: [], codec: codec)
        #expect(deleted.removals == [finished.id] && deleted.uploads.isEmpty)
        try await outbox.enqueue(OutboxPlan.finished(finished, codec: codec))
        #expect(await outbox.flush(api) == .sent)

        // Read back as the owner: the session finished, every set there.
        let stored = try #require(try await ownerAPI.get(codec.kinds.session, finished.session.id.rawValue))
        #expect(stored.properties["status"] == .string("finished"))
        let sets = try await ownerAPI.list([codec.kinds.set])
        #expect(sets.count == finished.sets.count + signedOut.sets.count)
        print("live: \(session.repository) holds \(session.repository)/training at version \(shipped + 1), " +
            "the sample written and deleted, session \(finished.session.id.rawValue) finished with \(sets.count) sets")
    }

    /// The manifest as a later build would ship it: the package document at
    /// `version`, nothing else changed.
    private func bumped(_ manifest: PackageManifest, to version: Int) -> PackageManifest {
        var out = manifest
        out.version = version
        out.documents = manifest.documents.map { document in
            guard case var .object(object) = document, object["kind"] == .string("substrate.reamde.dev/core/package"),
                  case var .object(data)? = object["data"] else { return document }
            data["version"] = WorkoutsCore.JSONValue.number(Decimal(version))
            object["data"] = WorkoutsCore.JSONValue.object(data)
            return WorkoutsCore.JSONValue.object(object)
        }
        return out
    }

    private func repository(_ owner: SubstrateClient) throws -> String {
        ProcessInfo.processInfo.environment["WORKOUTS_E2E_REPOSITORY"] ?? "ada.example.com"
    }

    private func redirectOf(_ data: Data) -> String? {
        guard case let .object(object)? = try? JSONDecoder().decode(WorkoutsCore.JSONValue.self, from: data) else { return nil }
        for key in ["redirect", "next"] {
            if case let .string(url)? = object[key], !url.isEmpty { return url }
        }
        return nil
    }
}
