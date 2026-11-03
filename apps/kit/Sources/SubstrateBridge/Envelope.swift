import Foundation
import SubstrateCore

/// The version of the envelope's own shape. What its bodies hold is the
/// protocol fingerprint's job (`ProtocolFingerprint`), checked at start.
public let envelopeVersion = 1

/// Every message either way: Kotlin's `post` and Swift's `deliver`.
public struct Envelope: Bridged, Equatable {
    public var v: Int
    public var op: Op
    /// The root a message is about, where it is about one.
    public var handle: Handle?
    /// A request's id, or a stream's: unique among the calls outstanding.
    public var id: Int?
    /// A stream chunk's place, from 0.
    public var seq: Int?
    public var body: JSONValue?

    public init(op: Op, handle: Handle? = nil, id: Int? = nil, seq: Int? = nil, body: JSONValue? = nil) {
        v = envelopeVersion
        self.op = op
        self.handle = handle
        self.id = id
        self.seq = seq
        self.body = body
    }
}

/// What a message does.
public enum Op: String, Bridged, Equatable {
    /// Kotlin: open a root (`OpenRequest`); answered `opened` or `error`.
    case open
    case opened
    /// Kotlin: a command to a handle's root.
    case command
    /// Swift: a handle's newest presentation.
    case presentation
    /// Swift: a keyed list's whole rows or a delta (`ListMessage`).
    case list
    /// Kotlin: a keyed list's delta did not apply; send it whole.
    case resync
    /// Kotlin: close a handle. Idempotent.
    case close
    /// Swift: the handle is closed, and nothing more comes for it.
    case closed
    /// A call by id, answered exactly once by `reply` or `error`.
    case request
    case reply
    /// A stream's values by id and `seq`, ended once by `end`, `error` or `cancel`.
    case chunk
    case end
    case error
    case cancel
    /// Swift: `post` validated the message and queued it.
    case accepted
    /// Kotlin: the device's locale or time zone changed (`EnvironmentUpdate`).
    case environment
}

/// A root opened over the bridge. A slot's id is used again once its root
/// closes, and its generation moves each time, so a message for the root
/// that was there before is dropped rather than delivered to the next one.
public struct Handle: Bridged, Hashable {
    public var id: Int
    public var generation: Int

    public init(id: Int, generation: Int) {
        self.id = id
        self.generation = generation
    }
}

/// What `open` asks for: the root's name in the app's registry and its
/// environment's settings.
public struct OpenRequest: Bridged, Equatable {
    public var root: String
    public var settings: JSONValue?

    public init(root: String, settings: JSONValue? = nil) {
        self.root = root
        self.settings = settings
    }
}

/// A failure either side reports. Nothing throws across the boundary: a
/// failure is this value, in an `error` message or a `post` answer.
public struct BridgeFailure: Bridged, Error, Equatable {
    public enum Code: String, Bridged, Equatable {
        /// Not an envelope, or a body that is not the op's.
        case malformed
        /// `open` named a root the app's registry does not hold.
        case unknownRoot
        /// A handle that was never opened, is closed, or is of an old generation.
        case staleHandle
        /// A reply, chunk or end for an id nothing waits on, or one already answered.
        case unknownCall
        /// The call was cancelled: its handle closed, or its caller gave up.
        case cancelled
        /// A stream's queue was full; the stream was cancelled.
        case overflow
        /// Swift code failed (a root refused a command, an encoding failed).
        case swiftFailure
        /// Kotlin code threw while answering.
        case kotlinException
        /// A JNI call failed: attaching a thread, a local frame, a string.
        case jniFailure
        /// The bridge was started with another protocol than this library's.
        case protocolMismatch
        /// A message before `start`, or after a start that failed.
        case notStarted
        /// The device could not do it now (a secure store before the first
        /// unlock, no network); not the same as a refusal.
        case unavailable
        /// Kotlin does not offer the service or call asked for.
        case unsupported
    }

    public var code: Code
    public var message: String

    public init(_ code: Code, _ message: String) {
        self.code = code
        self.message = message
    }
}

extension BridgeFailure: LocalizedError {
    public var errorDescription: String? { message }
}
