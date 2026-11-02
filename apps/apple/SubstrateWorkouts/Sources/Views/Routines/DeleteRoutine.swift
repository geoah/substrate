import WorkoutsCore
import SwiftUI

/// A routine the owner asked to delete, waiting for the confirmation.
struct RoutineToDelete: Identifiable, Hashable {
    let id: RoutineID
    let name: String
}

extension View {
    /// The confirmation before a routine is deleted, the same from the
    /// list's swipe and menu as from the preview's Delete. The delete goes to
    /// the signed-in substrate through the outbox; the routine's exercises
    /// stay, since past sessions name them.
    func confirmsRoutineDelete(_ pending: Binding<RoutineToDelete?>, onDeleted: @escaping () -> Void = {}) -> some View {
        modifier(DeleteRoutineConfirmation(pending: pending, onDeleted: onDeleted))
    }
}

private struct DeleteRoutineConfirmation: ViewModifier {
    @Binding var pending: RoutineToDelete?
    let onDeleted: () -> Void
    @Environment(SubstrateLink.self) private var link

    func body(content: Content) -> some View {
        content.confirmationDialog(
            pending.map { "Delete \u{201C}\($0.name)\u{201D}?" } ?? "",
            isPresented: Binding { pending != nil } set: { if !$0 { pending = nil } },
            titleVisibility: .visible,
            presenting: pending
        ) { routine in
            Button("Delete Routine", role: .destructive) {
                pending = nil
                onDeleted()
                Task { await link.deleteRoutine(routine.id) }
            }
            Button("Cancel", role: .cancel) { pending = nil }
        } message: { _ in
            Text("It is deleted from \(link.host ?? "your substrate"). The exercises it uses stay, and so do your past sessions.")
        }
    }
}
