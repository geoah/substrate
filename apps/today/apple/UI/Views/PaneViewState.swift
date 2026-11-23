import Observation
import SwiftUI
import TodayCore

/// What the views keep to themselves: which lines are opened in place,
/// which of a meeting's points to raise are ticked, what is typed under
/// "Other" on a suggested task until its Save, what was finished a moment
/// ago and can still be taken back, and whether what is done is shown.
/// None of it is a record; only the last is kept across launches.
@MainActor
@Observable
final class PaneViewState {
    var opened: Set<String> = []
    var ticked: Set<String> = []
    /// What is typed under "Other", by the suggested task's id.
    var words: [String: String] = [:]
    /// A field of a card or a page holds the keyboard: the day's own field,
    /// fixed at the bottom, steps aside so it does not cover what is typed.
    var writing = false
    /// The device asks for less motion: the screen says so, and what opens
    /// and closes here moves as `Motion` then says.
    var reduced = false

    /// How a panel moves when something here opens, closes or is finished.
    var motion: Animation { Motion.panel(still: reduced) }

    // MARK: What is done, shown or hidden

    private static let showsDoneKey = "today.showsDone"

    /// Whether "Earlier today" shows what is done. The device keeps the
    /// choice across launches, and it starts shown.
    private(set) var showsDone: Bool
    /// False for a drawing: its choice is its own and nothing is kept.
    private let keeps: Bool

    /// - Parameter showsDone: a drawing's fixed choice; nil reads the one the device kept.
    init(showsDone: Bool? = nil) {
        keeps = showsDone == nil
        self.showsDone = showsDone ?? (UserDefaults.standard.object(forKey: Self.showsDoneKey) as? Bool ?? true)
    }

    func show(done: Bool) {
        withAnimation(motion) { showsDone = done }
        if keeps { UserDefaults.standard.set(done, forKey: Self.showsDoneKey) }
    }

    // MARK: Finishing, with a way back

    /// What was finished and can still be taken back (`Finishing`): the
    /// command of each waits here and is sent when its time is up.
    private(set) var finishing = Finishing()
    /// How many finishes began and how many were sent: what a haptic answers.
    private(set) var begun = 0
    private(set) var sent = 0
    /// A drawing's fixed instant: no clock runs, and a count stands where
    /// this puts it.
    var frozen: Date?

    /// What was sent a moment ago, by what it finishes: the thing stays
    /// drawn finished until the day follows (`followed`), so it is never
    /// seen open again between its command and its record.
    private var written: [String: TodayCommand] = [:]
    /// How long a thing stays drawn finished when the day does not follow.
    private static let patience: Duration = .seconds(4)
    /// What the day has followed and is moving out of its place.
    @ObservationIgnored private var leaving: Set<String> = []
    /// How long a thing takes to leave its place (`Motion`), and a little more.
    private static let leave: Duration = .milliseconds(700)

    private var deliver: @MainActor (TodayCommand) -> Void = { _ in }
    private var wake: Task<Void, Never>?
    /// Moved when a phase ends, so what reads a phase is drawn again.
    private var beat = 0

    private var now: Date { frozen ?? Date() }

    /// A control that finishes something was pressed: its command waits,
    /// and `deliver` is where it goes when its time is up. A command that
    /// finishes nothing goes at once.
    func finish(_ command: TodayCommand, deliver: @escaping @MainActor (TodayCommand) -> Void) {
        self.deliver = deliver
        // The control becomes its countdown, and the other way of finishing the thing steps back.
        guard withAnimation(motion, { finishing.begin(command, at: now) }) else { return deliver(command) }
        begun += 1
        announce(command)
        schedule()
    }

    /// A drawing's state: a finish that began at an instant, with no clock.
    func draw(_ command: TodayCommand, began: Date, at instant: Date) {
        frozen = instant
        finishing.begin(command, at: began)
    }

    /// It is taken back: nothing was written.
    func takeBack(_ key: String) {
        finishing.cancel(key)
        withAnimation(motion) { beat += 1 }
        schedule()
    }

    func phase(_ key: String) -> Finishing.Phase? {
        _ = beat
        return finishing.phase(key, at: now) ?? (written[key] == nil ? nil : .settled)
    }

