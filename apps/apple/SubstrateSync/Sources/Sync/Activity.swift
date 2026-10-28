import Foundation
import os

/// One line of what a sync did, for the owner to read on the phone: when it
/// started, what it read and wrote, and every error with where it happened.
struct ActivityEntry: Codable, Identifiable, Equatable, Sendable {
    enum Level: String, Codable, Sendable { case info, warning, error }

    var id: UUID = UUID()
    var at: Date = Date()
    var source: Source?
    var level: Level
    var message: String

    /// One line of text, as the shared log reads it.
    var line: String {
        let when = ISO8601DateFormatter().string(from: at)
        return "\(when) \(level.rawValue.uppercased()) \(source?.title ?? "Sync"): \(message)"
    }
}

/// How far one source has got: what it is doing and, where it can count,
/// how many of how many.
struct SyncProgress: Equatable, Sendable {
    var step: String
    var done: Int = 0
    var total: Int = 0

    var fraction: Double? { total > 0 ? min(1, Double(done) / Double(total)) : nil }
}

/// The activity log: the newest entries in memory, the last `limit` kept in
/// Application Support across launches, and every entry in the system log
/// (subsystem dev.reamde.substrate.sync), where Console.app reads it.
final class Activity: @unchecked Sendable {
    static let shared = Activity()
    static let limit = 500

    private let lock = NSLock()
    private var entries: [ActivityEntry]
    private let logger = Logger(subsystem: "dev.reamde.substrate.sync", category: "sync")
    private var listener: (@MainActor @Sendable (ActivityEntry) -> Void)?
    /// Called on the main actor with every new entry.
    var onEntry: (@MainActor @Sendable (ActivityEntry) -> Void)? {
        get { lock.lock(); defer { lock.unlock() }; return listener }
        set { lock.lock(); listener = newValue; lock.unlock() }
    }

    private static var url: URL {
        let dir = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir.appendingPathComponent("activity.json")
    }

    private init() {
        entries = (try? Data(contentsOf: Self.url)).flatMap { try? JSONDecoder().decode([ActivityEntry].self, from: $0) } ?? []
    }

    /// Newest first.
    var all: [ActivityEntry] {
        lock.lock()
        defer { lock.unlock() }
        return entries.reversed()
    }

    func log(_ source: Source?, _ message: String, level: ActivityEntry.Level = .info) {
        let entry = ActivityEntry(source: source, level: level, message: message)
        switch level {
        // The message can name a contact or an event: private in the system
        // log, which a diagnostics report carries off the phone. The in-app
        // log keeps it, and leaves the phone only when the owner shares it.
        case .info: logger.notice("\(source?.rawValue ?? "sync", privacy: .public): \(message, privacy: .private)")
        case .warning: logger.warning("\(source?.rawValue ?? "sync", privacy: .public): \(message, privacy: .private)")
        case .error: logger.error("\(source?.rawValue ?? "sync", privacy: .public): \(message, privacy: .private)")
        }
        lock.lock()
        entries.append(entry)
        if entries.count > Self.limit { entries.removeFirst(entries.count - Self.limit) }
        let snapshot = entries
        let onEntry = listener
        lock.unlock()
        if let data = try? JSONEncoder().encode(snapshot) {
            try? data.write(to: Self.url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
        }
        if let onEntry { Task { @MainActor in onEntry(entry) } }
    }

    func clear() {
        lock.lock()
        entries = []
        lock.unlock()
        try? FileManager.default.removeItem(at: Self.url)
    }

    /// The whole log as text, oldest first, for sharing.
    var text: String {
        lock.lock()
        defer { lock.unlock() }
        return entries.map(\.line).joined(separator: "\n")
    }
}

/// A reason in someone else's words, such as a parked delivery's.
struct Message: LocalizedError {
    let text: String
    init(_ text: String) { self.text = text }
    var errorDescription: String? { text }
}

/// An error with where it happened, so a refusal reads as "Health,
/// 2026-10-20, flights climbed: …" rather than HealthKit's sentence alone.
struct ContextError: LocalizedError {
    let context: String
    let underlying: Error

    var errorDescription: String? { "\(context): \(explain(underlying))" }
}
