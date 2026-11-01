import CryptoKit
import Foundation
import SubstrateKit

/// What one pushed row was: the hash of what was sent and its keys, so an
/// unchanged row is skipped and a vanished key is cleared with a PATCH.
struct Pushed: Codable, Equatable, Sendable {
    var hash: String
    var keys: [String]
    /// An event's start, which decides whether its absence from a scan
    /// means it was deleted (inside the window) or aged out (outside it).
    var start: Date?
    /// An event's calendar row, so a deleted calendar's events are dropped
    /// here when the server's cascade removes them there.
    var calendar: String?

    init(_ json: JSON, start: Date? = nil, calendar: String? = nil) {
        hash = Pushed.hash(json)
        keys = json.keys
        self.start = start
        self.calendar = calendar
    }

    /// A row to write: its properties, and what to remember once it is.
    struct Row: Sendable {
        let props: JSON
        let pushed: Pushed

        init(_ props: JSON, _ pushed: Pushed) {
            self.props = props
            self.pushed = pushed
        }
    }

    static func hash(_ json: JSON) -> String {
        SHA256.hash(data: json.encoded).prefix(16).map { String(format: "%02x", $0) }.joined()
    }
}

enum Source: String, Codable, CaseIterable, Identifiable, Sendable {
    case health
    case contacts
    case calendars
    case reminders
    case places

    var id: String { rawValue }

    var title: String {
        switch self {
        case .health: "Health"
        case .contacts: "Contacts"
        case .calendars: "Calendars"
        case .reminders: "Reminders"
        case .places: "Places"
        }
    }

    /// What the source sends, in a few words, for where it is off or new.
    var summary: String {
        switch self {
        case .health: "Heart rate, activity, sleep, workouts and more"
        case .contacts: "Every contact on this phone"
        case .calendars: "Events from 90 days back to 180 ahead"
        case .reminders: "Every list and every reminder"
        case .places: "Where you stay, named by Apple Maps"
        }
    }

    var symbol: String {
        switch self {
        case .health: "heart.fill"
        case .contacts: "person.crop.circle"
        case .calendars: "calendar"
        case .reminders: "checklist"
        case .places: "mappin.and.ellipse"
        }
    }
}

/// One finished run of one source, for the history.
struct SyncRun: Codable, Equatable, Identifiable, Sendable {
    var id: Date { started }
    var source: Source
    var started: Date
    var finished: Date
    var summary: String?
    var error: String?
    /// Stopped by hand or by the system rather than failed: `error` says
    /// which, and nothing went wrong.
    var stopped: Bool?
}

/// What the device held at the last scan, beside what was pushed: the
/// numerator and denominator of every progress figure.
struct DeviceCounts: Codable, Equatable, Sendable {
    var contacts: Int?
    var calendars: Int?
    var events: Int?
    var reminders: Int?
}

/// One source's last outcome, as Home shows it.
struct SourceStatus: Codable, Equatable, Sendable {
    var lastSync: Date?
    /// What the last complete sync held and what it changed, in words:
    /// "412 contacts, 3 written".
    var counts: String?
    var lastError: String?
    /// The last run was stopped, not failed (SyncRun.stopped).
    var stopped: Bool?
}

/// The device's side of every sync, in Application Support. Losing it is
/// safe: the next sync writes everything again, and ids are stable.
struct LocalState: Codable, Sendable {
    /// One per install, lowercase: the address book's and event store's id
    /// hash it, and the Health account's id is named from it.
    /// The install's id, the same in every copy of the state: it lives in the
    /// Keychain, not in this file, so two copies loaded before the first save
    /// can never mint two ids (and name two Health accounts), and a reinstall
    /// keeps its address book, event store and account.
    var install: String { InstallID.current }
    var contacts: [String: Pushed] = [:]
    var calendars: [String: Pushed] = [:]
    var events: [String: Pushed] = [:]
    var reminderLists: [String: Pushed] = [:]
    var reminders: [String: Pushed] = [:]
    /// The visits pushed, by their PlaceLog key.
    var visits: [String: Pushed] = [:]
    /// The local days whose Health deliveries were both accepted.
    var healthDays: Set<String> = []
    /// Days delivered and not yet filed, with their fire ids: the next run
    /// reads their receipts before it sends anything.
    var healthPending: [String: [String]] = [:]
    /// One stable automation id per grouping.
    var automations: [String: String] = [:]
    var status: [Source: SourceStatus] = [:]
    /// The server and repository the pushed rows went to (`Session.identity`):
    /// signed in anywhere else, they are forgotten.
    var repository: String?
    /// Days the owner asked for beyond the backfill, sent oldest first by
    /// the next Health sync and dropped once accepted.
    var healthQueue: Set<String> = []
    /// The newest runs first, at most `historyLimit`.
    var runs: [SyncRun] = []
    var device = DeviceCounts()

    static let historyLimit = 60

    mutating func record(_ run: SyncRun) {
        runs.insert(run, at: 0)
        if runs.count > Self.historyLimit { runs.removeLast(runs.count - Self.historyLimit) }
    }

