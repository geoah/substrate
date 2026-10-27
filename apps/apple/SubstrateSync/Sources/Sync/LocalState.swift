import CryptoKit
import Foundation

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

    static func hash(_ json: JSON) -> String {
        SHA256.hash(data: json.encoded).prefix(16).map { String(format: "%02x", $0) }.joined()
    }
}

enum Source: String, Codable, CaseIterable, Identifiable, Sendable {
    case health
    case contacts
    case calendars

    var id: String { rawValue }

    var title: String {
        switch self {
        case .health: "Health"
        case .contacts: "Contacts"
        case .calendars: "Calendars"
        }
    }

    var symbol: String {
        switch self {
        case .health: "heart.fill"
        case .contacts: "person.crop.circle"
        case .calendars: "calendar"
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
}

/// What the device held at the last scan, beside what was pushed: the
/// numerator and denominator of every progress figure.
struct DeviceCounts: Codable, Equatable, Sendable {
    var contacts: Int?
    var calendars: Int?
    var events: Int?
}

/// One source's last outcome, as Home shows it.
struct SourceStatus: Codable, Equatable, Sendable {
    var lastSync: Date?
    /// What the last complete sync held and what it changed, in words:
    /// "412 contacts, 3 written".
    var counts: String?
    var lastError: String?
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
    /// The local days whose Health deliveries were both accepted.
    var healthDays: Set<String> = []
    /// One stable automation id per grouping.
    var automations: [String: String] = [:]
    var status: [Source: SourceStatus] = [:]
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

    /// Forgets what was pushed, keeping the install: a new sign-in may be a
    /// different repository, which has none of these rows.
    mutating func forgetPushed() {
        contacts = [:]
        calendars = [:]
        events = [:]
        healthDays = []
        healthQueue = []
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

/// The install's one id, made once and kept in the Keychain. Read once per
/// process (a `static let` initializes exactly once, under the runtime's
/// lock), taken over from an older build's state file where there is one.
enum InstallID {
    static let current: String = {
        if let id = Keychain.string(account: "install"), !id.isEmpty { return id }
        let id = LocalState.storedInstall() ?? UUID().uuidString.lowercased()
        Keychain.setString(id, account: "install")
        return id
    }()
}
