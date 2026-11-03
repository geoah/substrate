import Foundation

/// One controller as it runs: its state, its transient state, its
/// presentation, its journal, its effects in flight and its children.
/// Read and written only on its runtime's executor (every entry is a
/// `Runtime` method or a `LoopServices.hop`), which is what makes the
/// unchecked `Sendable` hold: an effect's task carries it back there and
/// touches nothing on the way.
final class Host<C: Controller>: @unchecked Sendable {
    let controller: C
    let path: [ChildKey]
    unowned let loop: any LoopServices
    let journal: (any JournalStore<C.Event, C.State>)?

    private(set) var state: C.State
    private(set) var transient: C.Transient
    private(set) var presentation: C.Presentation?
    private(set) var children: [ChildKey: Host<C.Child>] = [:]
    private(set) var closed = false
    /// Moved by `close` and `reset`: a result carrying an older one is
    /// dropped, whatever its key.
    private(set) var generation = 0
    private(set) var journalProblem: String?
    /// A child's applied event, offered to its parent to fold.
    var offerUp: ((C.Event) -> Void)?

    private var journaled: [C.Event] = []
    private var dirty = true
    private var epochs: [String: Int] = [:]
    private var running: [Int: Task<Void, Never>] = [:]
    private var runningKeys: [String: Int] = [:]
    private var nextTicket = 0
    private var tick: Task<Void, Never>?
    private var tickAt: Date?

    init(controller: C, path: [ChildKey], loop: any LoopServices, journal: (any JournalStore<C.Event, C.State>)?) {
        self.controller = controller
        self.path = path
        self.loop = loop
        self.journal = journal
        state = controller.initialState
        transient = controller.initialTransient
    }

    // MARK: Opening

    /// Folds the journal, if there is one, through the engine alone: no
    /// enrichment, no effect, no parent. Then makes the children the state
    /// keeps, each folding its own.
    func open() {
        if let journal { replay(journal) }
        reconcile(live: false)
    }

    private func replay(_ journal: any JournalStore<C.Event, C.State>) {
        let stored: StoredJournal<C.Event, C.State>
        do {
            guard let read = try journal.read() else { return }
            stored = read
        } catch is JournalUnavailable {
            // The device could not read it (a lock, an I/O error): it stays
            // where it is for the next launch.
            journalProblem = "The journal could not be read."
            return
        } catch {
            setAside(journal, because: error)
            return
        }
        do {
            var folded = controller.initialState
            for event in stored.events {
                folded = try controller.apply(event, to: folded)
            }
            journaled = stored.events
            state = folded
            // The fold is the truth and the stored state its cache: a cache
            // that differs is written again, best effort.
            if stored.cachedState != folded { try? journal.write(events: journaled, state: folded) }
            loop.trace(.replayed(path: path, events: stored.events.count))
        } catch {
            setAside(journal, because: error)
        }
    }

    private func setAside(_ journal: any JournalStore<C.Event, C.State>, because error: any Error) {
        do {
            let place = try journal.setAside(at: loop.environment.clock.now)
            journalProblem = "The journal could not be folded and was kept aside as \(place): \(error)"
            loop.trace(.setAside(path: path, place: place))
        } catch {
            journalProblem = "The journal could not be folded or kept aside: \(error)"
        }
    }

    // MARK: The loop

    func send(_ command: C.Command) {
        guard !closed else { return }
        let environment = loop.environment
        let context = Enrichment<C>(state: state, transient: transient, presentation: presentation, now: environment.clock.now,
                                    link: loop.link, environment: environment)
        step(controller.events(for: command, in: context))
    }

    /// Each event in turn: the engine, the journal, the transient state, the
    /// children, and the presentation and effects requested for after. The
    /// first refusal stops the rest.
    func step(_ events: [C.Event]) {
        for event in events {
            guard !closed else { return }
            let next: C.State
            do {
                next = try controller.apply(event, to: state)
            } catch {
                transient = controller.refusing(event, because: error, in: transient)
                dirty = true
                loop.trace(.refused(path: path, event: event, reason: String(describing: error)))
                loop.requestPresent(.now)
                return
            }
            record(event, state: next)
            state = next
            transient = controller.noting(event, applied: next, in: transient)
            dirty = true
            reconcile(live: true)
            let generation = generation
            loop.afterPresent { [self] in effects(of: event, on: next, generation: generation) }
            loop.requestPresent(controller.coalescing(event))
            offerUp?(event)
        }
    }

