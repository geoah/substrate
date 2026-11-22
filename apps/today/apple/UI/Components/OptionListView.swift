import SubstrateUI
import SwiftUI
import TodayCore

/// The one control for a choice: a list of rows, one option each, and
/// every row is always there. The chosen row is filled; tapping another
/// moves the choice. Choosing hides, moves, closes and sends nothing: a
/// tap only sends the row's command.
struct OptionListView: View {
    var list: OptionList
    /// On the urgent card, whose fill the bed sits on.
    var onTint = false

    @Environment(\.pane) private var pane
    @Environment(\.colorScheme) private var scheme

    init(_ list: OptionList, onTint: Bool = false) {
        self.list = list
        self.onTint = onTint
    }

    var body: some View {
        VStack(spacing: 2) {
            ForEach(list.rows) { row in
                Button {
                    pane.send(row.command)
                } label: {
                    OptionRowView(row: row, mark: list.mark)
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(row.selected ? .isSelected : [])
            }
        }
        .padding(4)
        .background(
            onTint ? TodayLook.urgent.color.opacity(scheme == .dark ? 0.16 : 0.10) : TodayLook.bed(scheme),
            in: RoundedRectangle(cornerRadius: TodayLook.bedRadius, style: .continuous))
    }
}

/// One option: its mark, what leads its name, the name with the Suggested
/// pill after it, one line saying what choosing it means (on the selected
/// row and on the assistant's), and the assistant's reason in its hue.
struct OptionRowView: View {
    var row: OptionRow
    var mark: OptionList.Mark

    @Environment(\.colorScheme) private var scheme

