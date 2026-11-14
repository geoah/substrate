import Foundation
import SubstrateCore
import SubstrateScreens
import TasksCore

/// One of `TasksFixtures.all` as the Android gallery draws it, from the file
/// Swift writes (`apps/tasks/android/fixtures/tasks.json`): the app's
/// presentation, the screen it is drawn as, and the editor's key or the
/// Settings screen where the screen is one of those.
public struct TasksGalleryFixture: Bridged, Equatable {
    public enum Screen: String, Bridged, Equatable {
        case sidebar
        case list
        case detail
        case editor
        case confirmation
        case settings
        case queue
        case suggestion
        case choice
        case collections
        case collection
        case approval
    }

    public var name: String
    public var screen: Screen
    public var app: TasksPresentation
    public var editor: String?
    public var settings: SettingsPresentation?

    public static var all: [TasksGalleryFixture] {
        TasksFixtures.all.map { fixture in
            var out = TasksGalleryFixture(name: fixture.name, screen: .sidebar, app: fixture.app)
            switch fixture.screen {
            case .sidebar: out.screen = .sidebar
            case .list: out.screen = .list
            case .detail: out.screen = .detail
            case let .editor(key):
                out.screen = .editor
                out.editor = key.rawValue
            case .confirmation: out.screen = .confirmation
            case let .settings(settings):
                out.screen = .settings
                out.settings = settings
            case .queue: out.screen = .queue
            case .suggestion: out.screen = .suggestion
            case .choice: out.screen = .choice
            case .collections: out.screen = .collections
            case .collection: out.screen = .collection
            case .approval: out.screen = .approval
            }
            return out
        }
    }
}
