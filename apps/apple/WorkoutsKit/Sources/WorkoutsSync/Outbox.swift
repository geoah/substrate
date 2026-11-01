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
        var finish = JSONObject()
        for key in finishKeys {
            if let value = session.properties[key] { finish[key] = value }
        }
        finish["status"] = .string(SessionStatus.finished.rawValue)
        finish["revision"] = .number(Decimal(revision + 1))
        out.append(OutboxEntry(method: .patch, kind: session.kind, recordID: sessionID, properties: finish, guarded: true, session: sessionID))
        return out
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
    /// Every write landed.
    case sent
    /// The server could not be reached; nothing is lost.
    case offline(String)
    /// The token was refused: sign in again.
    case signedOut
    /// The token lacks a scope the write needs.
    case needsApproval
    /// The server refused one or more writes, which wait with its words.
    case refused(String)
}

/// The writes waiting for the server, kept in `outbox.json` beside the
/// journal so a write made in the gym survives a relaunch, and sent in order
/// when the server answers.
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
    public private(set) var state: OutboxState = .idle

    public init(directory: URL, writingOptions: Data.WritingOptions = WorkoutsFiles.protectedWriting) {
        url = directory.appendingPathComponent(Self.fileName)
        self.writingOptions = writingOptions
        file = (try? JSONDecoder().decode(File.self, from: Data(contentsOf: url))) ?? File()
    }

    public var entries: [OutboxEntry] { file.entries }

    public func enqueue(_ entries: [OutboxEntry]) {
        file.entries += entries
        save()
    }

    /// Drops every write of a session still waiting, and answers whether the
    /// server already holds the session, which then needs a purge.
    @discardableResult
    public func drop(session: String, kind: String) -> Bool {
        file.entries.removeAll { $0.session == session }
        save()
        return file.versions["\(kind)/\(session)"] != nil
    }

    /// Forgets everything: a sign-out, or another repository.
    public func reset() {
        file = File()
        state = .idle
        save()
    }

    /// Sends what waits, in order. A write the server refuses for itself
    /// waits with its words, and holds back the rest of its session; the
    /// network, a busy server or the token stop the run, and nothing is lost.
    @discardableResult
    public func flush(_ api: WorkoutsAPI) async -> OutboxState {
        guard !flushing else { return state }
        flushing = true
        defer { flushing = false }
        var held = Set<String>()
        var refusal: String?
        var index = 0
        while index < file.entries.count {
            let entry = file.entries[index]
            if let session = entry.session, held.contains(session) {
                index += 1
                continue
            }
            do {
                try await send(entry, api)
                file.entries.remove(at: index)
                save()
            } catch {
                switch Self.classify(error) {
                case .drop:
                    file.entries.remove(at: index)
                    save()
                case .stop(let stopped):
                    state = stopped
                    return stopped
                case .hold(let words):
                    file.entries[index].attempts += 1
                    file.entries[index].lastError = words
                    save()
                    if let session = entry.session { held.insert(session) }
                    refusal = words
                    index += 1
                }
            }
        }
        state = refusal.map(OutboxState.refused) ?? .sent
        return state
    }

    private func send(_ entry: OutboxEntry, _ api: WorkoutsAPI) async throws {
        switch entry.method {
        case .put:
            file.versions[entry.key] = try await api.write("PUT", entry.kind, entry.recordID, properties: entry.properties ?? JSONObject())
        case .patch:
            let properties = entry.properties ?? JSONObject()
            do {
                let version = entry.guarded ? try await knownVersion(entry, api) : nil
                file.versions[entry.key] = try await api.write("PATCH", entry.kind, entry.recordID, properties: properties, ifVersion: version)
            } catch let error as ServerError where error.status == 409 && entry.guarded {
                // Somebody wrote it since: read it, and send the change again
                // on top, once.
                file.versions[entry.key] = nil
                let version = try await knownVersion(entry, api)
                file.versions[entry.key] = try await api.write("PATCH", entry.kind, entry.recordID, properties: properties, ifVersion: version)
            }
        case .purge:
            do {
                try await api.purge(entry.kind, entry.recordID, ifVersion: entry.guarded ? file.versions[entry.key] : nil)
            } catch let error as ServerError where error.status == 409 {
                try await api.purge(entry.kind, entry.recordID, ifVersion: nil)
            }
            file.versions[entry.key] = nil
        }
    }

    private func knownVersion(_ entry: OutboxEntry, _ api: WorkoutsAPI) async throws -> Int? {
        if let known = file.versions[entry.key] { return known }
        let record = try await api.get(entry.kind, entry.recordID)
        file.versions[entry.key] = record?.version
        return record?.version
    }

    enum Verdict {
        case drop
        case stop(OutboxState)
        case hold(String)
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
            return .hold(server.text)
        case let transport as TransportError where (transport.status ?? 500) >= 500 || transport.status == nil:
            return .stop(.offline(transport.text))
        case let url as URLError:
            return .stop(.offline(url.localizedDescription))
        default:
            return .hold(error.localizedDescription)
        }
    }

    private func save() {
        do {
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.withoutEscapingSlashes, .sortedKeys]
            try encoder.encode(file).write(to: url, options: writingOptions)
        } catch {
            // The next change writes the whole file again.
        }
    }
}
