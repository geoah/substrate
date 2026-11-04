#if canImport(SwiftUI)
import SubstrateScreens
import SwiftUI

/// A button that asks its question first, where there is one, from the
/// button itself: on a tablet and a computer the question points at it, so
/// it never floats over another part of the form.
public struct ConfirmingButton<Question: Codable & Hashable & Sendable>: View {
    let label: String
    let role: ButtonRole?
    let confirmation: ConfirmationPresentation<Question>?
    let action: () -> Void
    @State private var asking = false

    public init(
        _ label: String, role: ButtonRole? = nil, confirmation: ConfirmationPresentation<Question>?,
        action: @escaping () -> Void
    ) {
        self.label = label
        self.role = role
        self.confirmation = confirmation
        self.action = action
    }

    public var body: some View {
        Button(label, role: role) {
            if confirmation == nil { action() } else { asking = true }
        }
        .confirmationDialog(confirmation?.title ?? "", isPresented: $asking, titleVisibility: .visible, presenting: confirmation) {
            Button($0.confirm, role: .destructive, action: action)
            Button($0.cancel, role: .cancel) {}
        } message: {
            Text($0.message)
        }
    }
}
#endif
