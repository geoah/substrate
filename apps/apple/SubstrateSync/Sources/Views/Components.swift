import SwiftUI

/// A problem, set apart from everything around it: an icon, a title and
/// the server's or the system's own words, on a tinted ground, so it is never
/// mistaken for a button or a status line.
struct ErrorBox: View {
    var title = "Something went wrong"
    let message: String

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.red)
                .font(.title3)
            VStack(alignment: .leading, spacing: 4) {
                Text(title).font(.subheadline.weight(.semibold))
                Text(message)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
        }
        .padding(12)
        .background(Color.red.opacity(0.08), in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).stroke(Color.red.opacity(0.25)))
    }
}

/// A short state word in a capsule: what a step or a source is, at a glance.
struct StatusBadge: View {
    let text: String
    let color: Color

    var body: some View {
        Text(text)
            .font(.caption.weight(.semibold))
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .foregroundStyle(color)
            .background(color.opacity(0.12), in: Capsule())
    }
}

extension View {
    /// A full-width filled button: the one main action on a screen.
    func primaryAction() -> some View {
        buttonStyle(.borderedProminent)
            .controlSize(.large)
            .frame(maxWidth: .infinity)
    }

    /// A full-width outlined button: a second action beside the main one.
    func secondaryAction() -> some View {
        buttonStyle(.bordered)
            .controlSize(.large)
            .frame(maxWidth: .infinity)
    }
}

/// A button's label that fills the width it is given.
struct WideLabel: View {
    let title: String
    var systemImage: String?
    var busy = false

    var body: some View {
        HStack(spacing: 8) {
            if busy {
                ProgressView()
            } else if let systemImage {
                Image(systemName: systemImage)
            }
            Text(title)
        }
        .frame(maxWidth: .infinity)
    }
}
