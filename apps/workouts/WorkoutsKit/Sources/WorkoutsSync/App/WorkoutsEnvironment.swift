import SubstrateRuntime

private enum WorkoutsAccountsKey: AppEnvironmentKey {
    static let defaultValue: WorkoutsAccounts? = nil
}

public extension AppEnvironment {
    /// The signed-in repositories' folders; nil keeps none (a preview).
    var accounts: WorkoutsAccounts? {
        get { self[WorkoutsAccountsKey.self] }
        set { self[WorkoutsAccountsKey.self] = newValue }
    }
}
