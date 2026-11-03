import SubstrateCore

/// How an app reads an outbox file an older build of it wrote, at a format
/// before `Outbox.fileFormat`: every entry's kind and properties, and every
/// version and chain key (`kind/id`), respelled as this build spells them.
/// Workouts' format 1 named its package `~/training` on the device, which
/// its `LegacyKinds` respells. The file is written back once at this format.
public struct OutboxUpgrade: Sendable {
    let respell: @Sendable (String) -> String
    let respellProperties: @Sendable (JSONObject) -> JSONObject

    /// - Parameters:
    ///   - respell: a kind, or a record's `kind/id`, in this build's
    ///     spelling.
    ///   - respellProperties: an entry's properties in this build's
    ///     spelling (a reference names a kind).
    public init(respell: @escaping @Sendable (String) -> String,
                respellProperties: @escaping @Sendable (JSONObject) -> JSONObject)
    {
        self.respell = respell
        self.respellProperties = respellProperties
    }

    func apply(to file: inout Outbox.File) {
        for index in file.entries.indices {
            file.entries[index].kind = respell(file.entries[index].kind)
            file.entries[index].properties = file.entries[index].properties.map(respellProperties)
        }
        file.versions = Dictionary(file.versions.map { (respell($0.key), $0.value) }, uniquingKeysWith: { first, _ in first })
        file.chains = Dictionary(file.chains.map { (respell($0.key), $0.value) }, uniquingKeysWith: { first, _ in first })
        file.format = Outbox.fileFormat
    }
}
