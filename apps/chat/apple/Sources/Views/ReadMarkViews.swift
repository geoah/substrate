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
        HStack(spacing: ChatSpace.apart) {
            rule
            Text(line.label)
                .font(ChatType.label)
                .ink(.accent, on: .page)
            rule
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(line.spoken)
        .accessibilityAddTraits(.isHeader)
    }

    private var rule: some View {
        Rectangle().fill(ChatColours.newRule).frame(height: ChatLine.rule)
    }
}

/// The app's own question before the system's: what it would notify, in a
/// sentence, once a first reply settled. Turn On asks the system; Not Now
/// asks nothing, and Settings offers it again.
struct NotificationOfferView: View {
    @Environment(ChatHost.self) private var host
    let offer: NotificationOfferPresentation

    var body: some View {
        VStack(alignment: .leading, spacing: ChatSpace.parts) {
            HStack(alignment: .firstTextBaseline, spacing: ChatSpace.beside) {
                Image(systemName: "bell.badge")
                    .ink(.accent, on: .card)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: ChatSpace.pair) {
                    Text(offer.title)
                        .font(ChatType.heading)
                    Text(offer.message)
                        .font(ChatType.detail)
                        .ink(.quiet, on: .card)
                }
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityElement(children: .combine)
            }
            ButtonRow { buttons }
        }
        .card()
    }

    @ViewBuilder private var buttons: some View {
        Button(offer.allow) { host.send(.notifications(allow: true)) }
            .buttonStyle(.borderedProminent)
            .prominentTint()
            .touchTarget()
        Button(offer.decline) { host.send(.notifications(allow: false)) }
            .buttonStyle(.bordered)
            .touchTarget()
    }
}
