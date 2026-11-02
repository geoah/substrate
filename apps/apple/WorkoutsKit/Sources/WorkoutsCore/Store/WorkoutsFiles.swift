import Foundation

/// Where the app keeps its files and how it writes them (§2.3).
public enum WorkoutsFiles {
    /// Every app write: atomic, and readable after the first unlock. A Live
    /// Activity button runs app code while the phone is locked, which
    /// `.complete` protection would make unreadable (04 §4.1). The stores take
    /// their options as a parameter so the Mac tests can pass `.atomic` alone.
    public static let protectedWriting: Data.WritingOptions = [.atomic, .completeFileProtectionUntilFirstUserAuthentication]

    /// `Application Support/Workouts/`, created on first use and included in
    /// the device backup.
    public static func applicationSupportDirectory() throws -> URL {
        let base = try FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
        let directory = base.appendingPathComponent("Workouts", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        return directory
    }

    /// `Application Support/Workouts/` without creating it: when the first
    /// attempt fails (a full disk at launch), every write creates it again,
    /// so the app keeps one directory instead of saving nothing.
    public static func applicationSupportPath() -> URL? {
        FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first?
            .appendingPathComponent("Workouts", isDirectory: true)
    }

    /// Moves a file that cannot be read out of the way, beside it, as
    /// `<name>.unreadable-<seconds since 1970>.json`, so a new file can be
    /// written and the old one is kept for a look. Returns the new place.
    public static func setAside(_ url: URL, at date: Date) throws -> URL {
        let stem = url.deletingPathExtension().lastPathComponent
        let name = "\(stem).unreadable-\(Int(date.timeIntervalSince1970)).\(url.pathExtension)"
        let target = url.deletingLastPathComponent().appendingPathComponent(name, isDirectory: false)
        try? FileManager.default.removeItem(at: target)
        try FileManager.default.moveItem(at: url, to: target)
        return target
    }

    /// Writes `data` whole, creating the parent directory first. `.atomic` is
    /// always added: a crash mid-write must leave the old file, never half a
    /// new one.
    static func write(_ data: Data, to url: URL, options: Data.WritingOptions) throws {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try data.write(to: url, options: options.union(.atomic))
    }

    /// Removes the file; a file that is already gone is not an error.
    static func remove(_ url: URL) throws {
        do {
            try FileManager.default.removeItem(at: url)
        } catch let error as CocoaError where error.code == .fileNoSuchFile {
            return
        }
    }

    /// The file's contents, or nil when there is no file.
    static func read(_ url: URL) throws -> Data? {
        do {
            return try Data(contentsOf: url)
        } catch let error as CocoaError where error.code == .fileReadNoSuchFile || error.code == .fileNoSuchFile {
            return nil
        }
    }
}
