import Foundation
import SubstrateCore
import SubstrateDevice

/// A device-service call Swift asks Kotlin for (`request`), answered once by
/// `reply` with the value its comment names, or by `chunk`s, a chunked answer.
public enum DeviceRequest: Bridged, Equatable {
    /// Answered by a `TransportOutcome`.
    case transportFetch(request: TransportRequest)
    /// `TransportChunk`s, chunk by chunk: the head, then each line of the body.
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
    /// `NetworkPath`s, chunk by chunk, the current one first.
    case connectivityWatch
    /// Answered by a `BrowserPrepared`.
    case browserPrepare(route: BrowserRoute)
    /// The owner's browser on `page`, waiting for the callback on `redirect`
    /// that `browserPrepare` answered. Answered by a `BrowserAnswer`.
    case browserOpen(page: String, redirect: String)
    /// What `browserPrepare` took is let go of. Answered by `null`.
    case browserFinish(redirect: String)
    /// Answered by a `NotificationPermission`.
    case notificationsPermission
    /// Asks the owner where they were never asked. Answered by a
    /// `NotificationPermission`.
    case notificationsRequestPermission
    /// Answered by `null`.
    case notificationsRegister(categories: [NotificationCategory])
    /// Answered by `null`.
    case notificationsSchedule(request: NotificationRequest)
    /// Answered by `null`.
    case notificationsCancel(id: String)
    /// Answered by `null`.
    case notificationsCancelThread(thread: String)
    /// Answered by `null`.
    case notificationsCancelAll
    /// Answered by `null`.
    case notificationsBadge(count: Int)
    /// Answered by the ids, a `[String]`.
    case notificationsPending(prefix: String)
    /// A stream of `NotificationResponse`s, each once, the ones that came
    /// before anything read first.
    case notificationsResponses

    public var service: DeviceService {
        switch self {
        case .transportFetch, .transportLines: .transport
        case .secureRead, .secureWrite, .secureDelete: .secureStore
        case .dnsTXT: .dns
        case .connectivityCurrent, .connectivityWatch: .connectivity
        case .browserPrepare, .browserOpen, .browserFinish: .browser
        case .notificationsPermission, .notificationsRequestPermission, .notificationsRegister, .notificationsSchedule,
             .notificationsCancel, .notificationsCancelThread, .notificationsCancelAll, .notificationsBadge, .notificationsPending,
             .notificationsResponses: .notifications
        }
    }
}

/// The services a `DeviceRequest` reaches, by what may cross in them.
public enum DeviceService: String, Sendable {
    case transport
    case secureStore
    case dns
    case connectivity
    case browser
    case notifications

    /// The ones whose messages may carry a credential (docs/plans/app-kit.md
    /// §5.4): a bearer header out and a token endpoint's answer back, a
    /// stored session, and a sign-in's callback, whose code a token is
    /// exchanged for. Wherever a message of theirs is recorded, its body is not.
    public var carriesSecrets: Bool { self == .transport || self == .secureStore || self == .browser }
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
/// `TransportRequest.inlineLimit` bytes, and as a file in the spill folder
/// (`BridgeEnvironment.spill`) past it, which Kotlin sends and Swift deletes
/// once the call is over.
///
/// A request that `credentials` marks never spills, either way: its body and
/// its answer stay in the messages, which are never recorded with their
/// bodies, up to `credentialLimit`, and past it the call fails.
public struct TransportRequest: Bridged, Equatable {
    public static let inlineLimit = 256 * 1024
    /// The most a credential exchange's body or answer may be. A token
    /// response is a few kilobytes; this bounds the message, not the token.
    public static let credentialLimit = 1024 * 1024

    public var url: String
    public var method: String
    public var headers: [TransportHeader]
    public var body: String?
    public var bodyFile: String?
    public var timeoutMillis: Int
    /// The body or the answer may hold a credential (`BridgeTransport.carriesCredentials`).
    public var credentials: Bool

    public init(url: String, method: String, headers: [TransportHeader], body: String? = nil, bodyFile: String? = nil,
                timeoutMillis: Int, credentials: Bool = false)
    {
        self.url = url
        self.method = method
        self.headers = headers
        self.body = body
        self.bodyFile = bodyFile
        self.timeoutMillis = timeoutMillis
        self.credentials = credentials
    }
}

/// A response's head and its body: inline in base64 up to the request's
/// inline limit, else a file in the spill folder, which Swift deletes once it
/// has read it, and Kotlin when Swift refused it or the call was cancelled.
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
    /// Chunked answers cancelled because their queue was full.
    public var overflowed: Int = 0
    /// Calls answered `cancelled`.
    public var cancelled: Int = 0
    /// Messages Kotlin's `deliver` did not take (a JNI failure, a Kotlin exception).
    public var deliveryFailures: Int = 0
    /// Handles open now.
    public var open: Int = 0

    public init() {}
}

/// Where a sign-in's callback comes back, as `SignInRoute` says it.
public enum BrowserRoute: Bridged, Equatable {
    /// A release's https callback, which the system hands this app alone
    /// once the site's association file names it.
    case claimed(url: String)
    /// A loopback redirect (RFC 8252) at `path` on `127.0.0.1`, on a port
    /// Kotlin picks when it listens. `appName` is what the page the browser
    /// lands on tells the person to go back to.
    case loopback(appName: String, path: String)
}

/// A route made ready: the redirect the authorization request names, or why
/// there is none.
public enum BrowserPrepared: Bridged, Equatable {
    case ready(redirect: String)
    case failed(reason: String)
}

/// What a browser sign-in came back with: the callback's query, unchecked
/// (Swift holds it to its request), or why it did not.
public enum BrowserAnswer: Bridged, Equatable {
    case callback(query: [String: String])
    case cancelled
    /// No callback came before the attempt's deadline.
    case expired
    case failed(reason: String)
}
