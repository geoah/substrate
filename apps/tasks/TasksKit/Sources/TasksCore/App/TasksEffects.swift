import Foundation
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// What the root's events ask the device and the server for, and the work
/// each is. The records' own are the kit's `LinkedRecords`: writes go to
/// the outbox alone, and the link sees the outbox move and sends them.
public extension TasksApp {
    func effects(of event: Event, on state: TasksState, from previous: TasksState, presented presentation: TasksPresentation) -> [Effect] {
        var out: [Effect] = []
        switch event {
        case .opening:
            out += linked.openEffects.map(Effect.records) + [.readSettings, .readPermission]
        case let .records(records):
            out += linked.effects(of: records, on: state.linked, from: previous.linked).map(Effect.records)
            switch records {
            case .foregrounded: out += [.readUsage, .readPermission]
            case .cleared: out.append(.readUsage)
            default: break
            }
        case let .editorSaved(_, write, _):
            out += linked.effects(of: .wrote(write), on: state.linked, from: previous.linked).map(Effect.records)
        case let .settingsChanged(settings):
            out.append(.saveSettings(settings))
            if TasksSettings.reminds(settings), state.permission == .notAsked { out.append(.askPermission) }
        default:
            break
        }
        // Asked once there is something to remind of, never at a launch with
        // nothing due: the system asks only once.
        if TasksSettings.reminds(state.settings), state.permission == .notAsked, !out.contains(.askPermission),
           Self.hasDue(state), !Self.hasDue(previous) || previous.permission != .notAsked
        {
            out.append(.askPermission)
        }
        // The reminders follow the plan the presenter made, once the device
        // has said what it allows and the tasks and settings are read.
        if state.linked.loaded, state.settingsRead, state.permission != nil, presentation.reminders != state.scheduled {
            out.append(.schedule(presentation.reminders))
        }
        return out
    }

    private static func hasDue(_ state: TasksState) -> Bool {
        state.records.values.contains { $0.kind == TasksKinds.task && $0.properties["dueDay"] != nil }
    }

    func key(of effect: Effect) -> String? {
        switch effect {
        case let .records(effect): linked.key(of: effect)
        case .readSettings: "settings.read"
        case .readPermission, .askPermission: "permission"
        case .saveSettings: "settings"
        case .schedule: "reminders"
        case .readUsage: "usage"
        }
    }

    func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        let folder = environment.folder
        let notifications = environment.devices.notifications
        switch effect {
        case let .records(effect):
            return linked.perform(effect, in: environment).map(Event.records)
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
        }
    }
}
