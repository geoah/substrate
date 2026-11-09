import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// What the root's events ask the device and the server for, and the work
/// each is, beyond the data link's own, which are the kit's `LinkedRecords`
/// (`LinkedRecordsApp`): the settings link's, the kinds with a status, the
/// device's settings and reminders, and each decision's undo window. Writes
/// go to the outbox alone, and the link sees the outbox move and sends them.
public extension TasksApp {
    func ownEffects(of event: Event, on state: TasksState, from previous: TasksState, presented presentation: TasksPresentation) -> [Effect] {
        var out: [Effect] = []
        switch event {
        case let .records(records):
            if Self.shared(records) {
                out += settingsLinked.effects(of: records, on: state.settingsLinked, from: previous.settingsLinked).map(Effect.settings)
            }
            switch records {
            case .opened:
                out += [.readSettings, .readPermission]
            case .foregrounded:
                out += [.readUsage, .readPermission]
                if state.linked.link?.signedIn == true { out.append(.discover) }
            case .refreshAsked:
                if state.linked.link?.signedIn == true { out.append(.discover) }
            case .cleared:
                out.append(.readUsage)
            case let .linked(link):
                // At every sign-in and every new approval: the scopes say
                // which kinds the grant reads.
                let before = previous.linked.link
                if link.signedIn, before?.signedIn != true || before?.scopes != link.scopes || before?.generation != link.generation {
                    out.append(.discover)
                }
            default:
                break
            }
        case let .settings(settings):
            out += settingsLinked.effects(of: settings, on: state.settingsLinked, from: previous.settingsLinked).map(Effect.settings)
        case let .deciding(path, pending):
            out.append(.settle(path, pending.write))
        case let .undone(path):
            out.append(.unsettle(path))
        case let .settingsChanged(settings):
            out.append(.saveSettings(settings))
            // Asked when the owner turns reminders on, never before: the
            // system asks only once, and only an answer to a choice they just
            // made is one they expect.
            if TasksSettings.reminds(settings), !TasksSettings.reminds(previous.settings), state.permission == .notAsked {
                out.append(.askPermission)
            }
        default:
            break
        }
        // The data link was closed and opened again over new kinds, here
        // rather than by the records' own event: the kinds follow from the
        // settings and the declarations, whatever event moved them.
        let kinds = linked.kinds(in: state.linked)
        if kinds != linked.kinds(in: previous.linked) {
            out += linked.effects(of: .rekinded(kinds), on: state.linked, from: previous.linked).map(Effect.records)
                .filter { effect in !out.contains(effect) }
        }
        // The reminders follow the plan the presenter made, once the device
        // has said what it allows and the tasks and settings are read.
        if state.linked.loaded, state.settingsRead, state.permission != nil, presentation.reminders != state.scheduled {
            out.append(.schedule(presentation.reminders))
        }
        return out
    }

    func ownKey(of effect: Effect) -> String? {
        switch effect {
        case .records: nil
        case let .settings(settings): settingsLinked.key(of: settings).map { "settings.\($0)" }
        case .discover: "discover"
        case .readSettings: "settings.read"
        case .readPermission, .askPermission: "permission"
        case .saveSettings: "settings"
        case .schedule: "reminders"
        case .readUsage: "usage"
        case let .settle(path, _), let .unsettle(path): "decide.\(path)"
        }
    }

    func performOwn(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        let folder = environment.folder
        let notifications = environment.devices.notifications
        switch effect {
        case .records:
            return .none
        case let .settings(settings):
            return settingsLinked.perform(settings, in: environment).map(Event.settings)
        case .discover:
            let source = environment.client
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    let kinds = try await client.implementors(of: TasksKinds.lifecycle)
                    return .discovered(
                        kinds.map {
                            TaskKindDeclaration(identity: $0.identity, singular: $0.label?.singular, plural: $0.label?.plural, definition: $0.definition)
                        })
                } catch {
                    return .discoveryFailed(explain(error, app: Self.appName, server: client.server))
                }
            }
        case .readSettings:
            return .once {
                guard let folder else { return .settingsRead(SettingsValues(), usage: []) }
                return .settingsRead(Self.settingsFile.load(from: folder) ?? SettingsValues(), usage: folder.usage())
            }
        case .readPermission:
            guard let notifications else { return .none }
            return .once { .permission(await notifications.permission()) }
        case .askPermission:
            guard let notifications else { return .none }
            return .once { .permission(await notifications.requestPermission()) }
        case let .saveSettings(settings):
            guard let folder else { return .none }
            return .once {
                try? Self.settingsFile.save(settings, to: folder)
                return nil
            }
        case let .schedule(plan):
            guard let notifications else { return .none }
            return .once {
                await notifications.reconcile(plan, prefix: Reminders.prefix)
                return .scheduled(plan)
            }
        case .readUsage:
            guard let folder else { return .none }
            return .once { .usage(folder.usage()) }
        case let .settle(_, write):
            // The window is the clock's, so a test moves it; a cancelled
            // wait (the decision taken back) writes nothing.
            let clock = environment.clock
            return .once {
                try? await clock.sleep(until: clock.now.addingTimeInterval(Self.undoWindow))
                return Task.isCancelled ? nil : .records(.wrote(write))
            }
        case .unsettle:
            return .cancel
        }
    }
}
