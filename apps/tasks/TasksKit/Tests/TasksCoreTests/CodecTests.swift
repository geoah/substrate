import Foundation
import SubstrateCore
import SubstrateLocal
import Testing
@testable import TasksCore

@Suite("The codec")
struct CodecTests {
    @Test func aTaskReadsEveryPropertyAndAReferenceInEitherSpelling() {
        let record = taskRecord("t1", "Buy milk", list: "home", position: "1.5", dueDay: "2026-11-06", dueAt: "2026-11-06T14:00:00Z",
                                priority: "high")
        let task = TasksCodec.task(record, in: calendar)
        #expect(task.name == "Buy milk")
        #expect(task.list == "home")
        #expect(task.position == Decimal(string: "1.5"))
        #expect(task.due == .time(Date(timeIntervalSince1970: 1_793_973_600), day: CivilDay(year: 2026, month: 11, day: 6)))
        #expect(task.priority == .high)
        #expect(task.version == 1)
        var bare = record
        bare.properties["list"] = .string("\(TasksKinds.list)/home")
        #expect(TasksCodec.task(bare, in: calendar).list == "home")
    }

    @Test func aValueTheAppDoesNotKnowReadsAsTheDefault() {
        let task = TasksCodec.task(taskRecord("t1", "x", priority: "someday", status: "proposed"), in: calendar)
        #expect(task.priority == .none)
        #expect(task.status == .open)
    }

    @Test func aCreateNamesNoStateAndADayClearsNothingItNeverSet() {
        let task = TaskItem(id: "t1", name: "Buy milk", list: "home", position: 2, due: .day(today), priority: .low)
        let body = TasksCodec.create(task)
        #expect(body.keys == ["name", "list", "position", "dueDay", "priority"])
        #expect(body["list"] == .string("substrate.reamde.dev/tasks/list/home"))
        #expect(body["position"] == .string("2"))
        #expect(body["dueDay"] == .string("2026-11-03"))
    }

    @Test func aPatchCarriesOnlyWhatChangedAndAReopenClearsTheStamp() {
        var done = TaskItem(id: "t1", name: "Buy milk", due: .time(now, day: today), status: .done, completedAt: now, version: 3)
        var reopened = done
        reopened.status = .open
        reopened.completedAt = nil
        #expect(TasksCodec.patch(from: done, to: reopened) == ["status": "open", "completedAt": .null])
        done.status = .open
        var dayOnly = done
        dayOnly.due = .day(today)
        #expect(TasksCodec.patch(from: done, to: dayOnly) == ["dueDay": "2026-11-03", "dueAt": .null])
        var none = done
        none.due = nil
        #expect(TasksCodec.patch(from: done, to: none) == ["dueDay": .null, "dueAt": .null])
    }

    @Test func theCodecNamesOnlyWhatThePackageDeclares() throws {
        let package = try TasksPackage.bundled()
        #expect(package.package == TasksKinds.packageID)
        #expect(package.version >= 1)
        let task = try #require(package.properties[TasksKinds.task])
        #expect(Set(TasksCodec.taskProperties).isSubset(of: Set(task)), "the codec names \(TasksCodec.taskProperties), the kind declares \(task)")
        #expect(Set(task).isSubset(of: Set(TasksCodec.taskProperties + ["url"])))
        #expect(package.properties[TasksKinds.list].map(Set.init) == Set(TasksCodec.listProperties))
        // `dueAt` is no declared property: the temporal trait binds it as
        // its point, and the codec reads and writes it under that name alone.
        #expect(!task.contains("dueAt"))
        #expect(package.bindings[TasksKinds.task]?["temporal"]?["point"] == "dueAt")
        let timed = TaskItem(id: "t1", name: "Call", due: .time(now, day: today))
        let bound = Set(package.bindings[TasksKinds.task]?.values.flatMap(\.values) ?? [])
        let written = Set(TasksCodec.create(timed).keys).union(TasksCodec.patch(from: TaskItem(id: "t1", name: ""), to: timed).keys)
        #expect(written.isSubset(of: Set(task).union(bound)), "the codec writes \(written.subtracting(Set(task).union(bound)))")
        #expect(written.contains("dueAt"))
    }

    @Test func aCivilDayIsItsTextAndRefusesADayNoMonthHas() {
        #expect(CivilDay("2026-02-29") == nil)
        #expect(CivilDay("2028-02-29")?.description == "2028-02-29")
        #expect(CivilDay("2026-11-6") == nil)
        #expect(today.adding(days: 1, in: calendar).description == "2026-11-04")
    }
}
