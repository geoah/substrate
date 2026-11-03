import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import WorkoutsCore

/// What the app's events ask for, and the work: the files on the phone and
/// the signed-in repository's, through its client. Every write a sync step
/// makes is into the folder of the account it names, and its result is
/// dropped by the engine when that account is no longer signed in.
extension WorkoutsApp {
    public func effects(of event: Event, on state: State, presented presentation: WorkoutsPresentation) -> [Effect] {
        let account = state.signedIn
        switch event {
        case .opened:
            return [.loadBundled, .loadHistory]
        case .linked:
            if state.accountMoved {
                // A new account: its folder opened and its outbox followed;
                // it syncs once its package is known to be writable.
                return [.stopSync] + (account.map { [.openAccount($0), .followOutbox($0)] } ?? [])
            }
            return state.becameWritable ? account.map { [.sync($0)] } ?? [] : []
        case let .accountOpened(opened, _, _, _, _, _):
            return opened == account && state.canWrite ? [.sync(opened)] : []
        case let .catalogRead(read, _, _, _):
            guard read == account, state.historyLoaded else { return [] }
            return [exchange(read, state)]
        case .historyLoaded:
            guard let account, state.awaitingHistory else { return [] }
            return [exchange(account, state)]
        case let .exchanged(exchanged, _, _, _, _):
            return exchanged == account ? [.seedLibrary(exchanged)] : []
        case let .outboxRead(read, _, _):
            return read == account && !state.newConflicts.isEmpty ? [.reread(read, state.newConflicts)] : []
        case let .session(event):
            guard let account else { return [] }
            switch event {
            case let .finished(finished):
                // A package not yet writable takes nothing: the next sync's
                // exchange sends the session the server never held.
                return state.canWrite ? [.handOff(account, .finished(finished.entry.records))] : []
            case let .discard(_, session, _):
                return [.handOff(account, .discarded(session))]
            case let .engine(envelope):
                switch envelope.event {
                case let .addExercise(added) where added.exercise.isLocal:
                    return [.handOff(account, .exerciseAdded(added.exercise))]
                case let .replaceExercise(replaced) where replaced.exercise.isLocal:
                    return [.handOff(account, .exerciseAdded(replaced.exercise))]
                default:
                    return []
                }
            default:
                return []
            }
        case let .queued(command):
            return effects(of: command, on: state, account: account)
        default:
            return []
        }
    }

    private func exchange(_ account: AccountRef, _ state: State) -> Effect {
        .exchange(account, local: state.history, hidden: state.hidden, exercises: Array((state.catalog ?? Catalog()).exercises.values))
    }

    private func effects(of command: Command, on state: State, account: AccountRef?) -> [Effect] {
        switch command {
        case .sync:
            return account.map { state.canWrite ? [.sync($0)] : [] } ?? []
        case .addSampleRoutine:
            guard let account, state.addingSample else { return [] }
            return [.addSample(account)]
        case let .deleteRoutine(id):
            return account.map { [.deleteRoutine($0, id)] } ?? []
        case let .saveRoutine(editor, receipt):
            guard let account else { return [.answer(receipt, "the app is not signed in to your substrate.")] }
            if state.conflicts.contains(where: { $0.recordID == editor.routine.id.rawValue }) {
                return [.answer(receipt, WorkoutsPresentation.conflictFirst)]
            }
            return [.saveRoutine(account, editor, receipt: receipt)]
        case let .keepDraft(editor):
            return account.map { [.keepDraft($0, editor)] } ?? []
        case .dropDraft:
            return account.map { [.dropDraft($0)] } ?? []
        case let .resolve(id, keepMine):
            return account.map { [.resolve($0, id, keepMine: keepMine)] } ?? []
        case let .deleteSession(id, receipt):
            return [.deleteSession(account, id, receipt: receipt)]
        case let .importSessions(sessions, exercises, receipt):
            let entries = sessions.map { HistoryEntry(records: $0, local: HistoryLocal(health: HealthSaveState(status: .notAvailable))) }
            return [.importSessions(entries, exercises: exercises, origin: state.origin, receipt: receipt)]
        case let .importRecords(routines, measurements, exercises, receipt):
            guard let account else { return [.answer(receipt, "the app is not signed in to your substrate.")] }
            return [.importRecords(account, routines: routines, measurements: measurements, exercises: exercises, receipt: receipt)]
        case let .addLocalExercises(exercises, receipt):
            return [.addLocalExercises(exercises, origin: state.origin, receipt: receipt)]
        case let .healthLinked(id, workout, revision):
            return account.map { [.handOff($0, .healthLinked(id, workout: workout, revision: revision))] } ?? []
        case let .exerciseAdded(exercise):
            return account.map { [.handOff($0, .exerciseAdded(exercise))] } ?? []
        case .settings, .historyEntryChanged:
            return []
        }
    }

