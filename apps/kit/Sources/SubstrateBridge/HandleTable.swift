import Foundation

/// The open roots by handle. A closed slot is used again, under the next
/// generation, so a handle's id stays small and an old one never reaches the
/// root opened after it. Not thread-safe: the runtime holds it under its lock.
struct HandleTable<Value> {
    private struct Slot {
        var generation: Int
        var value: Value?
    }

    private var slots: [Slot] = []
    private var free: [Int] = []

    /// A slot for a value made after it, outside whatever lock holds the
    /// table: `lookup` finds nothing there until `fill`.
    mutating func reserve() -> Handle {
        if let index = free.popLast() {
            slots[index].generation += 1
            return Handle(id: index + 1, generation: slots[index].generation)
        }
        slots.append(Slot(generation: 1, value: nil))
        return Handle(id: slots.count, generation: 1)
    }

    mutating func fill(_ handle: Handle, with value: Value) {
        let index = handle.id - 1
        guard slots.indices.contains(index), slots[index].generation == handle.generation else { return }
        slots[index].value = value
    }

    /// Gives a reserved slot back unfilled.
    mutating func release(_ handle: Handle) {
        let index = handle.id - 1
        guard slots.indices.contains(index), slots[index].generation == handle.generation, slots[index].value == nil else { return }
        free.append(index)
    }

    /// The value under `handle`, or nil for one closed, of another
    /// generation, or never opened.
    func lookup(_ handle: Handle) -> Value? {
        let index = handle.id - 1
        guard slots.indices.contains(index), slots[index].generation == handle.generation else { return nil }
        return slots[index].value
    }

    /// Takes the value out and frees its slot; nil when there was none.
    mutating func remove(_ handle: Handle) -> Value? {
        let index = handle.id - 1
        guard slots.indices.contains(index), slots[index].generation == handle.generation,
              let value = slots[index].value
        else { return nil }
        slots[index].value = nil
        free.append(index)
        return value
    }

    /// Whether `handle` names a slot that was opened at that generation,
    /// whatever it holds now: closing it again is then not an error.
    func wasOpened(_ handle: Handle) -> Bool {
        let index = handle.id - 1
        return slots.indices.contains(index) && handle.generation >= 1 && handle.generation <= slots[index].generation
    }

    var values: [Value] { slots.compactMap(\.value) }
}
