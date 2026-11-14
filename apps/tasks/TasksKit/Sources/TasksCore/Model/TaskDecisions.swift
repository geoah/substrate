import Foundation
import SubstrateCore

/// What a decision on a task comes to (apps/tasks/contract/README.md,
/// "Commands"): one patch, a choice among several values, or nothing where
/// the decision is not offered or the value named is not one of its own.
public enum DecisionOutcome: Equatable, Sendable {
    /// The patch, on the version the app read: the status set to the value,
    /// then each property the move into the current state stamped set to
    /// null (a reopen), then the owner's edits.
    case write(TaskItem, properties: JSONObject)
    /// Several values: the owner picks one.
    case choose([String])
    case none
}

public extension TasksModel {
    func outcome(of decision: TaskDecision, on path: String, to value: String? = nil, edits: JSONObject = JSONObject()) -> DecisionOutcome {
        guard let task = tasks[path], let collection = collection(task.kind) else { return .none }
        let values = task.actions[decision]
        let chosen: String
        if let value {
            guard values.contains(value) else { return .none }
            chosen = value
        } else if values.count == 1 {
            chosen = values[0]
        } else {
            return values.isEmpty ? .none : .choose(values)
        }
        var properties = JSONObject()
        properties[collection.status.property] = .string(chosen)
        // Nothing removes a stamp on the way back, so a reopened task holds
        // no completion time only where the reopen clears it.
        if decision == .reopen, let current = task.status {
            for stamp in collection.declaration.stamps(into: current, of: collection.status.property) { properties[stamp] = .null }
        }
        for (key, value) in edits { properties[key] = value }
        return .write(task, properties: properties)
    }

    /// A status value by its declared label.
    func label(of value: String, for task: TaskItem) -> String {
        guard let collection = collection(task.kind) else { return value }
        return collection.declaration.label(of: value, in: collection.status.property)
    }
}
