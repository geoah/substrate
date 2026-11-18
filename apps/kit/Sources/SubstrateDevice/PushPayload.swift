import Foundation

/// What a push from the publisher's relay says, as the app reads it from a
/// notification's `userInfo`: the relay's `substrate` object, whose keys
/// arrive flattened under `substrate.` (`SystemNotifications` and the
/// notification service extension do it). It carries no words: an event,
/// a thread, the registration's generation and an opaque event id.
public struct PushPayload: Equatable, Sendable {
    /// The `userInfo` keys a push's values arrive under.
    public enum Key {
        public static let prefix = "substrate."
        public static let event = "substrate.event"
        public static let thread = "substrate.thread"
        public static let generation = "substrate.g"
        public static let eventID = "substrate.eventId"
        public static let version = "substrate.v"
    }

    /// The events a relay builds a push from.
    public enum Event: String, Sendable, Equatable, CaseIterable {
        case reply, failed, request, read
    }

    public var event: Event
    public var thread: String
    public var generation: String
    public var eventID: String

    /// The push a notification's `userInfo` carries; nil for a local
    /// notification, and for one whose event this build does not know.
    public init?(_ userInfo: [String: String]) {
        guard let event = userInfo[Key.event].flatMap(Event.init(rawValue:)), let thread = userInfo[Key.thread], !thread.isEmpty,
            let generation = userInfo[Key.generation], !generation.isEmpty
        else { return nil }
        self.event = event
        self.thread = thread
        self.generation = generation
        eventID = userInfo[Key.eventID] ?? ""
    }

    /// The thread a tapped notification opens: a local one names it at the
    /// top of its `userInfo` (`key`), a push at `substrate.thread`. A push
    /// opens it only while its generation is the install's current or
    /// pending one (`accepted`); one meant for an earlier registration
    /// (another server, another sign-in, one signed out) opens nothing.
    public static func thread(of userInfo: [String: String], key: String, accepted: (String) -> Bool) -> String? {
        if let thread = userInfo[key], !thread.isEmpty { return thread }
        guard let thread = userInfo[Key.thread], !thread.isEmpty, let generation = userInfo[Key.generation], accepted(generation)
        else { return nil }
        return thread
    }

    /// A push's `userInfo` as the system hands it, flattened to the strings
    /// a `NotificationResponse` carries: every top-level string, and each
    /// value of the relay's `substrate` object under `substrate.`. Numbers
    /// are written as text; `aps` and anything else that is not a string is
    /// left out.
    public static func flatten(_ userInfo: [AnyHashable: Any]) -> [String: String] {
        var out: [String: String] = [:]
        for (key, value) in userInfo {
            guard let key = key as? String else { continue }
            if let value = value as? String {
                out[key] = value
            } else if key == "substrate", let object = value as? [AnyHashable: Any] {
                for (inner, value) in object {
                    guard let inner = inner as? String, let text = text(value) else { continue }
                    out[Key.prefix + inner] = text
                }
            }
        }
        return out
    }

    private static func text(_ value: Any) -> String? {
        if let value = value as? String { return value }
        if let value = value as? Int { return String(value) }
        if let value = value as? NSNumber { return value.stringValue }
        return nil
    }
}

/// A notification shown and not yet dismissed: its id, the thread it groups
/// under, and its `userInfo`, flattened as a response's is.
public struct DeliveredNotification: Equatable, Sendable {
    public var id: String
    public var thread: String?
    public var userInfo: [String: String]

    public init(id: String, thread: String? = nil, userInfo: [String: String] = [:]) {
        self.id = id
        self.thread = thread
        self.userInfo = userInfo
    }

    /// The push it was, where it was one.
    public var push: PushPayload? { PushPayload(userInfo) }
}
