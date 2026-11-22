/// Every kind and trait name the app reads or writes (docs/records.md).
/// Nothing else in the code spells one. The app publishes none of them and
/// asks for `full`, so a repository may hold any part of this list: the
/// app reads only the kinds it finds (`Discovery`).
public enum TodayKinds {
    public static let authority = "substrate.reamde.dev"
    public static let packageID = "\(authority)/today"

    // Projects and tasks.
    public static let list = "\(authority)/tasks/list"
    public static let task = "\(authority)/tasks/task"

    // Routines: the trait a series binds, and the one log kind.
    public static let series = "\(authority)/routines/series"
    public static let log = "\(authority)/routines/routinelog"
    /// What a dose is of: read for its strength, which a dose's row says.
    public static let medication = "\(authority)/health/medication"

    // The app's own.
    public static let preference = "\(packageID)/preference"
    public static let plan = "\(packageID)/plan"
    public static let advice = "\(packageID)/advice"
    public static let digest = "\(packageID)/digest"
    public static let reviewmark = "\(packageID)/reviewmark"
    public static let dossier = "\(packageID)/dossier"
    public static let returnpoint = "\(packageID)/returnpoint"
    public static let rule = "\(packageID)/rule"
    /// What the owner said was wrong with a suggestion (today 4).
    public static let correction = "\(packageID)/correction"

    // Core.
    public static let patchRequest = "\(authority)/core/recordpatchrequest"
    public static let contact = "\(authority)/core/contact"
    public static let kind = "\(authority)/core/kind"
    public static let recurring = "\(authority)/core/recurring"
    public static let override = "\(authority)/core/override"
    public static let instruction = "\(authority)/llm/instruction"

    /// The assistant.
    public static let assistant = "\(authority)/assistant/moss"

    /// The kinds read whole and followed through the watch, where the
    /// repository holds them; the series kinds join them once discovered.
    public static let linkable = [
        list, task, log, medication, preference, plan, advice, digest, reviewmark, dossier, returnpoint, instruction, correction,
    ]

    /// The one preference record, and the digest and review mark that
    /// belong to no project.
    public static let preferenceID = "default"
    public static let unfiled = "unfiled"
}
