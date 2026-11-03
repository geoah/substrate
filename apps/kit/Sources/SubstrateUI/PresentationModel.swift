#if canImport(SwiftUI)
import Observation
import SwiftUI
import SubstrateRuntime

/// A root's presentations republished on the main actor for SwiftUI: a
/// view reads `presentation` and sends commands, and the runtime itself
/// runs on its own executor. Commands reach it in the order they were sent,
/// so a field's edits are never applied out of order. One per root, made
/// where the root is.
@MainActor
@Observable
public final class PresentationModel<Root: Controller> {
    public private(set) var presentation: Root.Presentation
    @ObservationIgnored public let runtime: Runtime<Root>
    @ObservationIgnored private var following: Task<Void, Never>?
    @ObservationIgnored private var sending: Task<Void, Never>?
    @ObservationIgnored private let commands: AsyncStream<Command>.Continuation
    /// The model made the runtime, so it closes it when it goes.
    @ObservationIgnored private var ownsRuntime = false

    private enum Command: Sendable {
        case root(Root.Command)
        case child(Root.Child.Command, ChildKey)
    }

    /// - Parameter first: what to draw until the runtime's first
    ///   presentation arrives, which is at once.
    public init(_ runtime: Runtime<Root>, first: Root.Presentation) {
        self.runtime = runtime
        presentation = first
        let (stream, commands) = AsyncStream<Command>.makeStream()
        self.commands = commands
        sending = Task {
            for await command in stream {
                switch command {
                case let .root(command): await runtime.send(command)
                case let .child(command, key): await runtime.send(command, to: key)
                }
            }
        }
        following = Task { [weak self, runtime] in
            for await next in await runtime.presentations() {
                guard let self else { return }
                if next != presentation { presentation = next }
            }
        }
    }

    /// A model over a new runtime for `root`, drawing the root's initial
    /// state until the runtime publishes (at once, from its journal where
    /// it keeps one).
    public convenience init(_ root: Root, environment: AppEnvironment, options: RuntimeOptions = RuntimeOptions()) {
        let first = root.present(PresenterInput(state: root.initialState, transient: root.initialTransient, now: environment.clock.now,
                                                locale: environment.locale, timeZone: environment.timeZone))
        self.init(Runtime(root, environment: environment, options: options), first: first)
        ownsRuntime = true
    }

    public func send(_ command: Root.Command) {
        commands.yield(.root(command))
    }

    public func send(_ command: Root.Child.Command, to key: ChildKey) {
        commands.yield(.child(command, key))
    }

    isolated deinit {
        commands.finish()
        following?.cancel()
        sending?.cancel()
        guard ownsRuntime else { return }
        let runtime = runtime
        Task { await runtime.close() }
    }
}
#endif
