import WorkoutsCore
import SwiftUI

/// One routine in the list (DESIGN.md §7.2): its name, its exercises in two
/// lines at most, when it was last done, and Start. Tapping the text opens
/// the preview; Start is its own button.
struct RoutineCard: View {
    let card: RoutineCardPresentation
    let open: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Button(action: open) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(card.name)
                        .font(.headline)
                        .foregroundStyle(.primary)
                        .multilineTextAlignment(.leading)
                    if !card.exercises.isEmpty {
                        Text(card.exercises)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                            .lineLimit(2)
                            .multilineTextAlignment(.leading)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityHint("Shows the routine")

            HStack(alignment: .center, spacing: 12) {
                if let lastDone = card.lastDone {
                    Text(lastDone)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Spacer(minLength: 0)
                StartSessionButton(request: .routine(card.id)) {
                    Text("Start")
                        .font(.body.weight(.semibold))
                        .padding(.horizontal, 12)
                }
                .buttonStyle(.borderedProminent)
                .accessibilityLabel("Start \(card.name)")
            }
        }
        .padding(.vertical, 6)
    }
}
