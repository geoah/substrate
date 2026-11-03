import Foundation
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting

/// A refusal of the fixtures' engines.
struct Refused: Error, Equatable, CustomStringConvertible {
    var description: String
}

/// Where a gated effect waits until the test opens its gate. A gate ignores
/// cancellation on purpose: its result arrives late, as a stream's last
/// callback does, and the runtime must drop it.
actor Gates {
    private var waiting: [String: [CheckedContinuation<Void, Never>]] = [:]
    private var open: Set<String> = []

    func wait(_ name: String) async {
        if open.contains(name) { return }
        await withCheckedContinuation { waiting[name, default: []].append($0) }
    }

    func release(_ name: String) {
        open.insert(name)
        for continuation in waiting.removeValue(forKey: name) ?? [] { continuation.resume() }
    }

    func isWaiting(_ name: String) -> Bool { !(waiting[name] ?? []).isEmpty }
}

/// Where Training's finish writes its history: it fails while `failing`.
actor HistoryShelf {
    private(set) var written: [[String]] = []
    var failing = false

    func setFailing(_ value: Bool) { failing = value }

    func write(_ sets: [String]) throws {
        if failing { throw Refused(description: "The disk is full") }
        written.append(sets)
    }
}

/// Workouts' session, small: a journal of a session's events, a rest timer
/// with a tick, and the finish in two events.
struct Training: Controller {
    enum Command: Codable, Equatable, Sendable {
        case start
        case log(String)
        case finish
        case rest(seconds: Int)
    }

    enum Event: Codable, Equatable, Sendable {
        case started(id: UUID, at: Date)
        case logged(String)
        case finishRequested
        case finished
        case finishFailed(String)
        case restStarted(until: Date)
        case restEnded
        /// At open over a replayed session: its effects restore what the
        /// device showed for it.
        case resumed
    }

    struct State: Codable, Equatable, Sendable {
        var id: UUID?
        var sets: [String] = []
        var finishing = false
        var restUntil: Date?
    }

    struct Transient: Equatable, Sendable {
        var refusal: String?
    }

    struct Presentation: Codable, Equatable, Sendable {
        var open: Bool
        var sets: [String]
        var finishing: Bool
        var refusal: String?
        var rest: String?
        var restUntil: Date?
    }

    enum Effect: Equatable, Sendable {
        case writeHistory([String])
        case announce(String)
        case restoreActivity(sets: Int)
    }

    let store: any JournalStore<Event, State>
    let shelf: HistoryShelf
    /// Asks for `resumed` at open, as Workouts does over a replayed session.
    var resumes = false

    var initialState: State { State() }
    var initialTransient: Transient { Transient() }

    func events(for command: Command, in context: Enrichment<Training>) -> [Event] {
        switch command {
        case .start: [.started(id: context.mint(), at: context.now)]
        case let .log(set): [.logged(set)]
        case .finish: [.finishRequested]
        case let .rest(seconds): [.restStarted(until: context.now.addingTimeInterval(TimeInterval(seconds)))]
        }
    }

    func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        switch event {
        case let .started(id, _):
            guard state.id == nil else { throw Refused(description: "A session is open") }
            next = State(id: id)
        case let .logged(set):
            guard state.id != nil, !state.finishing else { throw Refused(description: "No session is open") }
            next.sets.append(set)
        case .finishRequested:
            guard state.id != nil else { throw Refused(description: "No session is open") }
            next.finishing = true
        case .finished:
            next = State()
        case .finishFailed:
            next.finishing = false
        case let .restStarted(until):
            next.restUntil = until
        case .restEnded:
            next.restUntil = nil
        case .resumed:
            break
        }
        return next
    }

    func journaling(of event: Event) -> Journaling {
        switch event {
        case .started: .restart
        case .logged, .restStarted, .restEnded: .append
        case .finished: .clear
        case .finishRequested, .finishFailed, .resumed: .skip
        }
    }

    func transient(after event: Event, applied state: State, from transient: Transient) -> Transient {
        Transient(refusal: nil)
    }

    func transient(afterRefusing event: Event, because error: any Error, from transient: Transient) -> Transient {
        Transient(refusal: String(describing: error))
    }

    func present(_ input: PresenterInput<Training>) -> Presentation {
        let state = input.state
        var rest: String?
        if let until = state.restUntil, until > input.now {
            rest = "Rest \(Int(until.timeIntervalSince(input.now).rounded())) s"
        }
        return Presentation(open: state.id != nil, sets: state.sets, finishing: state.finishing,
                            refusal: input.transient.refusal, rest: rest, restUntil: rest == nil ? nil : state.restUntil)
    }

    func nextChange(of presentation: Presentation) -> Date? { presentation.restUntil }

    func event(atTick instant: Date) -> Event? { .restEnded }

    func effects(of event: Event, on state: State, presented presentation: Presentation) -> [Effect] {
        switch event {
        case .finishRequested: [.writeHistory(state.sets)]
        case let .logged(set): [.announce("\(set), \(presentation.sets.count) so far")]
        case .resumed: state.id == nil ? [] : [.restoreActivity(sets: state.sets.count)]
        default: []
        }
    }

    func event(atOpen replayed: Bool) -> Event? { resumes && replayed ? .resumed : nil }

    func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        switch effect {
        case let .writeHistory(sets):
            let shelf = shelf
            return .once {
                do {
                    try await shelf.write(sets)
                    return .finished
                } catch {
                    return .finishFailed(String(describing: error))
                }
            }
        case .announce, .restoreActivity:
            return .none
        }
    }

    func journal(in environment: AppEnvironment) -> (any JournalStore<Event, State>)? { store }
}

