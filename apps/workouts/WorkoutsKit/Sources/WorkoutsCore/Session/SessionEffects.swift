import Foundation
import SubstrateDevice
import SubstrateRuntime

/// What an applied event asks of the device, decided from the state and the
/// presentation it made, and run on the live path alone: a replayed journal
/// schedules no alert and moves no Live Activity, and the open's own event
/// restores them.
extension SessionController {
    public func effects(of event: Event, on state: State, presented presentation: SessionScreenPresentation) -> [Effect] {
        switch event {
        case .engine:
            return sync(state, presentation).map { [.countdown($0)] } ?? []
        case let .discard(_, session, then):
            return [.countdownEnded(session, final: nil)] + (then.map { [.prepareStart($0)] } ?? [])
        case let .finishRequested(_, finished):
            return [.writeHistory(finished)]
        case let .finished(finished):
            let summary = SessionPresenter.finishedSummary(finished.entry.records)
            let final = finished.activity.map { LiveActivityPayload(content: $0, revision: 0, finishedSummary: summary) }
            // The first Finish may show the Health permission sheet, so it
            // waits for the cover; over a cover that opened again for the
            // next session it saves without asking.
            return [.countdownEnded(finished.id, final: final), .health(.afterFinish(finished.then == nil ? .finish : .finishCovered))]
                + (finished.then.map { [.prepareStart($0)] } ?? [])
        case let .opened(replayed):
            var effects: [Effect] = [.registerCategory, .restore(sync(state, presentation)), .readAlerts, .listSetAside]
            if replayed, let id = state.session?.session.id { effects.append(.checkHistory(id)) }
            return effects
        case let .alreadyFinished(id, recoverHealth):
            return [.restore(nil)] + (recoverHealth ? [.health(.recover(id))] : [])
        case .foregrounded:
            return [.restore(sync(state, presentation)), .readAlerts, .health(.foreground)]
        case let .starting(request):
            return [.prepareStart(request)]
        case .noted(.coverVisible):
            // In front of the cover the alert stays quiet; away from it, it
            // shows: the one scheduled is replaced with the other.
            return sync(state, presentation).map { [.countdown($0)] } ?? []
        case .finishFailed, .finishRefused, .startDropped, .tick, .alertsRead, .setAsideFound, .noted:
            return []
        }
    }

    /// The alert and the Live Activity as the presentation says them; nil
    /// with no session open.
    func sync(_ state: State, _ presentation: SessionScreenPresentation) -> CountdownSync? {
        guard let session = state.session, let presented = presentation.session else { return nil }
        return CountdownSync(session: session.session.id, active: session.phase == .active, title: presented.title,
                             notification: presented.notification, quietInFront: presentation.coverVisible,
                             activity: LiveActivityPayload(content: presented.liveActivity, revision: state.revision))
    }

    /// The alert and the activity follow the newest change: one under way
    /// is stopped between its two steps, and ActivityKit drops an update
    /// older than one it has. A Start is one at a time, and a history write
    /// is never cut short.
    public func key(of effect: Effect) -> String? {
        switch effect {
        case .countdown, .restore, .countdownEnded: "countdown"
        case .prepareStart: "start"
        case .readAlerts: "alerts"
        case .checkHistory: "history"
        case .writeHistory, .listSetAside, .health, .registerCategory: nil
        }
    }

