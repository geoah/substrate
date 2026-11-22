import SubstrateUI
import SwiftUI
import TodayCore

/// What a view of the pane can do: send a command, open a page, open an
/// address outside the app. Views decide nothing themselves; a drawing has
/// actions that go nowhere.
struct PaneActions: Sendable {
    var send: @MainActor @Sendable (TodayCommand) -> Void = { _ in }
    var open: @MainActor @Sendable (TodayRoute) -> Void = { _ in }
    var openURL: @MainActor @Sendable (String) -> Void = { _ in }
    /// The assistant is asked about one thing, from its box.
    @MainActor func ask(_ subject: MossSubject, _ text: String) { send(.ask(subject, text)) }
}

extension EnvironmentValues {
    @Entry var pane = PaneActions()
    /// False where glass cannot be drawn (a gallery's offscreen window): the
    /// strip and the field are then a translucent fill.
    @Entry var drawsGlass = true
    /// What each icon button is called, from the presentation.
    @Entry var words = ControlWords()
    /// The ground the screen stands on, so an edge can fade into it.
    @Entry var ground = Ground.working
}

/// A button as the core presents it: its role's shape, its glyph before
/// the word, a right chevron after it when it opens a page. A tap sends its
/// command, opens its page, or opens its address.
struct PaneButton: View {
    var button: ButtonPresentation
    /// Inside a tinted box, where the quiet fill would not show.
    var onTint = false
    /// Lower, with the same target: the strip's All projects, a line's Open.
    var small = false
    /// An outlined button that stands beside a wide one, at its height: Skip.
    var beside = false

    @Environment(\.pane) private var pane
    /// Absent where a button is drawn outside the day's screen.
    @Environment(PaneViewState.self) private var state: PaneViewState?
    @State private var asking = false

    init(_ button: ButtonPresentation, onTint: Bool = false, small: Bool = false, beside: Bool = false) {
        self.button = button
        self.onTint = onTint
        self.small = small
        self.beside = beside
    }

    var body: some View {
        // A control that finishes something does not act at once: after its
        // tap it is a countdown and the way back, for its own command only.
        // The other way of finishing the same thing waits meanwhile.
        if let key = button.command?.finishing, let state, let waiting = state.waiting(key) {
            if waiting == button.command {
                FinishingControl(button: button, key: key, kind: kind, small: small)
            } else {
                plain.disabled(true)
            }
        } else {
            plain.disabled(!button.enabled)
        }
    }

    @ViewBuilder private var plain: some View {
        if button.iconOnly, let glyph = button.glyph {
            IconButton(glyph, label: button.title, small: small, look: button.role == .primary ? .filled : .quiet, action: act)
        } else {
            Button(action: act) {
                ButtonWord(text: button.title, glyph: button.glyph, opensPage: button.opens != nil)
            }
            .buttonStyle(.today(kind))
        }
    }

    private var kind: TodayButtonStyle.Kind {
        switch button.role {
        case .primary: button.wide ? .wide : .primary
        case .secondary: small ? .small : (onTint ? .onTint : .secondary)
        case .quiet: small ? .small : (beside ? .beside : .outlined)
        case .destructive: .destructive
        }
    }

    private func act() {
        if let command = button.command { pane.send(command) }
        if let route = button.opens { pane.open(route) }
        if let url = button.url { pane.openURL(url) }
    }
}

/// A control that finishes something, after its tap and until its command
/// is sent (`Finishing`). While it counts, an icon tick is a ring draining
/// around the undo arrow and a button with a word reads "Undo" over a fill
/// that drains; a tap takes the finish back. When the count is over the
/// tick is filled and the button stays "Undo" a moment longer: nothing is
/// written until then, so a tap still takes it back. With Reduce Motion
/// nothing drains, and the state lasts as long.
struct FinishingControl: View {
    var button: ButtonPresentation
    var key: String
    var kind: TodayButtonStyle.Kind
    var small = false

    @Environment(PaneViewState.self) private var state
    @Environment(\.accessibilityReduceMotion) private var still
    @Environment(\.colorScheme) private var scheme
    @State private var left = 1.0

    private var phase: Finishing.Phase { state.phase(key) ?? .settled }

