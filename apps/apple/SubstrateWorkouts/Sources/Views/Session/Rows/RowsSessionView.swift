import WorkoutsCore
import SwiftUI

/// The Rows session layout (DESIGN.md §7.6a), the default: the session's
/// blocks as opaque cards of one-line sets, the set to do now open in place,
/// and a floating glass capsule at the bottom. It reads
/// `presentation.rowsLayout` and sends the same commands as the Table. After
/// a Log the list brings the next open set into view, never while a finger
/// is on it.
struct RowsSessionView: View {
    let presentation: SessionPresentation
    let finish: () -> Void
    let showExercise: (ExerciseID) -> Void

    @Environment(SessionController.self) private var session
    @Environment(CoverModel.self) private var cover
    @Environment(RowsScreenModel.self) private var screen
    @State private var scrollPhase: ScrollPhase = .idle
    /// The open row changed while the list was being dragged: scroll once it settles.
    @State private var scrollPending = false

    private var layout: RowsLayout { presentation.rowsLayout }

    private var isResting: Bool {
        if case .resting = layout.capsule { return true }
        return false
    }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 12) {
                    if session.journalWriteFailed {
                        NotSavingBanner()
                    }
                    if session.alertsOff {
                        AlertsOffBanner()
                    }
                    ForEach(layout.blocks) { block in
                        RowsBlockView(block: block, presentation: presentation, isResting: isResting,
                                      showExercise: showExercise)
                            .id(block.id)
                    }
                    Button {
                        cover.sheet = .addExercise(after: nil)
                    } label: {
                        Label("Add exercise", systemImage: "plus")
                            .font(.headline)
                            .frame(maxWidth: .infinity, minHeight: 36)
                    }
                    .buttonStyle(.bordered)
                    .padding(.top, 4)
                }
                .padding(.horizontal, 16)
                .padding(.top, 4)
                .padding(.bottom, 16)
                .animation(.smooth(duration: 0.3), value: layout.openRow)
                .animation(.smooth(duration: 0.25), value: screen.fixing?.row)
            }
            .background(Color(.systemGroupedBackground))
            .scrollEdgeEffectStyle(.soft, for: .bottom)
            .safeAreaBar(edge: .bottom) { bottomBar(proxy) }
            .onScrollPhaseChange { _, phase in
                scrollPhase = phase
                if phase == .idle, scrollPending {
                    scrollPending = false
                    scrollToOpenRow(proxy)
                }
            }
            .onChange(of: layout.openRow) { _, _ in
                if scrollPhase == .idle {
                    scrollToOpenRow(proxy)
                } else {
                    scrollPending = true
                }
            }
            .onChange(of: presentation) { _, _ in
                screen.reconcile(with: layout, entryCheckRow: session.entryCheck?.row)
            }
            .onChange(of: screen.typing?.row) { _, row in
                // The keypad takes the bottom: keep the field it types into in view.
                guard let row else { return }
                Task {
                    try? await Task.sleep(for: .milliseconds(120))
                    withAnimation(.smooth) { proxy.scrollTo(row, anchor: nil) }
                }
            }
            .onChange(of: session.scrollTarget, initial: true) { _, target in
                guard let target else { return }
                session.scrollTarget = nil
                scroll(to: target, proxy: proxy)
            }
            .task {
                // A cover opened mid-session shows the set to do now.
                try? await Task.sleep(for: .milliseconds(80))
                scrollToOpenRow(proxy, animated: false)
            }
        }
    }

    // MARK: - The bottom

    private func bottomBar(_ proxy: ScrollViewProxy) -> some View {
        // Steps aside while a sheet is up: its glass would show through the sheet's.
        let hidden = cover.sheet != nil
        return VStack(spacing: 8) {
            if let typing = screen.typing {
                RowsKeypadPanel(
                    typing: typing,
                    tint: tint(for: typing),
                    onKey: { screen.type($0) },
                    onPatch: { screen.enter($0, session: session) },
                    onCancel: { screen.typing = nil }
                )
                .padding(.horizontal, 8)
                .transition(.move(edge: .bottom).combined(with: .opacity))
            } else {
                if let undo = screen.undo {
                    RowsUndoToast(undo: undo) { screen.undoLast(session: session) }
                        .padding(.horizontal, 16)
                }
                RowsCapsuleView(
                    capsule: layout.capsule,
                    layout: layout,
                    perform: { screen.perform($0, session: session, cover: cover) },
                    showOpenRow: { scrollToOpenRow(proxy) },
                    finish: finish,
                    addExercise: { cover.sheet = .addExercise(after: nil) }
                )
                .padding(.horizontal, 16)
            }
        }
        .padding(.bottom, 6)
        .animation(.smooth(duration: 0.25), value: screen.typing?.row)
        .animation(.smooth(duration: 0.25), value: screen.undo)
        .opacity(hidden ? 0 : 1)
        .allowsHitTesting(!hidden)
        .accessibilityHidden(hidden)
        .animation(.smooth(duration: 0.2), value: hidden)
    }

    private func tint(for typing: RowsScreenModel.Typing) -> Color {
        let editor = layout.item(typing.row)?.editor
        return RowsStyle.tint(editor?.tint ?? .accent)
    }

    // MARK: - Scrolling

    /// The least scroll that shows the open set whole; none when it already is.
    private func scrollToOpenRow(_ proxy: ScrollViewProxy, animated: Bool = true) {
        guard let row = layout.openRow, let block = layout.blocks.first(where: { $0.item(row) != nil }) else { return }
        Task {
            // A lazy block far away is laid out first, then its row is brought in.
            if animated {
                withAnimation(.smooth) { proxy.scrollTo(block.id, anchor: nil) }
            } else {
                proxy.scrollTo(block.id, anchor: nil)
            }
            try? await Task.sleep(for: .milliseconds(60))
            if animated {
                withAnimation(.smooth) { proxy.scrollTo(row, anchor: nil) }
            } else {
                proxy.scrollTo(row, anchor: nil)
            }
        }
    }

    /// The jump menu, a deep link, an added exercise, the entry check's Edit,
    /// Finish's way back. The open set takes the least scroll that shows it,
    /// and never while a finger is on the list (§7.6a).
    private func scroll(to target: ScrollTarget, proxy: ScrollViewProxy) {
        if let row = target.row, row == layout.openRow {
            if scrollPhase == .idle {
                scrollToOpenRow(proxy)
            } else {
                scrollPending = true
            }
            return
        }
        let slot = target.slot ?? target.row.flatMap { layout.item($0)?.slot }
        guard let slot, let block = layout.blocks.first(where: { $0.slots.contains(slot) }) else { return }
        Task {
            // The jump menu closes first; a scroll issued while it animates
            // out landed short or past the block on the simulator.
            try? await Task.sleep(for: .milliseconds(300))
            // A lazy list estimates the heights of blocks it has not laid out,
            // so jump without animation until the target is laid out, then
            // settle on it with one animated pass.
            for _ in 0..<2 {
                proxy.scrollTo(block.id, anchor: .top)
                try? await Task.sleep(for: .milliseconds(80))
            }
            if let row = target.row {
                withAnimation(.smooth) { proxy.scrollTo(row, anchor: .center) }
            } else {
                withAnimation(.smooth) { proxy.scrollTo(block.id, anchor: .top) }
            }
        }
    }
}

/// The Rows layout's title: the session's name over its clock, which a tap
/// pauses or resumes (the Table shows the clock in its header instead).
struct RowsTitleView: View {
    let title: String
    let header: SessionHeaderStats
    let togglePause: () -> Void

    var body: some View {
        Button(action: togglePause) {
            VStack(spacing: 0) {
                Text(title)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.primary)
                    .lineLimit(1)
                HStack(spacing: 3) {
                    Image(systemName: header.isPaused ? "pause.fill" : "stopwatch")
                        .font(.caption2.weight(.semibold))
                        .accessibilityHidden(true)
                    if header.isPaused {
                        Text(header.durationText)
                    } else {
                        SessionClockText(clockStart: header.clockStart, pausedAt: header.clockPausedAt)
                    }
                }
                .font(.footnote.weight(.semibold).monospacedDigit())
                .foregroundStyle(header.isPaused ? Color.orange : Color.accentColor)
                .lineLimit(1)
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityValue(header.isPaused ? "paused" : "")
        .accessibilityHint(header.isPaused ? "Resumes the session clock" : "Pauses the session clock")
    }
}
