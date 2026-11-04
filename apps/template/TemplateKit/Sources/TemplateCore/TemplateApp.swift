import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// Substrate Template's root, kept by the link (`LinkController<TemplateApp>`):
/// its items are the kit's `LinkedRecords` over its kinds, read once,
/// followed through the watch, every change written through the outbox. As
/// a `LinkedRecordsApp` it writes none of their stages: its engine, its
/// effects and their work are its own alone (here, Storage's sizes). Its
/// presentation is every screen of the app; the link's is the connection's,
/// beside it.
public struct TemplateApp: LinkedRecordsApp {
    public enum Command: Codable, Equatable, Sendable {
        case add(String)
        case delete(String)
        case refresh
        /// The app came to the front: what the system may have changed is
        /// read again.
        case foreground
        case clearSaved
        /// The updater found a newer build, or none.
        case offered(String?)
        case dismissProblem
    }

    public enum Event: Codable, Equatable, Sendable, LinkedRecordsEvent {
        case records(LinkedRecords.Event)
        case usage([AreaUsage])
        case offered(String?)
        case problemDismissed

        public var recordsEvent: LinkedRecords.Event? {
            if case let .records(event) = self { event } else { nil }
        }
    }

    public enum Effect: Equatable, Sendable, LinkedRecordsEffect {
        case records(LinkedRecords.Effect)
        case readUsage

        public var recordsEffect: LinkedRecords.Effect? {
            if case let .records(effect) = self { effect } else { nil }
        }
    }

    public struct State: Codable, Equatable, Sendable, LinkedRecordsHolding {
        public var linked = LinkedRecordsState()
        /// Each area of the repository's folder and its size, nil until read.
        public var usage: [AreaUsage]?
        public var problem: String?
        /// The newer build the updater offers, which About shows.
        public var offered: String?
        /// Moved each time the items may be drawn otherwise: the items'
        /// keyed list's revision, so the Android bridge sends a delta.
        public var revision = 0

        public init() {}
    }

    public static let appName = "Substrate Template"

    public let linked: LinkedRecords
    public let build: BuildFacts

    /// - Parameter words: the Status screen's words, as the shell made them
    ///   (`AppShell.Parts.words`, from `words(device:)`).
    public init(cache: RecordCache?, outbox: Outbox?, words: StatusWords, build: BuildFacts) {
        linked = LinkedRecords(kinds: TemplateKinds.all, cache: cache, outbox: outbox, words: words)
        self.build = build
    }

    /// The Status screen's words, and the linked records'.
    public static func words(device: String) -> StatusWords {
        StatusWords(app: appName, device: device, kept: "items", packageNames: [TemplateKinds.packageID: "Template"])
    }

    public var initialState: State { State() }

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
        case let .offered(version): return [.offered(version)]
        case .dismissProblem: return [.problemDismissed]
        }
    }

    // MARK: The engine

    /// After the records' own: `state.linked` has moved already.
    public func apply(_ event: Event, to state: State, from previous: State) throws -> State {
        var next = state
        switch event {
        case let .records(.notQueued(_, words)):
            next.problem = words
        case .records:
            break
        case let .usage(usage):
            next.usage = usage
        case let .offered(version):
            next.offered = version
        case .problemDismissed:
            next.problem = nil
        }
        if next.linked.records != previous.linked.records { next.revision += 1 }
        return next
    }

    // MARK: The effects

    /// Storage's sizes, read at open, when the app comes forward and after
    /// a clear.
    public func ownEffects(of event: Event, on state: State, from previous: State, presented presentation: TemplatePresentation) -> [Effect] {
        switch event {
        case .records(.opened), .records(.foregrounded), .records(.cleared): [.readUsage]
        default: []
        }
    }

    public func ownKey(of effect: Effect) -> String? {
        switch effect {
        case .records: nil
        case .readUsage: "usage"
        }
    }

    public func performOwn(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        switch effect {
        case .records:
            return .none
        case .readUsage:
            guard let folder = environment.folder else { return .none }
            return .once { .usage(folder.usage()) }
        }
    }

    // MARK: The presenter

    public func present(_ input: PresenterInput<TemplateApp>) -> TemplatePresentation {
        TemplatePresenter.present(input.state, linked: linked, build: build)
    }
}