    /// The journal, written synchronously: nothing between the engine and
    /// here may suspend, so a process killed after this line still has the
    /// event.
    private func record(_ event: C.Event, state next: C.State) {
        let journaling = controller.journaling(event)
        guard let journal, journaling != .skip else { return }
        do {
            switch journaling {
            case .skip:
                return
            case .append:
                journaled.append(event)
                try journal.write(events: journaled, state: next)
            case .restart:
                journaled = [event]
                try journal.write(events: journaled, state: next)
            case .clear:
                journaled = []
                try journal.delete()
            }
            journalProblem = nil
            loop.trace(.journaled(path: path, journaling, event: event))
        } catch {
            journalProblem = "The journal could not be written: \(error)"
        }
    }

    private func effects(of event: C.Event, on state: C.State, generation: Int) {
        guard !closed, generation == self.generation, let presentation else { return }
        for effect in controller.effects(of: event, on: state, presented: presentation) {
            loop.trace(.effect(path: path, effect: effect))
            if loop.performsEffects { start(effect) }
        }
    }

    private struct Ticket: Sendable {
        var id: Int
        var key: String?
        var epoch: Int
        var generation: Int
    }

    /// Starts an effect's work. Under a key, the one running there is
    /// cancelled first and the key's epoch moves, so its result is dropped
    /// if it comes anyway.
    private func start(_ effect: C.Effect) {
        let key = controller.key(of: effect)
        if let key {
            if let id = runningKeys.removeValue(forKey: key) { running.removeValue(forKey: id)?.cancel() }
            epochs[key, default: 0] += 1
        }
        let work = controller.perform(effect, in: loop.environment)
        nextTicket += 1
        let ticket = Ticket(id: nextTicket, key: key, epoch: key.flatMap { epochs[$0] } ?? 0, generation: generation)
        let hop = loop.hop
        // Held until the work ends, so a result for a controller closed
        // meanwhile still reaches it, to be counted and dropped.
        let host = self
        let task: Task<Void, Never>
        switch work {
        case .none, .cancel:
            return
        case let .once(body):
            task = Task {
                let event = await body()
                await hop { host.finish(ticket, delivering: event) }
            }
        case let .sequence(make):
            task = Task {
                for await event in make() {
                    if Task.isCancelled { break }
                    await hop { host.receive(event, ticket) }
                }
                await hop { host.finish(ticket, delivering: nil) }
            }
        }
        running[ticket.id] = task
        if let key { runningKeys[key] = ticket.id }
    }

    private func finish(_ ticket: Ticket, delivering event: C.Event?) {
        running[ticket.id] = nil
        if let key = ticket.key, runningKeys[key] == ticket.id { runningKeys[key] = nil }
        if let event { receive(event, ticket) }
    }

    private func receive(_ event: C.Event, _ ticket: Ticket) {
        guard !closed, ticket.generation == generation else {
            loop.count(delivered: false, drop: .generation)
            loop.trace(.dropped(path: path, .generation))
            return
        }
        if let key = ticket.key, epochs[key] != ticket.epoch {
            loop.count(delivered: false, drop: .epoch)
            loop.trace(.dropped(path: path, .epoch))
            return
        }
        loop.count(delivered: true, drop: nil)
        step([event])
    }

    // MARK: Presenting

