#if canImport(SwiftUI)
import Observation
import SwiftUI
import SubstrateRuntime

/// A root's presentations republished on the main actor for SwiftUI: a
/// view reads `presentation` and sends commands, and the runtime itself
/// runs on its own executor, where it opens (a journal folded) at the first
/// command or read, never on the main actor. Commands reach it in the order
/// they were sent, so a field's edits are never applied out of order. One
/// per root, made where the root is, once: a view keeps it in its state,
/// never makes it in an initialiser SwiftUI calls again.
@MainActor
@Observable
public final class PresentationModel<Root: Controller> {
    public private(set) var presentation: Root.Presentation
    @ObservationIgnored public let runtime: Runtime<Root>
    @ObservationIgnored private var following: Task<Void, Never>?
    @ObservationIgnored private var sending: Task<Void, Never>?
    @ObservationIgnored private let commands: AsyncStream<Command>.Continuation
    @ObservationIgnored private var signalHandlers: [(Root.Presentation) -> Void] = []
    /// The model made the runtime, so it closes it when it goes.
    @ObservationIgnored private var ownsRuntime = false

    private enum Command: Sendable {
        case root(Root.Command)
        case routed(any Sendable, [ChildKey])
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
                // A child closed since the command was sent takes nothing.
                case let .routed(command, path): try? await runtime.send(command, to: path)
                }
            }
        }
        following = Task { [weak self, runtime] in
            for await next in await runtime.presentations() {
                guard let self else { return }
                guard next != presentation else { continue }
                presentation = next
                for handler in signalHandlers { handler(next) }
            }
        }
    }

    /// A model over a new runtime for `root`, drawing the root's initial
    /// state until the runtime publishes (at once, once it has folded its
    /// journal on its own executor).
    public convenience init(_ root: Root, environment: AppEnvironment, options: RuntimeOptions = RuntimeOptions()) {
        let first = root.present(PresenterInput(state: root.initialState, transient: root.initialTransient, now: environment.clock.now,
                                                locale: environment.locale, timeZone: environment.timeZone))
        self.init(Runtime(root, environment: environment, options: options), first: first)
        ownsRuntime = true
    }

    public func send(_ command: Root.Command) {
        commands.yield(.root(command))
    }

    /// A command for the controller at `path`, the keys from the root down:
    /// `[LinkController.appKey]` for an app's root under its link, then the
    /// app's own keys.
    public func send<Command: Codable & Sendable>(_ command: Command, to path: [ChildKey]) {
        commands.yield(.routed(command, path))
    }

    /// Runs `perform` once for each `Signal` raised at `path` from now on,
    /// as the presentation that raised it arrives: a haptic played, a cover
    /// opened, a row scrolled to, never again when the tree is drawn again
    /// for something else. A signal raised before the call is not acted on.
    /// A serial other than the last one seen is a new signal, so one that
    /// starts afresh (its controller made again) acts from its first raise.
    public func onSignal<Value>(_ path: KeyPath<Root.Presentation, Signal<Value>?>, perform: @escaping @MainActor (Value) -> Void) {
        var seen = presentation[keyPath: path]?.serial ?? 0
        signalHandlers.append { presentation in
            let signal = presentation[keyPath: path]
            let serial = signal?.serial ?? 0
            guard serial != seen else { return }
            seen = serial
            if let value = signal?.value { perform(value) }
        }
    }

    /// The same, for a signal every presentation carries.
    public func onSignal<Value>(_ path: KeyPath<Root.Presentation, Signal<Value>>, perform: @escaping @MainActor (Value) -> Void) {
        onSignal(path.appending(path: \Signal<Value>.optional), perform: perform)
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

private extension Signal {
    var optional: Signal? { self }
}
#endif
