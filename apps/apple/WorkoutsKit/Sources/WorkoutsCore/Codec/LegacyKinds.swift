import Foundation

/// The training kinds as the builds before the package was published
/// spelled them on the phone: `~/training/…`, swapped for the repository's
/// authority at the server's edge and back (decision T3, replaced by T7).
/// Those builds' files are still on the phone after an update, every kind,
/// reference path and version key under `~`, and nothing on the phone ever
/// held the repository's own spelling, since the edge put `~` back on every
/// read. So each store reads a file from before its format moved through
/// here once, and writes it back in the published spelling under the moved
/// format, after which nothing is left to respell.
public enum LegacyKinds {
    /// The package id those builds spelled.
    public static let packageID = "~/\(KindNames.package)"

    /// `text` in the published spelling, or itself when it does not name
    /// the package: the package id, a kind under it or a record path.
    public static func respell(_ text: String) -> String {
        if text == packageID { return KindNames.packageID }
        if text.hasPrefix(packageID + "/") { return KindNames.packageID + text.dropFirst(packageID.count) }
        return text
    }

    /// Every string in `value` that names the package, respelled: what the
    /// old build swapped on its way out, so a file reads as the server was
    /// sent it.
    public static func respell(_ value: JSONValue) -> JSONValue {
        switch value {
        case let .string(text): .string(respell(text))
        case let .array(items): .array(items.map(respell))
        case let .object(object): .object(respell(object))
        default: value
        }
    }

    public static func respell(_ object: JSONObject) -> JSONObject {
        var out = JSONObject()
        for (key, item) in object {
            out[key] = respell(item)
        }
        return out
    }

    public static func respell(_ record: FlatRecord) -> FlatRecord {
        FlatRecord(kind: respell(record.kind), id: record.id, properties: respell(record.properties), version: record.version)
    }

    /// A record file from before its format moved, respelled, or nil when
    /// its format is `current` and nothing is to do.
    public static func respell(_ file: RecordFile, current: Int) -> RecordFile? {
        guard (file.format ?? 1) < current else { return nil }
        return RecordFile(source: file.source, format: current, records: file.records.map(respell))
    }
}
