import SubstrateUI
import SwiftUI
import TodayCore

/// The page a route names, as the presentation has it now.
struct PageView<Bottom: View>: View {
    var page: PagePresentation
    var back: (() -> Void)?
    /// The field, fixed at the bottom of every page but the plan, whose
    /// Save takes its place.
    @ViewBuilder var field: () -> Bottom

    var body: some View {
        switch page {
        case let .plan(plan):
            PageScaffold(back: back) {
                PlanPageView(plan: plan)
            } bottom: {
                PlanBar(plan: plan)
            }
        case let .projects(projects):
            PageScaffold(trailing: projects.edit, back: back) {
                ProjectsPageView(page: projects)
            } bottom: {
                field()
            }
        default:
            PageScaffold(back: back) {
                switch page {
                case let .priorities(priorities): PrioritiesPageView(page: priorities)
                case let .project(project): ProjectPageView(page: project)
                case let .person(person): PersonPageView(page: person)
                case let .meeting(meeting): MeetingPageView(page: meeting)
                case let .task(task): TaskPageView(page: task)
                case let .missing(missing):
                    PageTitle(title: missing.title)
                    PageText(missing.line, quiet: true)
                case .plan, .projects: EmptyView()
                }
            } bottom: {
                field()
            }
        }
    }
}

// MARK: Priorities

/// The priorities: only a project is one. A flag opens its three options
/// under the entry and a change applies at once, so what the assistant
/// suggests has the pill and is not selected.
struct PrioritiesPageView: View {
    var page: PrioritiesPage

    var body: some View {
        PageTitle(title: page.title)
        PageText(page.note, quiet: true)
        if !page.suggested.isEmpty {
            PageSection(label: page.suggestedTitle) {
                ForEach(Array(page.suggested.enumerated()), id: \.element.id) { index, entry in
                    PriorityEntryView(entry: entry, first: index == 0)
                }
            }
        }
        PageSection(label: page.entriesTitle) {
            ForEach(Array(page.entries.enumerated()), id: \.element.id) { index, entry in
                PriorityEntryView(entry: entry, first: index == 0)
            }
        }
        PageSection(label: page.addTitle) {
            if let empty = page.addEmpty {
                PageText(empty, quiet: true)
            } else {
                OptionListView(page.add)
            }
        }
    }
}

/// One priority on its page: stripe, tile, name, how today stands, the
/// flag icon button and Remove.
struct PriorityEntryView: View {
    var entry: PriorityEntry
    var first = false

    @Environment(PaneViewState.self) private var state
    @Environment(\.pane) private var pane
    @Environment(\.dynamicTypeSize) private var typeSize

