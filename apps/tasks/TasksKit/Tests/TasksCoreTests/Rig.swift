import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting

@testable import TasksCore

/// A Tasks root over a folder of its own, a scripted server as its client
/// and recorded notifications.
struct Rig {
    let folder: RepositoryFolder
    let cache: RecordCache
    let outbox: Outbox
    let server: ScriptedServer
    let notifications: RecordedNotifications
    let clock: TestClock
    let app: TasksApp

    init(notifications: RecordedNotifications = RecordedNotifications()) throws {
        folder = try RepositoryFolder(root: temporaryFolder("tasks"))
        cache = RecordCache(folder: folder)
        outbox = Outbox(folder: folder)
        server = ScriptedServer()
        self.notifications = notifications
        clock = TestClock()
        app = TasksApp(
            cache: cache, outbox: outbox, words: TasksApp.words(device: "this iPhone"), device: "this iPhone", build: BuildFacts(version: "0.1.0 (1)"))
    }

    var client: SubstrateClient {
        var client = SubstrateClient(server: TasksCoreTests.server, token: "sub_app_test", transport: server)
        client.pause = { _ in }
        return client
    }

    var environment: AppEnvironment {
        var environment = AppEnvironment.testing(clock: clock, ids: SequentialIDs(), transport: server, client: .fixed(client))
        environment.folder = folder
        environment.devices.notifications = notifications
        return environment
    }

    func harness(performsEffects: Bool = true) -> ControllerHarness<TasksApp> {
        ControllerHarness(app, clock: clock, environment: environment, performsEffects: performsEffects)
    }
}
