import Foundation
import WorkoutsCore

/// The training package's names between the phone and one repository.
///
/// On the phone the package is `~/training` (`KindNames.v1`); in a
/// repository it is `<authority>/training` (decision 0203). Every record and
/// manifest crosses the edge through here: a kind, a reference's path, a
/// package or kind id and a document's `authority`. Nothing else is touched,
/// so a reference into `substrate.reamde.dev/health` keeps its name.
public struct Rehome: Sendable, Hashable {
    public let authority: String

    public init(authority: String) {
        self.authority = authority
    }

    var local: String { KindNames.packageID(authority: KindNames.home) }
    var remote: String { KindNames.packageID(authority: authority) }

    /// The phone's spelling to the repository's.
    public func outbound(_ value: WorkoutsCore.JSONValue) -> WorkoutsCore.JSONValue { swap(value, from: local, to: remote, authorityTo: authority) }

    /// The repository's spelling to the phone's.
    public func inbound(_ value: WorkoutsCore.JSONValue) -> WorkoutsCore.JSONValue { swap(value, from: remote, to: local, authorityTo: KindNames.home) }

    public func outbound(_ record: FlatRecord) -> FlatRecord { rehome(record, outbound) }

    public func inbound(_ record: FlatRecord) -> FlatRecord { rehome(record, inbound) }

    /// A kind's name as the repository spells it.
    public func remoteKind(_ kind: String) -> String { swapped(kind, from: local, to: remote) ?? kind }

    private func rehome(_ record: FlatRecord, _ convert: (WorkoutsCore.JSONValue) -> WorkoutsCore.JSONValue) -> FlatRecord {
        guard case let .string(kind) = convert(.string(record.kind)),
              case let .object(properties) = convert(.object(record.properties))
        else { return record }
        return FlatRecord(kind: kind, id: record.id, properties: properties, version: record.version)
    }

    private func swap(_ value: WorkoutsCore.JSONValue, from: String, to: String, authorityTo: String) -> WorkoutsCore.JSONValue {
        switch value {
        case let .string(text):
            return .string(swapped(text, from: from, to: to) ?? text)
        case let .array(items):
            return .array(items.map { swap($0, from: from, to: to, authorityTo: authorityTo) })
        case let .object(object):
            var out = JSONObject()
            for (key, item) in object {
                // A manifest's `authority` is the package's: the one field
                // that names it without its package.
                if key == "authority", case let .string(text) = item, text == from.split(separator: "/").first.map(String.init) {
                    out[key] = .string(authorityTo)
                } else {
                    out[key] = swap(item, from: from, to: to, authorityTo: authorityTo)
                }
            }
            return .object(out)
        default:
            return value
        }
    }

    /// `text` with the package prefix swapped, or nil when it does not name
    /// the package: the package id itself, or anything under it.
    private func swapped(_ text: String, from: String, to: String) -> String? {
        if text == from { return to }
        if text.hasPrefix(from + "/") { return to + text.dropFirst(from.count) }
        return nil
    }
}
