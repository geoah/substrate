import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// What the root's events ask the device and the server for, beyond the
/// data link's own (the kit's `LinkedRecords`): what the repository holds,
/// the window reads, the contacts, the device's notes and reminders, and a
/// turn with the assistant.
public extension TodayApp {
    /// Written whole, since the records' own read and watch are held back
    /// while no kind is known: a read over no kinds would read every kind.
    func effects(of event: Event, on state: State, from previous: State, presented presentation: TodayPresentation) -> [Effect] {
        var out: [Effect] = []
        if let records = event.recordsEvent {
            out += linked.effects(of: records, on: state.linked, from: previous.linked).compactMap { effect -> Effect? in
                switch effect {
                case let .read(over: kinds) where kinds.isEmpty: nil
                case let .follow(_, over: kinds) where kinds.isEmpty: nil
                default: .records(effect)
                }
            }
        }
        return out + ownEffects(of: event, on: state, from: previous, presented: presentation)
    }

    func ownEffects(of event: Event, on state: State, from previous: State, presented presentation: TodayPresentation) -> [Effect] {
        var out: [Effect] = []
        let signedIn = state.linked.link?.signedIn == true
        switch event {
        case let .records(records):
            switch records {
            case .opened:
                out += [.loadLocal, .readUsage, .readPermission, .registerNotifications]
            case let .linked(link):
                // At every sign-in and every new approval: what the
                // repository holds may have changed.
                let before = previous.linked.link
                if link.signedIn, before?.signedIn != true || before?.tokenID != link.tokenID || before?.generation != link.generation {
                    out.append(.discover)
                } else if link.signedIn, link.online == true, before?.online != true, let discovery = state.discovery {
                    out.append(.readWindows(discovery))
                }
            case .foregrounded:
                out += [.readUsage, .readPermission]
                if signedIn {
                    if let discovery = state.discovery { out.append(.readWindows(discovery)) } else { out.append(.discover) }
                }
            case .refreshAsked:
                if signedIn { out.append(.discover) }
            case .cleared:
                out.append(.readUsage)
            case let .changed(key, _):
                // A series changed: its occurrences are computed again.
                if let discovery = state.discovery, let kind = RecordReference(path: key)?.kind, discovery.series.contains(kind), signedIn {
                    out.append(.readWindows(discovery))
                }
            default:
                break
            }
        case let .discovered(discovery):
            out.append(.saveDiscovery(discovery))
            // The data link was opened again over the kinds found.
            let kinds = linked.kinds(in: state.linked)
            if kinds != linked.kinds(in: previous.linked) {
                out += linked.effects(of: .rekinded(kinds), on: state.linked, from: previous.linked).compactMap { effect -> Effect? in
                    switch effect {
                    case let .read(over: kinds) where kinds.isEmpty: nil
                    case let .follow(_, over: kinds) where kinds.isEmpty: nil
                    default: .records(effect)
                    }
                }
            }
            out.append(.readWindows(discovery))
        case let .looked(at, _, _):
            // A build that reads more kinds than the kept answer covers, or
            // an answer hours old, asks the repository again: a package may
            // have been added since.
            if signedIn, state.linked.link?.online != false, let discovery = state.discovery, discovery.stale(at: at) { out.append(.discover) }
        case let .windowsRead(windows, _, _):
            out.append(.saveWindows(windows))
        case let .asked(exchange, message, context):
            out.append(.ask(exchange, message: message, context: context))
        case .stopped:
            out.append(.stopAsking)
        case let .later(plan):
            out.append(.remind(plan))
        case .permissionWanted:
            if state.permission == .notAsked || state.permission == nil { out.append(.askPermission) }
        case let .ticked(at):
            // The meetings are read again every quarter of an hour.
            if signedIn, let discovery = state.discovery, at.timeIntervalSince(state.windows.readAt ?? .distantPast) > 15 * 60 {
                out.append(.readWindows(discovery))
            }
        default:
            break
        }
        // The contacts the tasks name, read once each.
        if state.linked.records != previous.linked.records || state.windows != previous.windows, signedIn {
            let missing = Self.missingContacts(state)
            if !missing.isEmpty { out.append(.readContacts(missing)) }
        }
        if state.notes != previous.notes, state.notesRead, let notes = state.notes { out.append(.saveNotes(notes)) }
        // The reminders follow the plan the presenter made, once the device
        // has said what it allows.
        if state.linked.loaded, state.permission != nil, presentation.reminders != state.scheduled { out.append(.schedule(presentation.reminders)) }
        return out
    }

    /// The contacts open tasks and suggested tasks name that the root has
    /// not read or asked for, a few at a time.
    static func missingContacts(_ state: State) -> [String] {
        var wanted: [String] = []
        for record in state.linked.records.values where record.kind == TodayKinds.task {
            let status = record["status"]?.text ?? "open"
            guard status == "open" || status == "proposed" else { continue }
            for path in (record["people"]?.array ?? []).compactMap(\.referencePath) where state.contacts[path] == nil && !state.contactsAsked.contains(path) {
                if !wanted.contains(path) { wanted.append(path) }
            }
        }
        return Array(wanted.sorted().prefix(40))
    }