    /// One sync at a time, stopped when the account changes; one outbox
    /// followed; the rest run to their end.
    public func key(of effect: Effect) -> String? {
        switch effect {
        case .sync, .exchange, .seedLibrary, .stopSync: "sync"
        case .followOutbox: "outbox"
        case .openAccount: "account"
        default: nil
        }
    }

    // MARK: The work

    public func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        let accounts = environment.accounts
        let client = environment.client
        let directory = directory
        let codec = codec
        @Sendable func api() async -> WorkoutsAPI? { await client.current().map { WorkoutsAPI(client: $0) } }
        @Sendable func folder(_ ref: AccountRef) -> WorkoutsAccount? { accounts?.account(ref) }

        switch effect {
        case .loadBundled:
            return .once {
                let source = BundledCatalog(localExercises: directory.map { LocalExercises(directory: $0) })
                do {
                    return .bundledLoaded(try source.loadNow(), failure: nil)
                } catch {
                    return .bundledLoaded(Catalog(), failure: String(describing: error))
                }
            }
        case .loadHistory:
            guard let directory else { return .none }
            return .once {
                let load = HistoryStore(directory: directory).loadAll()
                let phone = SessionTombstones(directory: directory)
                return .historyLoaded(load.entries, unreadable: load.unreadable + (phone.unreadable.map { [$0] } ?? []), hidden: phone.all)
            }
        case let .openAccount(ref):
            guard let account = folder(ref) else { return .none }
            return .once {
                var unreadable: [UnreadableRecord] = []
                if let record = await account.outbox?.unreadableRecord { unreadable.append(record) }
                if let record = account.tombstones.unreadable { unreadable.append(record) }
                var draft: RoutineEditor?
                do {
                    draft = try account.drafts.load()
                } catch let error as UnreadableDraft {
                    unreadable.append(error.record)
                } catch {}
                let cached = account.remote.hasCache ? try? account.remote.loadNow() : nil
                return .accountOpened(ref, cached: cached, draft: draft, sample: account.sample.state, unreadable: unreadable,
                                      hidden: account.tombstones.all)
            }
        case let .followOutbox(ref):
            guard let outbox = folder(ref)?.outbox else { return .none }
            return .sequence {
                AsyncStream { continuation in
                    let task = Task {
                        for await _ in await outbox.changes() {
                            let entries = await outbox.entries
                            let waiting = Set(entries.filter { $0.kind == codec.kinds.routine && $0.batch == nil && !$0.parked }
                                .map { RoutineID($0.recordID) })
                            let conflicts = entries.filter { $0.parked && $0.conflict && $0.kind == codec.kinds.routine }
                            continuation.yield(.outboxRead(ref, waitingRoutines: waiting, conflicts: conflicts))
                        }
                        continuation.finish()
                    }
                    continuation.onTermination = { _ in task.cancel() }
                }
            }
        case .stopSync:
            return .cancel
        case let .sync(ref):
            guard let account = folder(ref) else { return .none }
            return steps { yield in
                yield(.syncStarted(ref))
                guard let api = await api() else { return yield(.syncEnded(ref, problem: nil)) }
                do {
                    let read = try await CatalogSync.read(api, remote: account.remote, outbox: account.outbox, sample: account.sample,
                                                          bundled: { try BundledCatalog().loadNow() })
                    yield(.catalogRead(ref, read.catalog, sample: account.sample.state, sampleProblem: read.sampleProblem))
                } catch {
                    yield(Self.failed(error, ref))
                }
            }
        case let .exchange(ref, local, hidden, exercises):
            guard let account = folder(ref), let outbox = account.outbox else { return .none }
            return steps { yield in
                guard let api = await api() else { return yield(.syncEnded(ref, problem: nil)) }
                do {
                    yield(try await Self.exchange(api, account: account, outbox: outbox, local: local, hidden: hidden, exercises: exercises,
                                                  directory: directory, codec: codec))
                } catch {
                    yield(Self.failed(error, ref))
                }
            }
        case let .seedLibrary(ref):
            guard let account = folder(ref) else { return .none }
            return steps { yield in
                defer { yield(.syncEnded(ref, problem: nil)) }
                guard let api = await api(), let library = try? ExerciseLibrary.bundled() else { return }
                // Quiet like the coach: what it meets is tried again at the next sync.
                guard let outcome = try? await account.library.ensure(api, library: library), !outcome.current else { return }
                if let catalog = try? await Self.refresh(api, account: account, codec: codec) { yield(.catalogShown(ref, catalog)) }
            }
        case let .handOff(ref, handOff):
            guard let outbox = folder(ref)?.outbox else { return .none }
            let sink = OutboxSink<Never, Never>(outbox: outbox, codec: codec, onQueued: {})
            return steps { yield in
                switch handOff {
                case let .finished(records): await sink.finished(records)
                case let .discarded(id): await sink.discarded(id)
                case let .exerciseAdded(exercise): await sink.exerciseAdded(exercise)
                case let .healthLinked(id, workout, revision): await sink.healthLinked(id, healthKitWorkout: workout, revision: revision)
                }
            }
        case let .addSample(ref):
            guard let account = folder(ref) else { return .none }
            return steps { yield in
                guard let api = await api() else { return yield(.addingSample(false)) }
                do {
                    try await account.sample.addBack(api, remote: account.remote, outbox: account.outbox, bundled: BundledCatalog().loadNow())
                    yield(.sample(ref, account.sample.state, problem: nil))
                    yield(.catalogShown(ref, try await Self.refresh(api, account: account, codec: codec)))
                } catch {
                    yield(.addingSample(false))
                    yield(Self.failed(error, ref))
                }
            }
        case let .deleteRoutine(ref, id):
            guard let account = folder(ref), let outbox = account.outbox else { return .none }
            return steps { yield in
                do {
                    try await outbox.delete(routine: id, codec: codec)
                } catch {
                    return
                }
                // A cache that cannot be rewritten still lets the delete go;
                // the next read leaves the routine out until it lands.
                if account.remote.hasCache, let catalog = try? account.remote.remove(routine: id) { yield(.catalogShown(ref, catalog)) }
            }
        case let .saveRoutine(ref, edited, receipt):
            guard let account = folder(ref), let outbox = account.outbox else { return .none }
            return steps { yield in
                let editor = edited.finished
                do {
                    if editor.isNew {
                        try await outbox.enqueue([OutboxPlan.routineCreated(editor.routine, codec: codec)])
                    } else {
                        try await outbox.edit(routine: editor.routine.id, patch: editor.patch(codec: codec), baseVersion: editor.baseVersion,
                                              codec: codec)
                    }
                } catch {
                    return yield(.outcome(receipt, problem: error.localizedDescription))
                }
                try? account.drafts.clear()
                yield(.draft(nil))
                if let catalog = try? account.remote.store(routine: editor.routine) { yield(.catalogShown(ref, catalog)) }
                yield(.outcome(receipt, problem: nil))
            }
        case let .keepDraft(ref, editor):
            guard let account = folder(ref) else { return .none }
            return .once {
                try? account.drafts.save(editor)
                return nil
            }
        case let .dropDraft(ref):
            guard let account = folder(ref) else { return .none }
            return .once {
                try? account.drafts.clear()
                return nil
            }
        case let .resolve(ref, id, keepMine):
            guard let account = folder(ref), let outbox = account.outbox else { return .none }
            return steps { yield in
                let routine = await outbox.entries.first { $0.id == id }.map { RoutineID($0.recordID) }
                guard (try? await outbox.resolve(id, keepMine: keepMine)) != nil, let routine, let api = await api() else { return }
                // Use Theirs shows the routine as the server holds it at
                // once; Keep My Edit, once its write lands and nothing waits.
                if keepMine { return }
                if let catalog = await Self.reread([routine], api: api, account: account, codec: codec) { yield(.catalogShown(ref, catalog)) }
            }
        case let .reread(ref, ids):
            guard let account = folder(ref) else { return .none }
            return steps { yield in
                guard let api = await api() else { return }
                if let catalog = await Self.reread(ids, api: api, account: account, codec: codec) { yield(.catalogShown(ref, catalog)) }
            }
        case let .deleteSession(ref, id, receipt):
            guard let directory else { return .none }
            return steps { yield in
                // The tombstone is kept first, in the signed-in account's
                // folder or on the phone: without it a later sync could
                // import the session again, so a delete that cannot be
                // recorded is refused.
                do {
                    if let ref, let account = folder(ref) {
                        try account.tombstones.add(id)
                    } else {
                        try SessionTombstones(directory: directory).add(id)
                    }
                    try HistoryStore(directory: directory).delete(id)
                } catch {
                    return yield(.outcome(receipt, problem: error.localizedDescription))
                }
                yield(.sessionDeleted(id))
                yield(.outcome(receipt, problem: nil))
                if let ref, let outbox = folder(ref)?.outbox { try? await outbox.purge(session: id, codec: codec) }
            }
        case let .importSessions(entries, exercises, origin, receipt):
            guard let directory else { return .none }
            return steps { yield in
                if !exercises.isEmpty {
                    do {
                        try Self.addLocal(exercises, origin: origin, directory: directory)
                        yield(.exercisesAdded(exercises))
                    } catch {
                        return yield(.outcome(receipt, problem: error.localizedDescription))
                    }
                }
                let store = HistoryStore(directory: directory)
                var written: [HistoryEntry] = []
                var failure: String?
                for entry in entries {
                    do {
                        try store.write(entry)
                        written.append(entry)
                    } catch {
                        failure = error.localizedDescription
                        break
                    }
                }
                yield(.sessionsImported(written))
                yield(.outcome(receipt, problem: failure.map {
                    "\(written.count) of \(entries.count) workouts were imported; the rest could not be saved on this iPhone: \($0)"
                }))
            }
        case let .importRecords(ref, routines, measurements, exercises, receipt):
            guard let account = folder(ref), let outbox = account.outbox else { return .none }
            return steps { yield in
                let named = Set(routines.flatMap { $0.slots.map(\.exercise) })
                var entries = exercises.filter { named.contains($0.id) }.map { OutboxPlan.exerciseAdded($0, codec: codec) }
                entries += routines.map { OutboxPlan.routineCreated($0, codec: codec) }
                entries += measurements.map { OutboxEntry(method: .put, kind: $0.kind, recordID: $0.id, properties: $0.properties) }
                do {
                    try await outbox.enqueue(entries)
                } catch {
                    return yield(.outcome(receipt, problem: error.localizedDescription))
                }
                var catalog: Catalog?
                for routine in routines { catalog = try? account.remote.store(routine: routine) }
                if let catalog { yield(.catalogShown(ref, catalog)) }
                yield(.outcome(receipt, problem: nil))
            }
        case let .addLocalExercises(exercises, origin, receipt):
            guard let directory else {
                return steps { yield in
                    yield(.exercisesAdded(exercises))
                    if let receipt { yield(.outcome(receipt, problem: nil)) }
                }
            }
            return steps { yield in
                do {
                    try Self.addLocal(exercises, origin: origin, directory: directory)
                } catch {
                    if let receipt { yield(.outcome(receipt, problem: error.localizedDescription)) }
                    return
                }
                yield(.exercisesAdded(exercises))
                if let receipt { yield(.outcome(receipt, problem: nil)) }
            }
        case let .answer(receipt, problem):
            return .once { .outcome(receipt, problem: problem) }
        }
    }

    /// Work that reports as it goes: each result an event, in order.
    private func steps(_ body: @escaping @Sendable (_ yield: @escaping @Sendable (Event) -> Void) async -> Void) -> Work<Event> {
        .sequence {
            AsyncStream { continuation in
                let task = Task {
                    await body { continuation.yield($0) }
                    continuation.finish()
                }
                continuation.onTermination = { _ in task.cancel() }
            }
        }
    }

    /// A step's failure: the link's to act on when the token was refused,
    /// the sync's to say otherwise.
    static func failed(_ error: any Error, _ ref: AccountRef) -> Event {
        if let report = LinkReport(error) { return .refused(report) }
        if let server = error as? ServerError, server.status == 403 { return .refused(.needsApproval) }
        return .syncEnded(ref, problem: (error as? LocalizedError)?.errorDescription ?? String(describing: error))
    }

    /// The phone's own exercises, kept on disk in one write, all or none.
    static func addLocal(_ exercises: [Exercise], origin: CatalogOrigin, directory: URL) throws {
        let local = exercises.map { exercise in
            var local = exercise
            local.isLocal = true
            return local
        }
        try LocalExercises(directory: directory).add(contentsOf: local)
    }

    /// The routines read again into the account's cache, laying what waits over them.
    static func refresh(_ api: WorkoutsAPI, account: WorkoutsAccount, codec: RecordCodec) async throws -> Catalog {
        let outbox = account.outbox
        return try await account.remote.refresh(api, hiding: { await outbox?.waitingDeletes ?? [] },
                                                waiting: { await outbox?.waitingRoutines(codec: codec) ?? [:] })
    }

    /// Routines read again from the server, one by one, into the cache.
    static func reread(_ ids: [RoutineID], api: WorkoutsAPI, account: WorkoutsAccount, codec: RecordCodec) async -> Catalog? {
        var catalog: Catalog?
        for id in ids {
            guard let record = try? await api.get(codec.kinds.routine, id.rawValue) else { continue }
            catalog = (try? account.remote.store(record: record, removing: id)) ?? catalog
        }
        return catalog
    }

    /// The history exchange (`HistoryExchange`): sessions on the server the
    /// phone lacks are imported and server edits merged, sessions deleted on
    /// the server after this phone sent them are removed here, all of it in
    /// the history files first; sessions and exercises the server never held
    /// are queued, and sessions deleted on the phone purged there.
    static func exchange(_ api: WorkoutsAPI, account: WorkoutsAccount, outbox: Outbox, local: [HistoryEntry], hidden: Set<String>,
                         exercises: [Exercise], directory: URL?, codec: RecordCodec) async throws -> Event
    {
        let snapshot = try await ServerHistory(api: api, codec: codec).snapshot()
        let held = Set(try await api.list([codec.kinds.exercise]).map(\.id))
        let queued = Set(await outbox.entries.compactMap(\.batch))
        let versions = await outbox.versions
        let plan = HistoryExchange.plan(server: snapshot, local: local.map(\.records), queued: queued, versions: versions,
                                        tombstones: account.tombstones.all, hidden: hidden, codec: codec)
        var imports: [HistoryEntry] = []
        var merges: [HistoryEntry] = []
        var removals: [SessionID] = []
        if let directory {
            let store = HistoryStore(directory: directory)
            let have = Set(local.map(\.id))
            for records in plan.imports where !have.contains(records.id) {
                let health = HealthSaveState(status: records.session.healthKitWorkout == nil ? .notAvailable : .saved)
                let entry = HistoryEntry(records: records, local: HistoryLocal(health: health))
                if (try? store.write(entry)) != nil { imports.append(entry) }
            }
            let locals = Dictionary(local.map { ($0.id, $0.local) }, uniquingKeysWith: { first, _ in first })
            for records in plan.merges {
                guard let kept = locals[records.id] else { continue }
                let entry = HistoryEntry(records: records, local: kept)
                if (try? store.write(entry)) != nil { merges.append(entry) }
            }
            let phone = SessionTombstones(directory: directory)
            for id in plan.removals {
                // An imported one is also kept from the next import of the
                // same account or file, as a delete on this phone is.
                if id.rawValue.hasPrefix("hevy-") { try? phone.add(id) }
                if (try? store.delete(id)) != nil { removals.append(id) }
            }
        }
        try await outbox.saw(plan.seen)
        for id in plan.removals { try await outbox.forget(codec.kinds.session, id.rawValue) }
        var entries: [OutboxEntry] = []
        for exercise in exercises.sorted(by: { $0.id.rawValue < $1.id.rawValue }) where exercise.isLocal && !held.contains(exercise.id.rawValue) {
            entries.append(OutboxPlan.exerciseAdded(exercise, codec: codec))
        }
        // The exercises a session to upload names that the repository
        // lacks: the sample's, for a session logged signed out.
        let queuedExercises = Set(entries.map(\.recordID))
        let bundled = (try? BundledCatalog().loadNow())?.exercises ?? [:]
        let known = bundled.merging(Dictionary(exercises.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })) { _, phone in phone }
        entries += OutboxPlan.exercisesFor(plan.uploads, server: held.union(queuedExercises), known: known, codec: codec)
        for records in plan.uploads.sorted(by: { $0.session.at < $1.session.at }) {
            entries += OutboxPlan.finished(records, codec: codec)
        }
        entries += plan.finishes
        entries += plan.purges.map { OutboxPlan.deleted($0, codec: codec) }
        if !entries.isEmpty { try await outbox.enqueue(entries) }
        return .exchanged(account.ref, imports: imports, merges: merges, removals: removals, unreadable: plan.unreadable)
    }
}
