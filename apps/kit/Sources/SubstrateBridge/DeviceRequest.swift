import Foundation
import SubstrateCore
import SubstrateDevice

/// A device-service call Swift asks Kotlin for (`request`), answered once by
/// `reply` with the value its comment names, or by a stream of `chunk`s.
public enum DeviceRequest: Bridged, Equatable {
    /// Answered by a `TransportOutcome`.
    case transportFetch(request: TransportRequest)
    /// A stream of `TransportChunk`s: the head, then each line of the body.
    case transportLines(request: TransportRequest)
    /// Answered by a `SecureItem`.
    case secureRead(store: String, account: String)
    /// `value` is the item's bytes in base64. Answered by a `SecureOutcome`.
    case secureWrite(store: String, account: String, value: String)
    /// Answered by a `SecureOutcome`.
    case secureDelete(store: String, account: String)
    /// Answered by a `DNSAnswer`.
    case dnsTXT(name: String, timeoutMillis: Int)
    /// Answered by a `NetworkPath`.
    case connectivityCurrent
    /// A stream of `NetworkPath`s, the current one first.
    case connectivityWatch

    public var service: DeviceService {
        switch self {
        case .transportFetch, .transportLines: .transport
        case .secureRead, .secureWrite, .secureDelete: .secureStore
        case .dnsTXT: .dns
        case .connectivityCurrent, .connectivityWatch: .connectivity
        }
    }
}

/// The services a `DeviceRequest` reaches, by what may cross in them.
public enum DeviceService: String, Sendable {
    case transport
    case secureStore
    case dns
    case connectivity

    /// The two whose messages may carry a token (docs/plans/app-kit.md
    /// §5.4): a bearer header out and a token endpoint's answer back, and a
    /// stored session. Wherever a message of theirs is recorded, its body is not.
    public var carriesSecrets: Bool { self == .transport || self == .secureStore }
}

/// One HTTP header, kept as a list: a name may repeat.
public struct TransportHeader: Bridged, Equatable {
    public var name: String
    public var value: String

    public init(name: String, value: String) {
        self.name = name
        self.value = value
    }
}

/// A request for OkHttp. A body travels in the message in base64 up to
/// `TransportRequest.inlineLimit` bytes, and as a file in the app's cache
/// folder past it; Kotlin deletes the file once sent.
public struct TransportRequest: Bridged, Equatable {
    public static let inlineLimit = 256 * 1024

    public var url: String
    public var method: String
    public var headers: [TransportHeader]
    public var body: String?
    public var bodyFile: String?
    public var timeoutMillis: Int

    public init(url: String, method: String, headers: [TransportHeader], body: String? = nil, bodyFile: String? = nil,
                timeoutMillis: Int)
    {
        self.url = url
        self.method = method
        self.headers = headers
        self.body = body
        self.bodyFile = bodyFile
        self.timeoutMillis = timeoutMillis
    }
}

/// A response's head and its body: inline in base64 up to the request's
/// inline limit, else a file in the cache folder Swift reads and deletes.
/// A redirect is this response itself: OkHttp never follows one.
public struct TransportResponse: Bridged, Equatable {
    public var status: Int
    public var url: String
    public var headers: [TransportHeader]
    public var body: String?
    public var bodyFile: String?

    public init(status: Int, url: String, headers: [TransportHeader], body: String? = nil, bodyFile: String? = nil) {
        self.status = status
        self.url = url
        self.headers = headers
        self.body = body
        self.bodyFile = bodyFile
    }
}

/// Why a request got no response, in the kinds `URLError` tells apart.
public enum TransportFailureKind: String, Bridged, Equatable {
    case offline
    case timedOut
    case cannotConnect
    case secureConnectionFailed
    case cancelled
    case other
}

public enum TransportOutcome: Bridged, Equatable {
    case response(response: TransportResponse)
    case failed(kind: TransportFailureKind, message: String)
}

/// A line stream's values: its head once, first, then each line without its
/// line break.
public enum TransportChunk: Bridged, Equatable {
    case head(response: TransportResponse)
    case line(text: String)
    case failed(kind: TransportFailureKind, message: String)
}

/// A secure store read. `reset` is a store whose key the device dropped or
/// that would not decrypt: Kotlin deleted what it held, and the item reads
/// as missing, so the app asks to sign in again rather than crash or keep
/// plaintext. `unavailable` is a store that cannot answer now.
public enum SecureItem: Bridged, Equatable {
    case found(value: String)
    case missing
    case unavailable(reason: String)
    case reset(reason: String)
}

public enum SecureOutcome: Bridged, Equatable {
    case done
    case unavailable(reason: String)
}

/// A TXT lookup through `DnsResolver.rawQuery`: the answer's response code
/// and each TXT record's character-strings, or why there was no answer.
public enum DNSAnswer: Bridged, Equatable {
    case answered(rcode: Int, records: [[String]])
    case failed(reason: String)
}

/// A call Kotlin asks Swift for (`request` the other way), for diagnostics.
public enum BridgeQuery: Bridged, Equatable {
    /// Answered by `BridgeStats`.
    case stats
}

/// The bridge's counters since start.
public struct BridgeStats: Bridged, Equatable {
    /// Messages Kotlin posted, and messages Swift delivered.
    public var posted: Int = 0
    public var delivered: Int = 0
    /// Messages for a handle closed or of an old generation, dropped.
    public var dropped: Int = 0
    /// Replies, chunks and ends for a call nothing waits on, refused.
    public var refused: Int = 0
    /// Presentations replaced before their turn.
    public var coalesced: Int = 0
    /// Streams cancelled because their queue was full.
    public var overflowed: Int = 0
    /// Calls answered `cancelled`.
    public var cancelled: Int = 0
    /// Messages Kotlin's `deliver` did not take (a JNI failure, a Kotlin exception).
    public var deliveryFailures: Int = 0
    /// Handles open now.
    public var open: Int = 0

    public init() {}
}
