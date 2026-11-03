import Foundation

/// The owner's settings (DESIGN.md §6.4, §7.10), kept in `UserDefaults` under
/// `workouts.*` keys and read through this one struct. A session snapshots
/// the rest and counting settings at Start (`session`), so a fold never
/// depends on a setting changed later.
public struct WorkoutsSettings: Codable, Equatable, Sendable {
    public var defaultUnit: MassUnit = .kg
    public var warmupRest: Seconds = RestDefaults.warmupRest
    public var setRest = SetRestDefaults()
    public var restAfterLastSet = true
    public var countWarmupsInVolume = false
    public var supersetAutoScroll = true
    public var keepAwake = true
    /// Rows by default; Table is the grid (§7.6).
    public var sessionLayout: SessionLayout = .rows
    /// Used at Start when Apple Health has no body mass (§9.5).
    public var typedBodyMass: Mass?

    public init() {}

    /// What `start` snapshots.
    public var session: SessionSettings {
        SessionSettings(defaultUnit: defaultUnit, warmupRest: warmupRest, setRest: setRest,
                        restAfterLastSet: restAfterLastSet, countWarmupsInVolume: countWarmupsInVolume)
    }

    /// What the screens outside a session format with.
    public var presenter: PresenterSettings {
        PresenterSettings(calendar: .current, defaultUnit: defaultUnit, countWarmupsInVolume: countWarmupsInVolume)
    }

    /// What the session in progress is presented with: Rows pre-fills the
    /// plan's RPE (§4.14), so its strip shows it and Log commits it; the
    /// Table pre-fills nothing, as before.
    public var sessionPresenter: PresenterSettings {
        PresenterSettings(calendar: .current, defaultUnit: defaultUnit, countWarmupsInVolume: countWarmupsInVolume,
                          prefillsRPE: sessionLayout == .rows)
    }

    // MARK: - Storage

    public enum Key {
        public static let defaultUnit = "workouts.defaultUnit"
        public static let warmupRest = "workouts.warmupRest"
        public static let setRest = "workouts.setRest"
        public static let restAfterLastSet = "workouts.restAfterLastSet"
        public static let countWarmupsInVolume = "workouts.countWarmupsInVolume"
        public static let supersetAutoScroll = "workouts.supersetAutoScroll"
        public static let keepAwake = "workouts.keepAwake"
        public static let sessionLayout = "workouts.sessionLayout"
        public static let bodyMass = "workouts.bodyMass"
    }

    /// The stored settings; a key never written keeps its default.
    public static func load(from defaults: UserDefaults = .standard) -> WorkoutsSettings {
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

    public func save(to defaults: UserDefaults = .standard) {
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
