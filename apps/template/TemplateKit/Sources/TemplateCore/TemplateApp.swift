import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// Substrate Template's root, kept by the link (`LinkController<TemplateApp>`):
/// its items are the kit's `LinkedRecords` over its kinds, read once,
/// followed through the watch, every change written through the outbox. Its
/// presentation is every screen of the app; the link's is the connection's,
/// beside it.
public struct TemplateApp: LinkedApp {
    public enum Command: Codable, Equatable, Sendable {
        case add(String)
        case delete(String)
        case refresh
        /// The app came to the front: what the system may have changed is
        /// read again.
        case foreground
        case clearSaved
        case dismissProblem
    }

    public enum Event: Codable, Equatable, Sendable {
        case records(LinkedRecords.Event)
        case opening
        case problemDismissed
    }

    public enum Effect: Equatable, Sendable {
        case records(LinkedRecords.Effect)
    }

    public struct State: Codable, Equatable, Sendable {
        public var linked = LinkedRecordsState()
        public var problem: String?

        public init() {}
    }

    public static let appName = "Substrate Template"

    public let linked: LinkedRecords
    public let build: BuildFacts

    public init(cache: RecordCache?, outbox: Outbox?, device: String, build: BuildFacts) {
        linked = LinkedRecords(kinds: TemplateKinds.all, cache: cache, outbox: outbox, words: Self.words(device: device))
        self.build = build
    }

    /// The Status screen's words, and the linked records'.
    public static func words(device: String) -> StatusWords {
        StatusWords(app: appName, device: device, kept: "items", packageNames: [TemplateKinds.packageID: "Template"])
    }

    public var initialState: State { State() }

    public func event(atOpen replayed: Bool) -> Event? { .opening }

    public func event(forLink link: LinkSnapshot) -> Event? { .records(.linked(link)) }

    public func report(of event: Event) -> LinkReport? {
        guard case let .records(event) = event else { return nil }
        return linked.report(of: event)
    }

    public func coalescing(of event: Event) -> Coalescing {
        guard case let .records(event) = event else { return .now }
        return linked.coalescing(of: event)
    }

    // MARK: Events

    public func events(for command: Command, in context: Enrichment<TemplateApp>) -> [Event] {
        let writes = RecordWrites(mint: { context.mint() })
        switch command {
        case let .add(name):
            let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !name.isEmpty else { return [] }
            return [.records(.wrote(writes.create(TemplateKinds.item, writes.newID(), properties: ["name": .string(name)])))]
        case let .delete(id):
            guard let record = context.state.linked.records[RecordFold.key(TemplateKinds.item, id)] else { return [] }
            return [.records(.wrote(writes.delete(TemplateKinds.item, id, version: record.version > 0 ? record.version : nil)))]
        case .refresh: return [.records(.refreshAsked)]
        case .foreground: return [.records(.foregrounded)]
        case .clearSaved: return [.records(.clearAsked)]
        case .dismissProblem: return [.problemDismissed]
        }
    }

    // MARK: The engine

    public func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        switch event {
        case let .records(event):
            next.linked = linked.apply(event, to: state.linked)
            if case let .notQueued(_, words) = event { next.problem = words }
        case .opening:
            break
        case .problemDismissed:
            next.problem = nil
        }
        return next
    }

    // MARK: The effects

    public func effects(of event: Event, on state: State, from previous: State, presented presentation: TemplatePresentation) -> [Effect] {
        switch event {
        case .opening: linked.openEffects.map(Effect.records)
        case let .records(event): linked.effects(of: event, on: state.linked, from: previous.linked).map(Effect.records)
        case .problemDismissed: []
        }
    }

    public func key(of effect: Effect) -> String? {
        switch effect {
        case let .records(effect): linked.key(of: effect)
        }
    }

    public func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        switch effect {
        case let .records(effect): linked.perform(effect, in: environment).map(Event.records)
        }
    }

    // MARK: The presenter

    public func present(_ input: PresenterInput<TemplateApp>) -> TemplatePresentation {
        TemplatePresenter.present(input.state, linked: linked, build: build)
    }
}
