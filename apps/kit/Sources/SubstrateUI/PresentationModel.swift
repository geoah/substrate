#if canImport(SwiftUI)
import Observation
import SwiftUI
import SubstrateRuntime

/// A root's presentations republished on the main actor for SwiftUI: a
/// view reads `presentation` and sends commands, and the runtime itself
/// runs on its own executor. One per root, made where the root is.
@MainActor
@Observable
public final class PresentationModel<Root: Controller> {
    public private(set) var presentation: Root.Presentation
    @ObservationIgnored public let runtime: Runtime<Root>
    @ObservationIgnored private var following: Task<Void, Never>?

    /// - Parameter first: what to draw until the runtime's first
    ///   presentation arrives, which is at once.
    public init(_ runtime: Runtime<Root>, first: Root.Presentation) {
        self.runtime = runtime
        presentation = first
        following = Task { [weak self, runtime] in
            for await next in await runtime.presentations() {
                guard let self else { return }
                if next != presentation { presentation = next }
            }
        }
    }

    /// A model over a runtime made from `root`, drawing its first
    /// presentation at once.
    public convenience init(_ root: Root, environment: AppEnvironment, options: RuntimeOptions = RuntimeOptions()) async {
        let runtime = Runtime(root, environment: environment, options: options)
        await self.init(runtime, first: runtime.presentation)
    }

    public func send(_ command: Root.Command) {
        let runtime = runtime
        Task { await runtime.send(command) }
    }

    public func send(_ command: Root.Child.Command, to key: ChildKey) {
        let runtime = runtime
        Task { await runtime.send(command, to: key) }
    }

    isolated deinit {
        following?.cancel()
    }
}
#endif