    /// The command that waits for a thing: a control shows the count only
    /// for its own command, and the other way of finishing the thing waits.
    func waiting(_ key: String) -> TodayCommand? { finishing.command(key) ?? written[key] }

    /// How much of a thing's countdown is left, from 1 to 0.
    func left(_ key: String) -> Double { finishing.left(key, at: now) }

    /// The day was drawn again from its records. What was sent and is not
    /// among what can still be finished (`open`) is in the day now: it is
    /// leaving its place, and stays drawn finished while it does.
    func followed(open: Set<String>) {
        for (key, command) in written where !open.contains(key) && !leaving.contains(key) {
            leaving.insert(key)
            Task { [weak self] in
                try? await Task.sleep(for: Self.leave)
                guard let self else { return }
                self.leaving.remove(key)
                if self.written[key] == command { self.written[key] = nil }
            }
        }
    }

    /// The app is leaving the foreground, or the screen is going away:
    /// everything that waits is sent now. A finish is never lost.
    func flush() {
        wake?.cancel()
        wake = nil
        for command in finishing.flush() {
            deliver(command)
            sent += 1
        }
        beat += 1
    }

    private func schedule() {
        wake?.cancel()
        wake = nil
        guard frozen == nil, let next = finishing.next(after: now) else { return }
        wake = Task { [weak self] in
            try? await Task.sleep(for: .seconds(max(0, next.timeIntervalSinceNow) + 0.02))
            guard !Task.isCancelled else { return }
            self?.tick()
        }
    }

    /// A wait is over: what is due is sent. The clock calls it; a test calls it at an instant of its own.
    func tick() {
        let due = finishing.due(at: now)
        withAnimation(motion) { beat += 1 }
        for command in due {
            if let key = command.finishing { linger(key, command) }
            deliver(command)
            sent += 1
        }
        schedule()
    }

    /// A command just sent keeps its thing drawn finished until the day
    /// follows, or for a few seconds where it never does (the write was
    /// refused): then the thing is drawn as its records say.
    private func linger(_ key: String, _ command: TodayCommand) {
        written[key] = command
        Task { [weak self] in
            try? await Task.sleep(for: Self.patience)
            guard let self, self.written[key] == command else { return }
            withAnimation(self.motion) { self.written[key] = nil }
        }
    }

    /// A screen reader hears what the press began, and that it can be undone.
    private func announce(_ command: TodayCommand) {
        let what: String =
            switch command {
            case .log(_, false): "Skipped"
            default: "Marked done"
            }
        AccessibilityNotification.Announcement("\(what) in three seconds. The same button undoes it.").post()
    }

    func isOpen(_ id: String) -> Bool { opened.contains(id) }

    /// Opens what is closed and closes what is open, in place, and it is
    /// seen to move: every panel of the day opens through here.
    func toggle(_ id: String) {
        withAnimation(motion) {
            if opened.contains(id) { opened.remove(id) } else { opened.insert(id) }
        }
    }

    /// The same, and what gives way to it closes in the same move.
    func toggle(_ id: String, closing other: String?) {
        withAnimation(motion) {
            if let other { opened.remove(other) }
            if opened.contains(id) { opened.remove(id) } else { opened.insert(id) }
        }
    }

    func open(_ id: String) -> Binding<Bool> {
        Binding(
            get: { self.opened.contains(id) },
            set: { open in
                withAnimation(self.motion) {
                    if open { self.opened.insert(id) } else { self.opened.remove(id) }
                }
            })
    }

    /// A point to raise is ticked, or the tick is taken off.
    func tick(_ id: String) {
        withAnimation(motion) {
            if ticked.contains(id) { ticked.remove(id) } else { ticked.insert(id) }
        }
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
            state.toggle(id)
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

/// A field of a card or a page while it holds the keyboard: the day's own
/// field steps aside. A scroll puts the keyboard away, as everywhere in
/// the app; a bar of its own over the keyboard would cover the field.
private struct Writes: ViewModifier {
    var focused: Bool

    @Environment(PaneViewState.self) private var state

    func body(content: Content) -> some View {
        content
            .onChange(of: focused) { _, now in withAnimation(state.motion) { state.writing = now } }
            .onDisappear { if focused { state.writing = false } }
    }
}

extension View {
    func writes(_ focused: Bool) -> some View { modifier(Writes(focused: focused)) }
}
