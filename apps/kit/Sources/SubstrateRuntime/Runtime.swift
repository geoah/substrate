import Foundation
import SubstrateCore

/// Runs one root controller and every child under it as one presentation
/// tree (§5.2): commands in, presentations out, on an executor of its own,
/// never the main thread's. A step runs from its command or result through
/// the engine to the journal write without a suspension; then the tree is
/// presented, now or at the end of the event's window, and published once;
/// then the effects its events asked for start. An effect's result is an
/// event through the same loop.
///
/// Making a runtime reads nothing: the root opens (its journal folded, its
/// children made, its event at open applied) on the runtime's own executor
/// at the first call, so a journal is never replayed on the caller's thread.
///
/// The UI reads `presentations()`; SubstrateUI's `PresentationModel`
/// republishes them on the main actor for SwiftUI, and the Android bridge
/// reads `encodedFrames()` and sends `send(encoded:)`.
public actor Runtime<Root: Controller> {
    private let loop: Loop<Root>
    private var opened = false

    public init(_ root: Root, environment: AppEnvironment, options: RuntimeOptions = RuntimeOptions()) {
        let box = RuntimeBox<Root>()
        let loop = Loop<Root>(environment: environment, options: options) { body in
            await box.runtime?.perform(body)
        }
        loop.root = Host(controller: root, path: [], loop: loop, journal: root.journal(in: environment))
        self.loop = loop
        box.runtime = self
    }

    /// Opens the root once: its journal folded through the engine with no
    /// effect run, the link and the children made, the events at open
    /// applied, and the first presentation made; then the effects those
    /// events asked for start.
    private func open() {
        guard !opened else { return }
        opened = true
        loop.root.open()
        loop.presentFirst()
    }

    private func perform(_ body: @Sendable () -> Void) {
        open()
        body()
        loop.settle()
    }

    // MARK: Reading

    /// The root's presentation tree as last published.
    public var presentation: Root.Presentation {
        open()
        // `presentFirst` ran in `open`, and a host presents at least once.
        return loop.published ?? loop.root.presentation!
    }

    public var state: Root.State {
        open()
        return loop.root.state
    }

    public var counters: RuntimeCounters { loop.counters }

    /// The state of the child kept under `key`, nil when none is.
    public func state(of key: ChildKey) -> Root.Child.State? {
        open()
        return loop.root.child(key)?.state
    }

    /// Every presentation the root publishes from now on, the current one
    /// first. A slow reader gets the newest and misses the ones between:
    /// a presentation is a snapshot.
    public func presentations() -> AsyncStream<Root.Presentation> {
        open()
        return loop.subscribe()
    }

    /// The presentations as JSON, every keyed list in them whole. One that
    /// does not encode arrives as its failure, and the next one still comes.
    /// Written by `ValueCoding`, as everything that crosses to Android is.
    public func encodedPresentations() -> AsyncStream<Result<Data, PresentationNotEncoded>> {
        open()
        let presentations = loop.subscribe()
        let (stream, continuation) = AsyncStream<Result<Data, PresentationNotEncoded>>.makeStream(bufferingPolicy: .bufferingNewest(1))
        let task = Task {
            for await presentation in presentations {
                do {
                    continuation.yield(.success(try ValueCoding.encode(presentation).data(pretty: false)))
                } catch {
                    continuation.yield(.failure(PresentationNotEncoded(reason: String(describing: error))))
                }
            }
            continuation.finish()
        }
        continuation.onTermination = { _ in task.cancel() }
        return stream
    }

    /// The presentations as the Android bridge sends them (§5.5): each tree
    /// as JSON with every keyed list in it written as a `KeyedListReference`,
    /// beside the lists whose rows moved since the frame before, which go
    /// first and as deltas. Unlike a presentation, a frame is never skipped:
    /// a list's deltas apply in order, so the stream buffers every frame,
    /// each encoded as the runtime publishes it.
    public func encodedFrames() -> AsyncStream<PresentationFrame> {
        open()
        let presentations = loop.subscribe()
        let (stream, continuation) = AsyncStream<PresentationFrame>.makeStream(bufferingPolicy: .unbounded)
        let task = Task {
            let crossing = KeyedListCrossing()
            for await presentation in presentations {
                do {
                    let (json, moved) = try crossing.encode(presentation)
                    continuation.yield(PresentationFrame(presentation: .success(json), lists: moved))
                } catch {
                    continuation.yield(PresentationFrame(presentation: .failure(PresentationNotEncoded(reason: String(describing: error))),
                                                         lists: []))
                }
            }
            continuation.finish()
        }
        continuation.onTermination = { _ in task.cancel() }
        return stream
    }

    // MARK: Writing

    public func send(_ command: Root.Command) {
        open()
        loop.root.send(command)
        loop.settle()
    }

    /// A command for the controller at `path`, the keys from the root down
    /// (none for the root itself). Throws when no controller is kept there,
    /// or the command is not of its `Command` type.
    public func send<Command: Sendable>(_ command: Command, to path: [ChildKey]) throws {
        open()
        defer { loop.settle() }
        try loop.root.route(path[...], command: .value(command))
    }

    /// A `RoutedCommand` as JSON, from the Android bridge. Read with
    /// `JSONValue`'s own reader and decoded by `ValueCoding`, never
    /// Foundation's coders, so a `JSONValue` inside a command (a record's
    /// properties) keeps every digit of its numbers.
    public func send(encoded data: Data) throws {
        open()
        defer { loop.settle() }
        let routed = try ValueCoding.decode(RoutedCommand.self, from: JSONValue(data: data))
        try loop.root.route(routed.path[...], command: .encoded(routed.command))
    }

    /// An event from outside the loop, applied as an effect's result is: a
    /// platform callback, or a test answering an effect it held.
    public func deliver(_ event: Root.Event) {
        open()
        loop.root.step([event])
        loop.settle()
    }

    /// A link's snapshot from outside the tree, delivered to the root and
    /// down to every child as `event(forLink:)`, and to each child made
    /// later: for a root that runs without a `LinkController` above it (a
    /// test, a preview, an extension). Under a link, the link's own state
    /// makes the snapshot and this is not called.
    public func linkChanged(_ snapshot: LinkSnapshot) {
        open()
        loop.link = snapshot
        loop.root.linkChanged(snapshot)
        loop.settle()
    }

    /// The device's locale or time zone changed: every presenter, enrichment
    /// and effect reads the new one from here on, and the whole tree is
    /// presented again in it, inside this step.
    public func environmentChanged(locale: Locale, timeZone: TimeZone) {
        open()
        guard locale != loop.environment.locale || timeZone != loop.environment.timeZone else { return }
        loop.environment.locale = locale
        loop.environment.timeZone = timeZone
        loop.root.environmentChanged()
        loop.settle()
    }

    /// The root back to its initial state with no journal; its controller
    /// generation moves, so every result still on its way is dropped.
    public func reset() {
        open()
        loop.root.reset()
        loop.settle()
    }

    /// Cancels every effect, drops every result still on its way, and ends
    /// the presentation streams. Idempotent.
    public func close() {
        opened = true
        loop.close()
    }
}

/// One presentation as it crosses to Android: the tree with its keyed
/// lists written as references, and the lists whose rows moved, which the
/// bridge sends before the tree so Kotlin holds every row the tree names.
public struct PresentationFrame: Sendable {
    public var presentation: Result<Data, PresentationNotEncoded>
    public var lists: [CarriedList]

    public init(presentation: Result<Data, PresentationNotEncoded>, lists: [CarriedList]) {
        self.presentation = presentation
        self.lists = lists
    }
}

/// A presentation that did not encode as JSON: a bug in its `Encodable`
/// conformance, which the bridge reports rather than draw nothing.
public struct PresentationNotEncoded: Error, Equatable, Sendable {
    public var reason: String

    public init(reason: String) {
        self.reason = reason
    }
}

/// The runtime as its effects' tasks reach it, without keeping it alive.
private final class RuntimeBox<Root: Controller>: @unchecked Sendable {
    weak var runtime: Runtime<Root>?
}
