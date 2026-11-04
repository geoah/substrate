/// WorkoutsCore holds what every Workouts surface shares and no UI framework
/// touches: the workout model and its record codec, the session engine, the
/// rest rules, the suggestion engine, the presentation, the file stores and
/// the seams WorkoutsSync plugs into. Foundation only; every public
/// type is `Sendable` and none is actor-isolated.
public enum WorkoutsFormats {
    /// The `format` of `active-session.json`, raised when the journal's shape changes.
    public static let journalFormat = 1
    /// The `format` of each `history/<session id>.json`, raised when its
    /// shape changes. 2 since the data-model review (2026-10-31), read
    /// through `HistoryMigration` from 1; 3 since the package was published
    /// (2026-11-03), read through `LegacyKinds` from 2 and below.
    public static let historyFormat = 3
    /// The `format` of `catalog.json` and `local-exercises.json`, files of
    /// flat records: 2 since the package was published, read through
    /// `LegacyKinds` from 1.
    public static let recordFileFormat = 2
    /// The `format` of `routine-draft.json`: 2 since the package was
    /// published, read through `LegacyKinds` from 1.
    public static let draftFormat = 2
}
