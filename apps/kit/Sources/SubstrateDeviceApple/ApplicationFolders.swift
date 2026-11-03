#if canImport(Darwin)
import Foundation
import SubstrateDevice

/// The app's folders on an Apple system: `Application Support/<name>/`,
/// kept out of backups, and `Caches/<name>/`. On iOS every write is
/// readable after the first unlock, the class the Keychain item has, so a
/// Live Activity button or background work reaches it with the phone
/// locked.
public struct ApplicationFolders: AppFolders {
    public let name: String

    public init(name: String) {
        self.name = name
    }

    public func data() throws -> URL {
        var url = try FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
            .appendingPathComponent(name, isDirectory: true)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        try url.setResourceValues(values)
        return url
    }

    public func cache() throws -> URL {
        let url = try FileManager.default.url(for: .cachesDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
            .appendingPathComponent(name, isDirectory: true)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }

    public var writingOptions: Data.WritingOptions {
        #if os(iOS)
        [.completeFileProtectionUntilFirstUserAuthentication]
        #else
        []
        #endif
    }
}
#endif
