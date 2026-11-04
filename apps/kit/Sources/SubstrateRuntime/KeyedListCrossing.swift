import Foundation
import SubstrateCore

/// Where a keyed list went in a presentation that crossed without its rows:
/// the list's name and the revision of it the presentation was drawn with.
/// Kotlin puts the rows back in its place before it decodes the tree.
public struct KeyedListReference: Codable, Hashable, Sendable {
    public var list: String
    public var revision: Int

    enum CodingKeys: String, CodingKey {
        case list = "$list"
        case revision
    }

    public init(list: String, revision: Int) {
        self.list = list
        self.revision = revision
    }
}

/// What takes a keyed list of any row type: the bridge's context, which
/// sends it whole the first time and as deltas after.
public protocol KeyedListReceiver {
    func receive<Row>(list name: String, _ list: KeyedList<Row>)
}

/// A keyed list a presentation carried apart, at the revision it crosses
/// at, with its row type kept for the receiver.
public struct CarriedList: Sendable {
    public let name: String
    public let revision: Int
    private let hand: @Sendable (any KeyedListReceiver) -> Void

    init<Row>(name: String, list: KeyedList<Row>) {
        self.name = name
        revision = list.revision
        hand = { $0.receive(list: name, list) }
    }

    public func hand(to receiver: some KeyedListReceiver) {
        hand(receiver)
    }
}

/// One stream of presentations crossing to Android, and the keyed lists in
/// it. Each list is named by where it sits in the tree, the coding path's
/// keys joined by `/` (`app/tasks`), and crosses at a revision of the
/// crossing's own, moved whenever its rows or their order differ from the
/// last it carried, so a list a controller made again from scratch (a
/// reset, a new child under the same key) still moves forward and Kotlin
/// never refuses it as old.
///
/// Read and written by one encoding at a time: `RuntimeFrames` encodes its
/// presentations in order on one task.
public final class KeyedListCrossing: @unchecked Sendable {
    public static let userInfoKey = CodingUserInfoKey(rawValue: "dev.reamde.substrate.keyedListCrossing")!

    private struct Last {
        var rows: any Sendable
        var same: (any Sendable) -> Bool
        var revision: Int
    }

    private var last: [String: Last] = [:]
    private var carried: [CarriedList] = []

    public init() {}

    /// Notes a list where it is being encoded and answers the reference the
    /// presentation writes in its place.
    func carry<Row>(_ list: KeyedList<Row>, at path: [any CodingKey]) -> KeyedListReference {
        let name = path.map { $0.intValue.map(String.init) ?? $0.stringValue }.joined(separator: "/")
        var revision = 1
        if let before = last[name] {
            revision = before.same(list.rows) ? before.revision : before.revision + 1
        }
        if last[name]?.revision != revision {
            let rows = list.rows
            last[name] = Last(rows: rows, same: { ($0 as? [Row]) == rows }, revision: revision)
            carried.append(CarriedList(name: name, list: KeyedList(rows, revision: revision)))
        }
        return KeyedListReference(list: name, revision: revision)
    }

    /// Encodes `presentation` with every keyed list in it written as its
    /// reference; answers the JSON and the lists whose revision moved, in
    /// the order the encoding met them.
    public func encode(_ presentation: some Encodable) throws -> (json: Data, moved: [CarriedList]) {
        carried = []
        defer { carried = [] }
        let json = try ValueCoding.encode(presentation, userInfo: [Self.userInfoKey: self]).data(pretty: false)
        return (json, carried)
    }
}
