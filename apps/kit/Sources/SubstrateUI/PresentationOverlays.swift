#if canImport(SwiftUI)
import SubstrateRuntime
import SubstrateScreens
import SwiftUI

public extension View {
    /// The question while the presentation asks one: its confirm drawn as
    /// destructive, its cancel as cancel. Dismissing it unanswered is the
    /// cancel.
    func confirmation<Question>(
        _ confirmation: ConfirmationPresentation<Question>?, confirm: @escaping () -> Void,
        dismiss: @escaping () -> Void
    ) -> some View {
        confirmationDialog(
            confirmation?.title ?? "", isPresented: Binding(get: { confirmation != nil }, set: { if !$0 { dismiss() } }),
            titleVisibility: .visible, presenting: confirmation
        ) { confirmation in
            Button(confirmation.confirm, role: .destructive, action: confirm)
            Button(confirmation.cancel, role: .cancel, action: dismiss)
        } message: { confirmation in
            Text(confirmation.message)
        }
    }

    /// A problem while the presentation holds one, in its words, with OK.
    func problem(_ problem: ProblemPresentation?, dismiss: @escaping () -> Void) -> some View {
        alert(problem?.title ?? "", isPresented: Binding(get: { problem != nil }, set: { if !$0 { dismiss() } }), presenting: problem) { _ in
            Button("OK", action: dismiss)
        } message: { problem in
            Text(problem.message)
        }
    }

    /// A sheet for the first of the children the presentation holds, by
    /// key (an editor open over the screen): closed by the system, it sends
    /// `dismiss` with the child's key, and the child's presentation going
    /// closes it.
    func childSheet<Child, Content: View>(
        _ children: [ChildKey: Child], dismiss: @escaping (ChildKey) -> Void,
        @ViewBuilder content: @escaping (ChildKey, Child) -> Content
    ) -> some View {
        sheet(
            item: Binding(
                get: { children.keys.sorted().first.map(SheetKey.init) },
                set: { key in if key == nil, let open = children.keys.sorted().first { dismiss(open) } })
        ) { key in
            if let child = children[key.key] { content(key.key, child) }
        }
    }
}

/// A child's key as a sheet's item.
struct SheetKey: Identifiable, Hashable {
    let key: ChildKey
    var id: ChildKey { key }
}
#endif
