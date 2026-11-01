import Foundation
import WorkoutsCore

/// How the session cover draws the session (DESIGN.md §7.6, §7.6a): Rows,
/// one line per set with the set to do now open in place, or Table, the
/// grid. Both read the same presentation and send the same commands.
enum SessionLayout: String, CaseIterable, Equatable {
    case rows
    case table

    var title: String {
        switch self {
        case .rows: "Rows"
        case .table: "Table"
        }
    }

    /// The session's More menu item that switches to the other layout.
    var switchTitle: String {
        switch self {
        case .rows: "Show as table"
        case .table: "Show as rows"
        }
    }

    var switchSymbol: String {
        switch self {
        case .rows: "tablecells"
        case .table: "list.bullet.rectangle"
        }
    }

    var other: SessionLayout { self == .rows ? .table : .rows }
}

/// The owner's settings (DESIGN.md §6.4, §7.10), kept in `UserDefaults` under
/// `workouts.*` keys and read through this one struct. A session snapshots
/// the rest and counting settings at Start (`session`), so a fold never
/// depends on a setting changed later.
struct WorkoutsSettings: Equatable {
    var defaultUnit: MassUnit = .kg
    var warmupRest: Seconds = RestDefaults.warmupRest
    var setRest = SetRestDefaults()
    var restAfterLastSet = true
    var countWarmupsInVolume = false
    var supersetAutoScroll = true
    var keepAwake = true
    /// Rows by default; Table is the grid (§7.6).
    var sessionLayout: SessionLayout = .rows
    /// Used at Start when Apple Health has no body mass (§9.5).
    var typedBodyMass: Mass?

    /// What `start` snapshots.
    var session: SessionSettings {
        SessionSettings(defaultUnit: defaultUnit, warmupRest: warmupRest, setRest: setRest,
                        restAfterLastSet: restAfterLastSet, countWarmupsInVolume: countWarmupsInVolume)
    }

    /// What the screens outside a session format with.
    var presenter: PresenterSettings {
        PresenterSettings(calendar: .current, defaultUnit: defaultUnit, countWarmupsInVolume: countWarmupsInVolume)
    }

    /// What the session in progress is presented with: Rows pre-fills the
    /// plan's RPE (§4.14), so its strip shows it and Log commits it; the
    /// Table pre-fills nothing, as before.
    var sessionPresenter: PresenterSettings {
        PresenterSettings(calendar: .current, defaultUnit: defaultUnit, countWarmupsInVolume: countWarmupsInVolume,
                          prefillsRPE: sessionLayout == .rows)
    }

    // MARK: - Storage

    enum Key {
        static let defaultUnit = "workouts.defaultUnit"
        static let warmupRest = "workouts.warmupRest"
        static let setRest = "workouts.setRest"
        static let restAfterLastSet = "workouts.restAfterLastSet"
        static let countWarmupsInVolume = "workouts.countWarmupsInVolume"
        static let supersetAutoScroll = "workouts.supersetAutoScroll"
        static let keepAwake = "workouts.keepAwake"
        static let sessionLayout = "workouts.sessionLayout"
        static let bodyMass = "workouts.bodyMass"
    }

    /// The stored settings; a key never written keeps its default.
    static func load(from defaults: UserDefaults = .standard) -> WorkoutsSettings {
        var settings = WorkoutsSettings()
        if let raw = defaults.string(forKey: Key.defaultUnit), let unit = MassUnit(rawValue: raw) { settings.defaultUnit = unit }
        if defaults.object(forKey: Key.warmupRest) != nil { settings.warmupRest = defaults.integer(forKey: Key.warmupRest) }
        if let data = defaults.data(forKey: Key.setRest), let rests = try? JSONDecoder().decode(SetRestDefaults.self, from: data) {
            settings.setRest = rests
        }
        settings.restAfterLastSet = defaults.object(forKey: Key.restAfterLastSet) as? Bool ?? true
        settings.countWarmupsInVolume = defaults.object(forKey: Key.countWarmupsInVolume) as? Bool ?? false
        settings.supersetAutoScroll = defaults.object(forKey: Key.supersetAutoScroll) as? Bool ?? true
        settings.keepAwake = defaults.object(forKey: Key.keepAwake) as? Bool ?? true
        if let raw = defaults.string(forKey: Key.sessionLayout), let layout = SessionLayout(rawValue: raw) {
            settings.sessionLayout = layout
        }
        if let data = defaults.data(forKey: Key.bodyMass) { settings.typedBodyMass = try? JSONDecoder().decode(Mass.self, from: data) }
        return settings
    }

    func save(to defaults: UserDefaults = .standard) {
        defaults.set(defaultUnit.rawValue, forKey: Key.defaultUnit)
        defaults.set(warmupRest, forKey: Key.warmupRest)
        defaults.set(try? JSONEncoder().encode(setRest), forKey: Key.setRest)
        defaults.set(restAfterLastSet, forKey: Key.restAfterLastSet)
        defaults.set(countWarmupsInVolume, forKey: Key.countWarmupsInVolume)
        defaults.set(supersetAutoScroll, forKey: Key.supersetAutoScroll)
        defaults.set(keepAwake, forKey: Key.keepAwake)
        defaults.set(sessionLayout.rawValue, forKey: Key.sessionLayout)
        if let typedBodyMass {
            defaults.set(try? JSONEncoder().encode(typedBodyMass), forKey: Key.bodyMass)
        } else {
            defaults.removeObject(forKey: Key.bodyMass)
        }
    }
}
