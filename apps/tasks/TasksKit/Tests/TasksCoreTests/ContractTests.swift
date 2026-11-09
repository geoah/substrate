import Foundation
import SubstrateCore
import Testing

@testable import TasksCore

/// The contract every platform runs (apps/tasks/contract): each case's
/// declarations, grant and records read through the model, held to its
/// `expect`, and each command to its `writes` and `choices`.
@Suite("The Tasks contract", .timeLimit(.minutes(10)))
struct ContractTests {
    static let folder = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("../../../contract")

    static var cases: [String] {
        ((try? FileManager.default.contentsOfDirectory(atPath: folder.path)) ?? []).filter { $0.hasSuffix(".json") }.sorted()
    }

    struct Case {
        var input: JSONValue
        var model: TasksModel
        var now: Date

        init(_ name: String) throws {
            let data = try Data(contentsOf: ContractTests.folder.appendingPathComponent(name))
            input = try JSONDecoder().decode(JSONValue.self, from: data)
            var calendar = Calendar(identifier: .gregorian)
            calendar.timeZone = try #require(input["timeZone"]?.text.flatMap(TimeZone.init(identifier:)))
            now = try #require(input["now"]?.text.flatMap(Timestamp.parse))
            let declarations = (input["declarations"]?.array ?? []).compactMap { entry -> TaskKindDeclaration? in
                guard let identity = entry["identity"]?.text else { return nil }
                return TaskKindDeclaration(
                    identity: identity, singular: entry["label"]?["singular"]?.text, plural: entry["label"]?["plural"]?.text,
                    definition: entry["definition"]?.object ?? [:])
            }
            let records = try JSONDecoder().decode([Record].self, from: JSONEncoder().encode(input["records"] ?? .array([])))
            let grant = Grants((input["grant"]?.array ?? []).compactMap(\.text))
            model = TasksModel(declarations: declarations, records: records, grant: grant, calendar: calendar)
        }

        var today: CivilDay { CivilDay(now, in: model.calendar) }
    }

    @Test func everyCaseIsHere() {
        #expect(Self.cases.count >= 4)
    }

    @Test(arguments: cases) func collections(_ name: String) throws {
        let c = try Case(name)
        let actual = c.model.collections.map { collection -> JSONValue in
            .object([
                "kind": .string(collection.kind), "enabled": .bool(collection.enabled), "viewOnly": .bool(collection.viewOnly),
                "creatable": .bool(collection.creatable), "title": text(collection.title), "due": text(collection.due),
                "dueDay": text(collection.dueDay), "priority": text(collection.priority), "order": text(collection.order),
                "group": collection.group.map { .object(["property": .string($0.property), "kind": .string($0.kind)]) } ?? .null,
            ])
        }
        #expect(JSONValue.array(actual) == c.input["expect"]?["collections"], "\(name)")
    }

    @Test(arguments: cases) func items(_ name: String) throws {
        let c = try Case(name)
        let expected = try #require(c.input["expect"]?["items"]?.object)
        #expect(Set(expected.keys) == Set(c.model.tasks.keys), "\(name)")
        for (path, want) in expected {
            let task = try #require(c.model.tasks[path], "\(name): \(path)")
            var actual: [String: JSONValue] = ["standing": .string(task.standing.rawValue)]
            for decision in TaskDecision.allCases { actual[decision.rawValue] = .array(task.actions[decision].map(JSONValue.string)) }
            actual["group"] =
                switch task.group {
                case nil: .null
                case .none?: "none"
                case .missing?: "missing"
                case let .record(path)?: .string(path)
                }
            actual["band"] = text(TasksModel.band(of: task, now: c.now, today: c.today, calendar: c.model.calendar)?.rawValue)
            #expect(JSONValue.object(JSONObject(actual)) == want, "\(name): \(path)")
        }
    }

    @Test(arguments: cases) func needsYouAndSuggestions(_ name: String) throws {
        let c = try Case(name)
        let banded = c.model.banded(filter: nil, now: c.now, today: c.today)
        let paths = { (band: TaskBand) in JSONValue.array(banded.filter { $0.band == band }.map { .string($0.task.id) }) }
        let actual: JSONValue = .object([
            "lateOrUrgent": paths(.lateOrUrgent), "comingUp": paths(.comingUp), "later": paths(.later),
            "noDate": number(banded.filter { $0.band == .noDate }.count), "suggestions": number(c.model.suggestions.count),
            "badge": number(c.model.lateOrUrgentCount(now: c.now, today: c.today)),
        ])
        #expect(actual == c.input["expect"]?["needsYou"], "\(name)")
        #expect(JSONValue.array(c.model.suggestions.map { .string($0.id) }) == c.input["expect"]?["suggestions"], "\(name)")
    }

    @Test(arguments: cases) func groupsAndPins(_ name: String) throws {
        let c = try Case(name)
        let actual = c.model.enabled.filter { $0.group != nil }.map { collection -> JSONValue in
            let rows = c.model.groupRows(of: collection).map { row -> JSONValue in
                let group: JSONValue =
                    switch row.group {
                    case let .record(path): .string(path)
                    case .none: "none"
                    case .missing: "missing"
                    }
                return .object(["group": group, "open": number(row.open)])
            }
            return .object(["collection": .string(collection.kind), "kind": .string(collection.group?.kind ?? ""), "rows": .array(rows)])
        }
        #expect(JSONValue.array(actual) == c.input["expect"]?["groups"], "\(name)")
        if let pins = c.input["expect"]?["pins"] {
            #expect(JSONValue.array(c.model.pins.map(JSONValue.string)) == pins, "\(name)")
        }
    }

    @Test(arguments: cases) func commands(_ name: String) throws {
        let c = try Case(name)
        for command in c.input["commands"]?.array ?? [] {
            let doing = try #require(command["do"]?.objectValue)
            let decision = try #require(TaskDecision.allCases.first { doing[$0.rawValue] != nil })
            let path = try #require(doing[decision.rawValue]?.text)
            let outcome = c.model.outcome(
                of: decision, on: path, to: doing["to"]?.text, edits: doing["edits"]?.objectValue ?? JSONObject())
            var writes: [JSONValue] = []
            var choices: JSONValue?
            switch outcome {
            case let .write(task, properties):
                writes = [
                    .object([
                        "op": "patch", "path": .string(task.id), "ifVersion": number(task.version ?? 0),
                        "properties": .object(properties),
                    ])
                ]
            case let .choose(values):
                choices = .array(values.map(JSONValue.string))
            case .none:
                break
            }
            #expect(JSONValue.array(writes) == command["writes"], "\(name): \(doing)")
            #expect(choices == command["choices"], "\(name): \(doing)")
        }
    }

    private func text(_ value: String?) -> JSONValue { value.map(JSONValue.string) ?? .null }
    private func number(_ value: Int) -> JSONValue { .number(JSONNumber(Decimal(value))) }
}
