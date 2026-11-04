import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting
@testable import TasksCore

let server = URL(string: "https://ada.example.com")!
let utc = TimeZone(identifier: "UTC")!

var calendar: Calendar {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = utc
    calendar.locale = Locale(identifier: "en_US")
    return calendar
}

/// 2026-11-03T09:00:00Z, the test clock's start: a Tuesday.
let now = Date(timeIntervalSince1970: 1_793_696_400)
let today = CivilDay(year: 2026, month: 11, day: 3)

func taskRecord(_ id: String, _ name: String, list: String? = nil, position: String? = nil, dueDay: String? = nil, dueAt: String? = nil,
                priority: String? = nil, status: String = "open", version: Int = 1) -> Record
{
    var properties: [String: JSONValue] = ["name": .string(name), "status": .string(status), "priority": .string(priority ?? "none")]
    if let list { properties["list"] = ["ref": .string("\(TasksKinds.list)/\(list)")] }
    if let position { properties["position"] = .string(position) }
    if let dueDay { properties["dueDay"] = .string(dueDay) }
    if let dueAt { properties["dueAt"] = .string(dueAt) }
    return Record(id: id, kind: TasksKinds.task, properties: properties, version: version, createdAt: "2026-11-01T10:00:00Z")
}

func listRecord(_ id: String, _ name: String, position: String? = nil, version: Int = 1) -> Record {
    var properties: [String: JSONValue] = ["name": .string(name)]
    if let position { properties["position"] = .string(position) }
    return Record(id: id, kind: TasksKinds.list, properties: properties, version: version, createdAt: "2026-11-01T10:00:00Z")
}

func json(_ record: Record) -> String {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
    return String(decoding: try! encoder.encode(record), as: UTF8.self)
}

func page(_ records: [Record]) -> String {
    #"{"records":[\#(records.map(json).joined(separator: ","))]}"#
}

func path(_ kind: String, _ id: String) -> String { "/api/v1/\(kind)/\(id)" }

let signedIn = LinkSnapshot(phase: .signedIn, server: server, repository: "ada.example.com", scopes: TasksManifest.scopes, online: true,
                            packages: [TasksKinds.packageID: 1])

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
        app = TasksApp(cache: cache, outbox: outbox, words: TasksApp.words(device: "this iPhone"), device: "this iPhone", build: BuildFacts(version: "0.1.0 (1)"))
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

/// A list's open rows' titles, as drawn.
func titles(_ presentation: TasksPresentation) -> [String] {
    presentation.list?.open.rows.map(\.title) ?? []
}

/// A formatter's time with its narrow spaces as plain ones, which ICU
/// versions write differently.
func spaced(_ text: String) -> String {
    text.replacingOccurrences(of: "\u{202F}", with: " ").replacingOccurrences(of: "\u{00A0}", with: " ")
}

/// What a test met that it did not expect.
struct Unexpected: Error {}
