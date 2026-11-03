/// Every kind name the app reads or writes. Nothing else in the code spells
/// one. The package is published under `substrate.reamde.dev`, so the names
/// are the same in every repository (apps/tasks/package/README.md).
public enum TasksKinds {
    public static let authority = "substrate.reamde.dev"
    public static let packageID = "\(authority)/tasks"
    public static let task = "\(packageID)/task"
    public static let list = "\(packageID)/list"
    /// Both, as a list read and the watch name them.
    public static let all = [task, list]
}
