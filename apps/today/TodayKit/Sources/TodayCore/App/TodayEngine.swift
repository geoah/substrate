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
                next.sourcesAsking = []
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
        case let .localRead(notes, windows, sources, at, today, zone):
            // What was read since the app opened stands over what the device kept.
            if let sources { next.sources = sources.merging(next.sources) { _, read in read } }
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
            next.sourcesAsking = []
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
            next.exchanges.removeAll { $0.subject == exchange.subject && $0.state != .waiting && $0.state != .writing && $0.subject == .day }
            next.exchanges.append(exchange)
        case let .answer(id, part):
            guard let index = next.exchanges.firstIndex(where: { $0.id == id }) else { break }
            switch part {
            case let .thread(thread): next.exchanges[index].thread = thread
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
        // The pages of what the tasks were made from, a few at a time and
        // one batch at once: the next is named when the last has answered.
        switch event {
        case .records, .localRead, .sourcesRead:
            if next.sourcesAsking.isEmpty, next.notesRead, next.linked.link?.signedIn == true, next.linked.link?.online != false {
                next.sourcesAsking = Self.missingSources(next)
            }
        default: break
        }
        if next != previous { next.revision = previous.revision &+ 1 }
        return next
    }

    /// The records open tasks and suggested tasks were made from whose
    /// page the root has not read or asked for, a few at a time.
    static func missingSources(_ state: State) -> [String] {
        var wanted: Set<String> = []
        for record in state.linked.records.values where record.kind == TodayKinds.task {
            let status = record["status"]?.text ?? "open"
            guard status == "open" || status == "proposed", let path = record["origin"]?.referencePath else { continue }
            guard state.sources[path] == nil, !state.sourcesNone.contains(path), !state.sourcesFailed.contains(path) else { continue }
            wanted.insert(path)
        }
        return Array(wanted.sorted().prefix(8))
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
