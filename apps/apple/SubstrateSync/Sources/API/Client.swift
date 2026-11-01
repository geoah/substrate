import Foundation
import SubstrateKit

struct CatalogItem: Decodable, Sendable {
    let id: String
    let version: Int?
    let installed: Bool
    let upgrade: Upgrade?

    struct Upgrade: Decodable, Sendable {}
}

/// The substrate API as this app speaks it, over the kit's client: the
/// session's bearer secret on every call, a record as Foundation JSON, and
/// the refusals the app tells apart.
struct Client: Sendable {
    let api: SubstrateClient

    var server: URL { api.server }

    /// A minute per request, and as many connections as the parallel writes
    /// (`eachLimited`) keep in flight.
    static let transport = URLSessionTransport(session: URLSessionTransport.makeSession(timeout: 60))

    init(server: URL, secret: String? = nil, transport: any HTTPTransport = Client.transport) {
        api = SubstrateClient(server: server, token: secret, transport: transport)
    }

    init(_ session: Session, transport: any HTTPTransport = Client.transport) {
        self.init(server: session.server, secret: session.secret, transport: transport)
    }

    func catalog() async throws -> [CatalogItem] {
        struct Page: Decodable { let items: [CatalogItem] }
        return try await api.send("GET", "api/v1/catalog").decode(Page.self).items
    }

    /// `install` for a provider (also its upgrade), `import` for a sample.
    /// The server installs what the bundle requires first and builds its
    /// indexes in the same request, so it is given minutes, not seconds, and
    /// never sent twice: a second install while the first still runs only
    /// queues behind it.
    func catalog(_ verb: String, _ id: String) async throws {
        try await api.send("POST", "api/v1/catalog/\(segment(id))/\(verb)", body: .json(Data("{}".utf8)),
                           timeout: Timeout.serverWork, retries: 0)
    }

    /// One live record's properties, or nil when there is none: a deleted
    /// record answers 200 with `deletedAt`, and reads as absent here.
    func get(_ kind: String, _ id: String, retries: Int = 3, timeout: TimeInterval = Timeout.read) async throws -> [String: Any]? {
        if case let .live(properties) = try await record(kind, id, retries: retries, timeout: timeout) { return properties }
        return nil
    }

    enum Stored {
        case live([String: Any])
        /// Tombstoned: there until the server collects it, and a write at its
        /// id restores it.
        case deleted
        case absent
    }

    /// One record as the server holds it. A reply that is not a record is an
    /// error, never an empty record: a receipt read as present when it is
    /// not would mark a Health day filed.
    func record(_ kind: String, _ id: String, retries: Int = 3, timeout: TimeInterval = Timeout.read) async throws -> Stored {
        let response = try await api.send(api.request("GET", SubstrateClient.recordPath(kind, id), timeout: timeout),
                                          accept: [404], retries: retries)
        if response.status == 404 { return .absent }
        guard let object = try? JSONSerialization.jsonObject(with: response.data) as? [String: Any], object["id"] != nil
        else { throw URLError(.cannotParseResponse) }
        if let deleted = object["deletedAt"], !(deleted is NSNull) { return .deleted }
        return .live(object["properties"] as? [String: Any] ?? [:])
    }

    /// PUT merges per property and never prunes.
    func put(_ kind: String, _ id: String, _ properties: Any) async throws {
        try await api.send("PUT", SubstrateClient.recordPath(kind, id), body: json(["properties": properties]), retries: 3)
    }

    /// Clears keys: each one set to null. Sent again on a busy server like a
    /// put: nulling a key twice is nulling it once.
    func clear(_ kind: String, _ id: String, keys: [String]) async throws {
        guard !keys.isEmpty else { return }
        let nulls = Dictionary(uniqueKeysWithValues: keys.map { ($0, NSNull() as Any) })
        try await api.send("PATCH", SubstrateClient.recordPath(kind, id), body: json(["properties": nulls]), retries: 3)
    }

    func delete(_ kind: String, _ id: String, retries: Int = 3, timeout: TimeInterval = Timeout.read) async throws {
        try await api.send(api.request("DELETE", SubstrateClient.recordPath(kind, id), timeout: timeout), accept: [404],
                           retries: retries)
    }

    /// One Health delivery to the webhook door, answering the fire id it was
    /// stored under. A 202 means stored, not filed: the provider's function
    /// runs after it, and writes its receipt at that id or parks the
    /// delivery (`parked`), so a caller confirms one or the other. `key` is
    /// the trigger's own, which a password session's delivery carries; with
    /// none the session's token is the credential, as an app's token holding
    /// `webhook:<trigger>` is.
    @discardableResult
    func deliver(authority: String, trigger: String, body: Data, headers: [(String, String)], key: String?) async throws -> String {
        let named = Dictionary(headers, uniquingKeysWith: { _, last in last })
        do {
            // A full spool drains as the server files: minutes, not seconds.
            return try await api.deliver(authority: authority, trigger: trigger, body: body,
                                         contentType: named["Content-Type"] ?? "application/json",
                                         headers: named.filter { $0.key != "Content-Type" }, credential: key)
        } catch {
            // The door's refusal is the webhook's, not the sign-in's: the
            // session stands, and a record read says whether it does not.
            throw WebhookRefused.from(error, app: key == nil) ?? error
        }
    }

