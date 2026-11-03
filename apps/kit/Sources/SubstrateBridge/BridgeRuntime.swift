import Foundation
import SubstrateCore

/// The bridge, apart from JNI: what `start` and `post` do and what `deliver`
/// is handed, so its rules run in a test on any host. Android's entry points
/// (`Android/JNI.swift`) drive `shared`; a test makes its own with an outlet
/// that keeps the messages.
public final class BridgeRuntime: @unchecked Sendable {
    /// The process's bridge on Android.
    public static let shared = BridgeRuntime()

    /// The roots the app offers; filled before `start` answers.
    public let roots = RootRegistry()

    private enum Phase {
        case idle
        case started(StartConfig, BridgeEnvironment, any BridgeOutlet)
        case failed(BridgeFailure)
    }

    private struct Open {
        let root: any BridgeRoot
        let context: BridgeContext
        let commands: AsyncStream<JSONValue>.Continuation
        let worker: Task<Void, Never>
    }

    private let lock = NSLock()
    private var phase = Phase.idle
    private var handles = HandleTable<Open>()
    let calls = CallTable()
    public let record = BridgeRecord()
    private var globalOutbox: HandleOutbox?

    public init() {}

    // MARK: start

    /// Starts the bridge once. `prepare` runs after the checks pass and
    /// before anything is kept (binding the main Looper, filling the roots);
    /// its failure is the answer, and the bridge stays unstarted. A second
    /// start answers what the first came to.
    public func start(_ config: StartConfig, outlet: any BridgeOutlet, fingerprint: String = ProtocolFingerprint.value,
                      log: (@Sendable (String) -> Void)? = nil, prepare: () -> BridgeFailure? = { nil }) -> StartAnswer
    {
        lock.lock()
        defer { lock.unlock() }
        switch phase {
        case let .started(started, _, _): return .started(fingerprint: started.fingerprint)
        case let .failed(failure): return .failed(failure: failure)
        case .idle: break
        }
        guard config.fingerprint == fingerprint else {
            let failure = BridgeFailure(.protocolMismatch,
                                        "Kotlin speaks protocol \(config.fingerprint) and Swift \(fingerprint): the app was built from two trees")
            phase = .failed(failure)
            return .failed(failure: failure)
        }
        if let failure = prepare() {
            phase = .failed(failure)
            return .failed(failure: failure)
        }
        if config.debug { record.log = log }
        let environment = BridgeEnvironment(config)
        phase = .started(config, environment, outlet)
        globalOutbox = HandleOutbox(handle: nil, record: record) { [weak self] in self?.deliver($0) }
        return .started(fingerprint: fingerprint)
    }

    public var environment: BridgeEnvironment? {
        lock.withLock {
            if case let .started(_, environment, _) = phase { return environment }
            return nil
        }
    }

    /// Device calls that belong to no handle (a session read at launch).
    public var device: DeviceChannel? {
        lock.withLock {
            guard case .started = phase, let globalOutbox else { return nil }
            return DeviceChannel(handle: nil, calls: calls, outbox: globalOutbox, record: record)
        }
    }

    public var stats: BridgeStats {
        var stats = record.snapshot
        stats.open = lock.withLock { handles.values.count }
        return stats
    }

    // MARK: post

    /// One message from Kotlin. Synchronous only in that it validates and
    /// queues: the answer is `accepted`, `opened` or `error`, and the work
    /// runs on Swift's executors.
    public func post(_ text: String) -> String {
        let envelope: Envelope
        do {
            envelope = try BridgeCodec.decodeEnvelope(text)
        } catch {
            record.record("in", text, service: nil)
            return BridgeCodec.failure(error)
        }
        record.count(\.posted)
        record.record("in", text, service: envelope.id.flatMap(calls.service(of:)))
        guard case .started = lock.withLock({ phase }) else {
            return BridgeCodec.failure(BridgeFailure(.notStarted, "the bridge has not started"), handle: envelope.handle, id: envelope.id)
        }
        do {
            return try route(envelope)
        } catch {
            if error.code == .staleHandle { record.count(\.dropped) }
            if error.code == .unknownCall { record.count(\.refused) }
            return BridgeCodec.failure(error, handle: envelope.handle, id: envelope.id)
        }
    }

    private func route(_ envelope: Envelope) throws(BridgeFailure) -> String {
        switch envelope.op {
        case .open:
            return try open(BridgeCodec.decode(OpenRequest.self, from: envelope.body))
        case .command:
            let open = try lookup(envelope.handle)
            open.commands.yield(envelope.body ?? .null)
        case .close:
            guard let handle = envelope.handle else { throw BridgeFailure(.malformed, "close names no handle") }
            try close(handle)
        case .resync:
            let open = try lookup(envelope.handle)
            open.context.lists.resync(try BridgeCodec.decode(ResyncRequest.self, from: envelope.body).list)
        case .reply:
            if let failure = calls.reply(try id(envelope), envelope.body) { throw failure }
        case .chunk:
            let id = try id(envelope)
            switch calls.chunk(id, seq: envelope.seq, envelope.body) {
            case .taken: break
            case .overflowed:
                record.count(\.overflowed)
                deliver(BridgeCodec.encode(op: .cancel, handle: envelope.handle, id: id))
            case let .refused(failure): throw failure
            }
        case .end:
            if let failure = calls.finish(try id(envelope), failure: nil) { throw failure }
        case .error:
            let failure = (try? BridgeCodec.decode(BridgeFailure.self, from: envelope.body))
                ?? BridgeFailure(.kotlinException, "Kotlin failed and said nothing readable")
            if let refused = calls.finish(try id(envelope), failure: failure) { throw refused }
        case .cancel:
            if calls.cancel(try id(envelope), "Kotlin cancelled it") { record.count(\.cancelled) }
        case .request:
            try answer(id: id(envelope), BridgeCodec.decode(BridgeQuery.self, from: envelope.body))
        case .opened, .presentation, .list, .closed, .accepted:
            throw BridgeFailure(.malformed, "\(envelope.op.rawValue) is Swift's to send, not Kotlin's")
        }
        return BridgeCodec.encode(op: .accepted, handle: envelope.handle, id: envelope.id)
    }

