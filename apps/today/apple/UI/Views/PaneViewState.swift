import Observation
import SwiftUI

/// What the views keep to themselves: which lines are opened in place,
/// which of a meeting's points to raise are ticked, and what is typed
/// under "Other" on a suggested task until its Save. Nothing here is a
/// record or a choice; closing the app forgets it.
@MainActor
@Observable
final class PaneViewState {
    var opened: Set<String> = []
    var ticked: Set<String> = []
    /// What is typed under "Other", by the suggested task's id.
    var words: [String: String] = [:]

    func isOpen(_ id: String) -> Bool { opened.contains(id) }

    func toggle(_ id: String) {
        if opened.contains(id) { opened.remove(id) } else { opened.insert(id) }
    }

    func open(_ id: String) -> Binding<Bool> {
        Binding(
            get: { self.opened.contains(id) },
            set: { open in
                if open { self.opened.insert(id) } else { self.opened.remove(id) }
            })
    }
}

/// The head of a row that opens in place: a button over the whole line,
/// ending with a down chevron that is up while it is open.
struct OpensInPlace<Label: View>: View {
    var id: String
    /// Said to VoiceOver with the label.
    var hint = "Opens in place"
    @ViewBuilder var label: () -> Label

    @Environment(PaneViewState.self) private var state

    var body: some View {
        Button {
            withAnimation(.snappy(duration: 0.2)) { state.toggle(id) }
        } label: {
            HStack(alignment: .top, spacing: 8) {
                label().frame(maxWidth: .infinity, alignment: .leading)
                RowChevron(turn: state.isOpen(id) ? .open : .closed).padding(.top, 2)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityHint(state.isOpen(id) ? "Closes" : hint)
    }
}