    var body: some View {
        Group {
            if button.iconOnly {
                if phase == .counting { ring } else { IconButton(Glyph.done, label: "Undo", small: small, look: .filled, action: back) }
            } else {
                Button(action: back) {
                    // The word it stands for, unseen under "Undo": the button keeps its width.
                    ZStack {
                        ButtonWord(text: button.title, glyph: button.glyph).hidden()
                        HStack(spacing: 6) {
                            Image(systemName: Symbol.undo.systemName).font(TodayLook.font(.footnote, weight: .bold))
                            Text("Undo")
                        }
                    }
                }
                .buttonStyle(.today(kind, left: phase == .counting ? left : 0))
            }
        }
        .onAppear(perform: run)
        .onChange(of: phase) { _, now in
            if now == .settled { left = 0 }
        }
        .accessibilityLabel("Undo")
        .accessibilityHint("\(button.title) in a few seconds unless you undo")
    }

    /// The tick while it counts: the undo arrow in the tick's circle, and a
    /// ring around it that drains.
    private var ring: some View {
        let side: CGFloat = small ? 32 : 36
        let ink = TodayLook.ink.color
        return Button(action: back) {
            ZStack {
                Circle().fill(ink.opacity(scheme == .dark ? 0.12 : 0.07))
                Circle().trim(from: 0, to: max(0, min(1, left)))
                    .stroke(ink, style: StrokeStyle(lineWidth: 2.5, lineCap: .round))
                    .rotationEffect(.degrees(-90))
                    .padding(1.25)
                Image(systemName: Symbol.undo.systemName)
                    .font(.system(size: small ? 12 : 14, weight: .bold))
                    .foregroundStyle(.ink)
            }
            .frame(width: side, height: side)
            .frame(minWidth: TodayLook.target, minHeight: TodayLook.target)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private func back() { state.takeBack(key) }

    /// The drain runs for what is left of the wait. A drawing stands where
    /// its fixed instant puts it, and with Reduce Motion nothing moves.
    private func run() {
        guard phase == .counting else {
            left = 0
            return
        }
        let remains = state.left(key)
        if still {
            left = 1
            return
        }
        left = remains
        guard state.frozen == nil else { return }
        withAnimation(.linear(duration: remains * Finishing.countdown)) { left = 0 }
    }
}

/// A project named as a chip: its tile and its name. It opens the
/// project's page, so it is a button with a whole target.
struct ProjectChipView: View {
    var chip: ProjectChip
    @Environment(\.pane) private var pane

    var body: some View {
        Button {
            pane.open(chip.route)
        } label: {
            HStack(spacing: 6) {
                TileView(tile: chip.tile, size: .chip)
                Text(chip.name)
                    .font(TodayLook.Text.button)
                    .foregroundStyle(.inkSecond)
                    .lineLimit(1)
                if chip.suggested { PillView(.suggested) }
            }
            .padding(.leading, 4)
            .padding(.trailing, 9)
            .frame(minHeight: 28)
            .background(TodayLook.ink.color.opacity(0.06), in: RoundedRectangle(cornerRadius: TodayLook.chipRadius, style: .continuous))
            .touchTargetHeight()
        }
        .buttonStyle(.plain)
        .accessibilityLabel(chip.name)
    }
}

/// A person named as a chip: the badge and what the chip says. It opens
/// the person's page.
struct PersonChipView: View {
    var chip: PersonChip
    @Environment(\.pane) private var pane

    var body: some View {
        Button {
            pane.open(chip.route)
        } label: {
            HStack(spacing: 6) {
                BadgeView(badge: chip.badge, size: .chip)
                Text(chip.label)
                    .font(TodayLook.Text.button)
                    .foregroundStyle(.inkSecond)
                    .lineLimit(1)
            }
            .padding(.leading, 4)
            .padding(.trailing, 9)
            .frame(minHeight: 28)
            .background(TodayLook.ink.color.opacity(0.06), in: RoundedRectangle(cornerRadius: TodayLook.chipRadius, style: .continuous))
            .touchTargetHeight()
        }
        .buttonStyle(.plain)
        .accessibilityLabel(chip.label)
    }
}

/// The chips under a title: its project, who it is for, and its state
/// words, wrapping where the width runs out.
struct ChipsView: View {
    var chips: Chips

    var body: some View {
        if !chips.isEmpty {
            WrapLayout(spacing: TodayLook.betweenChips) {
                if let project = chips.project { ProjectChipView(chip: project) }
                ForEach(chips.people) { PersonChipView(chip: $0) }
                ForEach(chips.pills, id: \.self) { PillView($0).frame(minHeight: 28) }
            }
        }
    }
}

extension View {
    /// A control drawn lower than a target is still touched in a whole one.
    func touchTargetHeight() -> some View {
        contentShape(TouchTarget())
    }
}
