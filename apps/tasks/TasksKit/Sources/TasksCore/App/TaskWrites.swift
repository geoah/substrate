import Foundation
import SubstrateCore
import SubstrateLocal

/// The writes a task's, a group's or a setting's change makes, through the
/// kit's `RecordWrites` with the codec's properties.
struct TaskWrites {
    let writes: RecordWrites

    init(mint: @escaping () -> UUID) {
        writes = RecordWrites(mint: mint)
    }

    func newID() -> String { writes.newID() }

    func create(_ task: TaskItem, in collection: TaskCollection) -> LocalWrite {
        writes.create(task.kind, task.recordID, properties: TasksCodec.create(task, in: collection))
    }

    func create(_ list: TaskGroup) -> LocalWrite {
        writes.create(list.kind, list.recordID, properties: TasksCodec.create(list))
    }

    func change(_ task: TaskItem, to next: TaskItem, in collection: TaskCollection) -> LocalWrite {
        writes.change(task.kind, task.recordID, properties: TasksCodec.patch(from: task, to: next, in: collection), version: task.version)
    }

    func change(_ list: TaskGroup, to next: TaskGroup) -> LocalWrite {
        writes.change(list.kind, list.recordID, properties: TasksCodec.patch(from: list, to: next), version: list.version)
    }

    /// A decision's patch, on the version the app read.
    func decide(_ task: TaskItem, properties: JSONObject) -> LocalWrite {
        writes.change(task.kind, task.recordID, properties: properties, version: task.version)
    }

    /// The owner's edit in the editor: on the version they saw.
    func edit(_ task: TaskItem, to next: TaskItem, in collection: TaskCollection) -> LocalWrite {
        writes.edit(task.kind, task.recordID, properties: TasksCodec.patch(from: task, to: next, in: collection), seen: task.version)
    }

    func delete(_ kind: String, _ id: String, version: Int?) -> LocalWrite {
        writes.delete(kind, id, version: version)
    }

    /// A settings record's change under its derived id. A record the device
    /// has not read is created first, a PUT with `ifVersion: 0` carrying
    /// what a create needs, and the change follows in its batch: a device
    /// that lost the race to create it has its create read as landed, and
    /// its change then goes onto the winner.
    func setting(_ kind: String, _ id: String, held version: Int?, created: JSONObject, change: JSONObject) -> LocalWrite {
        guard !change.isEmpty else { return LocalWrite(entries: []) }
        if let version { return writes.change(kind, id, properties: change, version: version) }
        let create = writes.create(kind, id, properties: created)
        let patch = writes.change(kind, id, properties: change, version: nil)
        return LocalWrite(entries: create.entries + patch.entries)
    }
}
