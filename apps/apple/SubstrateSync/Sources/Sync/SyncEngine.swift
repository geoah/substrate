import Contacts
import EventKit
import Foundation
import HealthKit
import UIKit

/// The owner's choices, kept in UserDefaults.
enum Settings {
    /// The calendar window: occurrences from this far back to this far ahead.
    static let windowBack = 90
    static let windowAhead = 180

    /// Places is off until the owner turns it on: it asks for Always
    /// location access. Every other source is on.
    static func enabled(_ source: Source) -> Bool {
        UserDefaults.standard.object(forKey: "enabled.\(source.rawValue)") as? Bool ?? (source != .places)
    }

    static func setEnabled(_ source: Source, _ on: Bool) {
        UserDefaults.standard.set(on, forKey: "enabled.\(source.rawValue)")
    }

    /// The calendars left out of the sync, by `calendarIdentifier`. The set
    /// holds the ones turned off, so a calendar added on the phone later is
    /// synced until the owner turns it off.
    static let excludedCalendarsKey = "calendars.excluded"

    static var excludedCalendars: Set<String> {
        Set(UserDefaults.standard.stringArray(forKey: excludedCalendarsKey) ?? [])
    }

    static func setCalendar(_ id: String, included: Bool) {
        var excluded = excludedCalendars
        if included { excluded.remove(id) } else { excluded.insert(id) }
        UserDefaults.standard.set(excluded.sorted(), forKey: excludedCalendarsKey)
    }

    /// How far back Health is synced. Every sync sends the last few days;
    /// the rest of the range goes once, a day with data at a time.
    enum History: String, CaseIterable, Identifiable, Sendable {
        case days14, days30, months3, months6, year1, all

        var id: String { rawValue }

        var title: String {
            switch self {
            case .days14: "14 days"
            case .days30: "30 days"
            case .months3: "3 months"
            case .months6: "6 months"
            case .year1: "1 year"
            case .all: "Everything"
            }
        }

        /// The first day of the range, or nil for everything the phone holds.
        func start(today: Date, calendar: Calendar) -> Date? {
            let day = calendar.startOfDay(for: today)
            switch self {
            case .days14: return calendar.date(byAdding: .day, value: -13, to: day)
            case .days30: return calendar.date(byAdding: .day, value: -29, to: day)
            case .months3: return calendar.date(byAdding: .month, value: -3, to: day)
            case .months6: return calendar.date(byAdding: .month, value: -6, to: day)
            case .year1: return calendar.date(byAdding: .year, value: -1, to: day)
            case .all: return nil
            }
        }
    }

    static let historyKey = "health.history"
    private static let oldBackfillKey = "health.backfillDays"

    static var history: History {
        if let raw = UserDefaults.standard.string(forKey: historyKey), let h = History(rawValue: raw) { return h }
        // The earlier day-count setting, carried over.
        switch UserDefaults.standard.integer(forKey: oldBackfillKey) {
        case 30: return .days30
        case 90: return .months3
        default: return .days14
        }
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
}

enum SyncError: LocalizedError {
    case noAccess(String)
    case notArmed
    case emptyScan(String)
    case unreadable(String)
    case signedOut

    var errorDescription: String? {
        switch self {
        case let .noAccess(what): "No access to \(what). Allow it in the Settings app."
        case .notArmed: "The Apple Health webhook is not armed. Open Set Up and run it again."
        case let .emptyScan(what):
            "The device returned no \(what), though earlier syncs pushed some. Nothing was deleted; sync again once they are back."
        case let .unreadable(what): "The device could not read your \(what) just now. Nothing was deleted; sync again in a moment."
        case .signedOut: "Your sign-in is no longer valid. Sign in again."
        }
    }
}

/// How much a run may do: everything when the owner asked or the system gave
/// a long task, and only the last few Health days, with a short wait for the
/// server, when a background wake has seconds.
enum RunBudget: Sendable {
    case full
    case background

