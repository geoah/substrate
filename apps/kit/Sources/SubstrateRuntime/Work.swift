/// What an effect does once the runtime starts it. Its results are events,
/// delivered through the same loop as a command's, and dropped when the
/// effect's key was started again, or its controller closed or reset,
/// since.
public enum Work<Event: Sendable>: Sendable {
    /// Nothing to run: the effect was a value for a test, or a no-op here.
    case none
    /// One result, or none.
    case once(@Sendable () async -> Event?)
    /// Results until the sequence ends or the effect is cancelled: a watch, a
    /// turn's stream, connectivity.
    case sequence(@Sendable () -> AsyncStream<Event>)
    /// Stops what runs under the effect's key; its late results are dropped.
    case cancel
}

public extension Work {
    /// The same work, its results made another controller's events: an
    /// app's root running a component's effects (the kit's linked records)
    /// as its own.
    func map<Other: Sendable>(_ transform: @escaping @Sendable (Event) -> Other) -> Work<Other> {
        switch self {
        case .none: .none
        case .cancel: .cancel
        case let .once(body): .once { await body().map(transform) }
        case let .sequence(make): .sequence { make().relayed(transform) }
        }
    }

    /// Results made by `body` as it runs, ended when it returns and cancelled
    /// when the runtime stops reading: an effect that follows something.
    static func relay(_ body: @escaping @Sendable (AsyncStream<Event>.Continuation) async -> Void) -> Work {
        .sequence {
            AsyncStream { continuation in
                let task = Task {
                    await body(continuation)
                    continuation.finish()
                }
                continuation.onTermination = { _ in task.cancel() }
            }
        }
    }
}

extension AsyncStream where Element: Sendable {
    func relayed<Other: Sendable>(_ transform: @escaping @Sendable (Element) -> Other) -> AsyncStream<Other> {
        AsyncStream<Other> { continuation in
            let task = Task {
                for await element in self { continuation.yield(transform(element)) }
                continuation.finish()
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }
}
