import Foundation
@testable import SubstrateBridge
import SubstrateBridgeCheck
import SubstrateCore
import SubstrateRuntime

/// Kotlin's side of the bridge in a test: every message Swift delivers,
/// kept in order, and a way to wait for one.
final class Outlet: BridgeOutlet, @unchecked Sendable {
    private let lock = NSLock()
    private var kept: [Envelope] = []
    /// Held, `deliver` blocks: a slow Kotlin.
    let gate = DispatchSemaphore(value: 1)
    var holding = false
    private var refusing: @Sendable (Envelope) -> Bool = { _ in false }

    func deliver(_ message: String) -> BridgeFailure? {
        if holding {
            gate.wait()
            gate.signal()
        }
        guard let envelope = try? BridgeCodec.decodeEnvelope(message) else { return BridgeFailure(.malformed, message) }
        if lock.withLock({ refusing })(envelope) { return BridgeFailure(.jniFailure, "the test refused \(envelope.op.rawValue)") }
        lock.withLock { kept.append(envelope) }
        return nil
    }

    /// A Kotlin whose `deliver` fails, as a JNI failure would, for each message `matches`.
    func refuse(_ matches: @escaping @Sendable (Envelope) -> Bool) {
        lock.withLock { refusing = matches }
    }

    var messages: [Envelope] { lock.withLock { kept } }

    func hold() {
        holding = true
        gate.wait()
    }

    func release() {
        gate.signal()
    }

    /// The first message after `index` that `matches`, waiting up to two seconds.
    func next(after index: Int = 0, _ matches: @escaping (Envelope) -> Bool) async throws -> Envelope {
        for _ in 0 ..< 400 {
            if let found = messages.dropFirst(index).first(where: matches) { return found }
            try await Task.sleep(for: .milliseconds(5))
        }
        throw BridgeFailure(.swiftFailure, "no such message among \(messages.map(\.op))")
    }
}

/// A root a test drives with commands: `{"present": n}` presents n,
/// `{"call": "dns"}` makes a device call and presents its answer or failure,
/// `{"stream": capacity, "read": n}` reads a stream, `{"rows": [ids]}`
/// publishes a keyed list.
actor TestRoot: BridgeRoot {
    let context: BridgeContext
    private(set) var commands: [JSONValue] = []
    private(set) var closed = false
    private(set) var environments: [BridgeEnvironment] = []
    private var rows = KeyedList<Row>()

    init(context: BridgeContext) {
        self.context = context
    }

    func receive(_ command: JSONValue) async throws {
        commands.append(command)
        if let count = command["present"]?.int {
            for step in 1 ... count { context.present(["step": JSONValue(integerLiteral: step)] as JSONValue) }
        }
        if command["call"] != nil {
            do {
                let answer = try await context.device.call(.dnsTXT(name: "example.com", timeoutMillis: 100), as: DNSAnswer.self)
                context.present(["answer": .string("\(answer)")] as JSONValue)
            } catch let failure as BridgeFailure {
                context.present(["failure": .string(failure.code.rawValue)] as JSONValue)
            }
        }
        if let capacity = command["stream"]?.int {
            var values: [JSONValue] = []
            do {
                for try await value in context.device.stream(.connectivityWatch, capacity: capacity) {
                    values.append(value)
                    if let pause = command["pause"]?.int { try await Task.sleep(for: .milliseconds(pause)) }
                }
                context.present(["streamed": .array(values)] as JSONValue)
            } catch let failure as BridgeFailure {
                context.present(["streamFailure": .string(failure.code.rawValue), "streamed": .array(values)] as JSONValue)
            }
        }
        if let ids = command["rows"]?.array {
            rows.update(ids.compactMap(\.string).map { Row(id: $0, text: command["text"]?.string ?? $0) })
            context.publish(list: "rows", rows)
        }
        if command["throw"] != nil { throw BridgeFailure(.swiftFailure, "asked to") }
    }

    func close() async {
        closed = true
    }

    func environmentChanged(_ environment: BridgeEnvironment) async {
        environments.append(environment)
    }

    struct Row: Codable, Equatable, Sendable, Identifiable {
        var id: String
        var text: String
    }
}

/// A started runtime with `TestRoot` registered as "test", and its outlet.
func started(fingerprint: String = "test") -> (BridgeRuntime, Outlet, Box<TestRoot>) {
    let runtime = BridgeRuntime()
    let outlet = Outlet()
    let box = Box<TestRoot>()
    CheckRoot.register(in: runtime.roots)
    runtime.roots.register("test") { _, context in
        let root = TestRoot(context: context)
        box.value = root
        return root
    }
    let answer = runtime.start(config(fingerprint: fingerprint), outlet: outlet, fingerprint: "test")
    precondition(answer == .started(fingerprint: "test"))
    return (runtime, outlet, box)
}

func config(fingerprint: String) -> StartConfig {
    StartConfig(fingerprint: fingerprint, locale: "en-GB", timeZone: "Europe/London", resources: "/tmp/resources", data: "/tmp/data",
                cache: "/tmp/cache", sdk: 36, debug: false)
}

final class Box<T>: @unchecked Sendable {
    private let lock = NSLock()
    private var held: T?
    var value: T? {
        get { lock.withLock { held } }
        set { lock.withLock { held = newValue } }
    }
}

func post(_ runtime: BridgeRuntime, _ envelope: Envelope) throws -> Envelope {
    try BridgeCodec.decodeEnvelope(runtime.post(BridgeCodec.text(envelope)))
}

func open(_ runtime: BridgeRuntime) throws -> Handle {
    let reply = try post(runtime, Envelope(op: .open, body: ["root": "test"]))
    guard reply.op == .opened, let handle = reply.handle else { throw BridgeFailure(.swiftFailure, "not opened: \(reply)") }
    return handle
}
