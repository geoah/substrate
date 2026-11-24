import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// The root's engine: what each event leaves in the state. Pure, so a test
/// folds events through it.
public extension TodayApp {
    func apply(_ event: Event, to state: State, from previous: State) throws -> State {
        var next = state
        switch event {
        case let .records(records):
            switch records {
            case let .notQueued(_, words): next.problem = words
            case .cleared:
                next.windows = Windows()
                next.requests = []
                next.contacts = [:]
                next.contactsAsked = []
                next.sources = [:]
                next.sourcesNone = []
                next.sourcesFailed = []
                next.sourcesChecked = []
                next.sourcesAsking = []
                next.projectsOpened = []
                next.exchanges = []
                next.threads = [:]
            // Back in front, or linked again: what could not be read is asked for again.
            case .foregrounded, .linked: next.sourcesFailed = []
            default: break
            }
            // The night starts at the last look of the day before: once the
            // preference is read, where the device's notes did not say.
            if var notes = next.notes, notes.nightFrom == nil, let woke = notes.wokeAt,
                let last = next.linked.records[RecordFold.key(TodayKinds.preference, TodayKinds.preferenceID)]?["lastActiveAt"]?.text
                    .flatMap(Timestamp.parse), last < woke
            {
                notes.nightFrom = last
                next.notes = notes
            }
        case let .discovered(discovery):
            next.discovery = discovery
            next.discoveryProblem = nil
            if discovery.linked != linked.kinds(in: next.linked) { next.linked = linked.apply(.rekinded(discovery.linked), to: next.linked) }
        case let .discoveryFailed(words):
            next.discoveryProblem = words
        case let .localRead(notes, windows, sources, moss, at, today, zone):
            // What was read since the app opened stands over what the device kept.
            if let sources { next.sources = sources.merging(next.sources) { _, read in read } }
            if let moss = moss?.recent(at: at, today: today) {
                next.threads = moss.threads.merging(next.threads) { _, opened in opened }
                let asked = Set(next.exchanges.map(\.id))
                next.exchanges = moss.exchanges.filter { !asked.contains($0.id) } + next.exchanges
            }
            next.notesRead = true
            next.zone = zone
            next.notes = Self.rolled(notes, at: at, today: today)
            if let windows, next.windows.readAt == nil { next.windows = windows }
        case let .windowsRead(windows, requests, stale):
            next.windows = windows
            next.requests = requests
            next.stale = stale
            next.windowsProblem = nil
        case let .windowsFailed(words):
            next.windowsProblem = words
        case let .contactsRead(records, asked):
            for record in records { next.contacts["\(record.kind)/\(record.id)"] = record }
            next.contactsAsked.formUnion(asked)
        case let .sourcesRead(found, none, failed):
            next.sources.merge(found) { _, read in read }
            next.sourcesNone.formUnion(none)
            next.sourcesFailed.formUnion(failed)
            next.sourcesChecked.formUnion(found.keys)
            next.sourcesAsking = []
        case let .pageOpened(route):
            // A project's page: what its timeline was written from is read for its pages, the latest opened first.
            if case let .project(id) = route, next.projectsOpened.last != id {
                next.projectsOpened.removeAll { $0 == id }
                next.projectsOpened.append(id)
            }
        case let .looked(at, today, zone):
            next.zone = zone
            if next.notesRead { next.notes = Self.rolled(next.notes, at: at, today: today) }
        case let .drafted(drafts):
            next.drafts = drafts
        case let .noted(notes):
            next.notes = notes
        case let .decided(ids):
            next.requests.removeAll { ids.contains($0.id) }
        case let .asked(exchange, _, _):
            // A conversation takes one question at a time. What was still being answered in it is closed where it
            // stands: the server takes the new message into the same turn, and its answer covers both.
            for index in next.exchanges.indices where next.exchanges[index].scope == exchange.scope && !exchange.scope.isEmpty {
                guard next.exchanges[index].state == .waiting || next.exchanges[index].state == .writing else { continue }
                next.exchanges[index].state = .done
                if next.exchanges[index].text.isEmpty { next.exchanges[index].text = MossKept.superseded }
            }
            next.exchanges.removeAll { $0.subject == exchange.subject && $0.state != .waiting && $0.state != .writing && $0.subject == .day }
            next.exchanges.append(exchange)
        case let .answer(id, part):
            guard let index = next.exchanges.firstIndex(where: { $0.id == id }) else { break }
            switch part {
            case let .thread(thread):
                // The thread the question went into is the conversation's from here on: the next one continues it.
                next.exchanges[index].thread = thread
                let scope = next.exchanges[index].scope
                if !scope.isEmpty { next.threads[scope] = MossThread(thread: thread, at: next.exchanges[index].at) }
            case let .delta(text):
                next.exchanges[index].text += text
                next.exchanges[index].state = .writing
                next.reachable = true
            case .step:
                let said = next.exchanges[index].text
                if !said.isEmpty, !said.hasSuffix("\n") { next.exchanges[index].text += "\n\n" }
            case let .done(reply):
                if let reply, !reply.isEmpty { next.exchanges[index].text = reply }
                next.exchanges[index].state = .done
                next.reachable = true
            case let .failed(words):
                next.exchanges[index].text = words
                next.exchanges[index].state = .failed
                if words.hasPrefix("Moss could not be reached") { next.reachable = false }
            }
        case let .answerClosed(id):
            next.exchanges.removeAll { $0.id == id }
        case let .kept(id):
            guard let index = next.exchanges.firstIndex(where: { $0.id == id }) else { break }
            next.exchanges[index].text = "Added as a task. Moss did not see it."
            next.exchanges[index].state = .done
        case .stopped:
            for index in next.exchanges.indices where next.exchanges[index].state == .waiting || next.exchanges[index].state == .writing {
                next.exchanges[index].state = .done
                if next.exchanges[index].text.isEmpty { next.exchanges[index].text = "Stopped." }
            }
        case .later:
            break
        case let .usage(usage):
            next.usage = usage
        case let .offered(version):
            next.offered = version
        case .problemDismissed:
            next.problem = nil
        case .permissionWanted:
            break
        case let .permission(permission):
            next.permission = permission
        case let .scheduled(plan):
            next.scheduled = plan
        case let .ticked(at):
            // Midnight passed with the pane open: the notes are a new day's.
            if next.notesRead, let zone = next.zone.flatMap(TimeZone.init(identifier:)) {
                let today = CivilDay(at, in: Self.calendar(zone))
                if next.notes?.day != today { next.notes = Self.rolled(next.notes, at: at, today: today) }
            }
        }
        // The pages of what the tasks were made from and the digests were
        // written from, a few at a time and one batch at once: the next is
        // named when the last has answered.
        switch event {
        case .records, .localRead, .sourcesRead, .pageOpened:
            if next.sourcesAsking.isEmpty, next.notesRead, next.linked.link?.signedIn == true, next.linked.link?.online != false {
                next.sourcesAsking = Self.missingSources(next)
            }
        default: break
        }
        if next != previous { next.revision = previous.revision &+ 1 }
        return next
    }

