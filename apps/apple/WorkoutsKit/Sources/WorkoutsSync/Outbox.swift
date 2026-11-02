import Foundation
import SubstrateKit
import WorkoutsCore

/// One write waiting for the server, in the phone's spelling.
public struct OutboxEntry: Codable, Sendable, Hashable, Identifiable {
    public enum Method: String, Codable, Sendable {
        case put = "PUT"
        case patch = "PATCH"
        /// `DELETE ?purge=true`: a discarded or deleted session.
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

    public init(method: Method, kind: String, recordID: String, properties: JSONObject? = nil, guarded: Bool = false,
                session: String? = nil)
    {
        id = UUID()
        self.method = method
        self.kind = kind
        self.recordID = recordID
        self.properties = properties
        self.guarded = guarded
        self.session = session
    }

    var key: String { "\(kind)/\(recordID)" }

    enum CodingKeys: String, CodingKey {
        case id, method, kind, recordID, properties, guarded, session, attempts, lastError, parked
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
    struct File: Codable {
        var format = 1
        var entries: [OutboxEntry] = []
        /// The version each record was last written or read at.
        var versions: [String: Int] = [:]
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
                commit { file in
                    if entry.method == .purge {
                        file.versions[entry.key] = nil
                    } else if let version {
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
                case let .park(words):
                    commit { file in
                        guard let index = file.entries.firstIndex(where: { $0.id == entry.id }) else { return }
                        file.entries[index].attempts += 1
                        file.entries[index].lastError = words
                        file.entries[index].parked = true
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
        case .put:
            return try await api.write("PUT", entry.kind, entry.recordID, properties: entry.properties ?? JSONObject())
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
    private func commit(_ edit: (inout File) -> Void) {
        edit(&file)
        try? save()
    }

    private func save() throws {
        do {
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.withoutEscapingSlashes, .sortedKeys]
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            try encoder.encode(file).write(to: url, options: writingOptions)
            saveProblem = nil
        } catch {
            saveProblem = error.localizedDescription
            throw error
        }
    }
}
