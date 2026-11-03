import Foundation
import SubstrateDevice

/// Ongoing activities a test reads back: every call in order, and what
/// shows now.
public final class RecordedActivities: OngoingActivities, @unchecked Sendable {
    public enum Call: Equatable, Sendable {
        case show(OngoingActivity)
        case end(String, final: Data?, dismissAt: Date?)
        case endAll(except: String?)
    }

    private let lock = NSLock()
    private var kept: [Call] = []
    private var shown: [String: OngoingActivity] = [:]

    public init() {}

    public var calls: [Call] { lock.withLock { kept } }

    /// The activities showing now, by id.
    public var showing: [String: OngoingActivity] { lock.withLock { shown } }

    public func show(_ activity: OngoingActivity) async {
        lock.withLock {
            kept.append(.show(activity))
            shown[activity.id] = activity
        }
    }

    public func end(_ id: String, final content: Data?, dismissAt: Date?) async {
        lock.withLock {
            kept.append(.end(id, final: content, dismissAt: dismissAt))
            shown[id] = nil
        }
    }

    public func endAll(except id: String?) async {
        lock.withLock {
            kept.append(.endAll(except: id))
            shown = shown.filter { $0.key == id }
        }
    }
}
