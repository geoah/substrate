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
    }

    @Test func aCivilDayIsItsTextAndRefusesADayNoMonthHas() {
        #expect(CivilDay("2026-02-29") == nil)
        #expect(CivilDay("2028-02-29")?.description == "2028-02-29")
        #expect(CivilDay("2026-11-6") == nil)
        #expect(today.adding(days: 1, in: calendar).description == "2026-11-04")
    }
}

@Suite("Positions")
struct PositionTests {
    @Test func aPositionIsBetweenItsNeighbours() {
        #expect(Position.between(1, 2) == Decimal(string: "1.5"))
        #expect(Position.between(nil, 1) == 0)
        #expect(Position.between(3, nil) == 4)
        #expect(Position.between(nil, nil) == 1)
        // Halving into the same gap stays exact.
        var high: Decimal = 2
        for _ in 0 ..< 60 { high = Position.between(1, high) }
        #expect(high > 1)
    }

    @Test func aMoveWritesTheOneRecordMoved() {
        let positions: [String: Decimal] = ["a": 1, "b": 2, "c": 3]
        #expect(Position.moves("c", to: 0, in: ["a", "b", "c"], positions: positions) == ["c": 0])
        #expect(Position.moves("a", to: 1, in: ["a", "b", "c"], positions: positions) == ["a": Decimal(string: "2.5")!])
        #expect(Position.moves("a", to: 2, in: ["a", "b", "c"], positions: positions) == ["a": 4])
    }

    @Test func aMoveAmongRecordsWithoutPositionsNumbersTheOnesBeforeIt() {
        let moves = Position.moves("a", to: 2, in: ["a", "x", "y", "z"], positions: ["a": 1])
        #expect(moves == ["x": 1, "y": 2, "a": 3])
    }
}

@Suite("The fold")
struct FoldTests {
    @Test func waitingWritesLieOverTheServersCopyAndARefusedOneDoesNot() {
        let key = RecordFold.key(TasksKinds.task, "t1")
        let server = [key: taskRecord("t1", "Milk")]
        let rename = OutboxEntry(method: .patch, kind: TasksKinds.task, recordID: "t1", properties: ["name": "Oat milk"])
        var refused = OutboxEntry(method: .patch, kind: TasksKinds.task, recordID: "t1", properties: ["priority": "high"])
        refused.parked = true
        let create = OutboxEntry(method: .put, kind: TasksKinds.task, recordID: "t2", properties: ["name": "Bread"])
        let folded = RecordFold.fold(server, [rename, refused, create])
        #expect(folded[key]?.properties["name"] == "Oat milk")
        #expect(folded[key]?.properties["priority"] == "none")
        #expect(folded[RecordFold.key(TasksKinds.task, "t2")]?.version == 0)
        let gone = OutboxEntry(method: .delete, kind: TasksKinds.task, recordID: "t1")
        #expect(RecordFold.fold(server, [gone])[key] == nil)
        // A patch of a record already gone makes no record.
        #expect(RecordFold.fold([:], [rename])[key] == nil)
    }
}
