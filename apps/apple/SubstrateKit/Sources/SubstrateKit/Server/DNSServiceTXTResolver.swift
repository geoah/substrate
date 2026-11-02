import dnssd
import Foundation

/// TXT lookups through the system resolver (`DNSServiceQueryRecord`), which
/// is what an app reads `_substrate.<name>` with: Foundation has no TXT
/// lookup, and the system resolver honours the device's DNS settings.
public struct DNSServiceTXTResolver: TXTResolver {
    /// How long a lookup waits for an answer before it fails.
    public var timeout: TimeInterval

    public init(timeout: TimeInterval = 5) {
        self.timeout = timeout
    }

    /// The lookup failed: no answer in time, or an error other than "no such
    /// name" or "no such record".
    public struct Failed: LocalizedError, Sendable {
        public let name: String
        public let code: Int32
        public var errorDescription: String? {
            code == 0 ? "Looking up \(name) took too long." : "Looking up \(name) failed (DNS-SD error \(code))."
        }
    }

    public func txt(_ name: String) async throws -> [String]? {
        let query = TXTQuery(name: name)
        return try await withTaskCancellationHandler {
            try await query.run(timeout: timeout)
        } onCancel: {
            query.cancel()
        }
    }

    /// One TXT record's rdata as one string: its length-prefixed
    /// character-strings joined, as a resolver joins a record split across
    /// 255-byte strings.
    static func text(_ rdata: [UInt8]) -> String {
        var joined: [UInt8] = []
        var i = 0
        while i < rdata.count {
            let length = Int(rdata[i])
            i += 1
            let end = min(i + length, rdata.count)
            joined += rdata[i ..< end]
            i = end
        }
        return String(decoding: joined, as: UTF8.self)
    }
}

/// One query in flight. Every field is read and written on `queue` alone,
/// which is also the queue DNS-SD calls back on.
private final class TXTQuery: @unchecked Sendable {
    let name: String
    let queue = DispatchQueue(label: "dev.reamde.substrate.txt")
    private var ref: DNSServiceRef?
    private var answers: [String] = []
    private var continuation: CheckedContinuation<[String]?, Error>?
    private var done = false

    init(name: String) {
        self.name = name
    }

    func run(timeout: TimeInterval) async throws -> [String]? {
        try await withCheckedThrowingContinuation { continuation in
            queue.async { self.start(continuation, timeout: timeout) }
        }
    }

    func cancel() {
        queue.async { self.finish(.failure(CancellationError())) }
    }

    private func start(_ continuation: CheckedContinuation<[String]?, Error>, timeout: TimeInterval) {
        // Cancelled before the query began.
        if done {
            continuation.resume(throwing: CancellationError())
            return
        }
        self.continuation = continuation
        var ref: DNSServiceRef?
        // Unretained: the caller holds this query until it finishes, and the
        // ref, deallocated in finish, is what calls back.
        let context = Unmanaged.passUnretained(self).toOpaque()
        let status = DNSServiceQueryRecord(&ref, DNSServiceFlags(kDNSServiceFlagsReturnIntermediates), 0, name,
                                           UInt16(kDNSServiceType_TXT), UInt16(kDNSServiceClass_IN), TXTQuery.reply, context)
        guard status == DNSServiceErrorType(kDNSServiceErr_NoError), let ref else {
            finish(.failure(DNSServiceTXTResolver.Failed(name: name, code: status)))
            return
        }
        self.ref = ref
        DNSServiceSetDispatchQueue(ref, queue)
        queue.asyncAfter(deadline: .now() + timeout) { [self] in
            finish(.failure(DNSServiceTXTResolver.Failed(name: name, code: 0)))
        }
    }

    private static let reply: DNSServiceQueryRecordReply = { _, flags, _, error, _, type, _, length, rdata, _, context in
        guard let context else { return }
        let query = Unmanaged<TXTQuery>.fromOpaque(context).takeUnretainedValue()
        let bytes = rdata.map { Array(UnsafeRawBufferPointer(start: $0, count: Int(length))) }
        query.received(flags: flags, error: error, type: type, rdata: bytes)
    }

    private func received(flags: DNSServiceFlags, error: DNSServiceErrorType, type: UInt16, rdata: [UInt8]?) {
        // ReturnIntermediates delivers a negative answer as this error rather
        // than waiting out the timeout.
        if error == DNSServiceErrorType(kDNSServiceErr_NoSuchRecord) || error == DNSServiceErrorType(kDNSServiceErr_NoSuchName) {
            finish(.success(nil))
            return
        }
        guard error == DNSServiceErrorType(kDNSServiceErr_NoError) else {
            finish(.failure(DNSServiceTXTResolver.Failed(name: name, code: error)))
            return
        }
        // An intermediate CNAME is not an answer.
        if flags & DNSServiceFlags(kDNSServiceFlagsAdd) != 0, type == UInt16(kDNSServiceType_TXT), let rdata {
            answers.append(DNSServiceTXTResolver.text(rdata))
        }
        if flags & DNSServiceFlags(kDNSServiceFlagsMoreComing) == 0, !answers.isEmpty {
            finish(.success(answers))
        }
    }

    private func finish(_ result: Result<[String]?, Error>) {
        guard !done else { return }
        done = true
        if let ref {
            DNSServiceRefDeallocate(ref)
            self.ref = nil
        }
        continuation?.resume(with: result)
        continuation = nil
    }
}
