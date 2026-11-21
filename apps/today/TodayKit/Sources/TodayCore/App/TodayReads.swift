import Foundation
import SubstrateAPI
import SubstrateCore

/// What the window reads returned, kept on the device so the pane draws
/// offline: the occurrences of every series from yesterday to seven days
/// ahead, and the meetings of today and the two days after.
public struct Windows: Codable, Equatable, Sendable {
    public var occurrences: [Record] = []
    public var meetings: [Record] = []
    public var readAt: Date?

    public init() {}
}

/// The reads the pane makes beside the linked kinds: what the repository
/// holds, the two window reads, the open requests, the sync status and a
/// contact by reference.
enum TodayReads {
    /// Whether the repository declares a kind: its declaration is read.
    static func declaration(_ client: SubstrateClient, _ kind: String) async throws -> Record? {
        try await client.record(TodayKinds.kind, kind)
    }

    static func discover(_ client: SubstrateClient) async throws -> Discovery {
        var found = Discovery()
        try await withThrowingTaskGroup(of: (String, Record?).self) { group in
            for kind in TodayKinds.linkable {
                group.addTask { (kind, try await declaration(client, kind)) }
            }
            for try await (kind, record) in group {
                guard let record else { continue }
                found.present.append(kind)
                let properties = record["properties"]?.object ?? [:]
                if kind == TodayKinds.task { found.taskMoves = properties["notBefore"] != nil }
                if kind == TodayKinds.list { found.listPriority = properties["priority"] != nil }
            }
        }
        found.present.sort()
        if !found.present.contains(TodayKinds.task) { found.taskMoves = false }
        if !found.present.contains(TodayKinds.list) { found.listPriority = false }
        // A trait the repository does not hold answers 404: no series then.
        found.series = ((try? await client.implementors(of: TodayKinds.series)) ?? []).map(\.identity).sorted()
        var calendars: Set<String> = []
        for trait in [TodayKinds.recurring, TodayKinds.override] {
            for kind in (try? await client.implementors(of: trait)) ?? [] where Discovery.isRange(kind.definition) { calendars.insert(kind.identity) }
        }
        found.calendars = calendars.sorted()
        return found
    }

    /// A window read: `at` bounded on both ends, ordered by it, every page.
    static func window(_ client: SubstrateClient, filter: JSONObject, from: Date, to: Date, limit: Int = 2000) async throws -> [Record] {
        var filter = filter
        filter["properties"] = .object(["at": .object(["gte": TodayWrites.stamp(from), "lt": TodayWrites.stamp(to)])])
        var out: [Record] = []
        var after: String?
        repeat {
            var query = [("first", "500"), ("filter", JSONValue.object(filter).jsonText), ("orderBy", "at")]
            if let after { query.append(("after", after)) }
            let page = try await client.send("GET", "api/v1/records", query: query).decode(RecordPage.self)
            out += page.records
            after = page.records.isEmpty ? nil : page.cursor
        } while after != nil && out.count < limit
        return out
    }

    static func windows(_ client: SubstrateClient, discovery: Discovery, now: Date, calendar: Calendar) async throws -> Windows {
        let today = calendar.startOfDay(for: now)
        func day(_ offset: Int) -> Date { calendar.date(byAdding: .day, value: offset, to: today) ?? today.addingTimeInterval(Double(offset) * 86400) }
        var out = Windows()
        if !discovery.series.isEmpty {
            out.occurrences = try await window(client, filter: ["implements": .string(TodayKinds.series)], from: day(-1), to: day(8))
        }
        if !discovery.calendars.isEmpty {
            out.meetings = try await window(client, filter: ["kinds": .array(discovery.calendars.map(JSONValue.string))], from: day(0), to: day(3))
        }
        out.readAt = now
        return out
    }

    /// The open requests: what the assistant suggests on records that exist.
    static func requests(_ client: SubstrateClient) async -> [Record] {
        let query = ListQuery(kinds: [TodayKinds.patchRequest], properties: ["decision": ["eq": "proposed"]], first: 200)
        return (try? await client.list(query).records) ?? []
    }

    /// The sources that have not synced, read tolerantly: a status this
    /// build cannot read says nothing is stale.
    static func stale(_ client: SubstrateClient) async -> [StaleSource] {
        guard let response = try? await client.send("GET", "api/v1/sync/status"), let value = try? JSONValue(data: response.data) else { return [] }
        let items = value["items"]?.array ?? value["records"]?.array ?? value.array ?? []
        return items.compactMap { item -> StaleSource? in
            let properties = item["properties"] ?? item
            let status = (properties["status"]?.text ?? properties["state"]?.text ?? item["status"]?.text ?? "").lowercased()
            guard ["failed", "error", "refused", "stopped", "unreachable", "stale"].contains(status) else { return nil }
            let kind = item["kind"]?.text ?? properties["kind"]?.text ?? ""
            let name = properties["name"]?.text ?? properties["label"]?.text ?? kindDisplayName(kind)
            let since = (properties["lastSyncedAt"]?.text ?? properties["syncedAt"]?.text ?? properties["lastSuccessAt"]?.text).flatMap(Timestamp.parse)
            return StaleSource(name: name.isEmpty ? "A source" : name, since: since)
        }
    }

    static func contacts(_ client: SubstrateClient, _ paths: [String]) async -> [Record] {
        var out: [Record] = []
        for path in paths {
            guard let reference = RecordReference(path: path), let record = try? await client.record(reference.kind, reference.recordID) else { continue }
            out.append(record)
        }
        return out
    }
}
