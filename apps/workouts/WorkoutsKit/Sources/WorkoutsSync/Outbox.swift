import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateDevice
import WorkoutsCore
import SubstrateCore

/// One write waiting for the server, in the phone's spelling.
public struct OutboxEntry: Codable, Sendable, Hashable, Identifiable {
    public enum Method: String, Codable, Sendable {
        case put = "PUT"
        case patch = "PATCH"
        /// `DELETE ?purge=true`: a discarded or deleted session, or a
        /// deleted routine.
        case purge = "DELETE"
    }

    public var id: UUID
    public var method: Method
    public var kind: String
    public var recordID: String
    public var properties: JSONObject?
    /// Sent with `ifVersion`: the version the outbox last saw the record at,
    /// read from the server when it has none.
    public var guarded: Bool
    /// The session the write belongs to, which a discard drops and a refusal
    /// holds back with it.
    public var session: String?
    public var attempts = 0
    /// The server's words for the last refusal, kept while the entry waits.
    public var lastError: String?
    /// Refused by the server for itself: the same write would be refused
    /// again, so it is not sent until the owner retries it, and the rest of
    /// its session waits behind it. The owner may discard it instead.
    public var parked = false
    /// An edit the owner made on what they saw at this version (a routine
    /// edit): sent with it as `ifVersion`, or with the version the app's
    /// own last write of the record answered when that is newer. A `409`
    /// is never merged over: the entry is parked as a `conflict` for the
    /// owner to choose.
    public var baseVersion: Int?
    /// Parked because the record changed on the server since `baseVersion`.
    public var conflict = false

    public init(method: Method, kind: String, recordID: String, properties: JSONObject? = nil, guarded: Bool = false,
                session: String? = nil, baseVersion: Int? = nil)
    {
        id = UUID()
        self.method = method
        self.kind = kind
        self.recordID = recordID
        self.properties = properties
        self.guarded = guarded
        self.session = session
        self.baseVersion = baseVersion
    }

    var key: String { "\(kind)/\(recordID)" }

    enum CodingKeys: String, CodingKey {
        case id, method, kind, recordID, properties, guarded, session, attempts, lastError, parked, baseVersion, conflict
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(UUID.self, forKey: .id)
        method = try c.decode(Method.self, forKey: .method)
        kind = try c.decode(String.self, forKey: .kind)
        recordID = try c.decode(String.self, forKey: .recordID)
        properties = try c.decodeIfPresent(JSONObject.self, forKey: .properties)
        guarded = try c.decodeIfPresent(Bool.self, forKey: .guarded) ?? false
        session = try c.decodeIfPresent(String.self, forKey: .session)
        attempts = try c.decodeIfPresent(Int.self, forKey: .attempts) ?? 0
        lastError = try c.decodeIfPresent(String.self, forKey: .lastError)
        // A file from before parking held every refusal and retried it.
        parked = try c.decodeIfPresent(Bool.self, forKey: .parked) ?? (lastError != nil)
        baseVersion = try c.decodeIfPresent(Int.self, forKey: .baseVersion)
        conflict = try c.decodeIfPresent(Bool.self, forKey: .conflict) ?? false
    }
}

/// What the outbox writes for each change, in the order the server needs
/// (the package's write rules, docs in SubstrateWorkouts/README.md):
///
/// - an exercise before anything names it;
/// - a session's create before its sets, since `set.session` must exist, and
///   never with `status`, since a create replayed after Finish would move the
///   state back;
/// - a chain's head before its members, since `set.continues` must exist;
/// - Finish and the Health link as patches under `ifVersion`, each raising
///   `revision`, so two writes never share one.
public enum OutboxPlan {
    /// The keys Finish moves, which the create leaves out or the patch sets.
    static let finishKeys = ["status", "endsAt", "utcOffsetEnd", "sessionRPE", "slots"]

