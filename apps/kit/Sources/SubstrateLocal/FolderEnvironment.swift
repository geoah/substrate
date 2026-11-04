import SubstrateRuntime

private enum FolderKey: AppEnvironmentKey {
    static let defaultValue: RepositoryFolder? = nil
}

public extension AppEnvironment {
    /// The signed-in repository's folder, nil while there is none.
    var folder: RepositoryFolder? {
        get { self[FolderKey.self] }
        set { self[FolderKey.self] = newValue }
    }
}