/// A reader of something remote: a keyed fetch that a newer one cancels, a
/// stream it follows until told to stop, and deltas drawn once per window,
/// as Chat's 50 ms flush does.
struct Feed: Controller {
    enum Command: Codable, Equatable, Sendable {
        case fetch(String)
        case delta(String)
        case mark
        case follow
        case unfollow
    }

    enum Event: Codable, Equatable, Sendable {
        case asked(String)
        case fetched(String)
        case delta(String)
        case marked
        case following(Bool)
        case heard(Int)
    }

    struct State: Codable, Equatable, Sendable {
        var text = ""
        var fetched: [String] = []
        var marks = 0
        var following = false
        var heard: [Int] = []
    }

    typealias Presentation = State

    enum Effect: Equatable, Sendable {
        case fetch(String)
        case follow
        case unfollow
    }

    let gates: Gates
    let source: Source

    var initialState: State { State() }

    func events(for command: Command, in context: Enrichment<Feed>) -> [Event] {
        switch command {
        case let .fetch(name): [.asked(name)]
        case let .delta(text): [.delta(text)]
        case .mark: [.marked]
        case .follow: [.following(true)]
        case .unfollow: [.following(false)]
        }
    }

    func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        switch event {
        case .asked: break
        case let .fetched(name): next.fetched.append(name)
        case let .delta(text): next.text += text
        case .marked: next.marks += 1
        case let .following(on): next.following = on
        case let .heard(value): next.heard.append(value)
        }
        return next
    }

    func coalescing(of event: Event) -> Coalescing {
        switch event {
        case .delta: .within(.milliseconds(50))
        case .heard: .within(.milliseconds(10))
        default: .now
        }
    }

    func present(_ input: PresenterInput<Feed>) -> State { input.state }

    func effects(of event: Event, on state: State, presented presentation: State) -> [Effect] {
        switch event {
        case let .asked(name): [.fetch(name)]
        case .following(true): [.follow]
        case .following(false): [.unfollow]
        default: []
        }
    }

    func key(of effect: Effect) -> String? {
        switch effect {
        case .fetch: "fetch"
        case .follow, .unfollow: "follow"
        }
    }

    func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        switch effect {
        case let .fetch(name):
            let gates = gates
            return .once {
                await gates.wait(name)
                return .fetched(name)
            }
        case .follow:
            let source = source
            return .sequence { source.stream().map(Event.heard) }
        case .unfollow:
            return .cancel
        }
    }
}

/// A stream a test feeds: each `stream()` is a new one, and `send` reaches
/// the newest.
final class Source: @unchecked Sendable {
    private let lock = NSLock()
    private var continuation: AsyncStream<Int>.Continuation?
    private(set) var opened = 0

    func stream() -> AsyncStream<Int> {
        let (stream, continuation) = AsyncStream<Int>.makeStream()
        lock.withLock {
            self.continuation = continuation
            opened += 1
        }
        return stream
    }

    func send(_ value: Int) {
        _ = lock.withLock { continuation }?.yield(value)
    }

    var isOpen: Bool { lock.withLock { continuation != nil } }
}

private extension AsyncStream {
    func map<T: Sendable>(_ transform: @escaping @Sendable (Element) -> T) -> AsyncStream<T> where Element: Sendable {
        let (stream, continuation) = AsyncStream<T>.makeStream()
        let task = Task {
            for await element in self { continuation.yield(transform(element)) }
            continuation.finish()
        }
        continuation.onTermination = { _ in task.cancel() }
        return stream
    }
}

/// Chat's list, small: rooms are children keyed by an id the app mints,
/// what a child says is folded into the parent's transcript, the parent's
/// presentation holds each child's, the topic the parent keeps is part of
/// each child's value, and a room bound to a thread closes any other room
/// on it, as binding a thread closes Chat's other conversation.
struct Lobby: Controller {
    enum Command: Codable, Equatable, Sendable {
        case open(String)
        case close(ChildKey)
        case topic(String)
    }

    enum Event: Codable, Equatable, Sendable {
        case opened(ChildKey, String)
        case closed(ChildKey)
        case said(ChildKey, String)
        case bound(ChildKey, String)
        case topicSet(String)
    }

    struct State: Codable, Equatable, Sendable {
        var rooms: [ChildKey: String] = [:]
        var threads: [ChildKey: String] = [:]
        var transcript: [String] = []
        var topic = ""
    }

