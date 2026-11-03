import Foundation
import SubstrateRuntime

/// A parent that hands its child a value it keeps changing and learns what
/// the child holds, and that starts over on an event of its own: the three
/// rules Chat's list and its conversations needed beyond the first cut.
struct Desk: Controller {
    enum Command: Codable, Equatable, Sendable {
        case open
        case post(String)
        case wait(String)
        case clear
    }

    enum Event: Codable, Equatable, Sendable {
        case opened(ChildKey)
        case posted(String)
        case asked(String)
        case answered(String)
        case cleared
        case clerkBusy(ChildKey, Bool)
    }

    struct State: Codable, Equatable, Sendable {
        var clerks: [ChildKey] = []
        var notice = ""
        var answers: [String] = []
        var busy: Set<ChildKey> = []
    }

    struct Presentation: Codable, Equatable, Sendable {
        var answers: [String]
        var busy: [ChildKey]
        var clerks: [ChildKey: Clerk.State]
    }

    enum Effect: Equatable, Sendable {
        case wait(String)
    }

    let gates: Gates

    var initialState: State { State() }

    func events(for command: Command, in context: Enrichment<Desk>) -> [Event] {
        switch command {
        case .open: [.opened(ChildKey(context.mint().uuidString.lowercased()))]
        case let .post(notice): [.posted(notice)]
        case let .wait(gate): [.asked(gate)]
        case .clear: [.cleared]
        }
    }

    func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        switch event {
        case let .opened(key): next.clerks.append(key)
        case let .posted(notice): next.notice = notice
        case .asked: break
        case let .answered(gate): next.answers.append(gate)
        case .cleared: next = State()
        case let .clerkBusy(key, busy):
            if busy { next.busy.insert(key) } else { next.busy.remove(key) }
        }
        return next
    }

    func present(_ input: PresenterInput<Desk>) -> Presentation {
        Presentation(answers: input.state.answers, busy: input.state.busy.sorted(), clerks: input.children)
    }

    func resetting(of event: Event, on state: State) -> Bool { event == .cleared }

    func children(in state: State) -> [ChildKey: Clerk] {
        Dictionary(uniqueKeysWithValues: state.clerks.map { ($0, Clerk(notice: state.notice, gates: gates)) })
    }

    func event(fromChild key: ChildKey, _ event: Clerk.Event, after state: Clerk.State) -> Event? {
        .clerkBusy(key, state.waiting != nil)
    }

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

/// The child: it reads each notice its parent hands it and waits on a gate
/// named for it, so a new value starts work of its own.
struct Clerk: Controller, Equatable {
    typealias Command = Never

    enum Event: Codable, Equatable, Sendable {
        case noticed(String)
        case done(String)
    }

    struct State: Codable, Equatable, Sendable {
        var read: [String] = []
        var waiting: String?
    }

    typealias Presentation = State

    enum Effect: Equatable, Sendable {
        case wait(String)
    }

    let notice: String
    let gates: Gates

    static func == (lhs: Clerk, rhs: Clerk) -> Bool { lhs.notice == rhs.notice && lhs.gates === rhs.gates }

    var initialState: State { State() }

    func events(for command: Never, in context: Enrichment<Clerk>) -> [Event] {}

    func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        switch event {
        case let .noticed(notice):
            next.read.append(notice)
            next.waiting = notice
        case .done:
            next.waiting = nil
        }
        return next
    }

    func present(_ input: PresenterInput<Clerk>) -> State { input.state }

    func event(replacing previous: Clerk) -> Event? { notice == previous.notice ? nil : .noticed(notice) }

    func effects(of event: Event, on state: State, presented presentation: State) -> [Effect] {
        if case let .noticed(notice) = event { return [.wait(notice)] }
        return []
    }

    func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        switch effect {
        case let .wait(gate):
            let gates = gates
            return .once {
                await gates.wait(gate)
                return .done(gate)
            }
        }
    }
}