    private func id(_ envelope: Envelope) throws(BridgeFailure) -> Int {
        guard let id = envelope.id else { throw BridgeFailure(.malformed, "\(envelope.op.rawValue) names no id") }
        return id
    }

    private func lookup(_ handle: Handle?) throws(BridgeFailure) -> Open {
        guard let handle else { throw BridgeFailure(.malformed, "the message names no handle") }
        guard let open = lock.withLock({ handles.lookup(handle) }) else {
            throw BridgeFailure(.staleHandle, "handle \(handle.id) generation \(handle.generation) is not open")
        }
        return open
    }

    private func open(_ request: OpenRequest) throws(BridgeFailure) -> String {
        guard let factory = roots.factory(request.root) else {
            throw BridgeFailure(.unknownRoot, "no root named \(request.root); the app offers \(roots.names.joined(separator: ", "))")
        }
        lock.lock()
        guard case let .started(_, environment, _) = phase else {
            lock.unlock()
            throw BridgeFailure(.notStarted, "the bridge has not started")
        }
        let handle = handles.reserve()
        lock.unlock()

        // Made outside the lock: a factory may call back into the bridge.
        let outbox = HandleOutbox(handle: handle, record: record) { [weak self] in self?.deliver($0) }
        let device = DeviceChannel(handle: handle, calls: calls, outbox: outbox, record: record)
        let context = BridgeContext(handle: handle, environment: environment, device: device, outbox: outbox)
        let root: any BridgeRoot
        do {
            root = try factory(request.settings, context)
        } catch {
            lock.withLock { handles.release(handle) }
            throw BridgeFailure(.swiftFailure, "root \(request.root) did not open: \(error.localizedDescription)")
        }
        let (commands, continuation) = AsyncStream<JSONValue>.makeStream()
        let worker = Task {
            for await command in commands {
                do {
                    try await root.receive(command)
                } catch {
                    outbox.enqueue(BridgeCodec.failure(BridgeFailure(.swiftFailure, "the command failed: \(error.localizedDescription)"),
                                                       handle: handle))
                }
            }
        }
        lock.withLock { handles.fill(handle, with: Open(root: root, context: context, commands: continuation, worker: worker)) }
        return BridgeCodec.encode(op: .opened, handle: handle)
    }

    /// Idempotent: a handle already closed, or of an old generation that was
    /// opened, is accepted again. Every call still outstanding is answered
    /// `cancelled` before the root hears of the close, and `closed` is the
    /// handle's last message.
    private func close(_ handle: Handle) throws(BridgeFailure) {
        lock.lock()
        let open = handles.remove(handle)
        let wasOpened = handles.wasOpened(handle)
        lock.unlock()
        guard let open else {
            if wasOpened { return }
            throw BridgeFailure(.staleHandle, "handle \(handle.id) generation \(handle.generation) was never opened")
        }
        open.context.outbox.beginClose()
        open.commands.finish()
        open.worker.cancel()
        for id in calls.cancelAll(handle: handle, "the handle closed") {
            record.count(\.cancelled)
            open.context.outbox.enqueue(BridgeCodec.encode(op: .cancel, handle: handle, id: id), forced: true)
        }
        let root = open.root
        let outbox = open.context.outbox
        Task {
            await root.close()
            outbox.close(with: BridgeCodec.encode(op: .closed, handle: handle))
        }
    }

    private func answer(id: Int, _ query: BridgeQuery) throws(BridgeFailure) {
        switch query {
        case .stats:
            do {
                deliver(try BridgeCodec.encode(op: .reply, id: id, body: stats))
            } catch {
                throw BridgeFailure(.swiftFailure, "the stats did not encode")
            }
        }
    }

    // MARK: deliver

    func deliver(_ message: String) {
        let outlet: (any BridgeOutlet)? = lock.withLock {
            if case let .started(_, _, outlet) = phase { return outlet }
            return nil
        }
        guard let outlet else { return }
        let envelope = try? BridgeCodec.decodeEnvelope(message)
        record.record("out", message, service: envelope?.id.flatMap(calls.service(of:)))
        if let failure = outlet.deliver(message) {
            record.count(\.deliveryFailures)
            record.log?("deliver failed: \(failure.code.rawValue): \(failure.message)")
        } else {
            record.count(\.delivered)
        }
    }
}
