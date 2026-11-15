import Foundation
import SubstrateAPI
import SubstrateCore

/// The reads the chat list is made of, as the records route takes them:
/// the lists of threads, a transcript's rows before the ones held, and the
/// search over messages. The kit's `ListQuery` names kinds and properties
/// alone, so the filters that carry another arm are written here.
enum ChatLists {
    /// Which threads a list read answers.
    enum Part: String, Codable, Hashable, Sendable {
        /// Every thread, as a server that keeps neither flag is asked.
        case every
        /// The ones the person has not archived: no mark of theirs with
        /// `archived: true` points at them (decision 0322).
        case kept
        case archived
        /// The pinned ones, however old. It also answers a pinned chat its
        /// person archived: the mark says which.
        case pinned
        /// Every thread that waits on a person, archived or not and however
        /// old.
        case waiting
    }

    /// How many chats one read of the list asks for.
    static let page = 100
    /// How many rows a read of a transcript's older rows asks for.
    static let olderRows = 200
    /// How many messages a search asks the server for.
    static let hits = 50

    /// The filter of a list of threads: the modes it holds, narrowed by the
    /// person's own marks where `part` asks.
    static func filter(_ part: Part, modes: [String]) -> JSONValue {
        var properties: JSONObject = [:]
        if modes.count == 1, let mode = modes.first {
            properties["mode"] = ["eq": .string(mode)]
        } else {
            properties["mode"] = ["in": .array(modes.map(JSONValue.string))]
        }
        if part == .waiting { properties["waitingOnPerson"] = ["gt": 0] }
        var filter: JSONObject = ["kinds": .array([.string(Kinds.thread)]), "properties": .object(properties)]
        var marked: JSONObject = ["readBy": ["eq": "me"]]
        switch part {
        case .every, .waiting:
            return .object(filter)
        case .kept, .archived:
            marked["archived"] = ["eq": true]
        case .pinned:
            marked["pinned"] = ["eq": true]
        }
        var arm: JSONObject = ["kind": .string(Kinds.threadMark), "property": "thread", "properties": .object(marked)]
        if part == .kept { arm["exists"] = false }
        filter["referencedBy"] = .object(arm)
        return .object(filter)
    }

    /// One page of a list of threads, most recently active first.
    static func threads(_ client: SubstrateClient, _ part: Part, modes: [String], first: Int = page, after: String? = nil) async throws -> RecordPage {
        var query = [("first", String(first)), ("filter", filter(part, modes: modes).jsonText), ("orderBy", "updatedAt:desc")]
        if let after { query.append(("after", after)) }
        return try await client.send("GET", "api/v1/records", query: query).decode(RecordPage.self)
    }

    /// Every thread of a list that is read whole (the pinned ones, the ones
    /// that wait), to its end: each is few by what it is, and one left out
    /// is a chat that waits on its person and cannot be seen.
    static func all(_ client: SubstrateClient, _ part: Part, modes: [String], page: Int = page) async throws -> RecordPage {
        var read = try await threads(client, part, modes: modes, first: page)
        var cursor = read.cursor
        while let after = cursor {
            let next = try await threads(client, part, modes: modes, first: page, after: after)
            read.records += next.records
            // A page that brings nothing ends it, whatever its cursor says.
            cursor = next.records.isEmpty ? nil : next.cursor
        }
        return read
    }

    /// The rows of a thread before the row whose `turn` is `turn`, newest
    /// first: a keyset of the transcript's own order, so it needs no cursor
    /// from the read that brought the rows held, which may have been the
    /// device's folder or a conversation's own. The row at `turn` comes
    /// again where two rows share the ordinal, and is dropped by its id.
    static func older(_ thread: String, before turn: Int, first: Int = olderRows) -> ListQuery {
        ListQuery(
            kinds: [Kinds.message], properties: ["thread": ["eq": .string(thread)], "turn": ["lte": .number(Decimal(turn))]], first: first,
            orderBy: "turn:desc,createdAt:desc")
    }

    /// The messages that hold a search's words, best first: the records
    /// route's ranked read, by its words alone (`mode=lexical`), so every
    /// hit holds a word to show. Open to a token with full access; an app's
    /// token reads messages thread by thread and is refused (`403`).
    static func search(_ client: SubstrateClient, _ words: String, first: Int = hits) async throws -> [Record] {
        let filter: JSONValue = ["kinds": .array([.string(Kinds.message)])]
        let query = [("q", words), ("mode", "lexical"), ("first", String(first)), ("filter", filter.jsonText)]
        return try await client.send("GET", "api/v1/records", query: query).decode(RecordPage.self).records
    }

    /// The same for an app's token, which names its threads: the list's
    /// `search` arm under the threads held, newest first. It ranks nothing.
    static func search(_ client: SubstrateClient, _ words: String, threads: [String], first: Int = hits) async throws -> [Record] {
        var out: [Record] = []
        for start in stride(from: 0, to: threads.count, by: 100) where out.count < first {
            let ids = threads[start ..< min(start + 100, threads.count)].map(JSONValue.string)
            let filter: JSONValue = [
                "kinds": .array([.string(Kinds.message)]), "properties": ["thread": ["in": .array(ids)]], "search": .string(words),
            ]
            let query = [("first", String(first)), ("filter", filter.jsonText), ("orderBy", "createdAt:desc")]
            out += try await client.send("GET", "api/v1/records", query: query).decode(RecordPage.self).records
        }
        return Array(out.prefix(first))
    }
}
