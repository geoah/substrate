import Foundation

/// Where an app keeps its files. `data` holds what must survive and never
/// leave the device (the repository's folder: outbox, record cache, journal),
/// excluded from backups and device transfers; `cache` what the system may
/// drop.
public struct AppFolders: Hashable, Sendable {
    public let data: URL
    public let cache: URL

    public init(data: URL, cache: URL) {
        self.data = data
        self.cache = cache
    }
}
