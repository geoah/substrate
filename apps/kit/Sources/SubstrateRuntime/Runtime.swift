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
/// The UI reads `presentations()`; SubstrateUI's `PresentationModel`
/// republishes them on the main actor for SwiftUI, and the Android bridge
/// reads `encodedPresentations()` and sends `send(encoded:)`.
public actor Runtime<Root: Controller> {
    private let loop: Loop<Root>

    /// Opens the root: its journal, if it keeps one, is folded through the
    /// engine with no effect run, and the first presentation made.
    public init(_ root: Root, environment: AppEnvironment, options: RuntimeOptions = RuntimeOptions()) {
        let box = RuntimeBox<Root>()
        let loop = Loop<Root>(environment: environment, options: options) { body in
            await box.runtime?.perform(body)
        }
        let host = Host(controller: root, path: [], loop: loop, journal: root.journal(in: environment))
        loop.root = host
        host.open()
        loop.presentFirst()
        self.loop = loop
        box.runtime = self
    }

    private func perform(_ body: @Sendable () -> Void) {
        body()
        loop.settle()
    }

    // MARK: Reading

    /// The root's presentation tree as last published.
    public var presentation: Root.Presentation {
        // `presentFirst` ran in `init`, and a host presents at least once.
        loop.published ?? loop.root.presentation!
    }

    public var state: Root.State { loop.root.state }

    public var counters: RuntimeCounters { loop.counters }

    /// The state of the child kept under `key`, nil when none is.
    public func state(of key: ChildKey) -> Root.Child.State? { loop.root.child(key)?.state }

    /// Every presentation the root publishes from now on, the current one
    /// first. A slow reader gets the newest and misses the ones between:
    /// a presentation is a snapshot.
    public func presentations() -> AsyncStream<Root.Presentation> {
        loop.subscribe()
    }

    /// The presentations as JSON, for the Android bridge.
    public func encodedPresentations() -> AsyncStream<Data> {
        let presentations = loop.subscribe()
        let (stream, continuation) = AsyncStream<Data>.makeStream(bufferingPolicy: .bufferingNewest(1))
        let task = Task {
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
            for await presentation in presentations {
                if let data = try? encoder.encode(presentation) { continuation.yield(data) }
            }
            continuation.finish()
        }
        continuation.onTermination = { _ in task.cancel() }
        return stream
    }

    // MARK: Writing

    public func send(_ command: Root.Command) {
        loop.root.send(command)
        loop.settle()
    }

    /// A command for the child kept under `key`; nothing when none is.
    public func send(_ command: Root.Child.Command, to key: ChildKey) {
        loop.root.child(key)?.send(command)
        loop.settle()
    }

    /// A `RoutedCommand` as JSON, from the Android bridge.
    public func send(encoded data: Data) throws {
        let routed = try JSONDecoder().decode(RoutedCommand.self, from: data)
        let command = try JSONEncoder().encode(routed.command)
        try loop.root.route(routed.path[...], command: command)
        loop.settle()
    }

    /// An event from outside the loop, applied as an effect's result is: a
    /// platform callback, or a test answering an effect it held.
    public func deliver(_ event: Root.Event) {
        loop.root.step([event])
        loop.settle()
    }

    /// The link's snapshot, delivered to the root and down to every child as
    /// `linkChanged`, and to each child made later.
    public func linkChanged(_ snapshot: LinkSnapshot) {
        loop.link = snapshot
        loop.root.linkChanged(snapshot)
        loop.settle()
    }

    /// The root back to its initial state with no journal; its controller
    /// generation moves, so every result still on its way is dropped.
    public func reset() {
        loop.root.reset()
        loop.settle()
    }

    /// Cancels every effect, drops every result still on its way, and ends
    /// the presentation streams. Idempotent.
    public func close() {
        loop.close()
    }
}

/// The runtime as its effects' tasks reach it, without keeping it alive.
private final class RuntimeBox<Root: Controller>: @unchecked Sendable {
    weak var runtime: Runtime<Root>?
}
