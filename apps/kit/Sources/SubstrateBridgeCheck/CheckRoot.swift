import Foundation
import SubstrateBridge
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// A command to the check root, which the bridge's runtime checks and the
/// gallery-free smoke run (`bridge-check`) send to exercise each path.
public enum CheckCommand: Bridged, Equatable {
    /// Presents `text` back: the round trip.
    case echo(text: String)
    /// Presents `count` times as fast as it can, each with `padding` bytes:
    /// coalescing, and a large presentation.
    case burst(count: Int, padding: Int)
    /// Publishes the keyed list `rows` with `count` rows.
    case rows(count: Int)
    case changeRow(id: String, text: String)
    case removeRow(id: String)
    /// A GET through `via`, timed.
    case fetch(url: String, via: CheckTransport)
    /// A line stream read with `readDelayMillis` between lines, from a queue
    /// of `capacity`.
    case lines(url: String, readDelayMillis: Int, capacity: Int)
    /// Writes `value` under `account` when there is one, then reads it.
    case secure(account: String, value: String?)
    case secureDelete(account: String)
    case txt(name: String)
    case connectivity
    /// A device call Kotlin does not answer until the handle closes.
    case hang
    /// Whether `@MainActor` work runs on Android's main thread.
    case mainActor
    /// Reads this target's resource through the folder Kotlin passed in.
    case resource
    /// The bridge's counters.
    case stats
}

public enum CheckTransport: String, Bridged, Equatable {
    case bridge
    case urlSession
}

/// What the check root shows: how many commands it has handled, and the
/// result of the last.
public struct CheckPresentation: Bridged, Equatable {
    public var step: Int
    public var result: CheckResult?
    public var padding: String?
}

public enum CheckResult: Bridged, Equatable {
    case ok(name: String, detail: String, millis: Double)
    case failed(name: String, detail: String)
}

/// One row of the check root's keyed list.
public struct CheckRow: Bridged, Equatable, Identifiable {
    public var id: String
    public var text: String
}

