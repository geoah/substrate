import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateLocal
import Testing

@testable import TodayCore

/// Against a dev server (apps/today/dev): the reads the pane makes, and one
/// write of each kind in the shape the outbox would send it. Runs only
/// when `TODAY_DEV_SERVER` and `TODAY_DEV_TOKEN` are set; it changes the
/// repository's day, which `apps/today/dev/seed.py` writes again.
@Suite("Against a dev server", .serialized, .timeLimit(.minutes(5)))
struct LiveTests {
    static let environment = ProcessInfo.processInfo.environment

    static var client: SubstrateClient? {
        guard let server = environment["TODAY_DEV_SERVER"].flatMap(URL.init(string:)), let token = environment["TODAY_DEV_TOKEN"] else { return nil }
        return SubstrateClient(server: server, token: token)
    }

    static var calendar: Calendar { TodayApp.calendar(.current) }

    static func state(_ client: SubstrateClient) async throws -> TodayApp.State {
        var state = TodayApp.State()
        let discovery = try await TodayReads.discover(client, now: Date())
        state.discovery = discovery
        state.linked.loaded = true
        for record in try await client.listAll(ListQuery(kinds: discovery.linked, first: 200), limit: 10000).records {
            state.linked.records[RecordFold.key(record.kind, record.id)] = record
        }
        state.windows = try await TodayReads.windows(client, discovery: discovery, now: Date(), calendar: calendar)
        state.requests = await TodayReads.requests(client)
        state.stale = await TodayReads.stale(client)
        let contacts = await TodayReads.contacts(client, TodayApp.missingContacts(state))
        for record in contacts { state.contacts["\(record.kind)/\(record.id)"] = record }
        var notes = DayNotes(day: CivilDay(Date(), in: calendar))
        notes.wokeAt = calendar.startOfDay(for: Date()).addingTimeInterval(7 * 3600)
        notes.nightFrom = calendar.startOfDay(for: Date()).addingTimeInterval(-2 * 3600)
        state.notes = notes
        state.notesRead = true
        return state
    }

    /// One outbox write, sent as the outbox sends it.
    static func send(_ write: LocalWrite, _ client: SubstrateClient) async throws {
        for entry in write.entries {
            let properties = entry.properties?.dictionary ?? [:]
            switch entry.method {
            case .put: _ = try await client.put(entry.kind, entry.recordID, properties: properties, ifVersion: entry.baseVersion)
            case .patch:
                let seen = try await client.record(entry.kind, entry.recordID)?.version
                _ = try await client.patch(
                    entry.kind, entry.recordID, properties: properties, ifVersion: entry.baseVersion.flatMap { $0 > 0 ? $0 : nil } ?? seen)
            case .delete: try await client.delete(entry.kind, entry.recordID)
            default: break
            }
        }
    }

    @Test func theReadsAnswerADay() async throws {
        guard let client = Self.client else { return }
        let state = try await Self.state(client)
        let discovery = try #require(state.discovery)
        print("LIVE discovery present:", discovery.present.map { $0.split(separator: "/").suffix(2).joined(separator: "/") })
        print(
            "LIVE discovery series:", discovery.series, "calendars:", discovery.calendars, "taskMoves:", discovery.taskMoves, "listPriority:",
            discovery.listPriority)
        print(
            "LIVE linked records:", state.linked.records.count, "occurrences:", state.windows.occurrences.count, "meetings:", state.windows.meetings.count,
            "requests:", state.requests.count, "stale:", state.stale.count, "contacts:", state.contacts.count)
        if let sample = state.windows.occurrences.first {
            print(
                "LIVE occurrence:", sample.kind, sample.id, sample.version, sample["at"]?.text ?? "-", sample["name"]?.text ?? "-",
                sample["status"]?.text ?? "-")
        }
        if let sample = state.windows.meetings.first {
            print(
                "LIVE meeting:", sample.kind, sample.id, sample["at"]?.text ?? "-", sample["endsAt"]?.text ?? "-", sample.title ?? "-",
                Array(sample.properties.keys).sorted())
        }
        #expect(discovery.present.contains(TodayKinds.task) && discovery.present.contains(TodayKinds.list) && discovery.present.contains(TodayKinds.log))
        #expect(discovery.taskMoves && discovery.listPriority)
        #expect(!discovery.series.isEmpty && !discovery.calendars.isEmpty)
        #expect(!state.windows.occurrences.isEmpty && state.windows.occurrences.allSatisfy { Occurrence($0) != nil })
        let presentation = TodayPresenter.present(state, linked: TodayFixtures.app.linked, build: TodayFixtures.app.build, now: Date(), calendar: Self.calendar)
        print("LIVE pane:\n" + outline(presentation.pane))
        #expect(!presentation.pane.strip.entries.isEmpty)
        #expect(presentation.pane.items.count > 3)
        if case let .plan(plan) = presentation.page(.plan) {
            print("LIVE plan groups:", plan.groups.map { "\($0.name) \($0.count)" }, "overlaps:", plan.overlaps.map(\.name))
        }
    }

