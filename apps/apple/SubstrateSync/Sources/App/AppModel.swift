import Contacts
import CryptoKit
import EventKit
import Foundation
import HealthKit
import SwiftUI
import UIKit

/// One thing Set up installs, imports, creates or arms.
struct SetupStep: Identifiable, Equatable {
    enum Kind: Equatable {
        case install(String)
        case importSample(String)
        case account
        case trigger
    }

    let id: String
    let kind: Kind
    let title: String
    var done: Bool
    var detail: String?
}

/// Everything the screens show, and the actions they take.
@MainActor
final class AppModel: ObservableObject {
    static let appleProvider = "providers.substrate.reamde.dev/apple"
    static let healthProvider = "providers.substrate.reamde.dev/applehealth"
    static let samples = [
        "samples.substrate.reamde.dev/observations",
        "samples.substrate.reamde.dev/health",
        "samples.substrate.reamde.dev/healthapple",
    ]
    /// The apple package version that declares the calendar kinds.
    static let appleVersion = 2

    @Published private(set) var session: Session?
    @Published private(set) var state = LocalState.load()
    @Published var steps: [SetupStep] = []
    @Published var setupChecked = false
    @Published var setupBusy = false
    @Published var setupError: String?
    /// A key someone else set on the trigger, waiting on the owner's choice.
    @Published var foreignKey = false
    /// Syncs the owner started that have not finished: they chain, so a
    /// second Sync Now (or a Send) waits its turn.
    @Published private(set) var runningSyncs = 0
    var syncing: Bool { runningSyncs > 0 }
    /// The sources the running syncs cover, and when the first began: a
    /// source in it that has not run since is queued.
    @Published private(set) var syncSources: Set<Source> = []
    @Published private(set) var syncStarted: Date?
    /// The phone's event calendars, for the picker.
    @Published private(set) var calendarChoices: [CalendarChoice] = []
    /// The server refused the token (it was revoked, or the repository was
    /// reset): nothing works until the owner signs in again.
    @Published var sessionExpired = false
    @Published private(set) var access: [Source: String] = [:]
    /// What each running source is doing now.
    @Published private(set) var progress: [Source: SyncProgress] = [:]
    /// The activity log, newest first.
    @Published private(set) var activity: [ActivityEntry] = Activity.shared.all
    /// The device's Health days with data, from the last coverage scan.
    @Published private(set) var healthAvailable: Set<String>?
    /// The start of every occurrence in the calendar window, from the last
    /// coverage scan.
    @Published private(set) var eventsAvailable: [Date]?
    @Published private(set) var coverageLoading = false
    @Published private(set) var coverageError: String?
    /// How far back the coverage scan looks for Health data.
    static let coverageYears = 10

    /// The Health days with data inside the history range synced, and those
    /// before it, from the last coverage scan.
    var healthInRange: Set<String>? { healthSplit?.inRange }
    var healthOutsideRange: Set<String>? { healthSplit?.older }

    private var healthSplit: (inRange: Set<String>, older: Set<String>)? {
        guard let available = healthAvailable else { return nil }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = .current
        guard let start = Settings.history.start(today: Date(), calendar: calendar) else { return (available, []) }
        let first = Formats.day(start, in: calendar.timeZone)
        let inRange = available.filter { $0 >= first }
        return (inRange, available.subtracting(inRange))
    }

    private static let generatedKeyDefaults = "trigger.generatedKeyHash"

    init() {
        session = Keychain.load()
        Task {
            // The model lives as long as the app, so the engine holds it.
            await SyncEngine.shared.setOnChange { state in self.state = state }
            await SyncEngine.shared.setOnProgress { source, step in self.progress[source] = step }
            await SyncEngine.shared.setOnSignedOut { self.sessionExpired = true }
        }
        refreshAccess()
        Activity.shared.onEntry = { [weak self] entry in
            guard let self else { return }
            self.activity.insert(entry, at: 0)
            if self.activity.count > Activity.limit { self.activity.removeLast() }
        }
    }

    var client: Client? { session.map { Client(server: $0.server, secret: $0.secret) } }
    var setupComplete: Bool { setupChecked && steps.allSatisfy(\.done) }

    // MARK: - sign in and out

    func signIn(server: URL, repository: String, password: String, totp: String?) async throws {
        let label = "Substrate Sync on \(UIDevice.current.name)"
        let result = try await Client(server: server).login(
            repository: repository, password: password, totp: totp, label: label)
        let session = Session(server: server, repository: result.repository, secret: result.secret, tokenID: result.token.id)
        // The state answers for one repository: signed in to any other, or
        // to this one again, what was pushed before is checked against the
        // server rather than trusted (the run forgets it where the identity
        // differs, and each source asks the server what it still holds).
        await SyncEngine.shared.cancel()
        let identity = session.identity
        state = await SyncEngine.shared.update { s in
            if s.repository != identity {
                s.forgetPushed()
                s.repository = identity
            }
        }
        Keychain.save(session)
        self.session = session
        sessionExpired = false
        setupChecked = false
        await checkSetup()
    }

