import Foundation
import SubstrateCore
import SubstrateRuntime

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
    /// The device's locale or time zone changed. It arrives in order with
    /// the commands, and `BridgeContext.environment` reads the new one from
    /// then on.
    func environmentChanged(_ environment: BridgeEnvironment) async
}

public extension BridgeRoot {
    func environmentChanged(_: BridgeEnvironment) async {}
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
    private let current: EnvironmentCell
    /// Device calls made for this root; closing the handle answers each one
    /// still outstanding `cancelled`.
    public let device: DeviceChannel
    let outbox: HandleOutbox
    let lists: KeyedLists

    init(handle: Handle, environment: BridgeEnvironment, device: DeviceChannel, outbox: HandleOutbox) {
        self.handle = handle
        current = EnvironmentCell(environment)
        self.device = device
        self.outbox = outbox
        lists = KeyedLists(handle: handle, outbox: outbox)
    }

    /// The device's settings now: an environment update replaces them.
    public var environment: BridgeEnvironment { current.value }

    func update(_ environment: BridgeEnvironment) {
        current.value = environment
    }

    /// The newest presentation. It is encoded when its turn comes, and one
    /// still waiting for its turn is replaced, so a slow consumer skips the
    /// ones between.
    public func present(_ presentation: some Encodable & Sendable) {
        outbox.present(presentation)
    }

    /// A presentation already encoded (`Runtime.encodedPresentations()`),
    /// coalesced as `present`'s are.
    public func present(encoded json: Data) {
        outbox.present(EncodedJSON(json))
    }

    /// A failure for this handle, as Kotlin hears it; the handle stays open.
    public func fail(_ failure: BridgeFailure) {
        _ = outbox.enqueue(BridgeCodec.failure(failure, handle: handle))
    }

    /// A keyed list, by name: the first time whole, after that as the
    /// `KeyedListDelta` from the revision sent before, never coalesced. A
    /// revision already sent is not sent again.
    public func publish<Row>(list name: String, _ list: KeyedList<Row>) {
        lists.publish(name, list)
    }

    /// The handle is closed: nothing sent through this context goes out.
    public var isClosed: Bool { outbox.isClosed }
}

/// One environment, read and replaced from any thread.
final class EnvironmentCell: @unchecked Sendable {
    private let lock = NSLock()
    private var held: BridgeEnvironment

    init(_ environment: BridgeEnvironment) {
        held = environment
    }

    var value: BridgeEnvironment {
        get { lock.withLock { held } }
        set { lock.withLock { held = newValue } }
    }
}
