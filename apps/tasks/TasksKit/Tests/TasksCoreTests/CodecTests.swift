import Foundation
import SubstrateCore
import SubstrateLocal
import Testing

@testable import TasksCore

@Suite("The codec", .timeLimit(.minutes(10)))
struct CodecTests {
    /// The app's own collection, read from the closure the build carries.
    func own() throws -> TaskCollection {
        let model = TasksModel(declarations: try TasksPackage.bundled().declarations, records: [], grant: Grants(TasksManifest.scopes), calendar: calendar)
        return try #require(model.collection(TasksKinds.task))
    }

    func model(_ records: [Record]) throws -> TasksModel {
        TasksModel(declarations: try TasksPackage.bundled().declarations, records: records, grant: Grants(TasksManifest.scopes), calendar: calendar)
    }

    @Test func aTaskReadsEveryPropertyAndAReferenceInEitherSpelling() throws {
        let record = taskRecord(
            "t1", "Buy milk", list: "home", position: "1.5", dueDay: "2026-11-06", dueAt: "2026-11-06T14:00:00Z",
            priority: "high")
        let task = try #require(try model([record, listRecord("home", "Home")]).tasks[t("t1")])
        #expect(task.id == t("t1"))
        #expect(task.recordID == "t1")
        #expect(task.name == "Buy milk")
        #expect(task.list == "home")
        #expect(task.position == Decimal(string: "1.5"))
        #expect(task.due == .time(Date(timeIntervalSince1970: 1_793_973_600), day: CivilDay(year: 2026, month: 11, day: 6)))
        #expect(task.priority == .high)
        #expect(task.version == 1)
        var bare = record
        bare.properties["list"] = .string("\(TasksKinds.list)/home")
        #expect(try model([bare, listRecord("home", "Home")]).tasks[t("t1")]?.list == "home")
        // A list the repository no longer holds is out of reach.
        #expect(try model([record]).tasks[t("t1")]?.group == .missing)
    }

    @Test func aValueTheAppDoesNotKnowReadsAsTheDefault() throws {
        let task = try #require(try model([taskRecord("t1", "x", priority: "someday", status: "proposed")]).tasks[t("t1")])
        #expect(task.priority == .none)
        #expect(task.standing == .proposed)
        #expect(task.actions.accept == ["open"])
    }

    @Test func aCreateNamesNoStateAndADayClearsNothingItNeverSet() throws {
        let task = TaskItem(
            recordID: "t1", name: "Buy milk", group: .record(ListSelection.list("home").listPath), position: 2, due: .day(today),
            priorityValue: "low", priority: .low)
        let body = TasksCodec.create(task, in: try own())
        #expect(body.keys == ["name", "list", "position", "dueDay", "priority"])
        #expect(body["list"] == .string("substrate.reamde.dev/tasks/list/home"))
        #expect(body["position"] == .string("2"))
        #expect(body["dueDay"] == .string("2026-11-03"))
    }

    @Test func aPatchCarriesOnlyWhatChangedAndNeverTheState() throws {
        let collection = try own()
        let done = TaskItem(recordID: "t1", name: "Buy milk", due: .time(now, day: today), status: "done", standing: .done, version: 3)
        var reopened = done
        reopened.status = "open"
        reopened.standing = .open
        #expect(TasksCodec.patch(from: done, to: reopened, in: collection).isEmpty)
        var dayOnly = done
        dayOnly.due = .day(today)
        #expect(TasksCodec.patch(from: done, to: dayOnly, in: collection) == ["dueDay": "2026-11-03", "dueAt": .null])
        var none = done
        none.due = nil
        #expect(TasksCodec.patch(from: done, to: none, in: collection) == ["dueDay": .null, "dueAt": .null])
    }

    @Test func theCodecNamesOnlyWhatThePackageDeclares() throws {
        let package = try TasksPackage.bundled()
        #expect(package.package == TasksKinds.packageID)
        #expect(package.version >= 2)
        let task = try #require(package.properties[TasksKinds.task])
        #expect(Set(TasksCodec.taskProperties).isSubset(of: Set(task)), "the codec names \(TasksCodec.taskProperties), the kind declares \(task)")
        #expect(Set(task).isSubset(of: Set(TasksCodec.taskProperties + ["url"])))
        #expect(package.properties[TasksKinds.list].map(Set.init) == Set(TasksCodec.listProperties))
        // `dueAt` is no declared property: the temporal trait binds it as
        // its point, and the codec reads and writes it under that name alone.
        #expect(!task.contains("dueAt"))
        #expect(package.bindings[TasksKinds.task]?["temporal"]?["point"] == "dueAt")
        let collection = try own()
        #expect(collection.due == "dueAt")
        let timed = TaskItem(recordID: "t1", name: "Call", due: .time(now, day: today))
        let bound = Set(package.bindings[TasksKinds.task]?.values.flatMap(\.values) ?? [])
        let written = Set(TasksCodec.create(timed, in: collection).keys)
            .union(TasksCodec.patch(from: TaskItem(recordID: "t1", name: ""), to: timed, in: collection).keys)
        #expect(written.isSubset(of: Set(task).union(bound)), "the codec writes \(written.subtracting(Set(task).union(bound)))")
        #expect(written.contains("dueAt"))
        // The settings kinds are the package's too, and bind no status.
        #expect(package.properties[TasksKinds.collection]?.contains("enabled") == true)
        #expect(package.properties[TasksKinds.preference]?.contains("pins") == true)
    }

    @Test func aCivilDayIsItsTextAndRefusesADayNoMonthHas() {
        #expect(CivilDay("2026-02-29") == nil)
        #expect(CivilDay("2028-02-29")?.description == "2028-02-29")
        #expect(CivilDay("2026-11-6") == nil)
        #expect(today.adding(days: 1, in: calendar).description == "2026-11-04")
    }
}
