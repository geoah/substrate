import Foundation

/// One part of a presentation as it goes out to a UI that observes parts:
/// the value, and the path of keys it is read under, from the root down. A
/// controller says which parts its presentation goes out in
/// (`Controller.parts(of:)`); the runtime sends a part again only when its
/// value differs from the one it last sent under that path.
public struct PresentationPart: Sendable {
    public let path: [ChildKey]
    public let value: any Sendable
    private let equals: @Sendable (any Sendable) -> Bool

    public init<Value: Equatable & Sendable>(_ value: Value, at path: [ChildKey] = []) {
        self.path = path
        self.value = value
        equals = { ($0 as? Value) == value }
    }

    private init(path: [ChildKey], value: any Sendable, equals: @escaping @Sendable (any Sendable) -> Bool) {
        self.path = path
        self.value = value
        self.equals = equals
    }

    /// The part one level down: a parent's place for a part of the child it
    /// keeps under `key`.
    public func under(_ key: ChildKey) -> PresentationPart {
        PresentationPart(path: [key] + path, value: value, equals: equals)
    }

    /// Whether `other` holds the same value, of the same type.
    public func same(as other: PresentationPart) -> Bool { equals(other.value) }
}

/// What a publish changed, for a reader of parts: the tree as it now stands,
/// the parts whose value moved, and the paths that hold none any more. Where
/// the reader was slower than the runtime, it is everything since it last
/// read: the newest value of each part that moved, and every path dropped.
public struct PartsUpdate<Presentation: Sendable>: Sendable {
    public var presentation: Presentation
    public var changed: [PresentationPart]
    public var removed: [[ChildKey]]
}

/// A runtime's presentations in parts, read one update at a time: the first
/// holds every part as it stands, each one after what moved since the one
/// before. Nothing between two reads is lost, and nothing waits in a queue:
/// what the runtime published while the reader was busy is merged, the
/// newest value of each part kept.
public struct PresentationParts<Presentation: Sendable>: AsyncSequence, Sendable {
    public typealias Element = PartsUpdate<Presentation>

    let box: PartsBox<Presentation>
    let wakes: AsyncStream<Void>

    public func makeAsyncIterator() -> Iterator {
        Iterator(box: box, wakes: wakes.makeAsyncIterator())
    }

    public struct Iterator: AsyncIteratorProtocol {
        let box: PartsBox<Presentation>
        var wakes: AsyncStream<Void>.Iterator

        public mutating func next() async -> PartsUpdate<Presentation>? {
            while await wakes.next() != nil {
                if let update = box.take() { return update }
            }
            return nil
        }
    }
}

/// What one reader of parts has not read yet. Written on the runtime's
/// executor and read on the reader's, under `lock`.
final class PartsBox<Presentation: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var presentation: Presentation?
    private var changed: [[ChildKey]: PresentationPart] = [:]
    /// The order the paths first moved in, so an update reads as the tree does.
    private var order: [[ChildKey]] = []
    private var removed: Set<[ChildKey]> = []

    func put(_ presentation: Presentation, changed parts: [PresentationPart], removed paths: [[ChildKey]]) {
        lock.withLock {
            self.presentation = presentation
            for path in paths {
                if changed.removeValue(forKey: path) != nil { order.removeAll { $0 == path } }
                removed.insert(path)
            }
            for part in parts {
                removed.remove(part.path)
                if changed.updateValue(part, forKey: part.path) == nil { order.append(part.path) }
            }
        }
    }

    func take() -> PartsUpdate<Presentation>? {
        lock.withLock {
            guard let presentation else { return nil }
            let update = PartsUpdate(
                presentation: presentation, changed: order.compactMap { changed[$0] }, removed: removed.sorted { $0.lexicographicallyPrecedes($1) })
            self.presentation = nil
            changed = [:]
            order = []
            removed = []
            return update
        }
    }
}
