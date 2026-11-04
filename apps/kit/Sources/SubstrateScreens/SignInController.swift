import Foundation
import SubstrateCore
import SubstrateRuntime

/// The Connect screen as a root of its own, where the sign-in is a screen
/// apart from the link (Android's shell): `ConnectController`'s every
/// command, event and effect, and beside its presentation a count of the
/// sign-ins it finished. That count is a signal: the UI acts once when it
/// moves, opening the link again over the session just kept, as Apple's
/// `AppShell` does in the step that keeps it.
public struct SignInController: Controller {
    public typealias Command = ConnectController.Command
    public typealias Event = ConnectController.Event
    public typealias Effect = ConnectController.Effect
    public typealias Presentation = SignInPresentation

    public struct State: Codable, Equatable, Sendable {
        public var connect: ConnectState
        public var signedIn = 0
    }

    public let connect: ConnectController

    public init(_ connect: ConnectController) {
        self.connect = connect
    }

    public var initialState: State { State(connect: connect.initialState) }

    public func events(for command: Command, in context: Enrichment<SignInController>) -> [Event] {
        connect.events(
            for: command,
            in: Enrichment(
                state: context.state.connect, transient: NoTransient(),
                presentation: context.presentation?.connect, now: context.now, link: context.link,
                environment: context.environment))
    }

    public func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        next.connect = try connect.apply(event, to: state.connect)
        if event == .signedIn { next.signedIn += 1 }
        return next
    }

    public func effects(of event: Event, on state: State, presented presentation: SignInPresentation) -> [Effect] {
        connect.effects(of: event, on: state.connect, presented: presentation.connect)
    }

    public func key(of effect: Effect) -> String? { connect.key(of: effect) }

    public func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        connect.perform(effect, in: environment)
    }

    public func present(_ input: PresenterInput<SignInController>) -> SignInPresentation {
        SignInPresentation(
            connect: connect.present(
                PresenterInput(
                    state: input.state.connect, transient: NoTransient(), now: input.now,
                    locale: input.locale, timeZone: input.timeZone, link: input.link)),
            signedIn: input.state.signedIn)
    }
}

/// The Connect screen, and how many sign-ins it finished: a signal the UI
/// acts on once each time it moves.
public struct SignInPresentation: Bridged, Equatable {
    public var connect: ConnectPresentation
    public var signedIn: Int
}