    public func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        let notifications = environment.devices.notifications
        let activities = environment.devices.activities
        let workouts = environment.workouts
        let clock = environment.clock
        switch effect {
        case let .countdown(sync):
            return .once {
                await Self.apply(sync, notifications: notifications, activities: activities, at: clock.now)
                return nil
            }
        case let .restore(sync):
            return .once {
                guard let sync, sync.active else {
                    await notifications?.cancel(Self.countdownID)
                    await activities?.endAll(except: nil)
                    return nil
                }
                await Self.apply(sync, notifications: notifications, activities: activities, at: clock.now)
                await activities?.endAll(except: sync.session.rawValue)
                return nil
            }
        case let .countdownEnded(session, final):
            return .once {
                await notifications?.cancel(Self.countdownID)
                let at = clock.now
                await activities?.end(session.rawValue, final: final?.encoded(),
                                      dismissAt: final == nil ? nil : at.addingTimeInterval(Self.finishedDisplay))
                return nil
            }
        case let .prepareStart(request):
            let typed = settings.typedBodyMass
            return .sequence {
                AsyncStream { continuation in
                    let task = Task {
                        let bodyMass = await workouts?.bodyMass(typed: typed) ?? typed
                        // Asked at the first Start (§9.6), before the first alert is scheduled.
                        if let notifications { continuation.yield(.alertsRead(await notifications.requestPermission())) }
                        // Nothing suspends from here to the Start's journal write.
                        continuation.yield(start(request, bodyMass: bodyMass, environment: environment))
                        continuation.finish()
                    }
                    continuation.onTermination = { _ in task.cancel() }
                }
            }
        case let .writeHistory(finished):
            let store = historyStore
            return .once {
                guard let store else { return .finished(finished) }
                do {
                    try store.write(finished.entry)
                } catch {
                    return .finishFailed("The session could not be saved on this iPhone: \(error.localizedDescription)",
                                         receipt: finished.receipt)
                }
                // Queued before the journal goes, so no death between here
                // and the next launch can lose the Health save.
                await workouts?.queueHealthSave(finished.id)
                return .finished(finished)
            }
        case let .checkHistory(id):
            let store = historyStore
            return .once {
                guard let saved = try? store?.read(id) else { return nil }
                return .alreadyFinished(id, recoverHealth: saved.local.health.status == .pending
                    && saved.records.session.healthKitWorkout == nil)
            }
        case .registerCategory:
            guard let notifications else { return .none }
            return .once {
                await notifications.register([NotificationCategory(id: Self.countdownCategory)])
                return nil
            }
        case .readAlerts:
            guard let notifications else { return .none }
            return .once { .alertsRead(await notifications.permission()) }
        case .listSetAside:
            guard let directory else { return .none }
            return .once {
                let names = (try? FileManager.default.contentsOfDirectory(atPath: directory.path)) ?? []
                let records = names.sorted().filter { $0.hasPrefix("active-session.unreadable-") }.map {
                    UnreadableRecord(source: $0, kind: "session in progress", reason: "Set aside at launch: it could not be read")
                }
                return records.isEmpty ? nil : .setAsideFound(records)
            }
        case let .health(work):
            guard let workouts else { return .none }
            return .once {
                switch work {
                case let .afterFinish(trigger):
                    try? await clock.sleep(until: clock.now.addingTimeInterval(0.7))
                    await workouts.processHealth(trigger)
                case let .recover(id):
                    await workouts.recoverHealthSave(id)
                case .foreground:
                    await workouts.processHealth(.foreground)
                }
                return nil
            }
        }
    }

    /// How long the finished summary stays on the Lock Screen.
    public static let finishedDisplay: TimeInterval = 5 * 60

    /// Every call runs the same steps in a fixed order (04 §2.4): the alert
    /// replaced, then, unless a newer change started meanwhile, the Live
    /// Activity updated, stamped with the instant of this change so a late,
    /// older update loses.
    static func apply(_ sync: CountdownSync, notifications: (any Notifications)?, activities: (any OngoingActivities)?,
                      at now: Date) async
    {
        await notifications?.cancel(countdownID)
        if sync.active, let notification = sync.notification, notification.fireDate > now {
            await notifications?.schedule(NotificationRequest(
                id: countdownID, title: notification.title, body: notification.body, fireAt: notification.fireDate,
                thread: sync.session.rawValue, category: countdownCategory, timeSensitive: true, showsWhileActive: !sync.quietInFront,
                userInfo: [sessionKey: sync.session.rawValue]))
        }
        // Finish ends the activity with its summary, never an "All sets
        // done" update first.
        guard !Task.isCancelled, sync.active else { return }
        await activities?.show(OngoingActivity(id: sync.session.rawValue, title: sync.title, content: sync.activity.encoded(),
                                               staleAt: sync.activity.staleAt, timestamp: now))
    }

    /// The Start, enriched once the device has answered: new ids, the
    /// instant floored to the second, the routine and its exercises as the
    /// catalogue holds them now, and the weaker side of each slot that
    /// starts with it from history.
    func start(_ request: StartRequest, bodyMass: Mass?, environment: AppEnvironment) -> Event {
        var routine: Routine?
        if case let .routine(id) = request {
            guard let found = catalog.routines[id] else { return .startDropped }
            routine = found
        }
        var exercises: [ExerciseID: Exercise] = [:]
        for slot in routine?.slots ?? [] {
            if let exercise = catalog.exercises[slot.exercise] { exercises[exercise.id] = exercise }
        }
        var firstSides: [String: Side] = [:]
        for slot in routine?.slots ?? [] where slot.sideOrder == .weakerfirst {
            if let side = Previous.weakerSide(of: slot.exercise, history: history) { firstSides[slot.key] = side }
        }
        let ids = MintedIDs(source: environment.ids)
        let now = environment.clock.now.flooredToSecond
        let zone = environment.timeZone
        let start = SessionEvent.Start(
            session: SessionID(ids.make(at: now)),
            routine: routine,
            exercises: Array(exercises.values).sorted { $0.id < $1.id },
            settings: settings.session,
            bodyMass: bodyMass,
            timeZone: zone.identifier,
            utcOffset: zone.secondsFromGMT(for: now) / 60,
            rowIDs: (0 ..< max(0, SessionEngine.rowIDCount(for: routine, exercises: exercises))).map { _ in RowID(ids.make(at: now)) },
            firstSides: firstSides
        )
        return .engine(EventEnvelope(id: EventID(ids.make(at: now)), at: now, event: .start(start)))
    }
}