    /// How many records are read for their page at once.
    static let sourceBatch = 8

    /// The records whose page the root has not read or asked for, a few at
    /// a time, in the order they are wanted:
    /// - what open tasks and suggested tasks were made from;
    /// - what the timeline of the project's page opened last was written
    ///   from, since that page is on the screen;
    /// - what the day draws of every digest, the newest first: each item
    ///   newer than its project's review mark, and each since the night began;
    /// - the timelines of the project pages opened before.
    ///
    /// A digest's item is read for the first record it names, its thread's
    /// opener. The items of a project nobody opened that are older than
    /// both its mark and the night are not read: a repository holds dozens
    /// of digests of up to thirty items.
    static func missingSources(_ state: State) -> [String] {
        var wanted: [String] = []
        var named: Set<String> = []
        func want(_ paths: some Sequence<String>) {
            for path in paths where named.insert(path).inserted && !sourceKnown(path, in: state) { wanted.append(path) }
        }
        var origins: Set<String> = []
        // Of each digest, by its project: the first record each item names whose page is not known, and when
        // the item happened. An item whose page is known costs a look-up and no more, since this is asked at
        // every change of the records.
        var unknown: [String: [(at: Date, path: String)]] = [:]
        var marks: [String: Date] = [:]
        for record in state.linked.records.values {
            switch record.kind {
            case TodayKinds.task:
                let status = record["status"]?.text ?? "open"
                if status == "open" || status == "proposed", let path = record["origin"]?.referencePath { origins.insert(path) }
            case TodayKinds.digest:
                let items = (record["items"]?.array ?? []).compactMap { item -> (at: Date, path: String)? in
                    guard let path = item["sources"]?.array?.lazy.compactMap(\.referencePath).first, !sourceKnown(path, in: state) else { return nil }
                    guard item["text"]?.text != nil, let at = item["at"]?.text.flatMap(Timestamp.parse) else { return nil }
                    return (at, path)
                }
                if !items.isEmpty { unknown[record["project"]?.referenceID ?? record.id] = items }
            case TodayKinds.reviewmark:
                if let through = record["through"]?.text.flatMap(Timestamp.parse) { marks[record["project"]?.referenceID ?? record.id] = through }
            default: break
            }
        }
        func newestFirst(_ items: [(at: Date, path: String)]) -> [String] { items.sorted { ($0.at, $1.path) > ($1.at, $0.path) }.map(\.path) }
        want(origins.sorted())
        if let shown = state.projectsOpened.last { want(newestFirst(unknown[shown] ?? [])) }
        if wanted.count < sourceBatch {
            // Where the night began, as the device's notes hold it: what is happening and the night draw every item since.
            let night = state.notes.flatMap { $0.nightFrom ?? $0.wokeAt?.addingTimeInterval(-8 * 3600) }
            var drawn: [(at: Date, path: String)] = []
            for (project, items) in unknown {
                guard let from = [marks[project], night].compactMap({ $0 }).min() else { continue }
                drawn += items.filter { $0.at > from }
            }
            want(newestFirst(drawn))
        }
        if wanted.count < sourceBatch {
            for project in state.projectsOpened.dropLast().reversed() { want(newestFirst(unknown[project] ?? [])) }
        }
        return Array(wanted.prefix(sourceBatch))
    }

    /// Whether a record's page needs no read: it was read, it names none,
    /// or its read failed and waits for the app to come back. A Slack
    /// message's page that says no channel is one an earlier build kept,
    /// or one whose channel has no name: it is read once more.
    static func sourceKnown(_ path: String, in state: State) -> Bool {
        if state.sourcesNone.contains(path) || state.sourcesFailed.contains(path) { return true }
        guard let page = state.sources[path] else { return false }
        return page.group != nil || state.sourcesChecked.contains(path) || !SourcePages.namesGroup(path)
    }

    /// The day's notes after a look: the same day's with the look kept, or
    /// a new day's that starts the night at the look before.
    static func rolled(_ notes: DayNotes?, at: Date, today: CivilDay) -> DayNotes {
        if var notes, notes.day == today {
            if notes.wokeAt == nil { notes.wokeAt = at }
            notes.lastLook = at
            return notes
        }
        var fresh = DayNotes(day: today)
        fresh.nightFrom = notes?.lastLook ?? notes?.wokeAt
        fresh.wokeAt = at
        fresh.lastLook = at
        return fresh
    }
}