    public static func finished(_ records: SessionRecords, codec: RecordCodec) -> [OutboxEntry] {
        let session = codec.encode(records.session)
        let sessionID = records.session.id.rawValue
        let revision = max(records.session.revision ?? 1, 1)
        var create = session.properties
        create["status"] = nil
        create["revision"] = .number(Decimal(revision))
        var out = [OutboxEntry(method: .put, kind: session.kind, recordID: sessionID, properties: create, session: sessionID)]
        for set in headsFirst(records.sets) {
            let record = codec.encode(set)
            out.append(OutboxEntry(method: .put, kind: record.kind, recordID: record.id, properties: record.properties, session: sessionID))
        }
        out.append(finish(records, revision: revision + 1, codec: codec))
        return out
    }

    /// Finish alone, at a revision: the patch that moves a session the
    /// server holds unfinished to finished.
    public static func finish(_ records: SessionRecords, revision: Int, codec: RecordCodec) -> OutboxEntry {
        let session = codec.encode(records.session)
        var finish = JSONObject()
        for key in finishKeys {
            if let value = session.properties[key] { finish[key] = value }
        }
        finish["status"] = .string(SessionStatus.finished.rawValue)
        finish["revision"] = .number(Decimal(revision))
        return OutboxEntry(method: .patch, kind: session.kind, recordID: records.session.id.rawValue, properties: finish, guarded: true,
                           session: records.session.id.rawValue)
    }

    /// The Health save's workout UUID on the session, a revision above
    /// Finish's.
    public static func healthLinked(_ session: SessionID, healthKitWorkout: String, revision: Int, codec: RecordCodec) -> OutboxEntry {
        OutboxEntry(method: .patch, kind: codec.kinds.session, recordID: session.rawValue,
                    properties: ["healthKitWorkout": .string(healthKitWorkout), "revision": .number(Decimal(revision + 2))],
                    guarded: true, session: session.rawValue)
    }

    public static func exerciseAdded(_ exercise: Exercise, codec: RecordCodec) -> OutboxEntry {
        var local = exercise
        local.isLocal = false
        let record = codec.encode(local)
        return OutboxEntry(method: .put, kind: record.kind, recordID: record.id, properties: record.properties)
    }

    public static func deleted(_ session: SessionID, codec: RecordCodec) -> OutboxEntry {
        OutboxEntry(method: .purge, kind: codec.kinds.session, recordID: session.rawValue, guarded: true, session: session.rawValue)
    }

    /// A routine deleted on the phone. Purged, so the same id written again
    /// (the sample added back) starts a fresh record. Unguarded: the delete
    /// is the owner's, whatever the agent wrote since.
    public static func routineDeleted(_ routine: RoutineID, codec: RecordCodec) -> OutboxEntry {
        OutboxEntry(method: .purge, kind: codec.kinds.routine, recordID: routine.rawValue)
    }

    /// A routine edited on the phone: the top-level properties the edit
    /// changed (`RoutineEditor.patch`), as a PATCH under the version the
    /// owner edited.
    public static func routineEdited(_ routine: RoutineID, patch: JSONObject, baseVersion: Int?, codec: RecordCodec) -> OutboxEntry {
        OutboxEntry(method: .patch, kind: codec.kinds.routine, recordID: routine.rawValue, properties: patch, guarded: true,
                    baseVersion: baseVersion ?? 0)
    }

    /// A new routine (a duplicate), written whole at an id nothing holds:
    /// `ifVersion: 0`, so a routine another writer made at the same id since
    /// is refused, never merged into.
    public static func routineCreated(_ routine: Routine, codec: RecordCodec) -> OutboxEntry {
        let record = codec.encode(routine)
        return OutboxEntry(method: .put, kind: record.kind, recordID: record.id, properties: record.properties, guarded: true,
                           baseVersion: 0)
    }

