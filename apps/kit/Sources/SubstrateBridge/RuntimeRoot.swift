import Foundation
import SubstrateCore
import SubstrateRuntime

/// Any `Runtime` as a bridge root, with no glue per controller: commands are
/// `RoutedCommand`s as JSON (`Runtime.send(encoded:)`), and each presentation
/// the runtime publishes goes out as it encoded it, coalesced on the way.
public final class RuntimeRoot<Root: Controller>: BridgeRoot {
    public let runtime: Runtime<Root>
    private let forwarding: Task<Void, Never>

    public init(_ runtime: Runtime<Root>, context: BridgeContext) async {
        self.runtime = runtime
        let presentations = await runtime.encodedPresentations()
        forwarding = Task {
            for await json in presentations {
                context.present(encoded: json)
            }
        }
    }

    public func receive(_ command: JSONValue) async throws {
        try await runtime.send(encoded: JSONEncoder().encode(command))
    }

    public func close() async {
        await runtime.close()
        forwarding.cancel()
    }
}

public extension RootRegistry {
    /// Offers a controller tree as the root `name`: `make` builds its root
    /// controller and environment from `open`'s settings and the handle's
    /// context (`BridgeContext.devices` holds Android's device services).
    func register<Root: Controller>(_ name: String, _ make: @escaping @Sendable (JSONValue?, BridgeContext) throws -> (Root, AppEnvironment)) {
        register(name) { settings, context in
            let (root, environment) = try make(settings, context)
            return PendingRuntimeRoot { await RuntimeRoot(Runtime(root, environment: environment), context: context) }
        }
    }
}

/// A runtime root made on Swift's executors, since a factory runs inside
/// `post` and reading a runtime's presentations suspends; commands wait for it.
private final class PendingRuntimeRoot<Root: Controller>: BridgeRoot {
    private let started: Task<RuntimeRoot<Root>, Never>

    init(_ make: @escaping @Sendable () async -> RuntimeRoot<Root>) {
        started = Task { await make() }
    }

    func receive(_ command: JSONValue) async throws {
        try await started.value.receive(command)
    }

    func close() async {
        await started.value.close()
    }
}

/// `RoutedCommand`'s shape on the wire, for the generated Kotlin: the child
/// keys from the root down, and the command as that controller encodes it.
/// `BridgeTests` holds the two to one encoding.
public struct RouteCommand: Bridged, Equatable {
    public var path: [String]
    public var command: JSONValue

    public init(path: [String] = [], command: JSONValue) {
        self.path = path
        self.command = command
    }
}
