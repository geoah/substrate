import SubstrateCore

/// How an app reads an outbox file an older build of it wrote, at a format
/// before `Outbox.fileFormat`: every entry's kind and properties, and every
/// version and chain key (`kind/id`), in this build's spelling.
/// Workouts' format 1 named its package `~/training` on the device, which
/// its `LegacyKinds` respells. The file is written back once at this format.
///
/// A record the respell moves is not the record the old build knew: the
/// moved kind may be empty on the server or hold copies the owner made, so a
/// version carried over would read a record it lacks as deleted there, and a
/// guard under it would be refused. Its versions and chains go, a write of
/// it goes without its guard, and a write the app derives again from its own
/// files (`rederives`) goes altogether. Nothing already in this build's
/// spelling is touched.
public struct OutboxUpgrade: Sendable {
    let respell: @Sendable (String) -> String
    let respellProperties: @Sendable (JSONObject) -> JSONObject
    let rederives: @Sendable (OutboxEntry) -> Bool

    /// - Parameters:
    ///   - respell: a kind, or a record's `kind/id`, in this build's
    ///     spelling.
    ///   - respellProperties: an entry's properties in this build's
    ///     spelling (a reference names a kind).
    ///   - rederives: a write of a moved record, in the old spelling, that
    ///     the app writes again from what it keeps itself, so it is dropped
    ///     rather than sent.
    public init(respell: @escaping @Sendable (String) -> String,
                respellProperties: @escaping @Sendable (JSONObject) -> JSONObject,
                rederives: @escaping @Sendable (OutboxEntry) -> Bool = { _ in false })
    {
        self.respell = respell
        self.respellProperties = respellProperties
        self.rederives = rederives
    }

    func apply(to file: inout Outbox.File) {
        file.entries = file.entries.compactMap { entry in
            let kind = respell(entry.kind)
            guard kind != entry.kind else { return entry }
            if rederives(entry) { return nil }
            var entry = entry
            entry.kind = kind
            entry.properties = entry.properties.map(respellProperties)
            entry.guarded = false
            entry.baseVersion = nil
            return entry
        }
        file.versions = file.versions.filter { respell($0.key) == $0.key }
        file.chains = file.chains.filter { respell($0.key) == $0.key }
        file.format = Outbox.fileFormat
    }
}