    var body: some View {
        HStack(alignment: .top, spacing: TodayLook.gap) {
            OptionMark(mark: mark, selected: row.selected)
            if let stripe = row.stripe { FlagSwatch(stripe: stripe).padding(.top, 1) }
            if let icon = row.icon { IconView(icon: icon, size: .chip).padding(.top, 1) }
            VStack(alignment: .leading, spacing: 2) {
                WrapLayout(spacing: 8) {
                    Text(row.name)
                        .font(row.selected ? TodayLook.Text.optionChosen : TodayLook.Text.option)
                        .foregroundStyle(.ink)
                        .multilineTextAlignment(.leading)
                    if row.suggested { PillView(.suggested) }
                }
                if let line = row.line, row.selected || row.suggested {
                    Text(line).font(TodayLook.Text.footnote).foregroundStyle(.inkSecond)
                }
                if let reason = row.reason, row.suggested {
                    Text(reason).font(TodayLook.Text.footnote).foregroundStyle(.mossWords)
                }
                if let quote = row.quote {
                    Text(quote).font(TodayLook.Text.footnote).italic().foregroundStyle(.inkSecond)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(TodayLook.gap)
        .frame(minHeight: TodayLook.target)
        .background {
            if row.selected {
                RoundedRectangle(cornerRadius: TodayLook.optionRadius, style: .continuous)
                    .fill(.card)
                    .shadow(color: .black.opacity(scheme == .dark ? 0.3 : 0.12), radius: 1.5, y: 1)
            }
        }
        .contentShape(Rectangle())
    }
}

/// The mark of an option: round when one can be chosen, square when
/// several can. Filled when it is the choice.
struct OptionMark: View {
    var mark: OptionList.Mark
    var selected: Bool

    var body: some View {
        let side = TodayLook.optionMark
        Group {
            switch mark {
            case .round:
                if selected {
                    Circle().strokeBorder(.action, lineWidth: 6.5)
                } else {
                    Circle().strokeBorder(TodayLook.ink.color.opacity(0.30), lineWidth: 2)
                }
            case .square:
                let shape = RoundedRectangle(cornerRadius: 7, style: .continuous)
                if selected {
                    shape.fill(.action)
                        .overlay {
                            Image(systemName: Symbol.tick.systemName)
                                .font(.system(size: 12, weight: .bold))
                                .foregroundStyle(.onAction)
                        }
                } else {
                    shape.strokeBorder(TodayLook.ink.color.opacity(0.30), lineWidth: 2)
                }
            }
        }
        .frame(width: side, height: side)
        .padding(.top, 1)
        .accessibilityHidden(true)
    }
}

/// A line of an open task that opens its option list in place: "Flag",
/// "Project", "When". Its chevron points down, and up while it is open.
struct DoorView: View {
    var door: Door
    @Binding var open: Bool

    @Environment(\.colorScheme) private var scheme

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Button {
                open.toggle()
            } label: {
                HStack(spacing: 9) {
                    Text(door.label)
                        .font(TodayLook.Text.sectionLabel)
                        .foregroundStyle(.inkSecond)
                        .frame(minWidth: 54, alignment: .leading)
                    if let stripe = door.stripe { FlagSwatch(stripe: stripe) }
                    if let icon = door.icon { IconView(icon: icon, size: .chip) }
                    Text(door.value)
                        .font(TodayLook.Text.bodyStrong)
                        .foregroundStyle(.ink)
                        .multilineTextAlignment(.leading)
                    ForEach(door.pills, id: \.self) { PillView($0) }
                    Spacer(minLength: 4)
                    RowChevron(turn: open ? .open : .closed)
                }
                .frame(minHeight: TodayLook.target)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityValue(door.value)
            .accessibilityHint(open ? "Closes its options" : "Shows its options")
            if open {
                OptionListView(door.options)
                    .padding(.bottom, door.notes.isEmpty ? 8 : 4)
                ForEach(door.notes, id: \.self) { NoteText($0).padding(.bottom, 6) }
            }
        }
        .overlay(alignment: .top) { Rectangle().fill(TodayLook.line(scheme)).frame(height: 0.5) }
    }
}

/// A note under a list or a card: one quiet sentence.
struct NoteText: View {
    var text: String

    init(_ text: String) { self.text = text }

    var body: some View {
        Text(text)
            .font(TodayLook.Text.footnote)
            .foregroundStyle(.inkSecond)
            .frame(maxWidth: .infinity, alignment: .leading)
            .fixedSize(horizontal: false, vertical: true)
    }
}

/// A flag's three options, opened under a row by its flag icon button,
/// with the lines that go under them.
struct FlagOptionsView: View {
    var control: FlagControl

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            OptionListView(control.options)
            ForEach(control.notes, id: \.self) { NoteText($0).padding(.horizontal, 6) }
        }
    }
}

/// A message a choice needs, shown ready under its list in the
/// assistant's hue. Choosing never sends: Send is its own button.
struct ReadyMessageView: View {
    var message: ReadyMessage

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(message.heading).font(TodayLook.Text.sectionLabel).foregroundStyle(.inkSecond)
            Text(message.text).font(TodayLook.Text.bodyStrong).foregroundStyle(.ink)
                .fixedSize(horizontal: false, vertical: true)
            if let send = message.send { PaneButton(send).padding(.top, 2) }
            if let note = message.note { NoteText(note) }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.mossFill, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
    }
}

/// The end of a save card: one line saying what was saved and when, and
/// the one wide button that says what it saves.
struct SaveRowView: View {
    var row: SaveRow

    var body: some View {
        VStack(alignment: .leading, spacing: TodayLook.gap) {
            if let saved = row.saved { SavedLine(saved) }
            if let button = row.button { PaneButton(button) }
        }
    }
}

/// What was done and when, after an action: a tick and one sentence.
struct SavedLine: View {
    var text: String

    init(_ text: String) { self.text = text }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Image(systemName: Symbol.tick.systemName).font(TodayLook.font(.caption, weight: .bold)).foregroundStyle(.ink)
                .accessibilityHidden(true)
            Text(text).font(TodayLook.Text.footnote.weight(.medium)).foregroundStyle(.inkSecond)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