    @Test func oneWriteOfEachKindLands() async throws {
        guard let client = Self.client else { return }
        let now = Date()
        var state = try await Self.state(client)
        func make() -> (Day, DayEngine, TodayWrites) {
            let day = TodayPresenter.day(state, now: now, calendar: Self.calendar)
            return (day, DayEngine(day), TodayWrites(day: day, mint: { UUID() }))
        }
        var (day, engine, writes) = make()

        // A routine row: every open occurrence logged done, then one skipped.
        let routines = engine.todaysRoutines.filter { !engine.open($0).isEmpty }
        if let row = routines.first {
            let open = engine.open(row)
            for occurrence in open { try await Self.send(writes.log(occurrence, done: true), client) }
            let log = try #require(try await client.record(TodayKinds.log, open[0].id))
            #expect(log["status"]?.text == "done" && log["series"]?.referencePath == open[0].series)
            print("LIVE log:", log.id, log.properties.keys.sorted())
        }
        if let row = routines.dropFirst().first, let occurrence = engine.open(row).first {
            try await Self.send(writes.log(occurrence, done: false), client)
            #expect(try await client.record(TodayKinds.log, occurrence.id)?["status"]?.text == "skipped")
        }

        // A task: its flag as the owner's, "When", done and open again.
        let task = try #require(day.tasks.first { $0.isOpen && !$0.urgent })
        try await Self.send(writes.flag(task, .low), client)
        var read = try #require(try await client.record(TodayKinds.task, task.id))
        #expect(read["priority"]?.text == "low" && read["prioritySetBy"]?.text == "owner")
        state.linked.records[RecordFold.key(read.kind, read.id)] = read
        (day, engine, writes) = make()
        try await Self.send(writes.move(try #require(day.task(task.id)), to: day.startOfTomorrow), client)
        read = try #require(try await client.record(TodayKinds.task, task.id))
        #expect(read["notBefore"]?.text.flatMap(Timestamp.parse) == day.startOfTomorrow && read["deferrals"]?.int == task.deferrals + 1)
        state.linked.records[RecordFold.key(read.kind, read.id)] = read
        (day, engine, writes) = make()
        try await Self.send(writes.status(try #require(day.task(task.id)), .done), client)
        #expect(try await client.record(TodayKinds.task, task.id)?["status"]?.text == "done")

        // A typed task, and a suggested task made a task.
        let added = writes.writes.newID()
        try await Self.send(writes.add("A task typed in a test", id: added), client)
        #expect(try await client.record(TodayKinds.task, added)?["status"]?.text == "open")
        if let proposed = day.tasks.first(where: \.isProposed) {
            try await Self.send(writes.answer(proposed, .task), client)
            let made = try #require(try await client.record(TodayKinds.task, proposed.id))
            #expect(made["status"]?.text == "open" && made["prioritySetBy"]?.text == "owner")
        }

        // A project's priority.
        let list = try #require(day.lists.first { $0.flag == nil })
        try await Self.send(writes.priority(list, .medium), client)
        #expect(try await client.record(TodayKinds.list, list.id)?["priority"]?.text == "medium")

        // The plan, a review mark and the preference: each under its fixed id.
        try await Self.send(
            writes.plan([
                "picked": .array([TodayWrites.reference(task.path)]), "savedAt": TodayWrites.stamp(now), "overlaps": TodayWrites.encode([PlanRecord.Overlap]()),
            ]), client)
        let plan = try #require(try await client.record(TodayKinds.plan, PlanRecord.id(day.today)))
        #expect(PlanRecord(plan)?.picked == [task.path] && PlanRecord(plan)?.savedAt != nil)
        if let digest = day.digests.first {
            try await Self.send(writes.mark(digest.key, through: now), client)
            #expect(try await client.record(TodayKinds.reviewmark, digest.key)?["through"]?.text != nil)
        }
        try await Self.send(writes.preference(["lastActiveAt": TodayWrites.stamp(now), "timezone": .string(TimeZone.current.identifier)]), client)
        #expect(try await client.record(TodayKinds.preference, TodayKinds.preferenceID)?["timezone"]?.text == TimeZone.current.identifier)

        // A request: a flag on a task accepted as the owner's, any other rejected.
        for suggestion in day.suggestions.prefix(2) {
            let accept = suggestion.target.hasPrefix(TodayKinds.task + "/")
            try await Self.send(writes.decide(suggestion, accept: accept), client)
            let decided = try #require(try await client.record(TodayKinds.patchRequest, suggestion.id))
            #expect(decided["decision"]?.text == (accept ? "accepted" : "rejected"))
            print("LIVE request:", suggestion.target, decided["decision"]?.text ?? "-")
            if accept, let target = RecordReference(path: suggestion.target), let record = try await client.record(target.kind, target.recordID) {
                #expect(record["prioritySetBy"]?.text == "owner" && record["priority"]?.text == suggestion.flag?.rawValue)
            }
        }
    }
}
