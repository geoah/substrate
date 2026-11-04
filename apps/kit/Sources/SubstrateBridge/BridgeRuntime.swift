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
        case started(StartConfig, any BridgeOutlet)
        case failed(BridgeFailure)
    }

    /// What a root hears, in the order Kotlin posted it.
    private enum Input {
        case command(JSONValue)
        case environment(BridgeEnvironment)
    }

    private struct Open {
        let root: any BridgeRoot
        let context: BridgeContext
        let inputs: AsyncStream<Input>.Continuation
        let worker: Task<Void, Never>
    }

    private let lock = NSLock()
    private var phase = Phase.idle
    /// The device's settings now: the start's, then each update's.
    private var current: BridgeEnvironment?
    private var handles = HandleTable<Open>()
    let calls: CallTable
    public let record = BridgeRecord()
    private var globalOutbox: HandleOutbox?

    public init() {
        calls = CallTable()
    }

    /// A runtime whose calls are `calls`', for a test that pauses them.
    init(calls: CallTable) {
        self.calls = calls
    }

    // MARK: start

    /// Starts the bridge once. `prepare` runs after the checks pass and
    /// before anything is kept (binding the main Looper, filling the roots);
    /// its failure is the answer, and the bridge stays unstarted. A second
    /// start answers what the first came to.
    public func start(
        _ config: StartConfig, outlet: any BridgeOutlet, fingerprint: String = ProtocolFingerprint.value,
        log: (@Sendable (String) -> Void)? = nil, prepare: () -> BridgeFailure? = { nil }
    ) -> StartAnswer {
        lock.lock()
        defer { lock.unlock() }
        switch phase {
        case let .started(started, _): return .started(fingerprint: started.fingerprint)
        case let .failed(failure): return .failed(failure: failure)
        case .idle: break
        }
        guard config.fingerprint == fingerprint else {
            let failure = BridgeFailure(
                .protocolMismatch,
                "Kotlin speaks protocol \(config.fingerprint) and Swift \(fingerprint): the app was built from two trees")
            phase = .failed(failure)
            return .failed(failure: failure)
        }
        if let failure = prepare() {
            phase = .failed(failure)
            return .failed(failure: failure)
        }
        if config.debug { record.log = log }
        current = BridgeEnvironment(config)
        phase = .started(config, outlet)
        globalOutbox = HandleOutbox(handle: nil, record: record) { [weak self] message in
            guard let self else { return BridgeRuntime.stopped }
            return self.deliver(message)
        }
        return .started(fingerprint: fingerprint)
    }

    public var environment: BridgeEnvironment? {
        lock.withLock { current }
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
            record.record("in", text, call: .unknown)
            return BridgeCodec.failure(error)
        }
        record.count(\.posted)
        // Read before the message acts: a reply recorded after it would find
        // its call gone, and is recorded as one of an unknown call anyway.
        record.record("in", text, call: envelope.id.flatMap(calls.service(of:)).map(Redaction.Call.device) ?? .unknown)
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
            open.inputs.yield(.command(envelope.body ?? .null))
        case .environment:
            update(try BridgeCodec.decode(EnvironmentUpdate.self, from: envelope.body))
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
                _ = deliver(BridgeCodec.encode(op: .cancel, handle: envelope.handle, id: id))
            case let .refused(failure): throw failure
            }
        case .end:
            if let failure = calls.finish(try id(envelope), failure: nil) { throw failure }
        case .error:
            let failure =
                (try? BridgeCodec.decode(BridgeFailure.self, from: envelope.body))
                ?? BridgeFailure(.kotlinException, "Kotlin failed and said nothing readable")
            if let refused = calls.finish(try id(envelope), failure: failure) { throw refused }
        case .cancel:
            if calls.cancel(try id(envelope), "Kotlin cancelled it") != nil { record.count(\.cancelled) }
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
        guard case .started = phase, let environment = current else {
            lock.unlock()
            throw BridgeFailure(.notStarted, "the bridge has not started")
        }
        let handle = handles.reserve()
        lock.unlock()

        // Made outside the lock: a factory may call back into the bridge.
        let outbox = HandleOutbox(handle: handle, record: record) { [weak self] message in
            guard let self else { return BridgeRuntime.stopped }
            return self.deliver(message)
        }
        outbox.onFailure { [weak self] failure in self?.fail(handle, failure) }
        let device = DeviceChannel(handle: handle, calls: calls, outbox: outbox, record: record)
        let context = BridgeContext(handle: handle, environment: environment, device: device, outbox: outbox)
        let root: any BridgeRoot
        do {
            root = try factory(request.settings, context)
        } catch {
            lock.withLock { handles.release(handle) }
            throw BridgeFailure(.swiftFailure, "root \(request.root) did not open: \(error.localizedDescription)")
        }
        let (inputs, continuation) = AsyncStream<Input>.makeStream()
        let worker = Task {
            for await input in inputs {
                switch input {
                case let .command(command):
                    do {
                        try await root.receive(command)
                    } catch {
                        outbox.enqueue(
                            BridgeCodec.failure(
                                BridgeFailure(.swiftFailure, "the command failed: \(error.localizedDescription)"),
                                handle: handle))
                    }
                case let .environment(environment):
                    await root.environmentChanged(environment)
                }
            }
        }
        // An update between taking the environment above and here reaches
        // this root too: it is read again under the lock that fills it.
        let missed: BridgeEnvironment? = lock.withLock {
            handles.fill(handle, with: Open(root: root, context: context, inputs: continuation, worker: worker))
            return current.flatMap { $0 == environment ? nil : $0 }
        }
        if let missed {
            context.update(missed)
            continuation.yield(.environment(missed))
        }
        // A delivery that failed before the handle was filled found nothing to fail.
        if let failure = outbox.deliveryFailure { fail(handle, failure) }
        return BridgeCodec.encode(op: .opened, handle: handle)
    }

    private func update(_ update: EnvironmentUpdate) {
        let opens: [Open] = lock.withLock {
            current = current?.updated(update)
            return handles.values
        }
        guard let environment = self.environment else { return }
        for open in opens {
            open.context.update(environment)
            open.inputs.yield(.environment(environment))
        }
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
        shut(open, handle, "the handle closed", notice: nil)
    }

    /// A message for the handle did not reach Kotlin, so what Kotlin shows
    /// of it is stale: the handle is closed from this side, as Kotlin's
    /// `close` would, with the failure sent before `closed` where it can be.
    /// Once, whichever comes first of this and a close.
    private func fail(_ handle: Handle, _ failure: BridgeFailure) {
        guard let open = lock.withLock({ handles.remove(handle) }) else { return }
        record.log?("handle \(handle.id) failed: \(failure.code.rawValue): \(failure.message)")
        shut(open, handle, "a message for the handle did not reach Kotlin", notice: BridgeCodec.failure(failure, handle: handle))
    }

    private func shut(_ open: Open, _ handle: Handle, _ reason: String, notice: String?) {
        let outbox = open.context.outbox
        outbox.beginClose()
        if let notice { outbox.enqueue(notice, forced: true, onFailure: { _ in }) }
        open.inputs.finish()
        open.worker.cancel()
        for (id, cancelled) in calls.cancelAll(handle: handle, reason) {
            record.count(\.cancelled)
            // A request that never left has nothing on Kotlin's side to stop.
            guard cancelled == .sent else { continue }
            outbox.enqueue(BridgeCodec.encode(op: .cancel, handle: handle, id: id), forced: true, onFailure: { _ in })
        }
        let root = open.root
        Task {
            await root.close()
            outbox.close(with: BridgeCodec.encode(op: .closed, handle: handle))
        }
    }

    private func answer(id: Int, _ query: BridgeQuery) throws(BridgeFailure) {
        switch query {
        case .stats:
            do {
                _ = deliver(try BridgeCodec.encode(op: .reply, id: id, body: stats))
            } catch {
                throw BridgeFailure(.swiftFailure, "the stats did not encode")
            }
        }
    }

    // MARK: deliver

    static let stopped = BridgeFailure(.notStarted, "the bridge is gone")

    /// Hands one message to Kotlin; the failure when Kotlin did not take it,
    /// for the caller to finish what waited on the message.
    func deliver(_ message: String) -> BridgeFailure? {
        let outlet: (any BridgeOutlet)? = lock.withLock {
            if case let .started(_, outlet) = phase { return outlet }
            return nil
        }
        guard let outlet else { return Self.stopped }
        // Swift replies only to Kotlin's queries; any other id is a device call's.
        let envelope = try? BridgeCodec.decodeEnvelope(message)
        let call: Redaction.Call =
            envelope?.op == .reply
            ? .query
            : envelope?.id.flatMap(calls.service(of:)).map(Redaction.Call.device) ?? .unknown
        record.record("out", message, call: call)
        if let failure = outlet.deliver(message) {
            record.count(\.deliveryFailures)
            record.log?("deliver failed: \(failure.code.rawValue): \(failure.message)")
            return failure
        }
        record.count(\.delivered)
        return nil
    }
}
