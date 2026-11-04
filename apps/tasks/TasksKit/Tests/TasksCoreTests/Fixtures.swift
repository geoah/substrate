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
