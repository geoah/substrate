import Foundation
import SubstrateAPI
import SubstrateCore

/// What the window reads returned, kept on the device so the pane draws
/// offline: the occurrences of every series from yesterday to seven days
/// ahead, the meetings of today and the two days after, and the calendars
/// those meetings name, which say whose each is.
public struct Windows: Codable, Equatable, Sendable {
    public var occurrences: [Record] = []
    public var meetings: [Record] = []
    /// Every record of the kinds the meetings' `calendar` references name.
    public var calendars: [Record] = []
    public var readAt: Date?

    public init() {}

    /// What an earlier build kept holds no calendars: it is read as it is.
    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        occurrences = try container.decodeIfPresent([Record].self, forKey: .occurrences) ?? []
        meetings = try container.decodeIfPresent([Record].self, forKey: .meetings) ?? []
        calendars = try container.decodeIfPresent([Record].self, forKey: .calendars) ?? []
        readAt = try container.decodeIfPresent(Date.self, forKey: .readAt)
    }

    /// The meetings on the owner's own calendars: the only ones drawn.
    public var ownMeetings: [Record] {
        let byPath = Dictionary(calendars.map { ("\($0.kind)/\($0.id)", $0) }, uniquingKeysWith: { first, _ in first })
        return meetings.filter { Meeting.isOwn($0, calendars: byPath) }
    }
}

/// The reads the pane makes beside the linked kinds: what the repository
/// holds, the two window reads, the open requests, the sync status and a
/// contact by reference.
enum TodayReads {
    /// Whether the repository declares a kind: its declaration is read.
    static func declaration(_ client: SubstrateClient, _ kind: String) async throws -> Record? {
        try await client.record(TodayKinds.kind, kind)
    }

    static func discover(_ client: SubstrateClient, now: Date) async throws -> Discovery {
        var found = Discovery()
        found.known = TodayKinds.linkable
        found.at = now
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
            out.calendars = try await calendars(client, of: out.meetings)
        }
        out.readAt = now
        return out
    }

    /// The calendars the meetings name in their `calendar` reference, every
    /// record of those kinds: whose a calendar is decides whether its events
    /// are drawn (`Meeting.isOwn`). A read that fails fails the window read,
    /// so the meetings kept are never without their calendars.
    static func calendars(_ client: SubstrateClient, of meetings: [Record]) async throws -> [Record] {
        let kinds = Set(meetings.compactMap { $0["calendar"]?.referencePath }.compactMap { RecordReference(path: $0)?.kind }).sorted()
        guard !kinds.isEmpty else { return [] }
        return try await client.listAll(ListQuery(kinds: kinds, first: 200), limit: 1000).records
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

    /// The page at its source of each record named: read, with what is
    /// read beside it (`SourcePages.related`): what a page is composed from
    /// where the record names none, and a Slack message's conversation,
    /// for the channel's name. A record that is gone, or that the app may
    /// not read, names no page; a read that fails otherwise is asked again
    /// later. A record beside it that the app may not read is left out, so
    /// a message keeps its page where its channel's name cannot be read.
    static func sources(_ client: SubstrateClient, _ paths: [String]) async -> (found: [String: SourcePage], none: [String], failed: [String]) {
        enum Read: Sendable {
            case record(Record)
            case none
            case failed
        }
        func read(_ paths: some Sequence<String>) async -> [String: Read] {
            await withTaskGroup(of: (String, Read).self) { group in
                for path in paths {
                    group.addTask {
                        guard let reference = RecordReference(path: path) else { return (path, .none) }
                        do {
                            return (path, try await client.record(reference.kind, reference.recordID).map(Read.record) ?? .none)
                        } catch let error as ServerError where error.status == 403 {
                            return (path, .none)
                        } catch is InsufficientScope {
                            return (path, .none)
                        } catch {
                            return (path, .failed)
                        }
                    }
                }
                var out: [String: Read] = [:]
                for await (path, answer) in group { out[path] = answer }
                return out
            }
        }
        let records = await read(Set(paths))
        // What is read beside them, each once: eight messages of one channel read its conversation one time.
        var beside: Set<String> = []
        for case let .record(record) in records.values { beside.formUnion(SourcePages.related(record)) }
        let related = await read(beside)
        var out: (found: [String: SourcePage], none: [String], failed: [String]) = ([:], [], [])
        for path in paths {
            guard case let .record(record)? = records[path] else {
                if case .failed? = records[path] { out.failed.append(path) } else { out.none.append(path) }
                continue
            }
            var with: [String: Record] = [:]
            var failed = false
            for other in SourcePages.related(record) {
                switch related[other] {
                case let .record(record)?: with[other] = record
                case .failed?: failed = true
                default: break
                }
            }
            if failed {
                out.failed.append(path)
            } else if let page = SourcePages.page(of: record, with: with) {
                out.found[path] = page
            } else {
                out.none.append(path)
            }
        }
        return out
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
