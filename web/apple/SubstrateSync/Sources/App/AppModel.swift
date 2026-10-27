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
    @Published private(set) var syncing = false
    @Published private(set) var access: [Source: String] = [:]
    /// What each running source is doing now.
    @Published private(set) var progress: [Source: String] = [:]
    /// The device's Health days with data, from the last coverage scan.
    @Published private(set) var healthAvailable: Set<String>?
    /// The start of every occurrence in the calendar window, from the last
    /// coverage scan.
    @Published private(set) var eventsAvailable: [Date]?
    @Published private(set) var coverageLoading = false
    @Published private(set) var coverageError: String?
    /// How far back the coverage scan looks for Health data.
    static let coverageYears = 10

    private static let generatedKeyDefaults = "trigger.generatedKeyHash"

    init() {
        session = Keychain.load()
        Task {
            await SyncEngine.shared.setOnChange { [weak self] state in self?.state = state }
            await SyncEngine.shared.setOnProgress { [weak self] source, text in self?.progress[source] = text }
        }
        refreshAccess()
    }

    var client: Client? { session.map { Client(server: $0.server, secret: $0.secret) } }
    var setupComplete: Bool { setupChecked && steps.allSatisfy(\.done) }

    // MARK: - sign in and out

    func signIn(server: URL, repository: String, password: String, totp: String?) async throws {
        let label = "Substrate Sync on \(UIDevice.current.name)"
        let result = try await Client(server: server).login(
            repository: repository, password: password, totp: totp, label: label)
        let session = Session(server: server, repository: result.repository, secret: result.secret, tokenID: result.token.id)
        if let previous = Keychain.load(), previous.server != server || previous.repository != result.repository {
            // Another repository holds none of these rows: stop a run that is
            // still pushing to the old one, and forget what it pushed.
            await SyncEngine.shared.cancel()
            state = await SyncEngine.shared.update { $0.forgetPushed() }
        }
        Keychain.save(session)
        self.session = session
        setupChecked = false
        await checkSetup()
    }

    /// Revokes the token, then forgets it. A revocation that fails still
    /// signs out here, and says so.
    func signOut() async -> String? {
        var problem: String?
        if let session, let client {
            do { try await client.revoke(token: session.tokenID) } catch {
                problem = "The token was not revoked: \(error.localizedDescription)"
            }
        }
        await SyncEngine.shared.cancel()
        Keychain.clear()
        session = nil
        steps = []
        setupChecked = false
        return problem
    }

    // MARK: - set up

    func checkSetup() async {
        guard let client else { return }
        setupError = nil
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
                    guard try await armTrigger(client, replaceKey: replaceKey) else {
                        foreignKey = true
                        await checkSetup()
                        return
                    }
                }
            }
        } catch {
            setupError = error.localizedDescription
        }
        await checkSetup()
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
        if key != existing { UserDefaults.standard.set(Self.keyHash(key), forKey: Self.generatedKeyDefaults) }
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
            eventsAvailable = await Task.detached {
                let store = EKEventStore()
                return store.events(matching: store.predicateForEvents(withStart: start, end: end, calendars: nil))
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

    func syncNow(_ only: Set<Source>? = nil) async {
        syncing = true
        defer { syncing = false }
        for source in Source.allCases where Settings.enabled(source) && (only?.contains(source) ?? true) {
            if access[source] == "Not asked yet" { await requestAccess(source) }
        }
        await SyncEngine.shared.sync(only)
        state = LocalState.load()
        await loadCoverage()
    }
}
