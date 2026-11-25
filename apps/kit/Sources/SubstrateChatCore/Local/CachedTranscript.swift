import Foundation
import SubstrateCore

/// One thread's rows as last read, and what they were read under.
struct CachedTranscript: Codable, Equatable, Sendable {
    static let format = 1

    var format = CachedTranscript.format
    /// The thread's version when the rows were read: they are current only
    /// while it is unchanged and the thread is settled.
    var threadVersion: Int
    /// The read filled its window: there may be older rows it did not hold.
    var full: Bool
    var records: [Record]
    var openedAt: Date?
}
