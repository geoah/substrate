import Foundation
import SubstrateLocal

/// The writes a task's or a list's change makes, through the kit's
/// `RecordWrites` with the codec's properties.
struct TaskWrites {
    let writes: RecordWrites

    init(mint: @escaping () -> UUID) {
        writes = RecordWrites(mint: mint)
    }

    func newID() -> String { writes.newID() }

    func create(_ task: TaskItem) -> LocalWrite {
        writes.create(TasksKinds.task, task.id, properties: TasksCodec.create(task))
    }

    func create(_ list: TaskList) -> LocalWrite {
        writes.create(TasksKinds.list, list.id, properties: TasksCodec.create(list))
    }

    func change(_ task: TaskItem, to next: TaskItem) -> LocalWrite {
        writes.change(TasksKinds.task, task.id, properties: TasksCodec.patch(from: task, to: next), version: task.version)
    }

    func change(_ list: TaskList, to next: TaskList) -> LocalWrite {
        writes.change(TasksKinds.list, list.id, properties: TasksCodec.patch(from: list, to: next), version: list.version)
    }

    /// The owner's edit in the editor: on the version they saw.
    func edit(_ task: TaskItem, to next: TaskItem) -> LocalWrite {
        writes.edit(TasksKinds.task, task.id, properties: TasksCodec.patch(from: task, to: next), seen: task.version)
    }

    func delete(_ kind: String, _ id: String, version: Int?) -> LocalWrite {
        writes.delete(kind, id, version: version)
    }
}