    func ownKey(of effect: Effect) -> String? {
        switch effect {
        case .records: nil
        case .discover: "discover"
        case .saveDiscovery: "kinds.save"
        case .loadLocal: "local"
        case .readWindows: "windows"
        case .saveWindows: "windows.save"
        case .readContacts: nil
        case .saveNotes: "notes"
        case .ask, .stopAsking: "ask"
        case .readUsage: "usage"
        case .readPermission, .askPermission: "permission"
        case .registerNotifications: "notifications.register"
        case .schedule: "reminders"
        case .remind: nil
        }
    }

    func performOwn(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        let folder = environment.folder
        let notifications = environment.devices.notifications
        let source = environment.client
        let clock = environment.clock
        let timeZone = environment.timeZone
        switch effect {
        case .records:
            return .none
        case .discover:
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    return .discovered(try await TodayReads.discover(client, now: clock.now))
                } catch {
                    return .discoveryFailed(explain(error, app: Self.appName, server: client.server))
                }
            }
        case let .saveDiscovery(discovery):
            guard let folder else { return .none }
            return .once {
                try? Self.kindsFile.save(discovery, to: folder)
                return nil
            }
        case .loadLocal:
            return .once {
                let now = clock.now
                return .localRead(
                    notes: folder.flatMap { Self.notesFile.load(from: $0) }, windows: folder.flatMap { Self.windowsFile.load(from: $0) }, at: now,
                    today: CivilDay(now, in: Self.calendar(timeZone)), zone: timeZone.identifier)
            }
        case let .readWindows(discovery):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    let windows = try await TodayReads.windows(client, discovery: discovery, now: clock.now, calendar: Self.calendar(timeZone))
                    async let requests = TodayReads.requests(client)
                    async let stale = TodayReads.stale(client)
                    return .windowsRead(windows, requests: await requests, stale: await stale)
                } catch {
                    return .windowsFailed(explain(error, app: Self.appName, server: client.server))
                }
            }
        case let .saveWindows(windows):
            guard let folder else { return .none }
            return .once {
                try? Self.windowsFile.save(windows, to: folder)
                return nil
            }
        case let .readContacts(paths):
            return .once {
                guard let client = await source.current() else { return nil }
                return .contactsRead(await TodayReads.contacts(client, paths), asked: paths)
            }
        case let .saveNotes(notes):
            guard let folder else { return .none }
            return .once {
                try? Self.notesFile.save(notes, to: folder)
                return nil
            }
        case let .ask(exchange, message, context):
            return .relay { continuation in
                guard let client = await source.current() else {
                    continuation.yield(.answer(exchange.id, .failed("Moss could not be reached. You are signed out.")))
                    return
                }
                for await event in Assistant.ask(
                    client, message: message, context: context, timezone: timeZone.identifier, messageID: exchange.id, app: Self.appName)
                {
                    continuation.yield(.answer(exchange.id, event))
                }
            }
        case .stopAsking:
            return .cancel
        case .readUsage:
            guard let folder else { return .none }
            return .once { .usage(folder.usage()) }
        case .readPermission:
            guard let notifications else { return .none }
            return .once { .permission(await notifications.permission()) }
        case .askPermission:
            guard let notifications else { return .none }
            return .once { .permission(await notifications.requestPermission()) }
        case .registerNotifications:
            guard let notifications else { return .none }
            return .once {
                await notifications.register([TodayReminders.categoryValue])
                return nil
            }
        case let .schedule(plan):
            guard let notifications else { return .none }
            return .once {
                await notifications.reconcile(plan.map(TodayReminders.request), prefix: TodayReminders.prefix)
                return .scheduled(plan)
            }
        case let .remind(plan):
            guard let notifications else { return .none }
            return .once {
                await notifications.schedule(TodayReminders.request(plan))
                return nil
            }
        }
    }
}

/// The reminders this device shows for routines: one for each row still
/// open, at its slot, with Taken, Skip and Later. Their words name no
/// medicine, person or clinic: the group, or "Medication", and a count.
public enum TodayReminders {
    /// Every routine reminder's id starts with it, so the app cancels its
    /// own alone.
    public static let prefix = "today.routine."
    public static let category = "today.routine"
    public static let taken = "taken"
    public static let skip = "skip"
    public static let later = "later"

    public static var categoryValue: NotificationCategory {
        NotificationCategory(
            id: category,
            actions: [
                NotificationAction(id: taken, title: "Taken"), NotificationAction(id: skip, title: "Skip"),
                NotificationAction(id: later, title: "Later"),
            ])
    }

    public static func request(_ plan: ReminderPlan) -> NotificationRequest {
        NotificationRequest(
            id: plan.id, title: plan.title, body: plan.body, fireAt: plan.fireAt, thread: "today.routines", category: category,
            timeSensitive: true, userInfo: ["row": plan.row])
    }

    /// The app's command for a notification the owner answered: Taken and
    /// Skip log the row, Later brings it back in an hour, and a tap opens
    /// the pane, where the row leads.
    public static func command(for response: NotificationResponse) -> TodayCommand? {
        guard response.category == category, let row = response.userInfo["row"] else { return nil }
        switch response.action {
        case .named(taken): return .log(row: row, done: true)
        case .named(skip): return .log(row: row, done: false)
        case .named(later): return .later(row: row)
        case .open: return .looked
        default: return nil
        }
    }
}
