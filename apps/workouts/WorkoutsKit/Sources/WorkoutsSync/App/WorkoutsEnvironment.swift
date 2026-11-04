import SubstrateRuntime

private enum WorkoutsFoldersKey: AppEnvironmentKey {
    static let defaultValue: WorkoutsFolders? = nil
}

public extension AppEnvironment {
    /// The signed-in repositories' folders; nil keeps none (a preview).
    var folders: WorkoutsFolders? {
        get { self[WorkoutsFoldersKey.self] }
        set { self[WorkoutsFoldersKey.self] = newValue }
    }
}