/// The root behind `bridge-check` and the bridge's contract tests.
public actor CheckRoot: BridgeRoot {
    private let context: BridgeContext
    private var step = 0
    private var rows = KeyedList<CheckRow>()

    public init(context: BridgeContext) {
        self.context = context
    }

    public func receive(_ command: JSONValue) async throws {
        let command = try BridgeCodec.decode(CheckCommand.self, from: command)
        step += 1
        let started = ContinuousClock.now
        let result: CheckResult
        do {
            result = try await run(command, started: started)
        } catch let failure as BridgeFailure {
            result = .failed(name: Self.name(of: command), detail: "\(failure.code.rawValue): \(failure.message)")
        } catch {
            result = .failed(name: Self.name(of: command), detail: "\(type(of: error)): \(error.localizedDescription)")
        }
        if case .burst = command { return }
        context.present(CheckPresentation(step: step, result: result))
    }

    public func close() async {}

    private func run(_ command: CheckCommand, started: ContinuousClock.Instant) async throws -> CheckResult {
        let name = Self.name(of: command)
        func ok(_ detail: String) -> CheckResult {
            .ok(name: name, detail: detail, millis: Self.millis(ContinuousClock.now - started))
        }
        switch command {
        case let .echo(text):
            return ok(text)
        case let .burst(count, padding):
            let pad = String(repeating: "x", count: padding)
            for index in 1 ... max(count, 1) {
                step += 1
                context.present(
                    CheckPresentation(
                        step: step, result: .ok(name: name, detail: "\(index)/\(count)", millis: 0),
                        padding: pad))
            }
            return ok("\(count)")
        case let .rows(count):
            rows.update((0 ..< count).map { CheckRow(id: "row-\($0)", text: "Row \($0)") })
            context.publish(list: "rows", rows)
            return ok("\(count) rows")
        case let .changeRow(id, text):
            guard rows[id] != nil else { return .failed(name: name, detail: "no row \(id)") }
            rows.update(rows.rows.map { $0.id == id ? CheckRow(id: id, text: text) : $0 })
            context.publish(list: "rows", rows)
            return ok(id)
        case let .removeRow(id):
            rows.update(rows.rows.filter { $0.id != id })
            context.publish(list: "rows", rows)
            return ok(id)
        case let .fetch(url, via):
            guard let url = URL(string: url) else { return .failed(name: name, detail: "not a URL") }
            var request = URLRequest(url: url)
            request.timeoutInterval = 20
            let transport: any HTTPTransport =
                switch via {
                case .bridge: BridgeTransport(channel: context.device, folder: context.environment.spill)
                case .urlSession: URLSessionTransport.shared
                }
            let (data, response) = try await transport.data(for: request)
            return ok("\(response.statusCode) \(data.count) bytes")
        case let .lines(url, delay, capacity):
            guard let url = URL(string: url) else { return .failed(name: name, detail: "not a URL") }
            let transport = BridgeTransport(channel: context.device, folder: context.environment.spill)
            let (head, lines) = try await transport.lines(for: URLRequest(url: url), capacity: capacity)
            var count = 0
            for try await _ in lines {
                count += 1
                if delay > 0 { try await Task.sleep(for: .milliseconds(delay)) }
            }
            return ok("\(head.statusCode) \(count) lines")
        case let .secure(account, value):
            let store = BridgeSecureStore(channel: context.device, store: "bridge-check")
            if let value { try store.write(Data(value.utf8), account: account) }
            switch store.read(account: account) {
            case let .found(data): return ok("found \(String(decoding: data, as: UTF8.self))")
            case .missing: return ok("missing")
            case let .failed(status): return .failed(name: name, detail: "failed \(status)")
            }
        case let .secureDelete(account):
            BridgeSecureStore(channel: context.device, store: "bridge-check").delete(account: account)
            return ok("deleted")
        case let .txt(host):
            switch try await BridgeTXTResolver(channel: context.device).txt(host) {
            case let .records(records): return ok("records \(records.joined(separator: " | "))")
            case .confirmedAbsent: return ok("confirmedAbsent")
            case let .unconfirmedAbsent(words): return ok("unconfirmedAbsent \(words)")
            case let .failed(words): return ok("failed \(words)")
            }
        case .connectivity:
            let path = await BridgeConnectivity(channel: context.device).current()
            return ok("online \(path.online) expensive \(path.expensive)")
        case .hang:
            for try await _ in context.device.chunks(.connectivityWatch, capacity: 1_000_000) {}
            return ok("ended")
        case .mainActor:
            let onMain = await MainActor.run { Thread.isMainThread }
            return onMain ? ok("main thread") : .failed(name: name, detail: "the main actor ran off the main thread")
        case .resource:
            guard let bundle = context.environment.bundle(package: "SubstrateKit", target: "SubstrateBridgeCheck"),
                let url = bundle.url(forResource: "check", withExtension: "txt")
            else { return .failed(name: name, detail: "no resource bundle under \(context.environment.resources.path)") }
            return ok(try String(contentsOf: url, encoding: .utf8).trimmingCharacters(in: .whitespacesAndNewlines))
        case .stats:
            return ok("\(BridgeRuntime.shared.stats)")
        }
    }

    static func name(of command: CheckCommand) -> String {
        String(describing: command).prefix { $0.isLetter }.description
    }

    static func millis(_ duration: Duration) -> Double {
        Double(duration.components.seconds) * 1000 + Double(duration.components.attoseconds) / 1e15
    }
}

/// The root behind `idle`: it takes commands and does nothing with them.
struct IdleRoot: BridgeRoot {
    func receive(_: JSONValue) async throws {}
    func close() async {}
}

/// The roots this library offers the bridge.
@_cdecl("substrate_bridge_roots")
public func substrate_bridge_roots(_ registry: UnsafeMutableRawPointer) {
    CheckRoot.register(in: RootRegistry.from(registry))
}

public extension CheckRoot {
    static func register(in registry: RootRegistry) {
        registry.register("check") { _, context in CheckRoot(context: context) }
        // A screen with nothing to do: it presents once, as it opens, and
        // never again, so a first presentation lost before Kotlin knew the
        // handle would leave it blank for good.
        registry.register("idle") { _, context in
            context.present(CheckPresentation(step: 1, result: .ok(name: "idle", detail: "presented at open", millis: 0)))
            return IdleRoot()
        }
        registry.register("counter") { _, context in (Counter(), context.appEnvironment()) }
        registry.register("ledger") { _, context in (Ledger(), context.appEnvironment()) }
        registry.register("gallery") { _, context in (GalleryRoot(), context.appEnvironment()) }
    }
}
