import Contacts
import EventKit
import Foundation
import HealthKit
import UIKit

/// The owner's choices, kept in UserDefaults.
enum Settings {
    static let backfillKey = "health.backfillDays"
    static let backfillChoices = [7, 14, 30, 90]
    /// The calendar window: occurrences from this far back to this far ahead.
    static let windowBack = 90
    static let windowAhead = 180

    static func enabled(_ source: Source) -> Bool {
        UserDefaults.standard.object(forKey: "enabled.\(source.rawValue)") as? Bool ?? true
    }

    static func setEnabled(_ source: Source, _ on: Bool) {
        UserDefaults.standard.set(on, forKey: "enabled.\(source.rawValue)")
    }

    /// How often to sync without the app open. The system picks the moment;
    /// this is the earliest it may, and how often HealthKit wakes the app.
    enum Frequency: String, CaseIterable, Identifiable {
        case manual, hourly, sixHours, daily

        var id: String { rawValue }

        var title: String {
            switch self {
            case .manual: "Only when I tap Sync Now"
            case .hourly: "Hourly"
            case .sixHours: "Every 6 hours"
            case .daily: "Daily"
            }
        }

        var interval: TimeInterval? {
            switch self {
            case .manual: nil
            case .hourly: 3600
            case .sixHours: 6 * 3600
            case .daily: 24 * 3600
            }
        }
    }

    static let frequencyKey = "sync.frequency"

    static var frequency: Frequency {
        UserDefaults.standard.string(forKey: frequencyKey).flatMap(Frequency.init(rawValue:)) ?? .hourly
    }

    static var backfillDays: Int {
        let v = UserDefaults.standard.integer(forKey: backfillKey)
        return backfillChoices.contains(v) ? v : 14
    }
}

enum SyncError: LocalizedError {
    case noAccess(String)
    case notArmed
    case emptyScan(String)

    var errorDescription: String? {
        switch self {
        case let .noAccess(what): "No access to \(what). Allow it in Settings."
        case .notArmed: "The Apple Health trigger is not armed. Run Set up again."
        case let .emptyScan(what):
            "The device returned no \(what), though earlier syncs pushed some. Nothing was deleted; sync again once they are back."
        }
    }
}