    struct Presentation: Codable, Equatable, Sendable {
        var transcript: [String]
        var rooms: [ChildKey: Room.Presentation]
    }

    let gates: Gates

    var initialState: State { State() }

    func events(for command: Command, in context: Enrichment<Lobby>) -> [Event] {
        switch command {
        case let .open(name): [.opened(ChildKey(context.mint().uuidString.lowercased()), name)]
        case let .close(key): [.closed(key)]
        case let .topic(topic): [.topicSet(topic)]
        }
    }

    func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        switch event {
        case let .opened(key, name): next.rooms[key] = name
        case let .closed(key):
            next.rooms[key] = nil
            next.threads[key] = nil
        case let .said(key, text): next.transcript.append("\(state.rooms[key] ?? "?"): \(text)")
        case let .bound(key, thread):
            for (other, bound) in state.threads where other != key && bound == thread {
                next.rooms[other] = nil
                next.threads[other] = nil
            }
            next.threads[key] = thread
        case let .topicSet(topic): next.topic = topic
        }
        return next
    }

    func present(_ input: PresenterInput<Lobby>) -> Presentation {
        Presentation(transcript: input.state.transcript, rooms: input.children)
    }

    func children(in state: State) -> [ChildKey: Room] {
        state.rooms.mapValues { Room(name: $0, topic: state.topic, gates: gates) }
    }

    func event(fromChild key: ChildKey, _ event: Room.Event) -> Event? {
        switch event {
        case let .spoke(text): .said(key, text)
        case let .bound(thread): .bound(key, thread)
        default: nil
        }
    }
}

struct Room: Controller, Equatable {
    enum Command: Codable, Equatable, Sendable {
        case say(String)
        case wait(String)
        case bind(String)
    }

    enum Event: Codable, Equatable, Sendable {
        case spoke(String)
        case linked(Int)
        case asked(String)
        case answered(String)
        case bound(String)
    }

    struct State: Codable, Equatable, Sendable {
        var spoken: [String] = []
        var link: Int?
        var answers: [String] = []
        var thread: String?
    }

    struct Presentation: Codable, Equatable, Sendable {
        var name: String
        var topic: String
        var spoken: [String]
        var link: Int?
        var answers: [String]
    }

    enum Effect: Equatable, Sendable {
        case wait(String)
    }

    let name: String
    let topic: String
    let gates: Gates

    static func == (lhs: Room, rhs: Room) -> Bool {
        lhs.name == rhs.name && lhs.topic == rhs.topic && lhs.gates === rhs.gates
    }

    var initialState: State { State() }

    func events(for command: Command, in context: Enrichment<Room>) -> [Event] {
        switch command {
        case let .say(text): [.spoke(text)]
        case let .wait(gate): [.asked(gate)]
        case let .bind(thread): [.bound(thread)]
        }
    }

    func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        switch event {
        case let .spoke(text): next.spoken.append(text)
        case let .linked(generation): next.link = generation
        case .asked: break
        case let .answered(gate): next.answers.append(gate)
        case let .bound(thread): next.thread = thread
        }
        return next
    }

    func present(_ input: PresenterInput<Room>) -> Presentation {
        Presentation(name: name, topic: topic, spoken: input.state.spoken, link: input.state.link, answers: input.state.answers)
    }

    func event(forLink link: LinkSnapshot) -> Event? { .linked(link.generation) }

    func effects(of event: Event, on state: State, presented presentation: Presentation) -> [Effect] {
        if case let .asked(gate) = event { return [.wait(gate)] }
        return []
    }

    func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        switch effect {
        case let .wait(gate):
            let gates = gates
            return .once {
                await gates.wait(gate)
                return .answered(gate)
            }
        }
    }
}

extension Lobby: LinkedApp {
    static func == (lhs: Lobby, rhs: Lobby) -> Bool { lhs.gates === rhs.gates }
}

/// A controller whose presentation fails to encode while `broken`: what the
/// bridge is told rather than nothing.
struct Fragile: Controller {
    enum Command: Codable, Equatable, Sendable { case set(Bool) }
    typealias Event = Command

    struct State: Codable, Equatable, Sendable {
        var broken = false

        init(broken: Bool = false) {
            self.broken = broken
        }

        init(from decoder: any Decoder) throws {
            broken = try decoder.singleValueContainer().decode(Bool.self)
        }

        func encode(to encoder: any Encoder) throws {
            if broken { throw EncodingError.invalidValue(self, .init(codingPath: [], debugDescription: "broken on purpose")) }
            var container = encoder.singleValueContainer()
            try container.encode(broken)
        }
    }

    typealias Presentation = State

    var initialState: State { State() }

    func events(for command: Command, in context: Enrichment<Fragile>) -> [Event] { [command] }

    func apply(_ event: Event, to state: State) throws -> State {
        guard case let .set(broken) = event else { return state }
        return State(broken: broken)
    }

    func present(_ input: PresenterInput<Fragile>) -> State { input.state }
}
