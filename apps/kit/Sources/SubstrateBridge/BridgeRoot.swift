import Foundation
import SubstrateCore

/// What the bridge needs from a root controller, and all it needs: commands
/// in, encoded, and a close. Presentations go out through the context the
/// root was made with. The app kit's runtime conforms its roots to this; the
/// bridge never learns what a command or a presentation holds, so it does
/// not change when an app adds a controller.
public protocol BridgeRoot: Sendable {
    /// One command, in the order Kotlin sent them, one at a time. A throw is
    /// reported to Kotlin as a `swiftFailure` for the handle; the root stays open.
    func receive(_ command: JSONValue) async throws
    /// The handle closed: stop. The context refuses everything after it.
    func close() async
}

/// Makes a root for `open`. It runs inside `post`, so it does no work beyond
/// building the root; the work starts from there on Swift's executors.
public typealias RootFactory = @Sendable (_ settings: JSONValue?, _ context: BridgeContext) throws -> any BridgeRoot

/// The roots an app offers by name, filled once before `start` answers.
public final class RootRegistry: @unchecked Sendable {
    private let lock = NSLock()
    private var factories: [String: RootFactory] = [:]

    public init() {}

    public func register(_ name: String, _ factory: @escaping RootFactory) {
        lock.withLock { factories[name] = factory }
    }

    func factory(_ name: String) -> RootFactory? {
        lock.withLock { factories[name] }
    }

    public var names: [String] {
        lock.withLock { factories.keys.sorted() }
    }
}

/// One open root's way out: its presentations and keyed lists, delivered in
/// order on its own queue, and the device services, whose calls the close of
/// its handle cancels.
public final class BridgeContext: Sendable {
    public let handle: Handle
    public let environment: BridgeEnvironment
    /// Device calls made for this root; closing the handle answers each one
    /// still outstanding `cancelled`.
    public let device: DeviceChannel
    let outbox: HandleOutbox
    let lists: KeyedLists

    init(handle: Handle, environment: BridgeEnvironment, device: DeviceChannel, outbox: HandleOutbox) {
        self.handle = handle
        self.environment = environment
        self.device = device
        self.outbox = outbox
        lists = KeyedLists(handle: handle, outbox: outbox)
    }

    /// The newest presentation. It is encoded when its turn comes, and one
    /// still waiting for its turn is replaced, so a slow consumer skips the
    /// ones between.
    public func present(_ presentation: some Encodable & Sendable) {
        outbox.present(presentation)
    }

    /// A keyed list's rows, in order: the first time whole, after that the
    /// rows inserted, changed and removed since, and never coalesced.
    public func publish<Row: Encodable & Equatable & Sendable>(list name: String, rows: [Row], id: (Row) -> String) {
        lists.publish(name, rows: rows, id: id)
    }

    /// The handle is closed: nothing sent through this context goes out.
    public var isClosed: Bool { outbox.isClosed }
}