    /// The exercises the outbox must write before `uploads`: every one an
    /// upload names (a slot or a set) that the repository lacks, from
    /// `known` (the phone's exercises, then the ones this build carries), in
    /// id order. A session logged signed out on the sample names the
    /// sample's exercises, and a repository that never got the sample would
    /// refuse it: a session's and a set's exercise must exist. An id no
    /// catalogue knows is left to the server's refusal, parked with its words.
    public static func exercisesFor(_ uploads: [SessionRecords], server: Set<String>, known: [ExerciseID: Exercise],
                                    codec: RecordCodec) -> [OutboxEntry]
    {
        var named = Set<ExerciseID>()
        for records in uploads {
            named.formUnion(records.session.slots.map(\.exercise))
            named.formUnion(records.sets.map(\.exercise))
        }
        return named.filter { !server.contains($0.rawValue) }
            .sorted { $0.rawValue < $1.rawValue }
            .compactMap { known[$0].map { exerciseAdded($0, codec: codec) } }
    }

    /// The sets in their order, each chain member after the set it continues.
    static func headsFirst(_ sets: [PerformedSet]) -> [PerformedSet] {
        let ids = Set(sets.map(\.id))
        var placed = Set<SetID>()
        var out: [PerformedSet] = []
        var waiting = sets.sorted { ($0.at, $0.order) < ($1.at, $1.order) }
        while !waiting.isEmpty {
            let before = waiting.count
            waiting.removeAll { set in
                guard let head = set.continues, ids.contains(head), !placed.contains(head) else {
                    out.append(set)
                    placed.insert(set.id)
                    return true
                }
                return false
            }
            if waiting.count == before {
                // A cycle no engine writes: send the rest as they are, and let
                // the server refuse what does not hold.
                out += waiting
                break
            }
        }
        return out
    }
}

/// How the last flush ended.
public enum OutboxState: Sendable, Hashable {
    case idle
    /// Every write that could go landed.
    case sent
    /// The server could not be reached; nothing is lost.
    case offline(String)
    /// The token was refused: sign in again.
    case signedOut
    /// The token lacks a scope the write needs.
    case needsApproval
    /// The server refused one or more writes, which wait, parked, with its
    /// words until the owner retries or discards them.
    case refused(String)
}

/// The outbox could not keep a change on disk: nothing was queued.
public struct OutboxNotSaved: LocalizedError, Sendable {
    public let reason: String
    public init(reason: String) { self.reason = reason }
    public var errorDescription: String? { "The change could not be kept on this iPhone: \(reason)" }
}