    var confirmDeadline: TimeInterval { self == .full ? 120 : 20 }
}

/// Whether an error is a stop rather than a failure: a cancelled task, or a
/// request URLSession cancelled with it.
func isCancellation(_ error: Error) -> Bool {
    if error is CancellationError { return true }
    if let e = error as? URLError, e.code == .cancelled { return true }
    if let e = error as? ContextError { return isCancellation(e.underlying) }
    return false
}

/// Whether the server refused the token: the session is over.
func isSignedOut(_ error: Error) -> Bool {
    if let e = error as? ServerError, e.status == 401 { return true }
    if let e = error as? ContextError { return isSignedOut(e.underlying) }
    if case SyncError.signedOut = error { return true }
    return false
}

/// One sync run at a time; each source in turn, its outcome recorded.
actor SyncEngine {
    static let shared = SyncEngine()

    static let appleKinds = "providers.substrate.reamde.dev/apple/"
    static let healthAccountKind = "providers.substrate.reamde.dev/applehealth/account"
    static let healthExportKind = "providers.substrate.reamde.dev/applehealth/export"
    static let triggerKind = "substrate.reamde.dev/core/trigger"
    static let trigger = "applehealth-webhook"
    /// Days a Health run sends and confirms together.
    static let chunk = 7

    /// The newest run, which waits for the one before it: chaining rather
    /// than polling, so a finished run never leaves a waiter spinning on the
    /// actor.
    private var running: Task<Void, Never>?
    private var inFlight: [Task<Void, Never>] = []
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

    /// Stops every queued and running sync where it can stop cleanly.
    func cancel() {
        for task in inFlight { task.cancel() }
    }

    /// Called on the main actor after every source finishes.
    var onChange: (@MainActor @Sendable (LocalState) -> Void)?
    /// Called on the main actor as a source works: what it is doing now, or
    /// nil when it is done.
    var onProgress: (@MainActor @Sendable (Source, SyncProgress?) -> Void)?
    /// Called on the main actor when the server refuses the token.
    var onSignedOut: (@MainActor @Sendable () -> Void)?

    func setOnChange(_ f: @escaping @MainActor @Sendable (LocalState) -> Void) {
        onChange = f
    }

    func setOnProgress(_ f: @escaping @MainActor @Sendable (Source, SyncProgress?) -> Void) {
        onProgress = f
    }

    func setOnSignedOut(_ f: @escaping @MainActor @Sendable () -> Void) {
        onSignedOut = f
    }

    private func progress(_ source: Source, _ step: String?, _ done: Int = 0, _ total: Int = 0, overall: Double? = nil) async {
        if let onProgress { await onProgress(source, step.map { SyncProgress(step: $0, done: done, total: total, overall: overall) }) }
    }

    /// How far a Health run over `days` has got, with each day half read and
    /// sent and half filed by the server: `before` days are done, and the
    /// chunk of `width` after them has got `part` of the way, where reading
    /// it is the first half and the receipts the second. It only rises, and
    /// it moves while the phone waits on the server.
    static func healthOverall(before: Int, width: Int, part: Double, days: Int) -> Double {
        guard days > 0 else { return 1 }
        return min(1, (Double(before) + Double(width) * min(1, max(0, part))) / Double(days))
    }

    private func log(_ source: Source?, _ message: String, _ level: ActivityEntry.Level = .info) {
        Activity.shared.log(source, message, level: level)
    }

    /// Logs the first few failed writes with the server's own words, and
    /// says how many more there were, so one refused row is readable and a
    /// thousand do not bury the log.
    private func logFailures(_ source: Source, _ what: String, _ failures: [(String, Error)]) {
        for (name, error) in failures.prefix(5) {
            log(source, "\(what) \(name) failed: \(explain(error))", .error)
        }
        if failures.count > 5 { log(source, "\(failures.count - 5) more \(what) writes failed", .error) }
    }

    /// A background wake-up syncs a source only when the chosen interval has
    /// passed since it last RAN, finished or not: a source whose runs keep
    /// failing is retried at the owner's frequency, not at every HealthKit
    /// sample. A wake while a sync runs or waits adds nothing; Sync Now
    /// always syncs.
    func syncIfDue(_ only: Set<Source>? = nil, budget: RunBudget = .background) async {
        guard let interval = Settings.frequency.interval, running == nil else { return }
        let due = Self.due(LocalState.load(), only: only, interval: interval, now: Date())
        if !due.isEmpty { await sync(due, budget: budget) }
    }

    static func due(_ state: LocalState, only: Set<Source>?, interval: TimeInterval, now: Date) -> Set<Source> {
        Set(Source.allCases.filter { source in
            let last = state.runs.first { $0.source == source }?.finished ?? state.status[source]?.lastSync
            return (only?.contains(source) ?? true) && (last.map { now.timeIntervalSince($0) >= interval * 0.9 } ?? true)
        })
    }

    /// Runs a sync of the sources given (all enabled ones by default) after
    /// the one already queued, if any: a background run never swallows the
    /// owner's Sync Now.
    func sync(_ only: Set<Source>? = nil, budget: RunBudget = .full) async {
        let previous = running
        let task = Task {
            await previous?.value
            await self.run(only, budget: budget)
        }
        running = task
        inFlight.append(task)
        await task.value
        inFlight.removeAll { $0 == task }
        if running == task {
            running = nil
            if !pending.isEmpty {
                var state = LocalState.load()
                commit(&state)
            }
        }
    }

    private func run(_ only: Set<Source>?, budget: RunBudget) async {
        guard let session = Keychain.load(), !Task.isCancelled else { return }
        do {
            _ = try InstallID.get()
        } catch {
            log(nil, "Sync skipped: \(error.localizedDescription)", .error)
            return
        }
        let client = Client(server: session.server, secret: session.secret)
        // The state answers for one repository: pushed to another (or to
        // this one before it was reset), it is forgotten.
        var bound = LocalState.load()
        if bound.repository != session.identity {
            if bound.repository != nil { log(nil, "Signed in to \(session.repository): what this phone pushed before is forgotten, and everything is sent again") }
            bound.forgetPushed()
            bound.repository = session.identity
            commit(&bound)
        }
        for source in Source.allCases where Settings.enabled(source) && (only?.contains(source) ?? true) {
            if Task.isCancelled { return }
            var state = LocalState.load()
            var status = state.status[source] ?? SourceStatus()
            let started = Date()
            await progress(source, "Starting")
            log(source, budget == .full ? "Sync started" : "Background sync started")
            var signedOut = false
            do {
                switch source {
                case .contacts: status.counts = try await syncContacts(client, &state)
                case .calendars: status.counts = try await syncCalendars(client, &state)
                case .health: status.counts = try await syncHealth(client, session, &state, budget: budget)
                case .reminders: status.counts = try await syncReminders(client, &state)
                case .places: status.counts = try await syncPlaces(client, &state)
                }
                status.lastSync = Date()
                status.lastError = nil
                status.stopped = nil
            } catch where isCancellation(error) {
                status.lastError = budget == .full ? "Stopped before it finished. Sync again to carry on." : "iOS ended the background time before it finished; the next sync carries on."
                status.stopped = true
            } catch where isSignedOut(error) {
                status.stopped = nil
                status.lastError = SyncError.signedOut.localizedDescription
                signedOut = true
            } catch {
                status.lastError = explain(error, server: session.server)
                status.stopped = nil
            }
            if let error = status.lastError {
                log(source, "Sync did not finish: \(error)", status.stopped == true ? .warning : .error)
            } else {
                log(source, "Sync finished: \(status.counts ?? "")")
            }
            state.status[source] = status
            state.record(SyncRun(source: source, started: started, finished: Date(), summary: status.lastError == nil ? status.counts : nil,
                                 error: status.lastError, stopped: status.stopped))
            commit(&state)
            await progress(source, nil)
            if let onChange { await onChange(state) }
            if signedOut {
                if let onSignedOut { await onSignedOut() }
                return
            }
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

    /// The deliveries among `open` the server refused, with its reason. The
    /// parked list also carries the ones it accepted and has not run yet, and
    /// the ones it is running: those are still open, not refused.
    static func refusals(_ items: [Client.Parked], among open: Set<String>) -> [String: String] {
        var out: [String: String] = [:]
        for p in items where p.refused {
            if let f = p.fireId, open.contains(f) { out[f] = p.lastError }
        }
        return out
    }

    /// Waits for the provider to file or park every delivery of a chunk, up
    /// to the budget's deadline, and says what became of each day.
    private func confirm(
        _ fires: [String: [String]], _ client: Client, deadline seconds: TimeInterval, label: String,
        overall: (@Sendable (Double) -> Double)? = nil
    ) async throws -> Outcome {
        let all = Set(fires.values.joined())
        var filed = Set<String>()
        var parked: [String: String] = [:]
        let deadline = Date().addingTimeInterval(seconds)
        // Every delivery is asked about at least once, so a zero deadline is
        // one look at the receipts.
        while true {
            try Task.checkCancellation()
            let open = all.subtracting(filed).subtracting(parked.keys)
            let settled = all.isEmpty ? 1 : Double(all.count - open.count) / Double(all.count)
            await progress(.health, "Waiting for the server to file \(label)", all.count - open.count, all.count,
                           overall: overall?(0.5 + settled / 2))
            // The deadline holds per request, so a server that hangs on
            // each read does not stretch one pass over every delivery.
            for fire in open {
                if seconds > 0, Date() > deadline { break }
                // A receipt is a small read: one that takes longer is a
                // server busy filing, asked again on the next pass.
                if try await client.get(Self.healthExportKind, fire, retries: 0, timeout: 10) != nil { filed.insert(fire) }
            }
            parked.merge(Self.refusals(try await client.parked(trigger: Self.trigger, timeout: 15), among: open)) { a, _ in a }
            if all.subtracting(filed).subtracting(parked.keys).isEmpty || Date() > deadline { break }
            try await Task.sleep(for: .seconds(2))
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
        // Under limited access the scan is the contacts the owner chose to
        // share, not the address book: a contact missing from it may still
        // exist, so nothing is deleted.
        let complete = auth == .authorized
        let ids = state.ids
        let addressbook = ids.addressbook
        let kind = Self.appleKinds + "contact"
        let book = Self.appleKinds + "addressbook"

        // The server, not this phone's memory, says what was pushed: an
        // address book it no longer holds (a reset repository, a deleted
        // row) means every contact is sent again.
        if try await client.get(book, addressbook) == nil, !state.contacts.isEmpty {
            log(.contacts, "The server holds none of this phone's contacts: sending them all again")
            state.contacts = [:]
        }

        let contacts: [CNContact] = try await Task.detached {
            var out: [CNContact] = []
            let request = CNContactFetchRequest(keysToFetch: ContactMapping.keys)
            request.unifyResults = true
            try CNContactStore().enumerateContacts(with: request) { c, _ in out.append(c) }
            return out
        }.value

        state.device.contacts = contacts.count
        log(.contacts, "Read \(contacts.count) contacts from the device" + (complete ? "" : " (limited access: only the ones you chose to share)"))
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
        let missing = previous.keys.filter { current[$0] == nil }
        let vanished = complete ? missing : []
        if !complete, !missing.isEmpty {
            log(.contacts, "\(missing.count) contacts this phone pushed before are not shared with the app now; they stay on the server", .warning)
        }
        let new = changed.keys.filter { previous[$0] == nil }.count
        log(.contacts, "\(new) new, \(changed.count - new) changed, \(contacts.count - changed.count) unchanged, \(vanished.count) gone from the device")
        let toWrite = changed.count
        await progress(.contacts, "Writing contacts", 0, toWrite)

        var failures: [Error] = []
        var named: [(String, Error)] = []
        var written = 0
        for (offset, batch) in Self.batches(Array(changed)) {
            if Task.isCancelled { break }
            let before = failures.count
            for ((id, props), result) in await eachLimited(batch, progress: { n in await self.progress(.contacts, "Writing contacts", offset + n, toWrite) }, { pair in
                let (id, props) = pair
                try await client.put(kind, id, props.foundation)
                let gone = Set(previous[id]?.keys ?? []).subtracting(props.keys)
                try await client.clear(kind, id, keys: gone.sorted())
            }) {
                switch result {
                case .success:
                    state.contacts[id] = Pushed(props)
                    written += 1
                case let .failure(e):
                    failures.append(e)
                    named.append((Self.label(props, id), e))
                }
            }
            commit(&state)
            if let lost = Self.unreachable(Array(failures[before...]), of: batch.count) { throw lost }
        }
        if !vanished.isEmpty { await progress(.contacts, "Removing contacts gone from the device", 0, vanished.count) }
        var removed = 0
        for (id, result) in await eachLimited(vanished, { id in try await client.delete(kind, id) }) {
            switch result {
            case .success:
                state.contacts[id] = nil
                removed += 1
            case let .failure(e):
                failures.append(e)
                named.append((id, e))
            }
        }
        commit(&state)
        try rethrowStop(failures)
        logFailures(.contacts, "contact", named)
        if let first = failures.first {
            throw ContextError(context: "\(failures.count) of \(changed.count + vanished.count) contact writes failed, the first", underlying: first)
        }

        try await client.put(book, addressbook, [
            "deviceName": name, "contactCount": contacts.count, "lastSyncedAt": Formats.utc(Date()),
        ])
        return "\(contacts.count) contacts · " + Self.changes(written, removed)
    }

    /// The network's error, when every write of a batch failed for the
    /// network rather than a refusal: the server is out of reach, and the
    /// run stops there instead of trying every row in turn, each waiting out
    /// its own timeout.
    /// A refused token stops the run at its first sight: every other row
    /// would be refused the same.
    static func unreachable(_ errors: [Error], of count: Int) -> Error? {
        if let signedOut = errors.first(where: isSignedOut) { return signedOut }
        guard count > 0, errors.count == count, errors.allSatisfy(isUnreachable) else { return nil }
        return errors.first
    }

    /// What a run changed, without the zeros: "3 written · 1 removed", or
    /// "no changes".
    static func changes(_ written: Int, _ removed: Int) -> String {
        let parts = [written > 0 ? "\(written) written" : nil, removed > 0 ? "\(removed) removed" : nil].compactMap { $0 }
        return parts.isEmpty ? "no changes" : parts.joined(separator: " · ")
    }

    /// Rows to write, a hundred at a time, each batch with how many came
    /// before it. The state is committed after each, so a run stopped or
    /// killed part way keeps what it wrote and the next sync carries on.
    static func batches<T>(_ items: [T], size: Int = 100) -> [(offset: Int, batch: [T])] {
        stride(from: 0, to: items.count, by: size).map { ($0, Array(items[$0 ..< min($0 + size, items.count)])) }
    }

    /// A batch that failed because the run was stopped is a stop, not a list
    /// of failed writes.
    private func rethrowStop(_ failures: [Error]) throws {
        if Task.isCancelled || failures.contains(where: isCancellation) { throw CancellationError() }
        if let signedOut = failures.first(where: isSignedOut) { throw signedOut }
    }

    // MARK: - Calendars

    private func syncCalendars(_ client: Client, _ state: inout LocalState) async throws -> String {
        guard EKEventStore.authorizationStatus(for: .event) == .fullAccess else { throw SyncError.noAccess("Calendars") }
        let ids = state.ids
        let eventstore = ids.eventstore
        let storeKind = Self.appleKinds + "eventstore"
        let calendarKind = Self.appleKinds + "calendar"
        let eventKind = Self.appleKinds + "event"

        if try await client.get(storeKind, eventstore) == nil, !state.calendars.isEmpty || !state.events.isEmpty {
            log(.calendars, "The server holds none of this phone's calendars: sending them all again")
            state.calendars = [:]
            state.events = [:]
        }

        let now = Date()
        let cal = Calendar.current
        let windowStart = cal.startOfDay(for: cal.date(byAdding: .day, value: -Settings.windowBack, to: now)!)
        let windowEnd = cal.startOfDay(for: cal.date(byAdding: .day, value: Settings.windowAhead, to: now)!)
        let window = windowStart ..< windowEnd

        let store = EKEventStore()
        let everything = store.calendars(for: .event)
        // A calendar the owner turned off is left out of the scan, so the
        // rules below remove it from the server, events and all, as they do a
        // calendar deleted on the phone.
        let excluded = Settings.excludedCalendars
        let calendars = everything.filter { !excluded.contains($0.calendarIdentifier) }
        let chosen = Set(calendars.map(\.calendarIdentifier))
        // A large window takes seconds to read: not on the actor, which the
        // progress callbacks and the app's updates go through.
        let occurrences: [EKEvent] = chosen.isEmpty ? [] : await Task.detached {
            let scanned = store.calendars(for: .event).filter { chosen.contains($0.calendarIdentifier) }
            return store.events(matching: store.predicateForEvents(withStart: windowStart, end: windowEnd, calendars: scanned))
        }.value

        state.device.calendars = calendars.count
        state.device.events = occurrences.count
        let off = everything.count - calendars.count
        log(.calendars, "Read \(calendars.count) calendars" + (off > 0 ? " (\(off) turned off)" : "") + " and \(occurrences.count) events from \(Formats.day(windowStart, in: .current)) to \(Formats.day(windowEnd, in: .current))")
        // No event at all, from calendars that held some in this window, is
        // a store still loading (EventKit answers empty while it syncs an
        // account), never every event deleted.
        let chosenIDs = Set(chosen.map(ids.calendar))
        if occurrences.isEmpty, state.events.values.contains(where: {
            chosenIDs.contains($0.calendar ?? "") && ($0.start.map(window.contains) ?? false)
        }) { throw SyncError.emptyScan("events") }
        let name = await deviceName
        try await client.put(storeKind, eventstore, ["deviceName": name])
        var failures: [Error] = []

        var currentCalendars: [String: JSON] = [:]
        for c in calendars { currentCalendars[ids.calendar(c.calendarIdentifier)] = EventMapping.calendar(c, eventstore: eventstore) }
        let previousCalendars = state.calendars
        let changedCalendars = currentCalendars.filter { previousCalendars[$0.key]?.hash != Pushed.hash($0.value) }
        await progress(.calendars, "Writing calendars", 0, changedCalendars.count)
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
        // A deleted calendar takes its events with it (the server cascades),
        // so an empty scan deletes nothing. Every calendar turned off is the
        // owner's choice, not an empty scan.
        if everything.isEmpty, !previousCalendars.isEmpty { throw SyncError.emptyScan("calendars") }
        let goneCalendars = previousCalendars.keys.filter { currentCalendars[$0] == nil }
        for (id, result) in await eachLimited(goneCalendars, { id in try await client.delete(calendarKind, id) }) {
            switch result {
            case .success:
                state.calendars[id] = nil
                state.events = state.events.filter { $0.value.calendar != id }
                log(.calendars, "Removed a calendar turned off or gone from the device, with its events")
            case let .failure(e): failures.append(e)
            }
        }
        if !failures.isEmpty {
            commit(&state)
            try rethrowStop(failures)
            throw ContextError(context: "\(failures.count) calendar writes failed, the first", underlying: failures[0])
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
        let new = changed.keys.filter { previous[$0] == nil }.count
        log(.calendars, "\(new) events new, \(changed.count - new) changed, \(current.count - changed.count) unchanged")
        await progress(.calendars, "Writing events", 0, toWrite)
        var named: [(String, Error)] = []
        var written = 0
        for (offset, batch) in Self.batches(Array(changed)) {
            if Task.isCancelled { break }
            let before = failures.count
            for ((id, o), result) in await eachLimited(batch, progress: { n in await self.progress(.calendars, "Writing events", offset + n, toWrite) }, { pair in
                let (id, o) = pair
                try await client.put(eventKind, id, o.props.foundation)
                try await client.clear(eventKind, id, keys: Set(previous[id]?.keys ?? []).subtracting(o.props.keys).sorted())
            }) {
                switch result {
                case .success:
                    state.events[id] = Pushed(o.props, start: o.start, calendar: o.calendar)
                    written += 1
                case let .failure(e):
                    failures.append(e)
                    named.append((Self.label(o.props, id), e))
                }
            }
            commit(&state)
            if let lost = Self.unreachable(Array(failures[before...]), of: batch.count) { throw lost }
        }
        // Absent from the scan: deleted when its start is inside the window,
        // kept when it aged out of it.
        let vanished = previous.filter { id, p in
            current[id] == nil && p.start.map(window.contains) ?? true
        }.map(\.key)
        if !vanished.isEmpty { await progress(.calendars, "Removing events gone from the device", 0, vanished.count) }
        var removed = 0
        for (id, result) in await eachLimited(vanished, { id in try await client.delete(eventKind, id) }) {
            switch result {
            case .success:
                state.events[id] = nil
                removed += 1
            case let .failure(e):
                failures.append(e)
                named.append((id, e))
            }
        }
        commit(&state)
        try rethrowStop(failures)
        logFailures(.calendars, "event", named)
        if let first = failures.first {
            throw ContextError(context: "\(failures.count) of \(changed.count + vanished.count) event writes failed, the first", underlying: first)
        }

        try await client.put(storeKind, eventstore, [
            "deviceName": name, "calendarCount": calendars.count, "eventCount": occurrences.count,
            "windowStart": Formats.utc(windowStart), "windowEnd": Formats.utc(windowEnd),
            "lastSyncedAt": Formats.utc(Date()),
        ])
        return "\(calendars.count) calendars · \(occurrences.count) events · " + Self.changes(written, removed)
    }

    // MARK: - Rows

    /// What `pushRows` did.
    struct Pushes {
        var written = 0
        var removed = 0
        var failures: [Error] = []
        var named: [(String, Error)] = []
    }

    /// Writes every row of `current` whose content moved since the state at
    /// `path` last recorded it, clears the keys a row lost, and, when
    /// `deleting`, deletes the rows gone from `current`; a hundred at a time,
    /// committing after each batch, so a run stopped part way keeps what it
    /// wrote. `current` maps an id to its properties and what to remember of
    /// it once written.
    private func pushRows(
        _ client: Client, _ state: inout LocalState, _ path: WritableKeyPath<LocalState, [String: Pushed]>,
        kind: String, _ current: [String: Pushed.Row], deleting: Bool, source: Source, noun: String
    ) async throws -> Pushes {
        let previous = state[keyPath: path]
        let changed = current.filter { previous[$0.key]?.hash != $0.value.pushed.hash }
        let new = changed.keys.filter { previous[$0] == nil }.count
        let vanished = deleting ? previous.keys.filter { current[$0] == nil } : []
        log(source, "\(new) \(noun) new, \(changed.count - new) changed, \(current.count - changed.count) unchanged" + (deleting ? ", \(vanished.count) gone" : ""))
        var out = Pushes()
        let toWrite = changed.count
        if toWrite > 0 { await progress(source, "Writing \(noun)", 0, toWrite) }
        for (offset, batch) in Self.batches(Array(changed)) {
            if Task.isCancelled { break }
            let before = out.failures.count
            for ((id, row), result) in await eachLimited(batch, progress: { n in await self.progress(source, "Writing \(noun)", offset + n, toWrite) }, { pair in
                let (id, row) = pair
                try await client.put(kind, id, row.props.foundation)
                try await client.clear(kind, id, keys: Set(previous[id]?.keys ?? []).subtracting(row.props.keys).sorted())
            }) {
                switch result {
                case .success:
                    state[keyPath: path][id] = row.pushed
                    out.written += 1
                case let .failure(e):
                    out.failures.append(e)
                    out.named.append((Self.label(row.props, id), e))
                }
            }
            commit(&state)
            if let lost = Self.unreachable(Array(out.failures[before...]), of: batch.count) { throw lost }
        }
        if !vanished.isEmpty, !Task.isCancelled {
            await progress(source, "Removing \(noun) gone from the device", 0, vanished.count)
            for (id, result) in await eachLimited(vanished, { id in try await client.delete(kind, id) }) {
                switch result {
                case .success:
                    state[keyPath: path][id] = nil
                    out.removed += 1
                case let .failure(e):
                    out.failures.append(e)
                    out.named.append((id, e))
                }
            }
            commit(&state)
        }
        return out
    }

    /// Ends a source's pushes: a stop is a stop, and any other failure is
    /// logged by name and thrown with how many of how many failed.
    private func settlePushes(_ source: Source, _ noun: String, _ pushes: [Pushes]) throws {
        let failures = pushes.flatMap(\.failures)
        try rethrowStop(failures)
        logFailures(source, noun, pushes.flatMap(\.named))
        if let first = failures.first {
            throw ContextError(context: "\(failures.count) \(noun) writes failed, the first", underlying: first)
        }
    }

    // MARK: - Reminders

    private func syncReminders(_ client: Client, _ state: inout LocalState) async throws -> String {
        guard EKEventStore.authorizationStatus(for: .reminder) == .fullAccess else { throw SyncError.noAccess("Reminders") }
        let ids = state.ids
        let eventstore = ids.eventstore
        let storeKind = Self.appleKinds + "eventstore"
        if try await client.get(storeKind, eventstore) == nil, !state.reminderLists.isEmpty || !state.reminders.isEmpty {
            log(.reminders, "The server holds none of this phone's reminders: sending them all again")
            state.reminderLists = [:]
            state.reminders = [:]
        }

        let store = EKEventStore()
        let lists = store.calendars(for: .reminder)
        await progress(.reminders, "Reading reminders")
        // Mapped inside EventKit's callback: a reminder is not sendable, its
        // properties are.
        // EventKit answers nil when it could not read the store: that is a
        // failure, never an empty store whose reminders the server forgets.
        let read: [(id: String, row: Pushed.Row)]? = await withCheckedContinuation { continuation in
            store.fetchReminders(matching: store.predicateForReminders(in: nil)) { reminders in
                continuation.resume(returning: reminders.map { all in
                    all.compactMap { r in
                        guard let list = r.calendar else { return nil }
                        let listID = ids.calendar(list.calendarIdentifier)
                        let props = ReminderMapping.reminder(r, list: listID)
                        return (ids.reminder(r.calendarItemIdentifier), Pushed.Row(props, Pushed(props, calendar: listID)))
                    }
                })
            }
        }
        guard let fetched = read else { throw SyncError.unreadable("reminders") }
        state.device.reminders = fetched.count
        log(.reminders, "Read \(lists.count) lists and \(fetched.count) reminders")
        // An empty answer is more often a store that is not ready than an
        // owner who deleted every list; it never empties the repository.
        if lists.isEmpty, !state.reminderLists.isEmpty { throw SyncError.emptyScan("reminder lists") }
        if fetched.isEmpty, !state.reminders.isEmpty { throw SyncError.emptyScan("reminders") }

        let name = await deviceName
        try await client.put(storeKind, eventstore, ["deviceName": name])
        var currentLists: [String: Pushed.Row] = [:]
        for l in lists {
            let props = ReminderMapping.list(l, eventstore: eventstore)
            currentLists[ids.calendar(l.calendarIdentifier)] = Pushed.Row(props, Pushed(props))
        }
        let listPushes = try await pushRows(client, &state, \.reminderLists, kind: ReminderMapping.listKind, currentLists,
                                        deleting: true, source: .reminders, noun: "lists")
        // A deleted list takes its reminders with it on the server.
        let goneLists = Set(state.reminders.values.compactMap(\.calendar)).subtracting(state.reminderLists.keys)
        state.reminders = state.reminders.filter { !goneLists.contains($0.value.calendar ?? "") }
        try settlePushes(.reminders, "list", [listPushes])

        let current = Dictionary(fetched.map { ($0.id, $0.row) }, uniquingKeysWith: { a, _ in a })
        let pushes = try await pushRows(client, &state, \.reminders, kind: ReminderMapping.kind, current,
                                    deleting: true, source: .reminders, noun: "reminders")
        try settlePushes(.reminders, "reminder", [pushes])

        try await client.put(storeKind, eventstore, [
            "deviceName": name, "reminderListCount": lists.count, "reminderCount": fetched.count,
            "lastSyncedAt": Formats.utc(Date()),
        ])
        return "\(lists.count) lists · \(fetched.count) reminders · " + Self.changes(pushes.written, pushes.removed)
    }

    // MARK: - Places

    private func syncPlaces(_ client: Client, _ state: inout LocalState) async throws -> String {
        let allowed = await PlaceMonitor.shared.authorization == .authorizedAlways
        var visits = PlaceLog.shared.all
        if visits.isEmpty, !allowed { throw SyncError.noAccess("your location (Always) for Places") }
        if !allowed { log(.places, "Location access is no longer Always: no new visits are recorded, and the ones kept are sent", .warning) }
        let ids = state.ids
        let visitstore = ids.visitstore
        if try await client.get(VisitMapping.storeKind, visitstore) == nil, !state.visits.isEmpty {
            log(.places, "The server holds none of this phone's visits: sending the year kept here again")
            state.visits = [:]
        }

        // The geocoder is rate-limited: a few visits a run, newest first, and
        // the rest at the next one. A failure is tried again next run.
        let unnamed = visits.filter { !$0.geocoded }.prefix(25)
        for (i, visit) in unnamed.enumerated() {
            if Task.isCancelled { break }
            await progress(.places, "Naming places", i, unnamed.count)
            do {
                PlaceLog.shared.named(visit.key, try await withTimeout(15) { try await PlaceNamer.name(visit) })
            } catch {
                log(.places, "Apple Maps did not name a place: \(error.localizedDescription); it is tried again next sync", .warning)
                break
            }
        }
        visits = PlaceLog.shared.all

        let name = await deviceName
        try await client.put(VisitMapping.storeKind, visitstore, ["deviceName": name])
        var current: [String: Pushed.Row] = [:]
        for v in visits {
            let props = VisitMapping.visit(v, visitstore: visitstore)
            current[ids.visit(v.key)] = Pushed.Row(props, Pushed(props, start: v.arrival ?? v.departure))
        }
        // A visit aged out of the year kept here stays in the repository.
        let pushes = try await pushRows(client, &state, \.visits, kind: VisitMapping.kind, current,
                                    deleting: false, source: .places, noun: "visits")
        try settlePushes(.places, "visit", [pushes])
        try await client.put(VisitMapping.storeKind, visitstore, [
            "deviceName": name, "visitCount": visits.count, "lastSyncedAt": Formats.utc(Date()),
        ])
        return "\(visits.count) visits · " + Self.changes(pushes.written, 0)
    }

    // MARK: - Health

    /// The days to send, oldest first:
    /// - the last `resend` days every time, because a watch can reach the
    ///   phone a day or more after its samples were taken;
    /// - every other day of the range not yet sent and, where `available` is
    ///   known, holding data (a long range of empty days is not read);
    /// - queued days (a retry, or the owner's from Coverage), in or out of
    ///   the range.
    static func healthDays(
        today: Date, from start: Date?, sent: Set<String>, queued: Set<String> = [], available: Set<String>? = nil,
        resend: Int = 3, calendar: Calendar
    ) -> [Date] {
        let todayStart = calendar.startOfDay(for: today)
        let parse = Coverage.dayParser(calendar)
        let first = start.map { calendar.startOfDay(for: $0) }
            ?? available.flatMap { $0.compactMap(parse.date(from:)).min() }
            ?? todayStart
        var out = Set<Date>()
        var day = first
        while day <= todayStart {
            let name = Formats.day(day, in: calendar.timeZone)
            let back = calendar.dateComponents([.day], from: day, to: todayStart).day ?? 0
            if back < resend || (!sent.contains(name) && (available?.contains(name) ?? true)) {
                out.insert(day)
            }
            guard let next = calendar.date(byAdding: .day, value: 1, to: day) else { break }
            day = next
        }
        for name in queued {
            if let d = parse.date(from: name), d <= todayStart { out.insert(d) }
        }
        return out.sorted()
    }

    private func syncHealth(_ client: Client, _ session: Session, _ state: inout LocalState, budget: RunBudget) async throws -> String {
        guard HealthReader.isAvailable else { throw SyncError.noAccess("Health on this device") }
        guard let trigger = try await client.get(Self.triggerKind, Self.trigger),
              trigger["enabled"] as? Bool == true,
              let key = ((trigger["source"] as? [String: Any])?["webhook"] as? [String: Any])?["key"] as? String,
              !key.isEmpty
        else { throw SyncError.notArmed }

        // The account every delivery names: one the server does not hold is
        // made here, as Set Up makes it, and every day is sent again, because
        // a missing account means the repository holds none of them.
        if try await client.get(Self.healthAccountKind, state.healthAccount) == nil {
            let device = await deviceName
            try await client.put(Self.healthAccountKind, state.healthAccount, [
                "displayName": device, "timezone": TimeZone.current.identifier,
            ])
            log(.health, "Created the Health account \(state.healthAccount)")
            if !state.healthDays.isEmpty {
                log(.health, "The server held no Health account for this phone: sending every day again")
                state.healthDays = []
            }
        }

        for g in Grouping.allCases where state.automation(g) == nil {
            state.automations[g.rawValue] = UUID().uuidString
        }
        commit(&state)

        // Days an earlier run delivered and the server had not yet filed:
        // their receipts are read first, so a day filed since is not sent
        // again. One still waiting, or parked, is sent again below.
        if !state.healthPending.isEmpty {
            let outcome = try await confirm(state.healthPending, client, deadline: 0, label: "earlier days")
            for day in outcome.sent {
                state.healthDays.insert(day)
                state.healthQueue.remove(day)
                state.healthPending[day] = nil
            }
            for day in outcome.failed.keys { state.healthPending[day] = nil }
            if !outcome.sent.isEmpty { log(.health, "\(outcome.sent.count) days sent earlier are filed now") }
            commit(&state)
        }
        let reader = HealthReader(store: health)
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = .current
        let zone = TimeZone.current
        let sessionID = UUID().uuidString
        let today = Date()
        let history = Settings.history
        let start = history.start(today: today, calendar: calendar)

        // A background wake has seconds: the last few days only. A run over
        // more than a month asks which days hold data first, so years of
        // history are not read one empty day at a time; the scan is kept, so
        // only its first run reads the whole history.
        var days: [Date]
        if budget == .background {
            days = Self.healthDays(today: today, from: calendar.date(byAdding: .day, value: -2, to: today), sent: [], calendar: calendar)
        } else {
            var available: Set<String>?
            let span = start.map { today.timeIntervalSince($0) } ?? .infinity
            if span > 31 * 86400 {
                await progress(.health, "Reading what the phone holds")
                // Without the answer every day of the history would be read
                // one at a time: a failure stops the run instead.
                do {
                    available = try await HealthScan.shared.days(since: start, store: health, calendar: calendar) { done, total, _ in
                        await self.progress(.health, "Reading what the phone holds", done, total)
                    }
                } catch where !isCancellation(error) {
                    throw ContextError(context: "Reading what the phone holds", underlying: error)
                }
            }
            days = Self.healthDays(today: today, from: start, sent: state.healthDays, queued: state.healthQueue,
                                   available: available, calendar: calendar)
        }
        if days.isEmpty {
            return "Nothing to send"
        }
        log(.health, "Sending \(days.count) days, \(Formats.day(days.first!, in: zone)) to \(Formats.day(days.last!, in: zone)) (history: \(history.title))")

        var posts = 0
        var sent = 0
        var empty = 0
        var refused = 0
        var firstRefusal: (day: String, reason: String)?
        var waiting = 0
        var skippedDays = 0
        /// Days read in part this run: filed or not, never counted sent.
        var partial = Set<String>()
        for (c, chunk) in stride(from: 0, to: days.count, by: Self.chunk).map({ Array(days[$0 ..< min($0 + Self.chunk, days.count)]) }).enumerated() {
            try Task.checkCancellation()
            // Each day's fire ids, settled after the chunk: the door's 202 is
            // "stored", and only the provider's receipt is "filed".
            var fires: [String: [String]] = [:]
            for (i, day) in chunk.enumerated() {
                try Task.checkCancellation()
                let name = Formats.day(day, in: zone)
                await progress(.health, "Reading and sending \(name)", c * Self.chunk + i, days.count,
                               overall: Self.healthOverall(before: c * Self.chunk, width: chunk.count,
                                                           part: Double(i) / Double(chunk.count) / 2, days: days.count))
                let span = day ..< calendar.date(byAdding: .day, value: 1, to: day)!
                var made: [String] = []
                var rows = 0
                var skipped: [String] = []
                for grouping in Grouping.allCases {
                    let delivery = grouping == .minutes
                        ? await reader.minutes(span, zone: zone)
                        : await reader.seconds(span, zone: zone)
                    // The reads take seconds and do not stop for a Stop.
                    try Task.checkCancellation()
                    skipped += delivery.skipped
                    rows += delivery.metrics.map(\.rows.count).reduce(0, +) + delivery.workouts.count
                    let headers = HealthDelivery.headers(
                        grouping: grouping, key: key, account: state.healthAccount,
                        automationID: state.automation(grouping)!, sessionID: sessionID)
                    // One at a time: the door takes eight in flight across
                    // every sender, and a day's order is the order its rows
                    // replace.
                    for body in delivery.bodies() {
                        do {
                            made.append(try await client.deliver(authority: session.repository, trigger: Self.trigger, body: body, headers: headers))
                        } catch {
                            throw ContextError(context: "Sending \(name) (\(grouping.rawValue))", underlying: error)
                        }
                        posts += 1
                    }
                }
                for line in skipped {
                    log(.health, "\(name): could not read \(line)", .warning)
                }
                // A day read again is not sent until it is filed again, so a
                // rescan that fails can never leave an older success standing.
                state.healthDays.remove(name)
                state.healthPending[name] = nil
                if made.isEmpty, skipped.isEmpty {
                    // Nothing on the phone for the day: nothing to retry,
                    // and nothing for Coverage to count as unsent.
                    state.healthQueue.remove(name)
                    await HealthScan.shared.forget(name)
                    empty += 1
                } else {
                    // Every delivery is confirmed, a day read in part too, so
                    // the phone never runs ahead of the server's filing: one
                    // that did ran minutes ahead, and the spool holding what
                    // the server has not filed answers 503 once full.
                    if !made.isEmpty { fires[name] = made }
                    state.healthQueue.insert(name)
                    if skipped.isEmpty {
                        // Kept as pending at once: a run stopped before the
                        // receipts are read asks for them next time rather
                        // than sending the day again.
                        state.healthPending[name] = made
                    } else {
                        // Read in part, or not at all: queued, so the next
                        // sync sends it whole, and never pending, which would
                        // let a later run count it sent. A day whose every
                        // read failed is not a day without data.
                        partial.insert(name)
                        skippedDays += 1
                    }
                }
                if !made.isEmpty { log(.health, "\(name): \(rows) rows in \(made.count) deliveries" + (skipped.isEmpty ? "" : ", \(skipped.count) parts skipped")) }
            }
            commit(&state)

            let before = c * Self.chunk, width = chunk.count, total = days.count
            let outcome = try await confirm(
                fires, client, deadline: budget.confirmDeadline,
                label: "\(Formats.day(chunk.first!, in: zone)) to \(Formats.day(chunk.last!, in: zone))",
                overall: { Self.healthOverall(before: before, width: width, part: $0, days: total) })
            let whole = outcome.sent.filter { !partial.contains($0) }
            for day in whole {
                state.healthDays.insert(day)
                state.healthQueue.remove(day)
                state.healthPending[day] = nil
            }
            for day in outcome.failed.keys { state.healthPending[day] = nil }
            for (day, reason) in outcome.failed.sorted(by: { $0.key < $1.key }) {
                log(.health, "\(day) was refused by the server and will be sent again: \(reason)", .error)
                if firstRefusal == nil { firstRefusal = (day, reason) }
            }
            sent += whole.count
            refused += outcome.failed.count
            waiting += outcome.pending.count
            commit(&state)
            if let onChange { await onChange(state) }
        }
        await progress(.health, "Done", days.count, days.count)
        if waiting > 0 {
            log(.health, "\(waiting) days were stored but not yet filed by the server; the next sync checks them again", .warning)
        }
        var summary = "\(sent) days sent · \(posts) deliveries"
        if empty > 0 { summary += " · \(empty) days with no data" }
        if waiting > 0 { summary += " · \(waiting) still being filed" }
        if skippedDays > 0 { summary += " · \(skippedDays) days to read again, see Activity" }
        // The server's own reason, for the first day it refused: the error
        // the owner reads is the one to act on, not a count.
        if let first = firstRefusal {
            let more = refused > 1 ? ", and \(refused - 1) more \(refused == 2 ? "day" : "days") (see Activity)" : ""
            throw Message("The server refused \(first.day)\(more): \(first.reason)\n\nThe rest went through (\(summary)). A refused day is sent again at the next sync.")
        }
        return summary
    }
}
