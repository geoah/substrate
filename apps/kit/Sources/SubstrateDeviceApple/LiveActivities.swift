#if canImport(ActivityKit) && os(iOS)
import ActivityKit
import Foundation
import SubstrateDevice

/// The attributes an app's Live Activity widget declares, made from the
/// activity's id and title, so `LiveActivities` can request one.
public protocol OngoingActivityAttributes: ActivityAttributes where ContentState: Hashable {
    init(activityID: String, title: String)
    var activityID: String { get }
}

/// Ongoing activities as Live Activities. The content arrives as JSON and
/// `content` reads it into the widget's state, so the app's core never
/// names an ActivityKit type. An update whose content is what was last
/// sent is skipped, so typing does not flood ActivityKit; an activity the
/// owner dismissed stays dismissed until it ends, across launches
/// (`dismissedKey` in the defaults).
public final class LiveActivities<Attributes: OngoingActivityAttributes>: OngoingActivities, @unchecked Sendable {
    public typealias Content = @Sendable (Data) -> Attributes.ContentState?

    private let content: Content
    private let defaults: UserDefaults
    private let dismissedKey: String
    // Guards the three sets below; `@unchecked Sendable` rests on it.
    private let lock = NSLock()
    /// Activities ended here, whose `.dismissed` is not the owner's.
    private var endedHere: Set<String> = []
    private var watched: Set<String> = []
    private var lastSent: [String: Data] = [:]

    public init(content: @escaping Content, defaults: UserDefaults = .standard, dismissedKey: String) {
        self.content = content
        self.defaults = defaults
        self.dismissedKey = dismissedKey
    }

    public func show(_ activity: OngoingActivity) async {
        guard let state = content(activity.content) else { return }
        let next = ActivityContent(state: state, staleDate: activity.staleAt)
        if let running = live(activity.id) {
            watch(running)
            let fresh = lock.withLock { () -> Bool in
                guard lastSent[running.id] != activity.content else { return false }
                lastSent[running.id] = activity.content
                return true
            }
            if fresh { await Self.update(running.id, next, timestamp: activity.timestamp) }
            return
        }
        guard !dismissed.contains(activity.id), ActivityAuthorizationInfo().areActivitiesEnabled else { return }
        guard let requested = try? Activity.request(attributes: Attributes(activityID: activity.id, title: activity.title),
                                                    content: next, pushType: nil)
        else { return }
        lock.withLock { lastSent[requested.id] = activity.content }
        watch(requested)
    }

    public func end(_ id: String, final data: Data?, dismissAt: Date?) async {
        let state = data.flatMap(content)
        for activity in Activity<Attributes>.activities
            where activity.attributes.activityID == id && activity.activityState != .ended && activity.activityState != .dismissed
        {
            lock.withLock { _ = endedHere.insert(activity.id) }
            let policy: ActivityUIDismissalPolicy = state != nil ? dismissAt.map { .after($0) } ?? .immediate : .immediate
            await Self.end(activity.id, state.map { ActivityContent(state: $0, staleDate: nil) }, policy: policy)
        }
        forget(id)
    }

    public func endAll(except id: String?) async {
        for activity in Activity<Attributes>.activities where activity.attributes.activityID != id {
            switch activity.activityState {
            case .active, .stale, .pending:
                lock.withLock { _ = endedHere.insert(activity.id) }
                await Self.end(activity.id, nil, policy: .immediate)
            default:
                break
            }
        }
        // A dismissal matters only for the activity still open.
        let keep: Set<String> = id.map { dismissed.contains($0) ? [$0] : [] } ?? []
        if dismissed != keep { dismissed = keep }
    }

    private func live(_ id: String) -> Activity<Attributes>? {
        Activity<Attributes>.activities.first { activity in
            guard activity.attributes.activityID == id else { return false }
            switch activity.activityState {
            case .active, .stale, .pending: return true
            case .dismissed:
                // Dismissed while this process was not watching.
                if !lock.withLock({ endedHere.contains(activity.id) }) { markDismissed(id) }
                return false
            default: return false
            }
        }
    }

    private func watch(_ activity: Activity<Attributes>) {
        let id = activity.id
        guard lock.withLock({ watched.insert(id).inserted }) else { return }
        let owner = activity.attributes.activityID
        let updates = activity.activityStateUpdates
        Task { [weak self] in
            for await state in updates {
                guard let self else { return }
                if state == .dismissed, !lock.withLock({ endedHere.contains(id) }) { markDismissed(owner) }
                if state == .ended || state == .dismissed { break }
            }
            self?.lock.withLock {
                self?.watched.remove(id)
                self?.lastSent[id] = nil
            }
        }
    }

    private func markDismissed(_ id: String) {
        var set = dismissed
        if set.insert(id).inserted { dismissed = set }
    }

    private func forget(_ id: String) {
        var set = dismissed
        if set.remove(id) != nil { dismissed = set }
    }

    private var dismissed: Set<String> {
        get { Set(defaults.stringArray(forKey: dismissedKey) ?? []) }
        set { defaults.set(newValue.sorted(), forKey: dismissedKey) }
    }

    // An `Activity` is not Sendable: it is found again by id where it is
    // updated or ended, and only its content crosses.

    private static func update(_ id: String, _ content: ActivityContent<Attributes.ContentState>, timestamp: Date) async {
        guard let activity = Activity<Attributes>.activities.first(where: { $0.id == id }) else { return }
        await activity.update(content, alertConfiguration: nil, timestamp: timestamp)
    }

    private static func end(_ id: String, _ content: ActivityContent<Attributes.ContentState>?, policy: ActivityUIDismissalPolicy) async {
        guard let activity = Activity<Attributes>.activities.first(where: { $0.id == id }) else { return }
        await activity.end(content, dismissalPolicy: policy, timestamp: Date())
    }
}
#endif
