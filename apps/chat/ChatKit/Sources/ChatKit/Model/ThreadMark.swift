import Foundation
import SubstrateCore

/// How far the person signed in has read one thread, as the server's
/// `llm/threadmark` record of it says (decision 0313): one record per thread
/// and person, which follows the person to every device. A thread the person
/// has no mark of is one they never opened.
struct ThreadMark: Codable, Equatable, Sendable {
    /// The newest message the person has seen; nil where they have no mark.
    var readThrough: String?
    /// The person chose Mark as Unread, and has not seen the end since.
    var markedUnread = false
    /// The record's version, 0 where the server holds none: an answer that
    /// left the server earlier never replaces a later one.
    var version = 0

    /// No mark: the person never opened the thread.
    static let none = ThreadMark()

    init(readThrough: String? = nil, markedUnread: Bool = false, version: Int = 0) {
        self.readThrough = readThrough
        self.markedUnread = markedUnread
        self.version = version
    }

    init(record: Record) {
        readThrough = record["readThrough"]?.referenceID
        markedUnread = record["markedUnread"]?.bool == true
        version = record.version
    }

    /// The server's rule, for a thread that has settled: marked unread, or
    /// its newest message is not the one read through. A thread that names
    /// no newest message settled on a server from before marks, or holds
    /// none, and is unread only when marked so.
    func unread(lastMessage: String?) -> Bool {
        if markedUnread { return true }
        guard let lastMessage else { return false }
        return readThrough != lastMessage
    }
}

/// What this device decided about a mark that the server has not answered
/// yet: the chat reads so here at once, and the write follows.
struct MarkChange: Codable, Equatable, Sendable {
    /// Read through this message.
    var readThrough: String?
    /// Mark as Unread, or the flag taken back on a thread with no message
    /// to read through. Nil leaves it to the server, which clears it when a
    /// position is taken.
    var markedUnread: Bool?
    /// When it goes out; nil is at once. A read by sight waits, so a run of
    /// replies is one write.
    var due: Date?
    /// Its write failed: it waits for the next chance, not for the clock.
    var held = false

    /// The body of the write. Never `readAt`, which is the server's.
    var properties: [String: JSONValue] {
        var out: [String: JSONValue] = [:]
        if let readThrough { out["readThrough"] = .string(readThrough) }
        if let markedUnread { out["markedUnread"] = .bool(markedUnread) }
        return out
    }

    /// The mark as it reads once this is written over it, for a thread
    /// whose newest message is `last`. Taking a position clears Mark as
    /// Unread unless the same write sets it. A read of a message that is no
    /// longer the thread's newest says nothing of the thread as it is now:
    /// another device may have read further, or marked the chat unread,
    /// since. Its position is left to the server, which never moves one
    /// back, and only a Mark as Unread made with it is laid over.
    func over(_ mark: ThreadMark, last: String?) -> ThreadMark {
        var out = mark
        if let readThrough, last == nil || readThrough == last {
            out.readThrough = readThrough
            out.markedUnread = markedUnread ?? false
        } else if let markedUnread {
            out.markedUnread = markedUnread
        }
        return out
    }

    /// This, then `later` made after it, as one write.
    func then(_ later: MarkChange) -> MarkChange {
        var out = later
        if later.readThrough == nil {
            out.readThrough = readThrough
            out.markedUnread = later.markedUnread ?? markedUnread
        }
        return out
    }
}