    /// Presents the children whose state moved, then this controller if it
    /// or any child moved. Answers whether the presentation changed.
    @discardableResult
    func present() -> Bool {
        var childChanged = false
        for key in children.keys.sorted() {
            if children[key]?.present() == true { childChanged = true }
        }
        guard dirty || childChanged || presentation == nil else { return false }
        dirty = false
        let environment = loop.environment
        let input = PresenterInput<C>(state: state, transient: transient, children: children.compactMapValues(\.presentation),
                                      now: environment.clock.now, locale: environment.locale, timeZone: environment.timeZone,
                                      link: loop.link, journalProblem: journalProblem)
        let next = controller.present(input)
        let changed = next != presentation
        presentation = next
        if changed { loop.trace(.presented(path: path)) }
        schedule(tickAt: controller.nextChange(of: next))
        return changed
    }

    /// One wake-up at the presentation's next change, replaced when the
    /// presentation names another instant.
    private func schedule(tickAt at: Date?) {
        guard at != tickAt else { return }
        tick?.cancel()
        tick = nil
        tickAt = at
        guard let at, !closed else { return }
        let generation = generation
        let clock = loop.environment.clock
        let hop = loop.hop
        let host = Weak(self)
        tick = Task {
            do { try await clock.sleep(until: at) } catch { return }
            await hop { host.target?.fire(at, generation: generation) }
        }
    }

    private func fire(_ at: Date, generation: Int) {
        guard !closed, generation == self.generation, tickAt == at else { return }
        tickAt = nil
        tick = nil
        if let event = controller.tick(at: at) {
            step([event])
        } else {
            // The presenter reads the instant: drawn again, it says what
            // is true now.
            dirty = true
            loop.requestPresent(.now)
        }
    }

    // MARK: Composition

    func linkChanged(_ link: LinkSnapshot) {
        guard !closed else { return }
        if let event = controller.linkChanged(link) {
            step([event])
        } else {
            dirty = true
            loop.requestPresent(.now)
        }
        for key in children.keys.sorted() {
            children[key]?.linkChanged(link)
        }
    }

    /// Makes the children the state keeps and closes the ones it dropped. A
    /// child made on the live path is told the link at once.
    private func reconcile(live: Bool) {
        let wanted = controller.children(in: state)
        for key in children.keys.sorted() where wanted[key] == nil {
            children.removeValue(forKey: key)?.close()
            dirty = true
        }
        for key in wanted.keys.sorted() where children[key] == nil {
            guard let child = wanted[key] else { continue }
            let host = Host<C.Child>(controller: child, path: path + [key], loop: loop, journal: child.journal(in: loop.environment))
            host.offerUp = { [unowned self] event in
                guard let up = controller.event(fromChild: key, event) else { return }
                step([up])
            }
            children[key] = host
            host.open()
            if live, let link = loop.link { host.linkChanged(link) }
            dirty = true
        }
    }

    func child(_ key: ChildKey) -> Host<C.Child>? { children[key] }

    func route(_ path: ArraySlice<ChildKey>, command data: Data) throws {
        guard let first = path.first else {
            let command: C.Command
            do {
                command = try JSONDecoder().decode(C.Command.self, from: data)
            } catch {
                throw UnroutableCommand(path: self.path, reason: "The command did not decode: \(error)")
            }
            send(command)
            return
        }
        guard let child = children[first] else {
            throw UnroutableCommand(path: self.path + [first], reason: "No child is kept under \(first).")
        }
        try child.route(path.dropFirst(), command: data)
    }

    // MARK: Ending

    /// Closes the controller and every child under it: their effects are
    /// cancelled, and a result still on its way is dropped.
    func close() {
        guard !closed else { return }
        closed = true
        generation += 1
        cancelAll()
    }

    /// Back to the initial state with no journal: every effect cancelled,
    /// every result still on its way dropped, every child closed.
    func reset() {
        generation += 1
        cancelAll()
        state = controller.initialState
        transient = controller.initialTransient
        journaled = []
        try? journal?.delete()
        journalProblem = nil
        dirty = true
        reconcile(live: true)
        loop.requestPresent(.now)
    }

    private func cancelAll() {
        for task in running.values { task.cancel() }
        running = [:]
        runningKeys = [:]
        tick?.cancel()
        tick = nil
        tickAt = nil
        for child in children.values { child.close() }
        children = [:]
    }
}
