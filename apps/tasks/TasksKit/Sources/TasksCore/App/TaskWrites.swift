import Foundation
import SubstrateCore
import SubstrateLocal

/// The outbox writes each change makes, every one in the batch of the
/// record it writes.
///
/// - A create is a PUT without a version, so every retry of it lands the
///   same row; it never names a state, which moves only by patch.
/// - A change is a patch of what changed, guarded by the version the
///   outbox last saw, read again on a conflict and sent once more on top:
///   a completed task, a moved one, a renamed list. A record not sent yet
///   has no version, and its patch follows its create in the batch.
/// - A delete is restorable (no purge), guarded the same way, and replaces
///   the record's writes still waiting.
struct TaskWrites {
    let mint: () -> UUID

    func newID() -> String { mint().uuidString.lowercased() }

    func create(_ task: TaskItem) -> LocalWrite {
        LocalWrite(entries: [OutboxEntry(id: mint(), method: .put, kind: TasksKinds.task, recordID: task.id, properties: TasksCodec.create(task),
                                         batch: LocalWrite.batch(TasksKinds.task, task.id))])
    }

    func create(_ list: TaskList) -> LocalWrite {
        LocalWrite(entries: [OutboxEntry(id: mint(), method: .put, kind: TasksKinds.list, recordID: list.id, properties: TasksCodec.create(list),
                                         batch: LocalWrite.batch(TasksKinds.list, list.id))])
    }

    func change(_ task: TaskItem, to next: TaskItem) -> LocalWrite {
        patch(TasksKinds.task, task.id, TasksCodec.patch(from: task, to: next), version: task.version)
    }

    func change(_ list: TaskList, to next: TaskList) -> LocalWrite {
        patch(TasksKinds.list, list.id, TasksCodec.patch(from: list, to: next), version: list.version)
    }

    func delete(_ kind: String, _ id: String, version: Int?) -> LocalWrite {
        let batch = LocalWrite.batch(kind, id)
        return LocalWrite(entries: [OutboxEntry(id: mint(), method: .delete, kind: kind, recordID: id, guarded: version != nil, batch: batch)],
                          replacing: batch)
    }

    private func patch(_ kind: String, _ id: String, _ properties: JSONObject, version: Int?) -> LocalWrite {
        guard !properties.isEmpty else { return LocalWrite(entries: []) }
        return LocalWrite(entries: [OutboxEntry(id: mint(), method: .patch, kind: kind, recordID: id, properties: properties,
                                                guarded: version != nil, batch: LocalWrite.batch(kind, id))])
    }
}
