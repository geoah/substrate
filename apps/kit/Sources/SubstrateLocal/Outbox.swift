import Foundation
import SubstrateAPI
import SubstrateCore

/// The writes waiting for the server, kept in the repository's folder so a
/// write made offline survives a relaunch, and sent in order when the server
/// answers. The file also keeps, for the repository, the version each record
/// was last written or read at, which is how an app tells a record it wrote
/// (and the server since deleted) from one it never sent.
///
/// The actor is reentrant at every network call, so the queue may change
/// while a write is in flight: an entry is found again by its id after each
/// call, never by its position, and `reset` moves a generation the flush
/// checks before it touches anything.
public actor Outbox {
    /// The file's `format`. Workouts' outbox wrote this one with each group
    /// under `session`, which an entry still reads.
    public static let fileFormat = 2

    struct File: Codable {
        var format = Outbox.fileFormat
        var entries: [OutboxEntry] = []
        /// The version each record was last written or read at.
        var versions: [String: Int] = [:]
        /// The app's own guarded edits that landed, per record: the version
        /// each one asserted and the version it made. An edit made on a
        /// version the app itself then moved is sent at the end of that
        /// chain; anything another writer did breaks it, so the edit meets a
        /// conflict instead of writing over it.
        var chains: [String: [String: Int]] = [:]

        init() {}

        enum CodingKeys: String, CodingKey { case format, entries, versions, chains }

        init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            format = try c.decodeIfPresent(Int.self, forKey: .format) ?? Outbox.fileFormat
            guard format == Outbox.fileFormat else {
                throw DecodingError.dataCorruptedError(forKey: .format, in: c, debugDescription: "outbox format \(format)")
            }
            entries = try c.decodeIfPresent([OutboxEntry].self, forKey: .entries) ?? []
            versions = try c.decodeIfPresent([String: Int].self, forKey: .versions) ?? [:]
            chains = try c.decodeIfPresent([String: [String: Int]].self, forKey: .chains) ?? [:]
        }

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

    public let folder: RepositoryFolder
    public let name: String
    public let area: FolderArea
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
    /// The file that was there at open and could not be read, set aside.
    public private(set) var unreadable: SetAsideFile?
    /// Why the last save failed, until one succeeds.
    public private(set) var saveProblem: String?

    public init(folder: RepositoryFolder, name: String = "outbox.json", area: FolderArea = .outbox, now: Date = Date()) {
        self.folder = folder
        self.name = name
        self.area = area
        // Never an empty queue over writes nobody can read: a file that does
        // not decode is kept beside, and said so.
        let loaded = try? folder.load(File.self, from: name, in: area, at: now)
        file = loaded?.value ?? File()
        unreadable = loaded?.aside
    }

    public var entries: [OutboxEntry] { file.entries }

    /// The entries the server refused, waiting for the owner.
    public var parked: [OutboxEntry] { file.entries.filter(\.parked) }

    /// The version a record was last written or read at, nil when this
    /// repository was never seen to hold it.
    public func version(of kind: String, _ id: String) -> Int? { file.versions["\(kind)/\(id)"] }

    public var versions: [String: Int] { file.versions }

    // MARK: Queueing

    /// Queues the writes, kept on disk before it returns; on a failed save
    /// nothing is queued and the error says why.
    public func enqueue(_ entries: [OutboxEntry]) throws {
        try change { $0.entries += entries }
    }

    /// Records the versions the server answered for records read rather than
    /// written, as `kind/id`.
    public func saw(_ seen: [String: Int]) throws {
        guard !seen.isEmpty else { return }
        try change { file in
            for (key, version) in seen { file.versions[key] = version }
        }
    }

    /// Forgets a record this repository no longer holds.
    public func forget(_ kind: String, _ id: String) throws {
        try change { $0.versions["\(kind)/\(id)"] = nil }
    }

    /// A group given up before it was sent (a session discarded): its
    /// waiting writes dropped and, when the server holds `purge`'s record or
    /// may once a write in flight lands, the purge queued, in one save.
    /// Answers whether the purge was queued.
    @discardableResult
    public func discard(group: String, purge: OutboxEntry) throws -> Bool {
        let held = file.versions[purge.key] != nil || inFlight?.group == group
        try change { file in
            file.entries.removeAll { $0.group == group }
            if held { file.entries.append(purge) }
        }
        return held
    }

    /// Drops a group's waiting writes and queues `purge` in one save: a
    /// group deleted on the device whatever the server holds.
    public func purge(group: String, with purge: OutboxEntry) throws {
        try change { file in
            file.entries.removeAll { $0.group == group }
            file.entries.append(purge)
        }
    }

    /// A record outside any group deleted on the device: any write of it
    /// still waiting dropped and its purge queued, in one save.
    public func delete(_ purge: OutboxEntry) throws {
        try change { file in
            file.entries.removeAll { $0.group == nil && $0.key == purge.key }
            file.entries.append(purge)
        }
    }

    /// The records outside any group whose purge waits, as `kind/id`: what
    /// a read of them leaves out until the purge lands.
    public var waitingDeletes: Set<String> {
        Set(file.entries.filter { $0.method == .purge && $0.group == nil }.map(\.key))
    }

    /// Takes back a record's purge that has not gone yet (it was added back
    /// before its delete reached the server).
    public func withdrawDelete(_ kind: String, _ id: String) throws {
        let key = "\(kind)/\(id)"
        try change { file in
            file.entries.removeAll { $0.method == .purge && $0.group == nil && $0.key == key && $0.id != inFlight?.id }
        }
    }

    /// Returns once no purge of the record is on the wire, or after `limit`:
    /// a purge already sent cannot be taken back, so what follows it waits
    /// for it to land.
    public func settle(_ kind: String, _ id: String, limit: Duration = .seconds(30)) async {
        let key = "\(kind)/\(id)"
        let clock = ContinuousClock()
        let deadline = clock.now + limit
        while inFlight?.key == key, inFlight?.method == .purge, clock.now < deadline {
            try? await Task.sleep(nanoseconds: 50_000_000)
        }
    }

    /// A guarded edit (an entry with `baseVersion`), kept on disk before it
    /// returns. The last write of the same record outside any group, when it
    /// is a guarded edit or create still waiting (not on the wire, not
    /// parked), takes this one on top, so edits made offline go as one write
    /// under the first one's version and a create not sent yet takes the
    /// edit into its body. Anything else (a parked conflict or refusal, a
    /// Keep My Edit going without a version) is left alone, and the edit
    /// queues behind it under its own version.
    public func edit(_ entry: OutboxEntry) throws {
        guard let patch = entry.properties, !patch.isEmpty else { return }
        try change { file in
            if let index = file.entries.lastIndex(where: { $0.group == nil && $0.key == entry.key }),
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

    /// The records of `kind` outside any group whose writes wait, by id, as
    /// what they will set. A parked write is not laid over: during a
    /// conflict the record shows as the server holds it, which Use Theirs
    /// keeps.
    public func waiting(kind: String) -> [String: WaitingWrite] {
        var out: [String: WaitingWrite] = [:]
        for entry in file.entries where entry.group == nil && entry.kind == kind && !entry.parked {
            switch entry.method {
            case .purge:
                out[entry.recordID] = nil
            case .put, .patch:
                var current = out[entry.recordID] ?? WaitingWrite(creates: entry.method == .put, properties: JSONObject())
                for (key, value) in entry.properties ?? JSONObject() {
                    current.properties[key] = value
                }
                out[entry.recordID] = current
            }
        }
        return out
    }

    // MARK: The owner's answers

    /// The owner's answer to a conflicting edit: `keepMine` sends it again
    /// without a version, so the properties it changed replace the server's
    /// and the rest stay as the other writer left them; otherwise it is
    /// dropped and the server's record stands.
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

    /// Gives up a parked write and, when it belongs to a group, every write
    /// of that group still waiting behind it.
    public func discard(_ id: UUID) throws {
        try change { file in
            guard let entry = file.entries.first(where: { $0.id == id }) else { return }
            if let group = entry.group {
                file.entries.removeAll { $0.group == group }
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

    // MARK: Sending

    /// Sends what waits, in order. A write the server refuses for itself is
    /// parked with its words and holds back the rest of its group; the
    /// network, a busy server or the token stop the run, and nothing is lost.
    /// Writes queued while it runs are sent in the same run.
    @discardableResult
    public func flush(_ writer: any RecordWriter) async -> OutboxState {
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
                outcome = .success(try await send(entry, writer, run: run))
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

    /// The first entry not tried in this run whose group nothing parked or
    /// earlier holds back.
    private func next(skipping tried: Set<UUID>) -> OutboxEntry? {
        var held = Set<String>()
        for entry in file.entries {
            if let group = entry.group, held.contains(group) { continue }
            if entry.parked || tried.contains(entry.id) {
                // A write tried and still here was refused or parked: the
                // rest of its group waits.
                if let group = entry.group { held.insert(group) }
                continue
            }
            return entry
        }
        return nil
    }

    /// One write, answering the record's new version. The versions it reads
    /// on the way are kept only while the run is current.
    private func send(_ entry: OutboxEntry, _ writer: any RecordWriter, run: Int) async throws -> Int? {
        let properties = entry.properties ?? JSONObject()
        switch entry.method {
        case .put where entry.baseVersion != nil:
            // A create at an id the device chose: refused if one is there.
            do {
                return try await writer.write("PUT", entry.kind, entry.recordID, properties: properties, ifVersion: entry.baseVersion ?? 0)
            } catch let error as ServerError where error.status == 409 {
                throw CreateRefused(kind: entry.kind, id: entry.recordID)
            }
        case .put:
            return try await writer.write("PUT", entry.kind, entry.recordID, properties: properties, ifVersion: nil)
        case .patch where entry.baseVersion != nil:
            // The owner's edit, on what they saw: never merged over.
            let version = file.asserted(entry.key, from: entry.baseVersion ?? 0)
            do {
                return try await writer.write("PATCH", entry.kind, entry.recordID, properties: properties, ifVersion: version)
            } catch let error as ServerError where error.status == 409 {
                throw EditConflict(kind: entry.kind, id: entry.recordID, server: error.text)
            }
        case .patch:
            do {
                let version = entry.guarded ? try await knownVersion(entry, writer, run: run) : nil
                return try await writer.write("PATCH", entry.kind, entry.recordID, properties: properties, ifVersion: version)
            } catch let error as ServerError where error.status == 409 && entry.guarded {
                // Somebody wrote it since: read it, and send the change again
                // on top, once.
                let version = try await writer.version(entry.kind, entry.recordID)
                return try await writer.write("PATCH", entry.kind, entry.recordID, properties: properties, ifVersion: version)
            }
        case .purge:
            do {
                try await writer.purge(entry.kind, entry.recordID, ifVersion: entry.guarded ? file.versions[entry.key] : nil)
            } catch let error as ServerError where error.status == 409 {
                try await writer.purge(entry.kind, entry.recordID, ifVersion: nil)
            }
            return nil
        }
    }

    private func knownVersion(_ entry: OutboxEntry, _ writer: any RecordWriter, run: Int) async throws -> Int? {
        if let known = file.versions[entry.key] { return known }
        guard let version = try await writer.version(entry.kind, entry.recordID) else {
            // The record is gone: a guarded patch of nothing is refused, and
            // parked for the owner.
            throw ServerError(status: 404, code: "not_found", message: "\(entry.kind)/\(entry.recordID) is not on your substrate any more.")
        }
        if run == generation { commit { $0.versions[entry.key] = version } }
        return version
    }

    enum Verdict: Equatable {
        case drop
        /// Parked for the owner to choose: the record changed on the server
        /// since the version an edit was made on.
        case conflict(String)
        case stop(OutboxState)
        /// Left queued, its group held for the rest of the run.
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

    // MARK: Saving

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
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.withoutEscapingSlashes, .sortedKeys]
            try folder.write(encoder.encode(file), to: name, in: area)
            saveProblem = nil
            landedUnsaved = false
        } catch {
            saveProblem = error.localizedDescription
            throw error
        }
    }
}
