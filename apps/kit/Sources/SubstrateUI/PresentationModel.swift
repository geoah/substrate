#if canImport(SwiftUI)
import Observation
import SwiftUI
import SubstrateRuntime

/// A root's presentations republished on the main actor for SwiftUI, in
/// parts: a view reads the part it draws by the path its controller says it
/// goes out under (`part(at:)`, from `Controller.parts(of:)`), and is drawn
/// again only when that part changes, so a list is not drawn again for what
/// moved in a child beside it. `presentation` is the whole tree, for a view
/// that draws all of it and for code that reads it outside a view; it is
/// set at every publish, so a view reading it is drawn again at each.
///
/// The runtime itself runs on its own executor, where it opens (a journal
/// folded) at the first command or read, never on the main actor, and where
/// each publish is split into its parts and compared with the one before:
/// the main actor only takes the parts that moved. Parts of one publish are
/// set together, so a view never sees one half of a step. Commands reach
/// the runtime in the order they were sent, so a field's edits are never
/// applied out of order. One per root, made where the root is, once: a view
/// keeps it in its state, never makes it in an initialiser SwiftUI calls
/// again.
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
    /// One observable value per path a part goes out under, made when the
    /// part is first published or first read, whichever comes first, and
    /// kept from then on, empty while the path holds no part.
    @ObservationIgnored private var slots: [[ChildKey]: PartSlot] = [:]
    /// The runtime has said how the tree stands: until it has, the parts
    /// are the first presentation's.
    @ObservationIgnored private var followed = false

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
        for part in runtime.controller.parts(of: first) { slot(part.path).value = part.value }
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
            for await update in await runtime.parts() {
                guard let self else { return }
                take(update)
            }
        }
    }

    /// A model over a new runtime for `root`, drawing the root's initial
    /// state until the runtime publishes (at once, once it has folded its
    /// journal on its own executor).
    public convenience init(_ root: Root, environment: AppEnvironment, options: RuntimeOptions = RuntimeOptions()) {
        let first = root.present(
            PresenterInput(
                state: root.initialState, transient: root.initialTransient, now: environment.clock.now,
                locale: environment.locale, timeZone: environment.timeZone))
        self.init(Runtime(root, environment: environment, options: options), first: first)
        ownsRuntime = true
    }

    /// The part published under `path`, the keys from the root down, nil
    /// while none is: a conversation's screen under its key, an app's own
    /// under `[LinkController.appKey]`. Read in a view's body, it draws the
    /// view again when this part changes, appears or goes, and for nothing
    /// else.
    public func part<Value>(at path: [ChildKey], as type: Value.Type = Value.self) -> Value? {
        slot(path).value as? Value
    }

    private func slot(_ path: [ChildKey]) -> PartSlot {
        if let slot = slots[path] { return slot }
        let slot = PartSlot()
        slots[path] = slot
        return slot
    }

    /// One update's parts, set in one turn of the main actor.
    private func take(_ update: PartsUpdate<Root.Presentation>) {
        let span = Meter.shared.begin(.republish)
        defer { Meter.shared.end(span) }
        var gone = update.removed
        if !followed {
            // The first update is every part the tree has: what the first
            // presentation named beyond them is gone.
            followed = true
            let held = Set(update.changed.map(\.path))
            gone += slots.keys.filter { !held.contains($0) }
        }
        for path in gone {
            // Whoever read it is drawn again and finds none. The slot
            // stays: a view that read the path before anything was
            // published there, or after it went, is told when a part is.
            if let slot = slots[path], slot.value != nil { slot.value = nil }
        }
        for part in update.changed { slot(part.path).value = part.value }
        presentation = update.presentation
        for handler in signalHandlers { handler(update.presentation) }
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

/// One part as SwiftUI observes it.
@MainActor
@Observable
private final class PartSlot {
    var value: (any Sendable)?
}

private extension Signal {
    var optional: Signal? { self }
}
#endif