    private var flagID: String { "priority:" + entry.id }
    private var flagOpen: Bool { state.isOpen(flagID) || entry.flag.open }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if typeSize.isAccessibilitySize {
                VStack(alignment: .leading, spacing: 4) {
                    name
                    HStack(spacing: 6) { controls }
                }
            } else {
                HStack(alignment: .center, spacing: 6) {
                    name
                    controls
                }
            }
            if flagOpen { FlagOptionsView(control: entry.flag) }
        }
        .padding(.vertical, 6)
        .rowLine(!first)
    }

    private var name: some View {
        Button {
            if let route = entry.opens { pane.open(route) }
        } label: {
            HStack(alignment: .center, spacing: TodayLook.lead) {
                TileView(tile: entry.tile, size: .row)
                VStack(alignment: .leading, spacing: 1) {
                    Text(entry.name).font(TodayLook.Text.rowTitle).foregroundStyle(.ink).multilineTextAlignment(.leading)
                    if let line = entry.line {
                        Text(line).font(TodayLook.Text.second).foregroundStyle(.inkSecond).multilineTextAlignment(.leading)
                    }
                }
                Spacer(minLength: 0)
            }
            .stripe(entry.flag.stripe, inset: 2)
            .frame(maxWidth: .infinity, minHeight: TodayLook.target, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(entry.opens == nil)
        .accessibilityValue(entry.flag.stripe.spoken)
    }

    @ViewBuilder private var controls: some View {
        IconButton(
            Symbol.flag, label: "Flag: \(entry.flag.word). Show the three options", small: true, look: flagOpen ? .pressed : .quiet
        ) {
            withAnimation(.snappy(duration: 0.2)) { state.toggle(flagID) }
        }
        if let remove = entry.remove { PaneButton(remove).fixedSize() }
    }
}

// MARK: All projects

/// Every project in one list: the ones the owner flagged first, in flag
/// order, then the rest by name.
struct ProjectsPageView: View {
    var page: ProjectsPage
    /// Beside the day on a wide screen, where the strip's edit button is
    /// already in view.
    var titled = true

    var body: some View {
        if titled { PageTitle(title: page.title) }
        if let empty = page.empty { PageText(empty, quiet: true) }
        if !page.priorities.isEmpty {
            PageSection(label: page.prioritiesTitle) {
                ForEach(Array(page.priorities.enumerated()), id: \.element.id) { index, line in
                    ProjectLineView(line: line, first: index == 0)
                }
            }
        }
        if !page.others.isEmpty {
            PageSection(label: page.othersTitle) {
                ForEach(Array(page.others.enumerated()), id: \.element.id) { index, line in
                    ProjectLineView(line: line, first: index == 0)
                }
            }
        }
    }
}

/// A project as a row that opens its page.
struct ProjectLineView: View {
    var line: ProjectLine
    var first = true
    @Environment(\.pane) private var pane

    var body: some View {
        Button {
            pane.open(line.opens)
        } label: {
            HStack(alignment: .top, spacing: TodayLook.lead) {
                TileView(tile: line.tile, size: .row)
                VStack(alignment: .leading, spacing: 2) {
                    Text(line.name).font(TodayLook.Text.rowTitle).foregroundStyle(.ink).multilineTextAlignment(.leading)
                    if let pill = line.pill { PillView(pill) }
                    if let text = line.line {
                        Text(text).font(TodayLook.Text.second).foregroundStyle(.inkSecond).multilineTextAlignment(.leading)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    if let news = line.news {
                        Text(news).font(TodayLook.Text.second.weight(.semibold)).foregroundStyle(.ink).multilineTextAlignment(.leading)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                RowChevron(turn: .page).padding(.top, 2)
            }
            .stripe(line.stripe, inset: 2)
            .padding(.vertical, TodayLook.gap)
            .frame(minHeight: TodayLook.target, alignment: .top)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .rowLine(!first)
        .accessibilityValue(line.stripe.spoken)
        .accessibilityHint("Opens its page")
    }
}

// MARK: A project

/// A project's page: its tasks, its suggested tasks and its timeline.
struct ProjectPageView: View {
    var page: ProjectPage

    @Environment(PaneViewState.self) private var state

    private var flagID: String { "project:" + page.id }

    var body: some View {
        PageTitle(icon: .tile(page.tile), title: page.name, stripe: page.flag?.stripe ?? .none) {
            if let flag = page.flag {
                IconButton(
                    Symbol.flag, label: "Flag: \(flag.word). Show the three options", small: true,
                    look: state.isOpen(flagID) || flag.open ? .pressed : .quiet
                ) {
                    withAnimation(.snappy(duration: 0.2)) { state.toggle(flagID) }
                }
            }
        }
        if let flag = page.flag, state.isOpen(flagID) || flag.open { FlagOptionsView(control: flag) }
        if let make = page.makePriority { PaneButton(make) }
        if let stale = page.stale { StaleNoteView(note: stale) }
        if let summary = page.summary { PageText(summary) }
        if let urgent = page.urgent {
            TaskLineView(line: urgent, onTint: true)
                .background(.urgentFill, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
        }
        if let moss = page.moss { MossBoxView(box: moss) }
        PageSection(label: page.labels.tasks) {
            if page.tasks.isEmpty {
                if let empty = page.tasksEmpty { PageText(empty, quiet: true) }
            } else {
                TaskLines(lines: page.tasks)
            }
        }
        if !page.done.isEmpty { PageSection(label: page.labels.done) { TaskLines(lines: page.done) } }
        if !page.notToday.isEmpty { PageSection(label: page.labels.notToday) { TaskLines(lines: page.notToday) } }
        if !page.suggested.isEmpty || page.suggestedEmpty != nil {
            PageSection(label: page.labels.suggested) {
                if let empty = page.suggestedEmpty, page.suggested.isEmpty { PageText(empty, quiet: true) }
                ForEach(Array(page.suggested.enumerated()), id: \.element.id) { index, item in
                    SuggestedItemView(item: item, first: index == 0)
                }
            }
        }
        if !page.meetings.isEmpty {
            PageSection(label: page.labels.meetings) { MeetingLines(lines: page.meetings) }
        }
        if !page.timeline.isEmpty || page.timelineEmpty != nil {
            PageSection(label: page.labels.timeline) {
                if let empty = page.timelineEmpty, page.timeline.isEmpty { PageText(empty, quiet: true) }
                ForEach(page.timeline) { group in
                    VStack(alignment: .leading, spacing: 6) {
                        Text(group.label).font(TodayLook.Text.button).foregroundStyle(.ink)
                        ForEach(group.lines) { TimelineLineView(line: $0) }
                    }
                    .padding(.top, 4)
                }
            }
        }
    }
}

/// Tasks as a list of rows that open their pages.
struct TaskLines: View {
    var lines: [TaskLine]

    var body: some View {
        RowList {
            ForEach(Array(lines.enumerated()), id: \.element.id) { index, line in
                TaskLineView(line: line, first: index == 0)
            }
        }
    }
}

/// Meetings as a list of rows that open their pages.
struct MeetingLines: View {
    var lines: [MeetingLine]
    @Environment(\.pane) private var pane

    var body: some View {
        RowList {
            ForEach(Array(lines.enumerated()), id: \.element.id) { index, line in
                Button {
                    pane.open(line.opens)
                } label: {
                    HStack(alignment: .top, spacing: 8) {
                        RowHead(icon: .glyph(.meeting), title: line.title, line: line.line)
                        RowChevron(turn: .page).padding(.top, 2)
                    }
                    .padding(.horizontal, 10)
                    .padding(.vertical, TodayLook.gap)
                    .frame(minHeight: TodayLook.target, alignment: .top)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .rowLine(index > 0)
                .accessibilityHint("Opens its page")
            }
        }
    }
}

// MARK: A person, a meeting, a task

/// A person's page: the tasks that involve the person, the suggestions,
/// and when the owner sees the person. A person has no flag.
struct PersonPageView: View {
    var page: PersonPage

    var body: some View {
        PageTitle(icon: .badge(page.badge), title: page.name)
        if let sees = page.sees { PageText(sees, quiet: true) }
        PageSection(label: page.tasksTitle) {
            if page.tasks.isEmpty {
                if let empty = page.tasksEmpty { PageText(empty, quiet: true) }
            } else {
                TaskLines(lines: page.tasks)
            }
        }
        if !page.suggested.isEmpty {
            PageSection(label: page.labels.suggested) {
                ForEach(Array(page.suggested.enumerated()), id: \.element.id) { index, item in
                    SuggestedItemView(item: item, first: index == 0)
                }
            }
        }
        if !page.meetings.isEmpty {
            PageSection(label: page.labels.meetings) { MeetingLines(lines: page.meetings) }
        }
        if let moss = page.moss { MossBoxView(box: moss) }
    }
}

struct MeetingPageView: View {
    var page: MeetingPage

    var body: some View {
        PageTitle(eyebrow: page.eyebrow, icon: .glyph(.meeting), title: page.title)
        if let meta = page.card.meta { PageText(meta, quiet: true) }
        MeetingCardView(card: page.card, scope: "page:" + page.id)
    }
}

struct TaskPageView: View {
    var page: TaskPage

    var body: some View {
        PageTitle(eyebrow: page.eyebrow, icon: .glyph(page.glyph), title: page.title, stripe: page.stripe)
        if let line = page.line { PageText(line, quiet: true) }
        if let suggestion = page.suggestion {
            Text(suggestion.origin).font(TodayLook.Text.footnote).foregroundStyle(.inkSecond)
                .fixedSize(horizontal: false, vertical: true)
            if let quote = suggestion.quote { QuoteText(quote, whole: true) }
            ChipsView(chips: suggestion.chips)
            OptionListView(suggestion.options)
            if let save = suggestion.save { SaveRowView(row: save) }
            if let moss = page.card.moss { MossBoxView(box: moss) }
        } else {
            TaskCardView(card: page.card, scope: "page:" + page.id, whole: true)
        }
    }
}

// MARK: The plan

/// The plan, as a page: everything on the table today, grouped by project.
/// A draft until it is saved, and saving keeps only what was ticked.
struct PlanPageView: View {
    var plan: PlanPage

    @Environment(PaneViewState.self) private var state

    var body: some View {
        PageTitle(icon: .glyph(.plan), title: plan.title)
        if let saved = plan.saved { SavedLine(saved) }
        if let opening = plan.opening { PageText(opening, quiet: true) }
        if let empty = plan.empty {
            PageText(empty, quiet: true)
        } else {
            PageText(plan.hint)
            PageText(plan.prioritiesLine, quiet: true)
        }
        ForEach(plan.groups) { PlanGroupView(group: $0) }
        if !plan.overlaps.isEmpty {
            PageSection(label: plan.overlapsTitle) {
                ForEach(plan.overlaps) { overlap in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(overlap.name).font(TodayLook.Text.rowTitle).foregroundStyle(.ink)
                        Text(overlap.text).font(TodayLook.Text.second).foregroundStyle(.inkSecond)
                            .fixedSize(horizontal: false, vertical: true)
                        OptionListView(overlap.options).padding(.top, 2)
                    }
                    .padding(.bottom, 6)
                }
            }
        }
        if !plan.sends.isEmpty {
            PageSection(label: plan.sendsTitle) {
                if let hint = plan.sendsHint { PageText(hint, quiet: true) }
                OptionListView(plan.sends)
            }
        }
        ForEach(plan.sent, id: \.self) { SavedLine($0) }
        if let note = plan.note {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Image(systemName: Glyph.moss.systemName).font(TodayLook.font(.footnote, weight: .semibold)).accessibilityHidden(true)
                Text(note).font(TodayLook.Text.second).fixedSize(horizontal: false, vertical: true)
            }
            .foregroundStyle(.mossWords)
        }
    }
}

/// One project's part of the plan: its header, which opens the project,
/// then what could be for today.
struct PlanGroupView: View {
    var group: PlanPage.Group

    @Environment(PaneViewState.self) private var state
    @Environment(\.pane) private var pane

    private var moreID: String { "plan:more:" + group.id }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Button {
                if let route = group.opens { pane.open(route) }
            } label: {
                HStack(spacing: 8) {
                    if let tile = group.tile { TileView(tile: tile, size: .row) }
                    Text(group.name).font(TodayLook.font(.headline, weight: .bold)).foregroundStyle(.ink).multilineTextAlignment(.leading)
                    Spacer(minLength: 4)
                    Text(group.count).font(TodayLook.Text.footnote.weight(.medium)).foregroundStyle(.inkSecond).fixedSize()
                    if group.opens != nil { RowChevron(turn: .page) }
                }
                .stripe(group.stripe, inset: 4)
                .frame(minHeight: TodayLook.target)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(group.opens == nil)
            .accessibilityValue(group.stripe.spoken)
            if let meetings = group.meetings {
                Text(meetings).font(TodayLook.Text.footnote).foregroundStyle(.inkSecond).fixedSize(horizontal: false, vertical: true)
            }
            if let empty = group.empty, group.items.isEmpty {
                Text(empty).font(TodayLook.Text.footnote).foregroundStyle(.inkSecond)
            }
            ForEach(Array(group.items.enumerated()), id: \.element.id) { index, item in
                PlanItemView(item: item, first: index == 0)
            }
            if !group.more.isEmpty, let title = group.moreTitle {
                OpensInPlace(id: moreID) {
                    Text(title).font(TodayLook.Text.button).foregroundStyle(.inkSecond)
                }
                .frame(minHeight: TodayLook.target)
                if state.isOpen(moreID) {
                    ForEach(Array(group.more.enumerated()), id: \.element.id) { index, item in
                        PlanItemView(item: item, first: index == 0)
                    }
                }
            }
        }
        .padding(.top, 8)
    }
}

/// One thing that could be for today: a square to tick, its reason, and
/// the flag icon button. Nothing moves on a tick.
struct PlanItemView: View {
    var item: PlanPage.Item
    var first = true

    @Environment(PaneViewState.self) private var state
    @Environment(\.pane) private var pane

    private var flagID: String { "plan:flag:" + item.id }
    private var flagOpen: Bool { state.isOpen(flagID) || item.flag.open }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .top, spacing: 4) {
                Button {
                    pane.send(item.toggle)
                } label: {
                    HStack(alignment: .top, spacing: TodayLook.gap) {
                        OptionMark(mark: .square, selected: item.ticked)
                        VStack(alignment: .leading, spacing: 3) {
                            Text(item.title)
                                .font(TodayLook.Text.rowTitle)
                                .foregroundStyle(item.ticked ? AnyShapeStyle(.ink) : AnyShapeStyle(.inkSecond))
                                .multilineTextAlignment(.leading)
                                .fixedSize(horizontal: false, vertical: true)
                            if item.suggested { PillView(.suggested) }
                            if let why = item.why {
                                Text(why).font(TodayLook.Text.second).foregroundStyle(.inkSecond).multilineTextAlignment(.leading)
                                    .fixedSize(horizontal: false, vertical: true)
                            }
                            if let line = item.line {
                                Text(line).font(TodayLook.Text.footnote).foregroundStyle(.inkSecond).multilineTextAlignment(.leading)
                                    .fixedSize(horizontal: false, vertical: true)
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .stripe(item.flag.stripe, inset: 0, room: 12)
                    .frame(minHeight: TodayLook.target, alignment: .top)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(item.ticked ? .isSelected : [])
                .accessibilityValue(item.flag.stripe.spoken)
                IconButton(
                    Symbol.flag, label: "Flag: \(item.flag.word). Show the three options", small: true, look: flagOpen ? .pressed : .quiet
                ) {
                    withAnimation(.snappy(duration: 0.2)) { state.toggle(flagID) }
                }
            }
            if flagOpen { FlagOptionsView(control: item.flag) }
        }
        .padding(.vertical, 8)
        .rowLine(!first)
    }
}

/// The plan's bar, in place of the field: one line that says exactly what
/// Save does, then the button.
struct PlanBar: View {
    var plan: PlanPage

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(plan.barLine).font(TodayLook.Text.second).foregroundStyle(.ink)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, 12)
            PaneButton(plan.save)
            if let reset = plan.reset { PaneButton(reset) }
        }
        .padding(8)
        .padding(.top, 4)
        .glass(in: RoundedRectangle(cornerRadius: 30, style: .continuous), dense: true)
        .padding(.horizontal, TodayLook.side - 2)
        .padding(.bottom, 6)
    }
}