    init() {}

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        contacts = try c.decodeIfPresent([String: Pushed].self, forKey: .contacts) ?? [:]
        calendars = try c.decodeIfPresent([String: Pushed].self, forKey: .calendars) ?? [:]
        events = try c.decodeIfPresent([String: Pushed].self, forKey: .events) ?? [:]
        healthDays = try c.decodeIfPresent(Set<String>.self, forKey: .healthDays) ?? []
        automations = try c.decodeIfPresent([String: String].self, forKey: .automations) ?? [:]
        status = try c.decodeIfPresent([Source: SourceStatus].self, forKey: .status) ?? [:]
        healthQueue = try c.decodeIfPresent(Set<String>.self, forKey: .healthQueue) ?? []
        runs = try c.decodeIfPresent([SyncRun].self, forKey: .runs) ?? []
        device = try c.decodeIfPresent(DeviceCounts.self, forKey: .device) ?? DeviceCounts()
        repository = try c.decodeIfPresent(String.self, forKey: .repository)
        healthPending = try c.decodeIfPresent([String: [String]].self, forKey: .healthPending) ?? [:]
        reminderLists = try c.decodeIfPresent([String: Pushed].self, forKey: .reminderLists) ?? [:]
        reminders = try c.decodeIfPresent([String: Pushed].self, forKey: .reminders) ?? [:]
        visits = try c.decodeIfPresent([String: Pushed].self, forKey: .visits) ?? [:]
    }

    var ids: AppleIDs { AppleIDs(install: install) }
    var healthAccount: String { "substrate-sync-" + install.prefix(8) }

    func automation(_ grouping: Grouping) -> String? { automations[grouping.rawValue] }

    private static var url: URL {
        let dir = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir.appendingPathComponent("sync-state.json")
    }

    /// The install id an older build kept in this file, if any, so the
    /// Keychain takes it over rather than minting a new one.
    static func storedInstall() -> String? {
        struct Old: Decodable { let install: String? }
        guard let data = try? Data(contentsOf: url) else { return nil }
        return (try? JSONDecoder().decode(Old.self, from: data))?.install
    }

    static func load() -> LocalState {
        guard let data = try? Data(contentsOf: url), let state = try? JSONDecoder().decode(LocalState.self, from: data)
        else { return LocalState() }
        return state
    }

    func save() {
        guard let data = try? JSONEncoder().encode(self) else { return }
        // Readable after the first unlock, for a background sync.
        try? data.write(to: Self.url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
    }

    /// Whether the pushed rows went to the session's repository. Compared
    /// without case: a host is, and builds before the kit's normaliser kept
    /// the server as typed.
    func isBound(to session: Session) -> Bool {
        repository?.lowercased() == session.identity
    }

    /// Forgets what was pushed, keeping the install: a new sign-in may be a
    /// different repository, which has none of these rows.
    mutating func forgetPushed() {
        contacts = [:]
        calendars = [:]
        events = [:]
        reminderLists = [:]
        reminders = [:]
        visits = [:]
        healthDays = []
        healthQueue = []
        healthPending = [:]
        runs = []
        status = [:]
    }
}

/// Runs `operation` over `items` with at most `limit` in flight, and returns
/// each item's outcome. There is no batch route, so this is how a sync
/// keeps a server busy without flooding it.
func eachLimited<T: Sendable, R: Sendable>(
    _ items: [T], limit: Int = 6, progress: (@Sendable (Int) async -> Void)? = nil,
    _ operation: @escaping @Sendable (T) async throws -> R
) async -> [(T, Result<R, Error>)] {
    await withTaskGroup(of: (T, Result<R, Error>).self) { group in
        var results: [(T, Result<R, Error>)] = []
        var iterator = items.makeIterator()
        func next() -> Bool {
            guard let item = iterator.next() else { return false }
            group.addTask {
                do { return (item, .success(try await operation(item))) } catch { return (item, .failure(error)) }
            }
            return true
        }
        for _ in 0 ..< limit where !next() { break }
        while let result = await group.next() {
            results.append(result)
            if let progress, results.count % 10 == 0 || results.count == items.count { await progress(results.count) }
            if Task.isCancelled { continue }
            _ = next()
        }
        return results
    }
}

/// The install's one id, made once and kept in the Keychain, and taken over
/// from an older build's state file where there is one. It is minted only
/// when the Keychain answers that there is none: any other failure (a phone
/// still locked since it started) refuses the sync instead, because a second
/// id would fork the address book, the event store and the Health account.
enum InstallID {
    private static let lock = NSLock()
    private nonisolated(unsafe) static var cached: String?

    struct Unavailable: LocalizedError {
        let status: OSStatus
        var errorDescription: String? {
            "The install id could not be read from the Keychain (status \(status)); unlock the phone and try again."
        }
    }

    static func get() throws -> String {
        lock.lock()
        defer { lock.unlock() }
        if let cached { return cached }
        switch SessionStore.sync.data(account: "install") {
        case let .found(data):
            let id = String(decoding: data, as: UTF8.self)
            cached = id
            return id
        case .missing:
            let id = LocalState.storedInstall() ?? UUID().uuidString.lowercased()
            // An id the Keychain did not keep would be minted again next
            // launch, and fork the address book and the Health account.
            do { try SessionStore.sync.setString(id, account: "install") } catch let e as SessionStore.NotSaved {
                throw Unavailable(status: e.status)
            }
            cached = id
            return id
        case let .failed(status):
            throw Unavailable(status: status)
        }
    }

    /// For display and ids once a run has passed `get()`.
    static var current: String { (try? get()) ?? "unavailable" }
}
