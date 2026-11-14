import ChatKit
import SwiftUI

#if os(iOS)
import UIKit

/// Whether something presented over this view covers it: a sheet or an
/// alert. UIKit dims the tint of everything behind such a presentation and
/// tells each view, which is the one sign a view behind one is given: its
/// scene stays active and it never disappears. A chat behind a sheet is on
/// screen and in front of nobody. A menu dims nothing, and covers little.
struct CoverProbe: UIViewRepresentable {
    @Binding var covered: Bool

    func makeUIView(context: Context) -> Probe {
        let view = Probe()
        view.isUserInteractionEnabled = false
        view.report = { covered = $0 }
        return view
    }

    func updateUIView(_ view: Probe, context: Context) {}

    final class Probe: UIView {
        var report: ((Bool) -> Void)?

        override func tintColorDidChange() {
            super.tintColorDidChange()
            let dimmed = tintAdjustmentMode == .dimmed
            // Told during a layout pass: the state moves after it.
            DispatchQueue.main.async { [weak self] in self?.report?(dimmed) }
        }
    }
}
#endif

/// The line above the first turn the person had not read when the chat was
/// opened: a rule in the app's colour with its word, one element to a
/// screen reader.
struct NewLineView: View {
    let line: NewLine

    var body: some View {
        HStack(spacing: 10) {
            rule
            Text(line.label)
                .font(.caption.weight(.semibold))
                .foregroundStyle(Color.accentColor)
            rule
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(line.spoken)
        .accessibilityAddTraits(.isHeader)
    }

    private var rule: some View {
        Rectangle().fill(Color.accentColor.opacity(0.5)).frame(height: 1)
    }
}

/// The app's own question before the system's: what it would notify, in a
/// sentence, once a first reply settled. Turn On asks the system; Not Now
/// asks nothing, and Settings offers it again.
struct NotificationOfferView: View {
    @Environment(ChatHost.self) private var host
    let offer: NotificationOfferPresentation

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Image(systemName: "bell.badge")
                    .foregroundStyle(Color.accentColor)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 3) {
                    Text(offer.title)
                        .font(.subheadline.weight(.semibold))
                    Text(offer.message)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityElement(children: .combine)
            }
            // Side by side, or one above the other where the text size
            // leaves no room.
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 10) { buttons }
                VStack(alignment: .leading, spacing: 8) { buttons }
            }
        }
        .card()
    }

    @ViewBuilder private var buttons: some View {
        Button(offer.allow) { host.send(.notifications(allow: true)) }
            .buttonStyle(.borderedProminent)
        Button(offer.decline) { host.send(.notifications(allow: false)) }
            .buttonStyle(.bordered)
    }
}