/// The writes waiting for the server, kept in `outbox.json` beside the
/// journal so a write made in the gym survives a relaunch, and sent in order
/// when the server answers. The file also keeps, for one repository, the
/// version each record was last written or read at, which is how the app
/// tells a session it uploaded (and the server since deleted) from one it
/// never sent.
///
/// The actor is reentrant at every network call, so the queue may change
/// while a write is in flight: an entry is found again by its id after each
/// call, never by its position, and `reset` bumps a generation the flush
/// checks before it touches anything.
public actor Outbox {
    /// The file's `format`: 2 since the package was published. A format 1
    /// file, from a build that spelled the package `~/training` on the
    /// phone, is read through `LegacyKinds`, every entry's kind and
    /// properties and every version and chain key, and written back once at
    /// this format.
    public static let fileFormat = 2

    struct File: Codable {
        var format = Outbox.fileFormat
        var entries: [OutboxEntry] = []
        /// The version each record was last written or read at.
        var versions: [String: Int] = [:]
        /// The app's own guarded edits that landed, per record: the version
        /// each one asserted and the version it made. An edit made on a
        /// version the app itself then moved is sent at the end of that
        /// chain; anything another writer did breaks it, so the edit meets
        /// a conflict instead of writing over it.
        var chains: [String: [String: Int]] = [:]

        init() {}

        enum CodingKeys: String, CodingKey { case format, entries, versions, chains }

        init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            format = try c.decodeIfPresent(Int.self, forKey: .format) ?? 1
            entries = try c.decodeIfPresent([OutboxEntry].self, forKey: .entries) ?? []
            versions = try c.decodeIfPresent([String: Int].self, forKey: .versions) ?? [:]
            chains = try c.decodeIfPresent([String: [String: Int]].self, forKey: .chains) ?? [:]
            guard format < Outbox.fileFormat else { return }
            for index in entries.indices {
                entries[index].kind = LegacyKinds.respell(entries[index].kind)
                entries[index].properties = entries[index].properties.map(LegacyKinds.respell)
            }
            versions = Dictionary(versions.map { (LegacyKinds.respell($0.key), $0.value) }, uniquingKeysWith: { first, _ in first })
            chains = Dictionary(chains.map { (LegacyKinds.respell($0.key), $0.value) }, uniquingKeysWith: { first, _ in first })
            format = Outbox.fileFormat
            respelled = true
        }

        /// Whether the file was read from a format before this one, through
        /// `LegacyKinds`; never written.
        var respelled = false

        /// The version an edit made on `base` asserts: `base`, moved along
        /// the app's own writes since.
        func asserted(_ key: String, from base: Int) -> Int {
            var version = base
            var seen = Set<Int>()
            while let next = chains[key]?[String(version)], seen.insert(version).inserted {
                version = next
            }
            return version
        }
    }

    public static let fileName = "outbox.json"

    public let url: URL
    private let writingOptions: Data.WritingOptions
    private var file: File
    private var flushing = false
    /// Moved by `reset`: a flush that finds it moved after a call stops.
    private var generation = 0
    /// The entry whose write is on the network.
    private var inFlight: OutboxEntry?
    /// When the flush last saved after a landed write, and whether a landed
    /// write since is kept in memory only.
    private var lastLandedSave: ContinuousClock.Instant?
    private var landedUnsaved = false
    public private(set) var state: OutboxState = .idle
    /// The file that was there at launch and could not be read, set aside.
    public private(set) var unreadable: UnreadableRecord?
    /// Why the last save failed, until one succeeds.
    public private(set) var saveProblem: String?

    public init(directory: URL, writingOptions: Data.WritingOptions = WorkoutsFiles.protectedWriting, now: Date = Date()) {
        url = directory.appendingPathComponent(Self.fileName)
        self.writingOptions = writingOptions
        guard let data = try? Data(contentsOf: url) else {
            file = File()
            return
        }
        do {
            file = try JSONDecoder().decode(File.self, from: data)
            // A file from before the package was published is written back
            // once in the published spelling, best effort: a write that
            // fails reads the same way at the next launch.
            if file.respelled { try? Self.encoded(file).write(to: url, options: writingOptions) }
        } catch {
            // Never an empty queue over writes nobody can read: the file is
            // kept beside, and said so.
            file = File()
            let aside = try? WorkoutsFiles.setAside(url, at: now)
            unreadable = UnreadableRecord(source: aside?.lastPathComponent ?? Self.fileName,
                                          reason: "The changes waiting for your substrate could not be read: \(error)")
        }
    }

    public var entries: [OutboxEntry] { file.entries }

    /// The entries the server refused, waiting for the owner.
    public var parked: [OutboxEntry] { file.entries.filter(\.parked) }

    /// The version a record was last written or read at, nil when this
    /// repository was never seen to hold it.
    public func version(of kind: String, _ id: String) -> Int? { file.versions["\(kind)/\(id)"] }

    public var versions: [String: Int] { file.versions }

    /// Queues the writes, kept on disk before it returns; on a failed save
    /// nothing is queued and the error says why.
    public func enqueue(_ entries: [OutboxEntry]) throws {
        try change { $0.entries += entries }
    }

    /// Records the versions the server answered for records read rather than
    /// written: an imported or merged session and its sets.
    public func saw(_ seen: [String: Int]) throws {
        guard !seen.isEmpty else { return }
        try change { file in
            for (key, version) in seen { file.versions[key] = version }
        }
    }

    /// Forgets a record this repository no longer holds: a session the
    /// server deleted, removed from the phone too.
    public func forget(_ kind: String, _ id: String) throws {
        try change { $0.versions["\(kind)/\(id)"] = nil }
    }

    /// A session discarded before it finished: its waiting writes dropped
    /// and, when the server holds it or may once a write in flight lands, its
    /// purge queued, in one save. Answers whether a purge was queued.
    @discardableResult
    public func discard(session: SessionID, codec: RecordCodec) throws -> Bool {
        let held = file.versions["\(codec.kinds.session)/\(session.rawValue)"] != nil || inFlight?.session == session.rawValue
        try change { file in
            file.entries.removeAll { $0.session == session.rawValue }
            if held { file.entries.append(OutboxPlan.deleted(session, codec: codec)) }
        }
        return held
    }

    /// Drops a session's waiting writes and queues its purge in one save:
    /// a session deleted on the phone.
    public func purge(session: SessionID, codec: RecordCodec) throws {
        try change { file in
            file.entries.removeAll { $0.session == session.rawValue }
            file.entries.append(OutboxPlan.deleted(session, codec: codec))
        }
    }

    /// The records whose delete waits, as `kind/id`, outside any session:
    /// what a catalogue read leaves out until the delete lands.
    public var waitingDeletes: Set<String> {
        Set(file.entries.filter { $0.method == .purge && $0.session == nil }.map(\.key))
    }

    /// A routine deleted on the phone: any write of it still waiting dropped
    /// and its purge queued, in one save.
    public func delete(routine: RoutineID, codec: RecordCodec) throws {
        let entry = OutboxPlan.routineDeleted(routine, codec: codec)
        try change { file in
            file.entries.removeAll { $0.session == nil && $0.key == entry.key }
            file.entries.append(entry)
        }
    }

    /// Takes back a routine's delete that has not gone yet: the sample added
    /// back before its delete reached the server.
    public func withdrawDelete(routine: RoutineID, codec: RecordCodec) throws {
        let key = OutboxPlan.routineDeleted(routine, codec: codec).key
        try change { file in
            file.entries.removeAll { $0.method == .purge && $0.session == nil && $0.key == key && $0.id != inFlight?.id }
        }
    }

    /// Returns once no delete of the routine is on the wire, or after
    /// `limit`: a purge already sent cannot be taken back, so what follows it
    /// waits for it to land.
    public func settle(routine: RoutineID, codec: RecordCodec, limit: Duration = .seconds(30)) async {
        let key = OutboxPlan.routineDeleted(routine, codec: codec).key
        let clock = ContinuousClock()
        let deadline = clock.now + limit
        while inFlight?.key == key, inFlight?.method == .purge, clock.now < deadline {
            try? await Task.sleep(for: .milliseconds(50))
        }
    }

    /// A routine edit, kept on disk before it returns. The last write of
    /// the same routine, when it is a guarded edit or create still waiting
    /// (not on the wire, not parked), takes this one on top, so edits made
    /// offline go as one write under the first one's version and a
    /// duplicate not sent yet takes the edit into its body. Anything else (a
    /// parked conflict or refusal, a Keep My Edit going without a version)
    /// is left alone, and the edit queues behind it under its own version.
    public func edit(routine: RoutineID, patch: JSONObject, baseVersion: Int?, codec: RecordCodec) throws {
        let entry = OutboxPlan.routineEdited(routine, patch: patch, baseVersion: baseVersion, codec: codec)
        guard !patch.isEmpty else { return }
        try change { file in
            if let index = file.entries.lastIndex(where: { $0.session == nil && $0.key == entry.key }),
               file.entries[index].id != inFlight?.id, !file.entries[index].parked,
               file.entries[index].baseVersion != nil, file.entries[index].method != .purge
            {
                var merged = file.entries[index].properties ?? JSONObject()
                for (key, value) in patch {
                    if file.entries[index].method == .put, value == .null { merged[key] = nil } else { merged[key] = value }
                }
                file.entries[index].properties = merged
                return
            }
            file.entries.append(entry)
        }
    }

    /// The routines whose writes wait, as the properties they will set,
    /// merged in queue order (`null` clears), and whether the write creates
    /// the routine: what a catalogue read lays over the server's answer
    /// until they land.
    public func waitingRoutines(codec: RecordCodec) -> [String: (creates: Bool, properties: JSONObject)] {
        var out: [String: (creates: Bool, properties: JSONObject)] = [:]
        // A parked write is not laid over: during a conflict the routine
        // shows as the server holds it, which is what Use Theirs keeps.
        for entry in file.entries where entry.session == nil && entry.kind == codec.kinds.routine && !entry.parked {
            switch entry.method {
            case .purge:
                out[entry.recordID] = nil
            case .put, .patch:
                var current = out[entry.recordID] ?? (creates: entry.method == .put, properties: JSONObject())
                for (key, value) in entry.properties ?? JSONObject() {
                    current.properties[key] = value
                }
                out[entry.recordID] = current
            }
        }
        return out
    }

    /// The owner's answer to a conflicting edit: `keepMine` sends it again
    /// without a version, so the properties it changed replace the
    /// server's and the rest stay as the other writer left them; otherwise
    /// it is dropped and the server's routine stands.
    public func resolve(_ id: UUID, keepMine: Bool) throws {
        try change { file in
            guard let index = file.entries.firstIndex(where: { $0.id == id }) else { return }
            if keepMine {
                file.entries[index].baseVersion = nil
                file.entries[index].guarded = false
                file.entries[index].conflict = false
                file.entries[index].parked = false
                file.entries[index].lastError = nil
            } else {
                file.entries.remove(at: index)
            }
        }
    }

    /// Sends a parked write again at the next flush.
    public func retry(_ id: UUID) throws {
        try change { file in
            guard let index = file.entries.firstIndex(where: { $0.id == id }) else { return }
            file.entries[index].parked = false
        }
    }

    /// Gives up a parked write and, when it belongs to a session, every write
    /// of that session still waiting behind it.
    public func discard(_ id: UUID) throws {
        try change { file in
            guard let entry = file.entries.first(where: { $0.id == id }) else { return }
            if let session = entry.session {
                file.entries.removeAll { $0.session == session }
            } else {
                file.entries.removeAll { $0.id == id }
            }
        }
    }

    /// Forgets everything: another repository's data in this folder. A
    /// flush in progress stops at its next step without writing.
    public func reset() throws {
        generation += 1
        state = .idle
        try change { $0 = File() }
    }

    /// Sends what waits, in order. A write the server refuses for itself is
    /// parked with its words and holds back the rest of its session; the
    /// network, a busy server or the token stop the run, and nothing is lost.
    /// Writes queued while it runs are sent in the same run.
    @discardableResult
    public func flush(_ api: WorkoutsAPI) async -> OutboxState {
        guard !flushing else { return state }
        flushing = true
        defer {
            flushing = false
            inFlight = nil
            if landedUnsaved { try? save() }
        }
        let run = generation
        var tried = Set<UUID>()
        var refusal: String?
        while let entry = next(skipping: tried) {
            tried.insert(entry.id)
            inFlight = entry
            let outcome: Result<Int?, any Error>
            do {
                outcome = .success(try await send(entry, api, run: run))
            } catch {
                outcome = .failure(error)
            }
            inFlight = nil
            guard run == generation else { return state }
            switch outcome {
            case let .success(version):
                commit(replayable: entry.method == .put && entry.baseVersion == nil) { file in
                    if entry.method == .purge {
                        file.versions[entry.key] = nil
                        file.chains[entry.key] = nil
                    } else if let version {
                        if let base = entry.baseVersion {
                            // A create asserted 0 itself; an edit, the end of the chain.
                            let asserted = entry.method == .put ? base : file.asserted(entry.key, from: base)
                            file.chains[entry.key, default: [:]][String(asserted)] = version
                        }
                        file.versions[entry.key] = version
                    }
                    file.entries.removeAll { $0.id == entry.id }
                }
            case let .failure(error):
                switch Self.classify(error) {
                case .drop:
                    commit { $0.entries.removeAll { $0.id == entry.id } }
                case let .stop(stopped):
                    state = stopped
                    return stopped
                case let .hold(words):
                    commit { file in
                        guard let index = file.entries.firstIndex(where: { $0.id == entry.id }) else { return }
                        file.entries[index].attempts += 1
                        file.entries[index].lastError = words
                    }
                case let .park(words), let .conflict(words):
                    let conflict = Self.classify(error) == .conflict(words)
                    commit { file in
                        guard let index = file.entries.firstIndex(where: { $0.id == entry.id }) else { return }
                        file.entries[index].attempts += 1
                        file.entries[index].lastError = words
                        file.entries[index].parked = true
                        file.entries[index].conflict = conflict
                    }
                    refusal = words
                }
            }
        }
        if refusal == nil, let parked = file.entries.first(where: \.parked) { refusal = parked.lastError ?? "" }
        state = refusal.map(OutboxState.refused) ?? .sent
        return state
    }

    /// The first entry not tried in this run whose session nothing parked
    /// or earlier holds back.
    private func next(skipping tried: Set<UUID>) -> OutboxEntry? {
        var held = Set<String>()
        for entry in file.entries {
            if let session = entry.session, held.contains(session) { continue }
            if entry.parked || tried.contains(entry.id) {
                // A write tried and still here was refused or parked: the
                // rest of its session waits.
                if let session = entry.session { held.insert(session) }
                continue
            }
            return entry
        }
        return nil
    }

    /// One write, answering the record's new version. The versions it reads
    /// on the way are kept only while the run is current.
    private func send(_ entry: OutboxEntry, _ api: WorkoutsAPI, run: Int) async throws -> Int? {
        switch entry.method {
        case .put where entry.baseVersion != nil:
            // A create at an id the phone chose: refused if one is there.
            do {
                return try await api.write("PUT", entry.kind, entry.recordID, properties: entry.properties ?? JSONObject(),
                                           ifVersion: entry.baseVersion ?? 0)
            } catch let error as ServerError where error.status == 409 {
                throw CreateRefused(id: entry.recordID)
            }
        case .put:
            return try await api.write("PUT", entry.kind, entry.recordID, properties: entry.properties ?? JSONObject())
        case .patch where entry.baseVersion != nil:
            // The owner's edit, on what they saw: never merged over.
            let properties = entry.properties ?? JSONObject()
            let version = file.asserted(entry.key, from: entry.baseVersion ?? 0)
            do {
                return try await api.write("PATCH", entry.kind, entry.recordID, properties: properties, ifVersion: version)
            } catch let error as ServerError where error.status == 409 {
                throw EditConflict(kind: entry.kind, id: entry.recordID, server: error.text)
            }
        case .patch:
            let properties = entry.properties ?? JSONObject()
            do {
                let version = entry.guarded ? try await knownVersion(entry, api, run: run) : nil
                return try await api.write("PATCH", entry.kind, entry.recordID, properties: properties, ifVersion: version)
            } catch let error as ServerError where error.status == 409 && entry.guarded {
                // Somebody wrote it since: read it, and send the change again
                // on top, once.
                let version = try await api.get(entry.kind, entry.recordID)?.version
                return try await api.write("PATCH", entry.kind, entry.recordID, properties: properties, ifVersion: version)
            }
        case .purge:
            do {
                try await api.purge(entry.kind, entry.recordID, ifVersion: entry.guarded ? file.versions[entry.key] : nil)
            } catch let error as ServerError where error.status == 409 {
                try await api.purge(entry.kind, entry.recordID, ifVersion: nil)
            }
            return nil
        }
    }

    private func knownVersion(_ entry: OutboxEntry, _ api: WorkoutsAPI, run: Int) async throws -> Int? {
        if let known = file.versions[entry.key] { return known }
        guard let record = try await api.get(entry.kind, entry.recordID) else {
            // The record is gone: a guarded patch of nothing is refused, and
            // parked for the owner.
            throw ServerError(status: 404, code: "not_found", message: "\(entry.kind)/\(entry.recordID) is not on your substrate any more.")
        }
        if run == generation, let version = record.version { commit { $0.versions[entry.key] = version } }
        return record.version
    }

    enum Verdict: Equatable {
        case drop
        /// Parked for the owner to choose: the record changed on the server
        /// since the version an edit was made on.
        case conflict(String)
        case stop(OutboxState)
        /// Left queued, its session held for the rest of the run.
        case hold(String)
        case park(String)
    }

    /// What a failed write means for the run.
    static func classify(_ error: any Error) -> Verdict {
        switch error {
        case is SessionExpired:
            return .stop(.signedOut)
        case is InsufficientScope:
            return .stop(.needsApproval)
        case let conflict as EditConflict:
            return .conflict(conflict.localizedDescription)
        case let server as ServerError:
            // A newer write of the record landed first: this one is stale.
            if server.status == 422, server.text.contains("below the stored revision") { return .drop }
            if server.status == 429 || server.status >= 500 { return .stop(.offline(server.text)) }
            // Moved again since the read: it waits for the next run.
            if server.status == 409 { return .hold(server.text) }
            return .park(server.text)
        case let transport as TransportError where (transport.status ?? 500) >= 500 || transport.status == nil:
            return .stop(.offline(transport.text))
        case let url as URLError:
            return .stop(.offline(url.localizedDescription))
        default:
            return .park(error.localizedDescription)
        }
    }

    /// A change the caller asked for: saved before it returns, and undone in
    /// memory when the save fails, so the queue never claims what the disk
    /// does not hold.
    private func change(_ edit: (inout File) -> Void) throws {
        let before = file
        edit(&file)
        do {
            try save()
        } catch {
            file = before
            throw OutboxNotSaved(reason: error.localizedDescription)
        }
    }

    /// A change the flush makes after the server answered: kept in memory
    /// whatever the disk says, since the server holds it now, and saved
    /// again with the next change. A failed save is reported in
    /// `saveProblem`; at worst a write is sent again after a relaunch, which
    /// every write here survives.
    ///
    /// A landed write that is safe to send again (a PUT that merges, not a
    /// create that asserts nothing is there) is saved at most once a second
    /// and when the flush ends: every save rewrites the whole queue, and an
    /// import queues thousands of them.
    private func commit(replayable: Bool = false, _ edit: (inout File) -> Void) {
        edit(&file)
        let now = ContinuousClock.now
        if replayable, let last = lastLandedSave, now - last < .seconds(1) {
            landedUnsaved = true
            return
        }
        lastLandedSave = now
        try? save()
    }

    private func save() throws {
        do {
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            try Self.encoded(file).write(to: url, options: writingOptions)
            saveProblem = nil
            landedUnsaved = false
        } catch {
            saveProblem = error.localizedDescription
            throw error
        }
    }

    private static func encoded(_ file: File) throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.withoutEscapingSlashes, .sortedKeys]
        return try encoder.encode(file)
    }
}

/// A guarded edit met a newer version on the server: another device or the
/// coach changed the record since the owner began editing.
public struct EditConflict: LocalizedError, Sendable {
    public let kind: String
    public let id: String
    public let server: String
    public var errorDescription: String? {
        "It changed on your substrate since you began editing it (the coach or another device), so your edit was not sent."
    }
}

/// A create met a record already at its id: parked, never merged into.
public struct CreateRefused: LocalizedError, Sendable {
    public let id: String
    public var errorDescription: String? {
        "Another routine was saved as \u{201C}\(id)\u{201D} meanwhile, so the copy was not written. Discard it and duplicate again."
    }
}
