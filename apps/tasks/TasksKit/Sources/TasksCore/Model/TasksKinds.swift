/// Every kind and trait name the app names itself. Nothing else in the code
/// spells one: the other kinds it shows are the owner's, found through the
/// lifecycle trait. The package is published under `substrate.reamde.dev`,
/// so the names are the same in every repository
/// (apps/tasks/package/README.md).
public enum TasksKinds {
    public static let authority = "substrate.reamde.dev"
    public static let packageID = "\(authority)/tasks"
    public static let task = "\(packageID)/task"
    public static let list = "\(packageID)/list"
    /// What the owner chose about one collection, and across them.
    public static let collection = "\(packageID)/collection"
    public static let preference = "\(packageID)/preference"
    /// The app's own two, as the data link opens over them before anything
    /// else is known.
    public static let all = [task, list]
    /// The settings kinds, on a link of their own that stays open.
    public static let settings = [collection, preference]

    /// Every kind with a status binds it; the app's candidates are its
    /// implementors.
    public static let lifecycle = "substrate.reamde.dev/core/lifecycle"
    public static let temporal = "substrate.reamde.dev/core/temporal"
}
