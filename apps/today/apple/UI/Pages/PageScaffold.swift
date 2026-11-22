import SubstrateUI
import SwiftUI
import TodayCore

/// A page: a bar with only the back button and at most one icon button at
/// the right, and under it one card that says, once, the icon of the thing
/// and its title. Nothing opens a panel inside a panel.
struct PageScaffold<Content: View, Bottom: View>: View {
    /// The one icon button a page may have at the right of its bar.
    var trailing: ButtonPresentation?
    /// Nil where the page stands beside the day and has nowhere to go back to.
    var back: (() -> Void)?
    /// False where the page is several cards, each drawn by its content:
    /// the suggested tasks, a save card for each project.
    var carded = true
    @ViewBuilder var content: () -> Content
    /// What is fixed at the bottom: the field, or the plan's Save.
    @ViewBuilder var bottom: () -> Bottom

    @Environment(\.words) private var words

    var body: some View {
        ScrollView {
            Group {
                if carded {
                    VStack(alignment: .leading, spacing: 12, content: content).card()
                } else {
                    VStack(alignment: .leading, spacing: TodayLook.gap, content: content)
                }
            }
            .padding(.horizontal, TodayLook.side)
            .padding(.top, back == nil && trailing == nil ? 6 : 10)
            .padding(.bottom, 16)
        }
        .scrollDismissesKeyboard(.interactively)
        .safeAreaInset(edge: .top, spacing: 0) {
            if back != nil || trailing != nil { bar }
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            // With nothing fixed there (a page beside the day) the fade would be the inset's whole content, and
            // its ground would be laid over the page.
            if Bottom.self != EmptyView.self { bottom().edgeFade(.bottom) }
        }
        .edgeFade(.top)
    }

    /// The bar stands on the ground itself (`GroundBar`), so nothing shows
    /// through its buttons.
    private var bar: some View {
        HStack {
            if let back { IconButton(Symbol.back, label: words.back, action: back) }
            Spacer()
            if let trailing { PaneButton(trailing) }
        }
        // In a window on an iPad the system's window controls sit where the back button would: it stands beside them.
        .containerCornerOffset(.leading, sizeToFit: true)
        .padding(.horizontal, 10)
        .padding(.bottom, 4)
        .groundBar()
    }
}

/// A page's title, said once: the icon of the thing (a tile, a badge or a
/// glyph) and its name, with a line above it only when that line is
/// information, and the flag's stripe on the leading edge.
struct PageTitle<Trailing: View>: View {
    var eyebrow: String?
    var icon: Icon?
    var title: String
    var stripe: Stripe = .none
    @ViewBuilder var trailing: () -> Trailing

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            if let eyebrow {
                Text(eyebrow).font(TodayLook.Text.sectionLabel).foregroundStyle(.inkSecond)
            }
            HStack(alignment: .top, spacing: TodayLook.lead) {
                if let icon { IconView(icon: icon, size: .page).padding(.top, 1) }
                Text(title)
                    .font(TodayLook.Text.pageTitle)
                    .foregroundStyle(.ink)
                    .multilineTextAlignment(.leading)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .accessibilityAddTraits(.isHeader)
                trailing()
            }
            .stripe(stripe, inset: 3, room: 12)
            .accessibilityValue(stripe.spoken)
        }
    }
}

extension PageTitle where Trailing == EmptyView {
    init(eyebrow: String? = nil, icon: Icon? = nil, title: String, stripe: Stripe = .none) {
        self.init(eyebrow: eyebrow, icon: icon, title: title, stripe: stripe) { EmptyView() }
    }
}

/// A section of a page: a plain label, then what it holds.
struct PageSection<Content: View>: View {
    var label: String?
    @ViewBuilder var content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let label { SectionLabel(label).padding(.top, 4) }
            content()
        }
    }
}

/// A sentence of a page.
struct PageText: View {
    var text: String
    var quiet = false

    init(_ text: String, quiet: Bool = false) {
        self.text = text
        self.quiet = quiet
    }

    var body: some View {
        Text(text)
            .font(TodayLook.Text.body)
            .foregroundStyle(quiet ? AnyShapeStyle(.inkSecond) : AnyShapeStyle(.ink))
            .multilineTextAlignment(.leading)
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}
