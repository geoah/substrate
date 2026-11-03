import Foundation
import SubstrateCore

/// The device's local notifications (§5.4): whether the owner lets the app
/// alert, the categories its notifications answer with, one request
/// scheduled or replaced by its id, a request taken back, and what the owner
/// did with one. A tap or an action is a `NotificationResponse` on
/// `responses()`, which the app turns into a command; no app sets a delegate
/// of its own.
public protocol Notifications: Sendable {
    /// Where the owner's permission stands now.
    func permission() async -> NotificationPermission
    /// Asks the owner once, where they were never asked; answers where the
    /// permission stands after.
    func requestPermission() async -> NotificationPermission
    /// The categories the app's notifications name, each with the actions
    /// it offers, replacing the ones registered before. Registered before
    /// the first notification of a category is shown.
    func register(_ categories: [NotificationCategory]) async
    /// Schedules the request, replacing a pending one of the same id: shown
    /// at its `fireAt`, or at once where it has none. A request whose instant
    /// has passed is dropped, since what it would say is over. A permission
    /// refused makes it a quiet no-op.
    func schedule(_ request: NotificationRequest) async
    /// Removes the pending and the delivered notification of this id.
    func cancel(_ id: String) async
    /// Removes the delivered notifications of one thread (a chat opened:
    /// what it notified is no longer news).
    func cancel(thread: String) async
    /// Removes every pending and delivered notification, and the badge (a
    /// sign-out).
    func cancelAll() async
    /// The number on the app's icon.
    func setBadge(_ count: Int) async
    /// The ids scheduled and not yet shown, among those starting with
    /// `prefix`: what an app that schedules a whole plan takes back when
    /// the plan no longer holds them, across launches.
    func pending(prefix: String) async -> [String]
    /// What the owner does with the app's notifications from now on: a tap,
    /// an action, a dismissal, each once. One that came before anything
    /// read the stream (the tap that launched the app) is kept for the first
    /// reader.
    func responses() -> AsyncStream<NotificationResponse>
}

public extension Notifications {
    /// A whole plan made the one this device keeps: every request under
    /// `prefix` the plan no longer holds cancelled, then each it holds
    /// scheduled, which replaces one of the same id. An app that reminds of
    /// what its records say (a task's due time) calls this with the plan its
    /// presenter made, so a record that moved or went takes its reminder
    /// with it, and nothing of another app's prefix is touched.
    func reconcile(_ plan: [NotificationRequest], prefix: String) async {
        let wanted = Set(plan.map(\.id))
        for id in await pending(prefix: prefix) where !wanted.contains(id) { await cancel(id) }
        for request in plan { await schedule(request) }
    }

    func register(_ categories: [NotificationCategory]) async {}
    func cancel(thread: String) async {}
    func cancelAll() async {}
    func setBadge(_ count: Int) async {}
    func pending(prefix: String) async -> [String] { [] }
    func responses() -> AsyncStream<NotificationResponse> { AsyncStream { $0.finish() } }
}

/// What the owner allows.
public enum NotificationPermission: String, Bridged, Equatable {
    /// Never asked: the app asks when it first needs to.
    case notAsked
    /// Refused, or allowed with nowhere to show one.
    case off
    /// Allowed, but not time sensitive: a Focus may hold one back.
    case notTimeSensitive
    /// Allowed, time sensitive.
    case on
}

/// One notification, shown at an instant or at once.
public struct NotificationRequest: Bridged, Equatable {
    public var id: String
    public var title: String
    /// A line under the title: what the notification is about.
    public var subtitle: String?
    public var body: String?
    /// When it shows; nil shows it at once.
    public var fireAt: Date?
    /// Notifications of one thread group together.
    public var thread: String?
    /// One of the categories the app registered: the actions it offers, and
    /// what its responses carry.
    public var category: String?
    /// Breaks through a Focus where the app may.
    public var timeSensitive: Bool
    /// Shown while the app is in front. False keeps it quiet there, where
    /// the app already says it (a chat on screen, a countdown on the session
    /// screen); in the background it shows either way.
    public var showsWhileActive: Bool
    public var userInfo: [String: String]

    public init(id: String, title: String, body: String? = nil, fireAt: Date? = nil, thread: String? = nil, category: String? = nil,
                timeSensitive: Bool = false, showsWhileActive: Bool = true, userInfo: [String: String] = [:],
                subtitle: String? = nil)
    {
        self.id = id
        self.title = title
        self.subtitle = subtitle
        self.body = body
        self.fireAt = fireAt
        self.thread = thread
        self.category = category
        self.timeSensitive = timeSensitive
        self.showsWhileActive = showsWhileActive
        self.userInfo = userInfo
    }
}

/// A kind of notification the app shows, and the actions it answers with
/// beside a tap.
public struct NotificationCategory: Bridged, Equatable {
    public var id: String
    public var actions: [NotificationAction]
    /// A dismissal is a response too (`.dismiss`).
    public var reportsDismissal: Bool

    public init(id: String, actions: [NotificationAction] = [], reportsDismissal: Bool = false) {
        self.id = id
        self.actions = actions
        self.reportsDismissal = reportsDismissal
    }
}

/// A button on a notification.
public struct NotificationAction: Bridged, Equatable {
    public var id: String
    public var title: String
    /// Brings the app to the front, as a tap does; otherwise it answers in
    /// the background.
    public var opensApp: Bool
    /// Drawn as one that removes or gives something up.
    public var destructive: Bool
    /// Asks for a line of text first (a reply), which the response carries.
    public var input: NotificationTextInput?

    public init(id: String, title: String, opensApp: Bool = false, destructive: Bool = false, input: NotificationTextInput? = nil) {
        self.id = id
        self.title = title
        self.opensApp = opensApp
        self.destructive = destructive
        self.input = input
    }
}

/// The field an action asks for its text in.
public struct NotificationTextInput: Bridged, Equatable {
    /// The button that sends it.
    public var button: String
    public var placeholder: String

    public init(button: String, placeholder: String) {
        self.button = button
        self.placeholder = placeholder
    }
}

/// What the owner did with a notification: the request's id, category and
/// `userInfo`, and what was chosen, for the app to turn into a command.
public struct NotificationResponse: Bridged, Equatable {
    public enum Action: Bridged, Equatable {
        /// The notification itself was tapped.
        case open
        /// It was dismissed, where its category reports that.
        case dismiss
        /// One of its category's actions, by id.
        case named(String)
    }

    public var id: String
    public var category: String?
    public var action: Action
    public var userInfo: [String: String]
    /// The line an action with an input was sent with.
    public var text: String?

    public init(id: String, category: String?, action: Action, userInfo: [String: String] = [:], text: String? = nil) {
        self.id = id
        self.category = category
        self.action = action
        self.userInfo = userInfo
        self.text = text
    }
}
