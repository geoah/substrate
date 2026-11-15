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
    /// When the server took the position.
    var readAt: Date?
    /// The person keeps the thread above the rest of their list (decision
    /// 0322). Absent from a server older than the flag, and until set.
    var pinned = false
    /// The person put the thread away: it is in no list, no badge and no
    /// notification of theirs, but while it waits on a person.
    var archived = false

    /// No mark: the person never opened the thread.
    static let none = ThreadMark()

    init(
        readThrough: String? = nil, markedUnread: Bool = false, version: Int = 0, readAt: Date? = nil, pinned: Bool = false,
        archived: Bool = false
    ) {
        self.readThrough = readThrough
        self.markedUnread = markedUnread
        self.version = version
        self.readAt = readAt
        self.pinned = pinned
        self.archived = archived
    }

    init(record: Record) {
        readThrough = record["readThrough"]?.referenceID
        markedUnread = record["markedUnread"]?.bool == true
        version = record.version
        readAt = record["readAt"]?.string.flatMap(Timestamp.parse)
        pinned = record["pinned"]?.bool == true
        archived = record["archived"]?.bool == true
    }

    /// As a file an earlier build wrote holds it: with neither flag.
    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        readThrough = try container.decodeIfPresent(String.self, forKey: .readThrough)
        markedUnread = try container.decodeIfPresent(Bool.self, forKey: .markedUnread) ?? false
        version = try container.decodeIfPresent(Int.self, forKey: .version) ?? 0
        readAt = try container.decodeIfPresent(Date.self, forKey: .readAt)
        pinned = try container.decodeIfPresent(Bool.self, forKey: .pinned) ?? false
        archived = try container.decodeIfPresent(Bool.self, forKey: .archived) ?? false
    }

    /// Whether the position was taken after `thread`'s row settled and
    /// names another message than the row's newest. Such a position is,
    /// but for a device that read from an older row itself, of a reply the
    /// row does not hold yet: the row is the older of the two.
    func ahead(of thread: ChatThread) -> Bool {
        guard let readThrough, let last = thread.lastMessage, readThrough != last, let readAt,
            let settled = thread.record["finishedAt"]?.string.flatMap(Timestamp.parse)
        else { return false }
        return readAt > settled
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
    /// Pin or Unpin, Archive or Unarchive: each rides with whatever else
    /// the write carries, and moves neither the position nor the other
    /// flag. Sent only where the server keeps the flags (`records` 9).
    var pinned: Bool?
    var archived: Bool?

    /// The body of the write. Never `readAt`, which is the server's.
    var properties: [String: JSONValue] {
        var out: [String: JSONValue] = [:]
        if let readThrough { out["readThrough"] = .string(readThrough) }
        if let markedUnread { out["markedUnread"] = .bool(markedUnread) }
        if let pinned { out["pinned"] = .bool(pinned) }
        if let archived { out["archived"] = .bool(archived) }
        return out
    }

    /// Whether it moves a flag: a write the person asked for by name, whose
    /// refusal is said.
    var flags: Bool { pinned != nil || archived != nil }

    /// The change without its flags; nil where it moved nothing else.
    var unflagged: MarkChange? {
        var out = self
        (out.pinned, out.archived) = (nil, nil)
        return out.readThrough == nil && out.markedUnread == nil ? nil : out
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
        if let pinned { out.pinned = pinned }
        if let archived { out.archived = archived }
        return out
    }

    /// This, then `later` made after it, as one write.
    func then(_ later: MarkChange) -> MarkChange {
        var out = later
        if later.readThrough == nil {
            out.readThrough = readThrough
            out.markedUnread = later.markedUnread ?? markedUnread
        }
        out.pinned = later.pinned ?? pinned
        out.archived = later.archived ?? archived
        return out
    }
}