    /// Stores bytes as a blob and answers its digest, the value a `blobref`
    /// property takes. The record naming it must follow within a minute: the
    /// server collects a blob nothing references after that.
    func putBlob(_ data: Data, mediaType: String, name: String?) async throws -> String {
        try await api.putBlob(data, mediaType: mediaType, name: name).digest
    }

    /// The trigger's parked list: what its function refused, kept with its
    /// reason, and what it has accepted and not settled yet.
    func parked(trigger: String, timeout: TimeInterval = Timeout.read) async throws -> [ParkedDelivery] {
        try await api.parked(trigger: trigger, timeout: timeout)
    }

    private func json(_ object: Any) throws -> RequestBody {
        .json(try JSONSerialization.data(withJSONObject: object, options: [.withoutEscapingSlashes]))
    }
}

/// How long a call may take, by what it asks of the server. A read the
/// server answers from its index is quick or something is wrong; an install
/// does real work; an upload is bounded by the phone's uplink.
enum Timeout {
    static let quick: TimeInterval = SubstrateClient.Timeout.quick
    static let read: TimeInterval = SubstrateClient.Timeout.read
    static let upload: TimeInterval = SubstrateClient.Timeout.upload

    /// An install. A debug build reads SUBSTRATE_SYNC_SERVER_WORK_SECONDS,
    /// so a test stands in for a slow server without waiting minutes.
    static var serverWork: TimeInterval {
        #if DEBUG
        if let seconds = ProcessInfo.processInfo.environment["SUBSTRATE_SYNC_SERVER_WORK_SECONDS"].flatMap(Double.init) {
            return seconds
        }
        #endif
        return SubstrateClient.Timeout.serverWork
    }
}

/// An error in a sentence a person can act on, in the kit's words for this
/// app.
func explain(_ error: Error, server: URL? = nil) -> String {
    SubstrateKit.explain(error, app: "Substrate Sync", server: server)
}

/// The webhook door refused a Health delivery. Under a password session the
/// trigger's key was refused, and Set Up arms the webhook again; under an app
/// session the webhook is off or the app's access no longer reaches it, and
/// only the owner's approval changes that.
struct WebhookRefused: LocalizedError {
    let text: String
    var app = false

    var errorDescription: String? {
        if app {
            return "The server did not take this phone's Health (\(text)). Its webhook is off, or Substrate Sync's access no longer reaches it: review its access on Home."
        }
        return "The server refused this phone's Health key (\(text)). Open Set Up and run it to arm the webhook again."
    }

    /// The refusal a delivery's error is, if it is one. The door answers 404
    /// for every refusal of its own and 401 for a signature; under a password
    /// session a 404 is the server's word as it is, since the key was read
    /// from the trigger this run.
    static func from(_ error: Error, app: Bool) -> WebhookRefused? {
        switch error {
        case let e as SessionExpired: return WebhookRefused(text: e.message ?? "401", app: app)
        case let e as InsufficientScope: return WebhookRefused(text: e.message ?? "403", app: app)
        case let e as ServerError where e.status == 403 || (app && e.status == 404): return WebhookRefused(text: e.text, app: app)
        case let e as TransportError where e.status == 403 || (app && e.status == 404): return WebhookRefused(text: e.text, app: app)
        default: return nil
        }
    }
}

/// Runs `work`, and gives up on it after `seconds` with a timeout, whether
/// or not it answers its cancellation: MapKit's geocoder and an item
/// provider carry no timeout of their own.
func withTimeout<T: Sendable>(_ seconds: TimeInterval, _ work: @escaping @Sendable () async throws -> T) async throws -> T {
    let gate = ResumeOnce<T>()
    return try await withCheckedThrowingContinuation { continuation in
        gate.set(continuation)
        let worker = Task {
            do { gate.resume(.success(try await work())) } catch { gate.resume(.failure(error)) }
        }
        Task {
            try? await Task.sleep(for: .seconds(seconds))
            worker.cancel()
            gate.resume(.failure(URLError(.timedOut)))
        }
    }
}

/// A continuation resumed by whichever of its racers gets there first.
private final class ResumeOnce<T: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var continuation: CheckedContinuation<T, Error>?

    func set(_ c: CheckedContinuation<T, Error>) {
        lock.lock()
        continuation = c
        lock.unlock()
    }

    func resume(_ result: Result<T, Error>) {
        lock.lock()
        let c = continuation
        continuation = nil
        lock.unlock()
        c?.resume(with: result)
    }
}

/// A call that ran out of time may have finished on the server all the
/// same: an install, for one, goes on after the phone stops waiting.
func timedOut(_ error: Error) -> Bool { (error as? URLError)?.code == .timedOut }