    /// Signs out here at once; the token is revoked on the server as a best
    /// effort afterwards, so a server that is gone, or that already refuses
    /// the token, never holds the owner on this screen.
    func signOut() {
        let old = session
        Task { await SyncEngine.shared.cancel() }
        Keychain.clear()
        session = nil
        steps = []
        setupChecked = false
        setupError = nil
        sessionExpired = false
        if let old {
            Task.detached { try? await Client(server: old.server, secret: old.secret).revoke(token: old.tokenID) }
        }
    }

    // MARK: - set up

    /// `keepError` keeps the error the step that ran before it reported.
    func checkSetup(keepError: Bool = false) async {
        guard let client else { return }
        if !keepError { setupError = nil }
        do {
            let catalog = Dictionary(try await client.catalog().map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
            var steps: [SetupStep] = []
            for id in [Self.appleProvider, Self.healthProvider] {
                let item = catalog[id]
                var detail: String?
                var done = item?.installed == true && item?.upgrade == nil
                if item == nil {
                    detail = "This server does not ship it."
                } else if item?.installed == true, item?.upgrade != nil {
                    detail = "Installed; version \(item?.version ?? 0) is available."
                }
                if id == Self.appleProvider, let v = item?.version, v < Self.appleVersion {
                    done = false
                    detail = "This server ships version \(v); calendars need version \(Self.appleVersion)."
                }
                steps.append(SetupStep(id: id, kind: .install(id), title: "Install \(id)", done: done, detail: detail))
            }
            for id in Self.samples {
                let item = catalog[id]
                steps.append(SetupStep(
                    id: id, kind: .importSample(id), title: "Import \(id)", done: item?.installed == true,
                    detail: item == nil ? "This server does not ship it." : nil))
            }
            let account = try await client.get(SyncEngine.healthAccountKind, state.healthAccount)
            steps.append(SetupStep(
                id: "account", kind: .account, title: "Create the Health account \(state.healthAccount)",
                done: account != nil))
            let trigger = try await client.get(SyncEngine.triggerKind, SyncEngine.trigger)
            steps.append(SetupStep(
                id: "trigger", kind: .trigger, title: "Arm the trigger \(SyncEngine.trigger)",
                done: Self.armed(trigger),
                detail: trigger == nil ? "Appears once the Apple Health provider is installed." : nil))
            self.steps = steps
            setupChecked = true
        } catch {
            if isSignedOut(error) { sessionExpired = true }
            setupError = error.localizedDescription
        }
    }

    /// Does every missing step, in order, and stops at the first refusal.
    /// `replaceKey` answers the question a foreign key raises.
    func runSetup(replaceKey: Bool? = nil) async {
        guard let client else { return }
        setupBusy = true
        setupError = nil
        defer { setupBusy = false }
        do {
            for step in steps where !step.done {
                switch step.kind {
                case let .install(id): try await client.catalog("install", id)
                case let .importSample(id): try await client.catalog("import", id)
                case .account:
                    try await client.put(SyncEngine.healthAccountKind, state.healthAccount, [
                        "displayName": UIDevice.current.name, "timezone": TimeZone.current.identifier,
                    ])
                case .trigger:
                    break
                }
            }
            // The trigger is read after the installs, never from the steps
            // computed before them: installing a provider writes its shipped
            // trigger again, which can disarm one that was armed.
            if !Self.armed(try await client.get(SyncEngine.triggerKind, SyncEngine.trigger)) {
                guard try await armTrigger(client, replaceKey: replaceKey) else {
                    foreignKey = true
                    await checkSetup(keepError: true)
                    return
                }
            }
        } catch {
            if isSignedOut(error) { sessionExpired = true }
            setupError = error.localizedDescription
        }
        await checkSetup(keepError: true)
    }

    /// Reads the trigger, keeps or sets its key, enables it, and writes the
    /// whole source object back. False when a key this app did not set is
    /// there and the owner has not said whether to keep it.
    private func armTrigger(_ client: Client, replaceKey: Bool?) async throws -> Bool {
        guard let trigger = try await client.get(SyncEngine.triggerKind, SyncEngine.trigger) else {
            throw ServerError(status: 404, data: Data(#"{"error":{"message":"The applehealth-webhook trigger is not installed."}}"#.utf8))
        }
        var source = trigger["source"] as? [String: Any] ?? [:]
        var webhook = source["webhook"] as? [String: Any] ?? [:]
        let existing = webhook["key"] as? String ?? ""
        // Only the key's hash is kept here, to tell a key this app set from
        // one someone else did; the key itself lives on the trigger.
        let ours = UserDefaults.standard.string(forKey: Self.generatedKeyDefaults)
        var key = existing
        if existing.isEmpty || replaceKey == true {
            key = Self.newKey()
        } else if Self.keyHash(existing) != ours, replaceKey == nil {
            return false
        }
        // Remembered either way: a key this app set, or one the owner chose
        // to keep, is not asked about again.
        UserDefaults.standard.set(Self.keyHash(key), forKey: Self.generatedKeyDefaults)
        webhook["key"] = key
        source["webhook"] = webhook
        try await client.put(SyncEngine.triggerKind, SyncEngine.trigger, ["enabled": true, "source": source])
        foreignKey = false
        return true
    }

    static func keyHash(_ key: String) -> String {
        SHA256.hash(data: Data(key.utf8)).map { String(format: "%02x", $0) }.joined()
    }

    static func armed(_ trigger: [String: Any]?) -> Bool {
        guard let trigger, trigger["enabled"] as? Bool == true else { return false }
        let key = ((trigger["source"] as? [String: Any])?["webhook"] as? [String: Any])?["key"] as? String
        return !(key ?? "").isEmpty
    }

    /// 32 characters of [A-Za-z0-9_-], from the system's random source.
    static func newKey() -> String {
        let alphabet = Array("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-")
        var bytes = [UInt8](repeating: 0, count: 32)
        _ = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        return String(bytes.map { alphabet[Int($0) % alphabet.count] })
    }

    // MARK: - access

    func refreshAccess() {
        let contacts: String = switch CNContactStore.authorizationStatus(for: .contacts) {
        case .authorized: "Allowed"
        case .limited: "Allowed for some contacts"
        case .denied, .restricted: "Not allowed"
        default: "Not asked yet"
        }
        let calendars: String = switch EKEventStore.authorizationStatus(for: .event) {
        case .fullAccess: "Allowed"
        case .writeOnly: "Add only: full access is needed"
        case .denied, .restricted: "Not allowed"
        default: "Not asked yet"
        }
        // HealthKit never says whether reading was allowed; it says whether
        // the question was asked.
        let health: String
        if !HealthReader.isAvailable {
            health = "Not available on this device"
        } else {
            health = UserDefaults.standard.bool(forKey: "health.asked") ? "Asked" : "Not asked yet"
        }
        access = [.contacts: contacts, .calendars: calendars, .health: health]
    }

    func requestAccess(_ source: Source) async {
        switch source {
        case .contacts:
            _ = try? await CNContactStore().requestAccess(for: .contacts)
        case .calendars:
            _ = try? await EKEventStore().requestFullAccessToEvents()
        case .health:
            guard HealthReader.isAvailable else { break }
            try? await SyncEngine.shared.health.requestAuthorization(toShare: SampleData.shareTypes, read: HealthReader.readTypes)
            UserDefaults.standard.set(true, forKey: "health.asked")
            Background.observeHealth()
        }
        refreshAccess()
    }

    // MARK: - sync

    // MARK: - coverage

    /// Where one source stands, for the overview and its page.
    func overview(_ source: Source) -> SourceOverview {
        let running = progress[source] != nil
        var queued = false
        if syncing, !running, syncSources.contains(source), let started = syncStarted {
            queued = !(state.runs.first { $0.source == source }.map { $0.finished >= started } ?? false)
        }
        return SourceOverview.of(
            enabled: Settings.enabled(source), access: access[source], running: running, queued: queued,
            status: state.status[source], waiting: source == .health ? healthWaiting.count : 0)
    }

    /// What a source has synced of what the phone holds: Health's days with
    /// data inside the history, every contact, the events in the calendar
    /// window of the calendars turned on.
    func figures(_ source: Source) -> (synced: Int, total: Int, noun: String)? {
        switch source {
        case .health:
            guard let inRange = healthInRange else { return nil }
            return (inRange.intersection(state.healthDays).count, inRange.count, "days")
        case .contacts:
            guard let total = state.device.contacts else { return nil }
            return (min(state.contacts.count, total), total, "contacts")
        case .calendars:
            guard let total = eventsAvailable?.count ?? state.device.events else { return nil }
            let window = Self.window
            let synced = state.events.values.filter { $0.start.map(window.contains) ?? false }.count
            return (min(synced, total), total, "events")
        }
    }

    /// The figures in a few words, for the overview.
    func coverageLine(_ source: Source) -> String? {
        guard let f = figures(source), f.total > 0 else { return nil }
        return f.synced >= f.total ? "All \(f.total.formatted()) \(f.noun) synced" : "\(f.synced.formatted()) of \(f.total.formatted()) \(f.noun) synced"
    }

    /// The calendar window, from today.
    static var window: Range<Date> {
        let cal = Calendar.current
        let now = Date()
        return cal.startOfDay(for: cal.date(byAdding: .day, value: -Settings.windowBack, to: now)!)
            ..< cal.startOfDay(for: cal.date(byAdding: .day, value: Settings.windowAhead, to: now)!)
    }

    /// When the source last ran, whether or not the run finished: a source
    /// whose every run ended in an error has still run.
    func lastRun(_ source: Source) -> Date? {
        state.runs.first { $0.source == source }?.finished ?? state.status[source]?.lastSync
    }

    /// Health days delivered and not yet filed by the server.
    var healthWaiting: Set<String> { Set(state.healthPending.keys).subtracting(state.healthDays) }

    /// Reads the phone's event calendars, grouped by account in the picker.
    func loadCalendars() {
        guard EKEventStore.authorizationStatus(for: .event) == .fullAccess else {
            calendarChoices = []
            return
        }
        calendarChoices = EKEventStore().calendars(for: .event).map { c in
            let note: String? = switch c.type {
            case .subscription: "Subscribed"
            case .birthday: "Birthdays from Contacts"
            default: c.allowsContentModifications ? nil : "Read-only"
            }
            return CalendarChoice(
                id: c.calendarIdentifier, title: c.title, account: c.source?.title ?? "Other", note: note,
                color: Color(cgColor: c.cgColor))
        }
        .sorted { ($0.account, $0.title) < ($1.account, $1.title) }
    }

    /// Reads what the device holds, to set beside what was synced.
    func loadCoverage() async {
        guard !coverageLoading else { return }
        coverageLoading = true
        coverageError = nil
        defer { coverageLoading = false }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = .current
        if HealthReader.isAvailable, UserDefaults.standard.bool(forKey: "health.asked") {
            do {
                healthAvailable = try await HealthReader(store: SyncEngine.shared.health)
                    .availableDays(years: Self.coverageYears, calendar: calendar)
            } catch {
                coverageError = error.localizedDescription
            }
        }
        if EKEventStore.authorizationStatus(for: .event) == .fullAccess {
            let now = Date()
            let start = calendar.startOfDay(for: calendar.date(byAdding: .day, value: -Settings.windowBack, to: now)!)
            let end = calendar.startOfDay(for: calendar.date(byAdding: .day, value: Settings.windowAhead, to: now)!)
            let excluded = Settings.excludedCalendars
            eventsAvailable = await Task.detached {
                let store = EKEventStore()
                let calendars = store.calendars(for: .event).filter { !excluded.contains($0.calendarIdentifier) }
                guard !calendars.isEmpty else { return [] }
                return store.events(matching: store.predicateForEvents(withStart: start, end: end, calendars: calendars))
                    .compactMap(\.startDate)
            }.value
        }
    }

    /// Queues days for the next Health sync and starts it.
    func send(days: [String]) async {
        state = await SyncEngine.shared.update { $0.healthQueue.formUnion(days) }
        await syncNow([.health])
    }

    func setFrequency(_ frequency: Settings.Frequency) {
        UserDefaults.standard.set(frequency.rawValue, forKey: Settings.frequencyKey)
        Background.schedule()
        Background.updateHealthDelivery()
    }

    /// Stops the running sync where it can stop cleanly.
    func cancelSync() {
        Activity.shared.log(nil, "Sync stopped by hand", level: .warning)
        Task { await SyncEngine.shared.cancel() }
    }

    func clearActivity() {
        Activity.shared.clear()
        activity = []
    }

    func syncNow(_ only: Set<Source>? = nil) async {
        if runningSyncs == 0 {
            syncStarted = Date()
            syncSources = []
        }
        runningSyncs += 1
        syncSources.formUnion(Source.allCases.filter { Settings.enabled($0) && (only?.contains($0) ?? true) })
        defer {
            runningSyncs -= 1
            if runningSyncs == 0 {
                syncSources = []
                syncStarted = nil
            }
        }
        for source in Source.allCases where Settings.enabled(source) && (only?.contains(source) ?? true) {
            if access[source] == "Not asked yet" { await requestAccess(source) }
        }
        await SyncEngine.shared.sync(only, budget: .full)
        state = LocalState.load()
        await loadCoverage()
    }
}
