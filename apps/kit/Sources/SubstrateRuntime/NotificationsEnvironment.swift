import SubstrateDevice

private enum NotificationsKey: AppEnvironmentKey {
    static let defaultValue: (any Notifications)? = nil
}

public extension AppEnvironment {
    /// The device's notifications, nil where the app shows none.
    var notifications: (any Notifications)? {
        get { self[NotificationsKey.self] }
        set { self[NotificationsKey.self] = newValue }
    }
}
