import WorkoutsCore
import SwiftUI

/// Every Start in the app: a routine card's, the preview's, "Start empty
/// session". With a session open it asks first, in a dialog anchored to the
/// button that was tapped (DESIGN.md §7.4, req 22). The caller picks the
/// button style.
struct StartSessionButton<Label: View>: View {
    let request: StartRequest
    @ViewBuilder let label: () -> Label

    @Environment(AppModel.self) private var app
    @Environment(SessionController.self) private var session
    @State private var askConflict = false
    @State private var askDiscard = false
    @State private var starting = false

    var body: some View {
        Button {
            if session.isActive {
                askConflict = true
            } else {
                starting = true
                Task {
                    await session.start(request)
                    starting = false
                }
            }
        } label: {
            label()
        }
        .disabled(starting)
        .confirmationDialog(SessionPresentation.conflictTitle, isPresented: $askConflict, titleVisibility: .visible) {
            Button("Resume it") { app.isSessionPresented = true }
            Button("Finish it and start this one") { session.finishThenStart(request) }
            Button("Discard it and start this one", role: .destructive) { askDiscard = true }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(session.presentation?.summary ?? "")
        }
        .alert(session.presentation?.discardBeforeStartQuestion ?? "Discard the open session?", isPresented: $askDiscard) {
            Button("Discard", role: .destructive) {
                Task { await session.discardThenStart(request) }
            }
            Button("Cancel", role: .cancel) {}
        }
    }
}
