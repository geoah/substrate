import Foundation
import SubstrateCore

/// The owner's memories and instructions (Settings → Assistant): read when
/// Settings shows them, and written one record at a time, on screen at
/// once and put back when the server refuses. Each write names the version
/// the owner saw, so one over a record that moved or went since is refused
/// and the list read again, never applied blind; a delete of one already
/// gone is done.
extension ChatApp {
    func guidance(_ state: inout State, _ step: ChatAppStep.GuidanceStep) {
        switch step {
        case .asked:
            guard state.session != nil, !state.guidance.reading else { return }
            state.guidance.reading = true
            state.asks.append(.readGuidance)
        case let .read(memories, instructions):
            state.guidance.memories = memories
            state.guidance.instructions = instructions
            state.guidance.read = true
            state.guidance.reading = false
            state.guidance.problem = nil
        case let .readFailed(failure):
            state.guidance.reading = false
            if failure.report == nil { state.guidance.problem = failure.words }
        case let .changeAsked(reference, change):
            guard !state.guidance.busy.contains(reference.path), let before = state.guidance.record(reference) else { return }
            state.guidance.busy.insert(reference.path)
            state.guidance.failed[reference.path] = nil
            var after = before
            switch change {
            case let .active(on):
                after.properties["active"] = .bool(on)
                state.guidance.put(after)
                state.asks.append(.writeGuidance(reference, properties: ["active": .bool(on)], before: before))
            case let .text(text):
                after.properties["text"] = .string(text)
                state.guidance.put(after)
                state.asks.append(.writeGuidance(reference, properties: ["text": .string(text)], before: before))
            case .delete:
                state.guidance.remove(reference)
                state.asks.append(.deleteGuidance(reference, before: before))
            }
        case let .written(record):
            state.guidance.busy.remove(RecordReference(kind: record.kind, id: record.id).path)
            state.guidance.put(record)
        case let .deleted(reference):
            state.guidance.busy.remove(reference.path)
        case let .writeFailed(reference, failure, before):
            state.guidance.busy.remove(reference.path)
            state.guidance.put(before)
            guard failure.report == nil else { return }
            switch failure.status {
            case 409:
                state.guidance.failed[reference.path] = "It changed since you opened it, so nothing was saved. This is what it says now."
                guidance(&state, .asked)
            case 404:
                // Only a switch or a rewording gets here: the kit's delete
                // takes a 404 as done, the record being gone already.
                state.guidance.failed[reference.path] = "It was deleted since you opened it, so nothing was saved."
                guidance(&state, .asked)
            default:
                state.guidance.failed[reference.path] = "That didn't go through: \(failure.words)"
            }
        }
    }
}