/// One sync run at a time; each source in turn, its outcome recorded.
actor SyncEngine {
    static let shared = SyncEngine()

    static let appleKinds = "providers.substrate.reamde.dev/apple/"
    static let healthAccountKind = "providers.substrate.reamde.dev/applehealth/account"
    static let healthExportKind = "providers.substrate.reamde.dev/applehealth/export"
    static let triggerKind = "substrate.reamde.dev/core/trigger"
    static let trigger = "applehealth-webhook"

    private var running: Task<Void, Never>?
    let health = HKHealthStore()
    /// Changes the app asked for while a run holds its own copy of the state;
    /// the run applies them at its next save, so neither side's write is lost.
    private var pending: [@Sendable (inout LocalState) -> Void] = []

    /// The one way anything but a run changes the state file.
    func update(_ change: @escaping @Sendable (inout LocalState) -> Void) -> LocalState {
        if running != nil {
            pending.append(change)
            var copy = LocalState.load()
            change(&copy)
            return copy
        }
        var state = LocalState.load()
        change(&state)
        commit(&state)
        return state
    }

    /// A run's save: the app's pending changes first, then the file.
    private func commit(_ state: inout LocalState) {
        for change in pending { change(&state) }
        pending = []
        state.save()
    }

    /// Stops the running sync where it can stop cleanly.
    func cancel() {
        running?.cancel()
    }

    /// Called on the main actor after every source finishes.
    var onChange: (@MainActor @Sendable (LocalState) -> Void)?
    /// Called on the main actor as a source works: what it is doing now, or
    /// nil when it is done.
    var onProgress: (@MainActor @Sendable (Source, SyncProgress?) -> Void)?

    func setOnChange(_ f: @escaping @MainActor @Sendable (LocalState) -> Void) {
        onChange = f
    }

    func setOnProgress(_ f: @escaping @MainActor @Sendable (Source, SyncProgress?) -> Void) {
        onProgress = f
    }

    private func progress(_ source: Source, _ step: String?, _ done: Int = 0, _ total: Int = 0) async {
        if let onProgress { await onProgress(source, step.map { SyncProgress(step: $0, done: done, total: total) }) }
    }

    private func log(_ source: Source?, _ message: String, _ level: ActivityEntry.Level = .info) {
        Activity.shared.log(source, message, level: level)
    }

    /// Logs the first few failed writes with the server's own words, and
    /// says how many more there were, so one refused row is readable and a
    /// thousand do not bury the log.
    private func logFailures(_ source: Source, _ what: String, _ failures: [(String, Error)]) {
        for (name, error) in failures.prefix(5) {
            log(source, "\(what) \(name) failed: \(error.localizedDescription)", .error)
        }
        if failures.count > 5 { log(source, "\(failures.count - 5) more \(what) writes failed", .error) }
    }

    /// A background wake-up syncs a source only when the chosen interval has
    /// passed since its last sync; Sync Now always does.
    func syncIfDue(_ only: Set<Source>? = nil) async {
        guard let interval = Settings.frequency.interval else { return }
        let state = LocalState.load()
        let due = Set(Source.allCases.filter { source in
            (only?.contains(source) ?? true)
                && (state.status[source]?.lastSync.map { Date().timeIntervalSince($0) >= interval * 0.9 } ?? true)
        })
        if !due.isEmpty { await sync(due) }
    }

    /// Runs a sync of the sources given (all enabled ones by default), after
    /// the one already running, if any: a background run never swallows the
    /// owner's Sync Now.
    func sync(_ only: Set<Source>? = nil) async {
        while let running {
            await running.value
        }
        let task = Task { await self.run(only) }
        running = task
        await task.value
        running = nil
        if !pending.isEmpty {
            var state = LocalState.load()
            commit(&state)
        }
    }

    private func run(_ only: Set<Source>?) async {
        guard let session = Keychain.load() else { return }
        let client = Client(server: session.server, secret: session.secret)
        for source in Source.allCases where Settings.enabled(source) && (only?.contains(source) ?? true) {
            if Task.isCancelled { return }
            var state = LocalState.load()
            var status = state.status[source] ?? SourceStatus()
            let started = Date()
            await progress(source, "Starting")
            log(source, "Sync started")
            do {
                switch source {
                case .contacts: status.counts = try await syncContacts(client, &state)
                case .calendars: status.counts = try await syncCalendars(client, &state)
                case .health: status.counts = try await syncHealth(client, session, &state)
                }
                status.lastSync = Date()
                status.lastError = nil
            } catch is CancellationError {
                status.lastError = "Stopped before it finished."
            } catch {
                status.lastError = error.localizedDescription
            }
            if let error = status.lastError {
                log(source, "Sync failed: \(error)", .error)
            } else {
                log(source, "Sync finished: \(status.counts ?? "")")
            }
            state.status[source] = status
            state.record(SyncRun(source: source, started: started, finished: Date(), summary: status.lastError == nil ? status.counts : nil, error: status.lastError))
            commit(&state)
            await progress(source, nil)
            if let onChange { await onChange(state) }
        }
    }

    /// What became of one run's deliveries, by day.
    struct Outcome: Equatable {
        var sent: [String] = []
        var failed: [String: String] = [:]
        var pending: [String] = []
    }

    /// A day is sent when every delivery it made is filed (the provider wrote
    /// its receipt), refused when any parked, and pending otherwise.
    static func settle(_ fires: [String: [String]], filed: Set<String>, parked: [String: String]) -> Outcome {
        var out = Outcome()
        for (day, ids) in fires.sorted(by: { $0.key < $1.key }) {
            if let reason = ids.lazy.compactMap({ parked[$0] }).first {
                out.failed[day] = reason
            } else if ids.allSatisfy(filed.contains) {
                out.sent.append(day)
            } else {
                out.pending.append(day)
            }
        }
        return out
    }

    /// Waits, up to three minutes, for the provider to file or park every
    /// delivery the run posted, and says what became of each day.
    private func confirm(_ fires: [String: [String]], _ client: Client) async throws -> Outcome {
        let all = Set(fires.values.joined())
        var filed = Set<String>()
        var parked: [String: String] = [:]
        let deadline = Date().addingTimeInterval(180)
        while true {
            let open = all.subtracting(filed).subtracting(parked.keys)
            await progress(.health, "Waiting for the server to file the deliveries", all.count - open.count, all.count)
            if open.isEmpty || Date() > deadline { break }
            try Task.checkCancellation()
            for fire in open where try await client.get(Self.healthExportKind, fire) != nil {
                filed.insert(fire)
            }
            for p in try await client.parked(trigger: Self.trigger) {
                if let f = p.fireId, open.contains(f) { parked[f] = p.lastError }
            }
            if all.subtracting(filed).subtracting(parked.keys).isEmpty { continue }
            try await Task.sleep(for: .seconds(3))
        }
        return Self.settle(fires, filed: filed, parked: parked)
    }

    /// A row's name for the log: its display name or title where it has one.
    static func label(_ props: JSON, _ id: String) -> String {
        for key in ["displayName", "eventTitle", "organizationName", "calendarTitle"] {
            if let v = props.objectValue(key), case let .string(name) = v { return "\"\(name)\"" }
        }
        return id
    }

    private var deviceName: String {
        get async { await MainActor.run { UIDevice.current.name } }
    }

    // MARK: - Contacts

    private func syncContacts(_ client: Client, _ state: inout LocalState) async throws -> String {
        let auth = CNContactStore.authorizationStatus(for: .contacts)
        guard auth == .authorized || auth == .limited else { throw SyncError.noAccess("Contacts") }
        let ids = state.ids
        let addressbook = ids.addressbook
        let kind = Self.appleKinds + "contact"
        let book = Self.appleKinds + "addressbook"

        let contacts: [CNContact] = try await Task.detached {
            var out: [CNContact] = []
            let request = CNContactFetchRequest(keysToFetch: ContactMapping.keys)
            request.unifyResults = true
            try CNContactStore().enumerateContacts(with: request) { c, _ in out.append(c) }
            return out
        }.value

        state.device.contacts = contacts.count
        await progress(.contacts, "Read \(contacts.count) contacts")
        log(.contacts, "Read \(contacts.count) contacts from the device")
        let name = await deviceName
        try await client.put(book, addressbook, ["deviceName": name])

        var current: [String: JSON] = [:]
        for c in contacts { current[ids.contact(c.identifier)] = ContactMapping.properties(c, addressbook: addressbook) }
        let previous = state.contacts
        let changed = current.filter { previous[$0.key]?.hash != Pushed.hash($0.value) }
        // An empty answer is more often a store that is not ready, or access
        // narrowed to nothing, than an owner who deleted every contact; it
        // never empties the repository.
        if contacts.isEmpty, !previous.isEmpty { throw SyncError.emptyScan("contacts") }
        let vanished = previous.keys.filter { current[$0] == nil }
        log(.contacts, "\(changed.count) new or changed, \(contacts.count - changed.count) unchanged, \(vanished.count) gone from the device")
        let toWrite = changed.count
        await progress(.contacts, "Writing contacts", 0, toWrite)

        let writes = await eachLimited(Array(changed), progress: { n in await self.progress(.contacts, "Writing contacts", n, toWrite) }) { pair in
            let (id, props) = pair
            try await client.put(kind, id, props.foundation)
            let gone = Set(previous[id]?.keys ?? []).subtracting(props.keys)
            try await client.clear(kind, id, keys: gone.sorted())
        }
        var failures: [Error] = []
        var named: [(String, Error)] = []
        for ((id, props), result) in writes {
            switch result {
            case .success: state.contacts[id] = Pushed(props)
            case let .failure(e):
                failures.append(e)
                named.append((Self.label(props, id), e))
            }
        }
        if !vanished.isEmpty { await progress(.contacts, "Removing contacts gone from the device", 0, vanished.count) }
        let deletes = await eachLimited(vanished) { id in try await client.delete(kind, id) }
        for (id, result) in deletes {
            switch result {
            case .success: state.contacts[id] = nil
            case let .failure(e):
                failures.append(e)
                named.append((id, e))
            }
        }
        commit(&state)
        logFailures(.contacts, "contact", named)
        if let first = failures.first {
            throw ContextError(context: "\(failures.count) of \(changed.count + vanished.count) contact writes failed, the first", underlying: first)
        }

        try await client.put(book, addressbook, [
            "deviceName": name, "contactCount": contacts.count, "lastSyncedAt": Formats.utc(Date()),
        ])
        return "\(contacts.count) contacts · \(changed.count) written · \(vanished.count) removed"
    }

    // MARK: - Calendars

    private func syncCalendars(_ client: Client, _ state: inout LocalState) async throws -> String {
        guard EKEventStore.authorizationStatus(for: .event) == .fullAccess else { throw SyncError.noAccess("Calendars") }
        let ids = state.ids
        let eventstore = ids.eventstore
        let storeKind = Self.appleKinds + "eventstore"
        let calendarKind = Self.appleKinds + "calendar"
        let eventKind = Self.appleKinds + "event"

        let now = Date()
        let cal = Calendar.current
        let windowStart = cal.startOfDay(for: cal.date(byAdding: .day, value: -Settings.windowBack, to: now)!)
        let windowEnd = cal.startOfDay(for: cal.date(byAdding: .day, value: Settings.windowAhead, to: now)!)
        let window = windowStart ..< windowEnd

        let store = EKEventStore()
        let calendars = store.calendars(for: .event)
        // A large window takes seconds to read: not on the actor, which the
        // progress callbacks and the app's updates go through.
        let occurrences = await Task.detached {
            store.events(matching: store.predicateForEvents(withStart: windowStart, end: windowEnd, calendars: nil))
        }.value

        state.device.calendars = calendars.count
        state.device.events = occurrences.count
        await progress(.calendars, "Read \(calendars.count) calendars, \(occurrences.count) events")
        log(.calendars, "Read \(calendars.count) calendars and \(occurrences.count) events from \(Formats.day(windowStart, in: .current)) to \(Formats.day(windowEnd, in: .current))")
        let name = await deviceName
        try await client.put(storeKind, eventstore, ["deviceName": name])
        var failures: [Error] = []

        var currentCalendars: [String: JSON] = [:]
        for c in calendars { currentCalendars[ids.calendar(c.calendarIdentifier)] = EventMapping.calendar(c, eventstore: eventstore) }
        let previousCalendars = state.calendars
        let changedCalendars = currentCalendars.filter { previousCalendars[$0.key]?.hash != Pushed.hash($0.value) }
        for ((id, props), result) in await eachLimited(Array(changedCalendars), { pair in
            let (id, props) = pair
            try await client.put(calendarKind, id, props.foundation)
            try await client.clear(calendarKind, id, keys: Set(previousCalendars[id]?.keys ?? []).subtracting(props.keys).sorted())
        }) {
            switch result {
            case .success: state.calendars[id] = Pushed(props)
            case let .failure(e): failures.append(e)
            }
        }
        // A calendar gone from the device goes with its events: the server
        // cascades, and this side forgets them.
        // The same for calendars: a deleted calendar takes its events with it,
        // so an empty scan deletes nothing.
        if calendars.isEmpty, !previousCalendars.isEmpty { throw SyncError.emptyScan("calendars") }
        let goneCalendars = previousCalendars.keys.filter { currentCalendars[$0] == nil }
        for (id, result) in await eachLimited(goneCalendars, { id in try await client.delete(calendarKind, id) }) {
            switch result {
            case .success:
                state.calendars[id] = nil
                state.events = state.events.filter { $0.value.calendar != id }
                log(.calendars, "Removed a calendar gone from the device, with its events")
            case let .failure(e): failures.append(e)
            }
        }
        if let first = failures.first {
            commit(&state)
            throw ContextError(context: "\(failures.count) calendar writes failed, the first", underlying: first)
        }

        struct Occurrence: Sendable { let props: JSON; let start: Date?; let calendar: String }
        var current: [String: Occurrence] = [:]
        for e in occurrences {
            guard let calendar = e.calendar else { continue }
            let calendarID = ids.calendar(calendar.calendarIdentifier)
            let id = ids.event(key: EventMapping.key(e))
            current[id] = Occurrence(props: EventMapping.event(e, calendar: calendarID), start: e.startDate, calendar: calendarID)
        }
        let previous = state.events
        let changed = current.filter { previous[$0.key]?.hash != Pushed.hash($0.value.props) }
        let toWrite = changed.count
        log(.calendars, "\(changed.count) events new or changed, \(current.count - changed.count) unchanged")
        await progress(.calendars, "Writing events", 0, toWrite)
        var named: [(String, Error)] = []
        for ((id, o), result) in await eachLimited(Array(changed), progress: { n in await self.progress(.calendars, "Writing events", n, toWrite) }, { pair in
            let (id, o) = pair
            try await client.put(eventKind, id, o.props.foundation)
            try await client.clear(eventKind, id, keys: Set(previous[id]?.keys ?? []).subtracting(o.props.keys).sorted())
        }) {
            switch result {
            case .success: state.events[id] = Pushed(o.props, start: o.start, calendar: o.calendar)
            case let .failure(e):
                failures.append(e)
                named.append((Self.label(o.props, id), e))
            }
        }
        // Absent from the scan: deleted when its start is inside the window,
        // kept when it aged out of it.
        let vanished = previous.filter { id, p in
            current[id] == nil && p.start.map(window.contains) ?? true
        }.map(\.key)
        if !vanished.isEmpty { await progress(.calendars, "Removing events gone from the device", 0, vanished.count) }
        for (id, result) in await eachLimited(vanished, { id in try await client.delete(eventKind, id) }) {
            switch result {
            case .success: state.events[id] = nil
            case let .failure(e):
                failures.append(e)
                named.append((id, e))
            }
        }
        commit(&state)
        logFailures(.calendars, "event", named)
        if let first = failures.first {
            throw ContextError(context: "\(failures.count) of \(changed.count + vanished.count) event writes failed, the first", underlying: first)
        }

        try await client.put(storeKind, eventstore, [
            "deviceName": name, "calendarCount": calendars.count, "eventCount": occurrences.count,
            "windowStart": Formats.utc(windowStart), "windowEnd": Formats.utc(windowEnd),
            "lastSyncedAt": Formats.utc(Date()),
        ])
        return "\(calendars.count) calendars · \(occurrences.count) events · \(changed.count) written · \(vanished.count) removed"
    }

    // MARK: - Health

    /// The days to send, oldest first: on the first sync the whole backfill,
    /// after that yesterday, today and any day in the backfill not yet sent.
    /// Queued days (the owner's, from the coverage view) come before them.
    /// The last `resend` days go again every time, because a watch can reach
    /// the phone a day or more after its samples were taken.
    static func healthDays(
        today: Date, backfill: Int, sent: Set<String>, queued: Set<String> = [], resend: Int = 3, calendar: Calendar
    ) -> [Date] {
        let start = calendar.startOfDay(for: today)
        var days = (0 ..< backfill).reversed().compactMap { back -> Date? in
            guard let day = calendar.date(byAdding: .day, value: -back, to: start) else { return nil }
            let name = Formats.day(day, in: calendar.timeZone)
            return back < resend || !sent.contains(name) ? day : nil
        }
        let names = Set(days.map { Formats.day($0, in: calendar.timeZone) })
        let parse = DateFormatter()
        parse.locale = Locale(identifier: "en_US_POSIX")
        parse.calendar = calendar
        parse.timeZone = calendar.timeZone
        parse.dateFormat = "yyyy-MM-dd"
        let extra = queued.subtracting(names).compactMap(parse.date(from:)).filter { $0 < start }
        days = (extra + days).sorted()
        return days
    }

    private func syncHealth(_ client: Client, _ session: Session, _ state: inout LocalState) async throws -> String {
        guard HealthReader.isAvailable else { throw SyncError.noAccess("Health on this device") }
        guard let trigger = try await client.get(Self.triggerKind, Self.trigger),
              trigger["enabled"] as? Bool == true,
              let key = ((trigger["source"] as? [String: Any])?["webhook"] as? [String: Any])?["key"] as? String,
              !key.isEmpty
        else { throw SyncError.notArmed }

        // The account every delivery names: one the sync cannot find is made
        // here, as Set Up makes it, rather than every delivery parking.
        if try await client.get(Self.healthAccountKind, state.healthAccount) == nil {
            let device = await deviceName
            try await client.put(Self.healthAccountKind, state.healthAccount, [
                "displayName": device, "timezone": TimeZone.current.identifier,
            ])
            log(.health, "Created the Health account \(state.healthAccount)")
        }

        for g in Grouping.allCases where state.automation(g) == nil {
            state.automations[g.rawValue] = UUID().uuidString
        }
        commit(&state)
        let reader = HealthReader(store: health)
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = .current
        let zone = TimeZone.current
        let sessionID = UUID().uuidString
        let days = Self.healthDays(
            today: Date(), backfill: Settings.backfillDays, sent: state.healthDays, queued: state.healthQueue, calendar: calendar)
        log(.health, days.isEmpty ? "No day to send" : "Sending \(days.count) days, \(Formats.day(days.first!, in: zone)) to \(Formats.day(days.last!, in: zone))")
        var posts = 0
        var skippedDays = 0
        // Each day's fire ids, settled after the posts: the door's 202 is
        // "stored", and only the provider's receipt is "filed".
        var fires: [String: [String]] = [:]
        for (i, day) in days.enumerated() {
            try Task.checkCancellation()
            let name = Formats.day(day, in: zone)
            await progress(.health, "Reading and sending \(name)", i, days.count)
            let span = day ..< calendar.date(byAdding: .day, value: 1, to: day)!
            var accepted = 0
            var rows = 0
            var skipped: [String] = []
            for grouping in Grouping.allCases {
                let delivery = grouping == .minutes
                    ? await reader.minutes(span, zone: zone)
                    : await reader.seconds(span, zone: zone)
                skipped += delivery.skipped
                rows += delivery.metrics.map(\.rows.count).reduce(0, +) + delivery.workouts.count
                let headers = HealthDelivery.headers(
                    grouping: grouping, key: key, account: state.healthAccount,
                    automationID: state.automation(grouping)!, sessionID: sessionID)
                // One at a time: the door takes eight in flight across every
                // sender, and a day's order is the order its rows replace.
                for body in delivery.bodies() {
                    do {
                        let fire = try await client.deliver(authority: session.repository, trigger: Self.trigger, body: body, headers: headers)
                        fires[name, default: []].append(fire)
                    } catch {
                        throw ContextError(context: "Sending \(name) (\(grouping.rawValue))", underlying: error)
                    }
                    posts += 1
                    accepted += 1
                }
            }
            for line in skipped {
                log(.health, "\(name): could not read \(line)", .warning)
            }
            log(.health, "\(name): \(rows) rows in \(accepted) deliveries" + (skipped.isEmpty ? "" : ", \(skipped.count) skipped"))
            // A day HealthKit answered empty (no data yet, or no read access,
            // which HealthKit does not tell apart) stays unsent, and so does
            // one with a part it could not read: the next sync reads it again
            // and sends it whole.
            if accepted == 0 || !skipped.isEmpty {
                fires[name] = nil
                if !skipped.isEmpty { skippedDays += 1 }
            }
        }

        let outcome = try await confirm(fires, client)
        for day in outcome.sent {
            state.healthDays.insert(day)
            state.healthQueue.remove(day)
        }
        // A day that did not file goes again next time, wherever it is.
        for day in Array(outcome.failed.keys) + outcome.pending {
            state.healthDays.remove(day)
            state.healthQueue.insert(day)
        }
        for (day, reason) in outcome.failed.sorted(by: { $0.key < $1.key }) {
            log(.health, "\(day) was refused by the server and will be sent again: \(reason)", .error)
        }
        if !outcome.pending.isEmpty {
            log(.health, "\(outcome.pending.count) days were stored but not yet filed; the next sync sends them again", .warning)
        }
        commit(&state)
        if let onChange { await onChange(state) }
        let sentDays = outcome.sent.count
        if let (day, reason) = outcome.failed.min(by: { $0.key < $1.key }) {
            throw ContextError(context: "\(outcome.failed.count) of \(days.count) days were refused, the first \(day)",
                               underlying: Message(reason))
        }
        await progress(.health, "Done", days.count, days.count)
        return "\(sentDays) of \(days.count) days sent · \(posts) deliveries"
            + (skippedDays > 0 ? " · \(skippedDays) days with parts skipped, see Activity" : "")
    }
}
