import Foundation

extension TodayCommand {
    /// The thing a command finishes, named as its row or its item is: a
    /// task's row, a routine's row, one occurrence of a group. Nil for a
    /// command that finishes nothing. A view does not send such a command
    /// at once: it waits in a `Finishing`, where it can be taken back.
    public var finishing: String? {
        switch self {
        case let .done(id): "task:" + id
        case let .log(row, _): row
        case let .logOccurrence(id, _): id
        default: nil
        }
    }
}

/// What the owner finished and can still take back. A frequent action that
/// can be reversed gets a way back and no question: so a tap on a control
/// that finishes something writes nothing. For `countdown` the control
/// counts down and is the way back; for `settle` more the thing is drawn
/// finished where it stands, and a tap still takes it back; then its
/// command is due. Leaving the app sends everything at once (`flush`): a
/// finish is never lost.
///
/// It is a value with no clock of its own: whoever holds it says what time
/// it is, asks when to look again (`next`), and sends what is `due`.
public struct Finishing: Equatable, Sendable {
    /// How long the control counts down.
    public static let countdown: TimeInterval = 3
    /// How long the thing is then drawn finished before its command is sent.
    public static let settle: TimeInterval = 2

    public enum Phase: String, Codable, Hashable, Sendable {
        /// The control is a countdown, and a tap on it cancels.
        case counting
        /// Drawn finished where it stands; nothing is written yet.
        case settled
    }

    public struct Pending: Equatable, Sendable {
        /// What it finishes (`TodayCommand.finishing`).
        public var key: String
        public var command: TodayCommand
        public var began: Date
    }

    /// In the order they were begun, which is the order they are sent in.
    public private(set) var pending: [Pending] = []

    public init() {}

    public var isEmpty: Bool { pending.isEmpty }

    /// A finish begins: false for a command that finishes nothing. A second
    /// way of finishing the same thing takes the place of the first.
    @discardableResult
    public mutating func begin(_ command: TodayCommand, at now: Date) -> Bool {
        guard let key = command.finishing else { return false }
        pending.removeAll { $0.key == key }
        pending.append(Pending(key: key, command: command, began: now))
        return true
    }

    /// It is taken back: nothing was written, and nothing will be.
    public mutating func cancel(_ key: String) {
        pending.removeAll { $0.key == key }
    }

    /// The command that waits for a thing.
    public func command(_ key: String) -> TodayCommand? { pending.first { $0.key == key }?.command }

    public func phase(_ key: String, at now: Date) -> Phase? {
        guard let item = pending.first(where: { $0.key == key }) else { return nil }
        return now.timeIntervalSince(item.began) < Finishing.countdown ? .counting : .settled
    }

    /// How much of the countdown a thing has left, from 1 to 0.
    public func left(_ key: String, at now: Date) -> Double {
        guard let item = pending.first(where: { $0.key == key }) else { return 0 }
        return min(1, max(0, 1 - now.timeIntervalSince(item.began) / Finishing.countdown))
    }

    /// The next instant something changes: a countdown ends, or a command
    /// is due. Nil when nothing waits.
    public func next(after now: Date) -> Date? {
        pending.flatMap { item in
            [item.began.addingTimeInterval(Finishing.countdown), item.began.addingTimeInterval(Finishing.countdown + Finishing.settle)]
        }.filter { $0 > now }.min()
    }

    /// The commands whose wait is over, in the order they were begun. They
    /// wait no more.
    public mutating func due(at now: Date) -> [TodayCommand] {
        let over = pending.filter { now.timeIntervalSince($0.began) >= Finishing.countdown + Finishing.settle }
        pending.removeAll { item in over.contains { $0.key == item.key } }
        return over.map(\.command)
    }

    /// Everything that waits, in order, at once: the app is leaving.
    public mutating func flush() -> [TodayCommand] {
        let all = pending.map(\.command)
        pending = []
        return all
    }
}
