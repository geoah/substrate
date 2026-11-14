import Foundation
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// The root's engine: every event applied to the state, purely, after the
/// data link's own events are applied by the kit's `LinkedRecords`
/// (`LinkedRecordsApp`): `state` holds them, `previous` is from before. The
/// settings link's events, and the ones both links hear, are applied here.
public extension TasksApp {
    func apply(_ event: Event, to state: TasksState, from previous: TasksState) throws -> TasksState {
        var next = state
        switch event {
        case let .records(records):
            if Self.shared(records) { next.settingsLinked = settingsLinked.apply(records, to: state.settingsLinked) }
            switch records {
            case let .enqueued(ids, _, _):
                next.settling.subtract(ids)
                // The outbox has an editor's write: its editor closes.
                for (key, save) in state.saves where save.writes(ids) {
                    next.editors[key] = nil
                    next.saves[key] = nil
                }
            case let .notQueued(ids, words):
                next.settling.subtract(ids)
                // An editor's write is said in the editor, beside its draft;
                // any other in the app's problem.
                let editors = state.saves.filter { $0.value.writes(ids) }.keys
                for key in editors { next.saves[key] = .notKept(words) }
                if editors.isEmpty { next.problem = words }
            case let .wrote(write):
                // A decision's write went to the outbox: its window is over.
                for (path, pending) in state.pending where pending.write == write {
                    next.pending[path] = nil
                    next.settling.formUnion(write.entries.map(\.id))
                }
                if let accepted = state.pending.first(where: { $0.value.write == write }), accepted.value.decision == .accept {
                    next.edits[accepted.key] = nil
                }
            case let .linked(link):
                answer(&next, link: link)
            default:
                break
            }
        case let .settings(settings):
            next.settingsLinked = settingsLinked.apply(settings, to: state.settingsLinked)
            if case let .notQueued(_, words) = settings { next.problem = words }
        case let .discovered(declarations):
            next.declarations = declarations
            next.discoveryProblem = nil
        case let .discoveryFailed(words):
            next.discoveryProblem = words
        case let .settingsRead(settings, usage):
            next.settings = settings
            next.usage = usage
            next.settingsRead = true
        // A view telling the state it already holds (a tab bar or a list
        // drawn again) moves nothing, and forgets no tap waiting for its
        // task.
        case let .sectionSelected(section):
            guard section != state.section else { break }
            // Each section keeps its own stack: what it showed and had open
            // when the owner left it comes back with it.
            next.stacks[state.section] = SectionStack(selection: state.selection, openTask: state.openTask)
            let kept = state.stacks[section]
            next.section = section
            next.tapped = nil
            next.picked = nil
            next.selection = kept?.selection ?? (section == TasksMenu.needsYou ? .needsYou : nil)
            next.openTask = kept?.openTask
            if section == TasksMenu.settings {
                next.selection = state.selection
                next.openTask = nil
            }
            if section == TasksMenu.lists, next.selection == .needsYou { next.selection = nil }
            if section == TasksMenu.needsYou, next.selection == nil { next.selection = .needsYou }
        case let .shown(selection):
            guard selection != state.selection || state.openTask != nil else { break }
            next.selection = selection
            next.openTask = nil
            next.tapped = nil
            next.picked = nil
        case let .filtered(filter):
            next.filter = filter
        case let .deciding(path, pending):
            next.pending[path] = pending
            next.choice = nil
        case let .undone(path):
            next.pending[path] = nil
        case let .decisionsRead(decisions):
            for (path, decision) in decisions where next.pending[path] == nil { next.pending[path] = decision }
        case let .decisionDropped(path):
            next.pending[path] = nil
        case let .chose(choice):
            next.choice = choice
        case let .suggestionSet(path, edits):
            next.edits[path] = edits.isEmpty ? nil : edits
        case let .picking(picked):
            next.picked = picked
        case let .opened(task):
            guard task != state.openTask else { break }
            next.openTask = task
            next.tapped = nil
        case let .editorOpened(key, seed):
            next.editors[key] = seed
        case let .editorSaving(key, ids):
            next.saves[key] = .sending(ids)
        case let .editorClosed(key):
            next.editors[key] = nil
            next.saves[key] = nil
        case let .asked(confirmation):
            next.confirmation = confirmation
        case .answered:
            next.confirmation = nil
        case let .settingsShown(page):
            next.settingsPage = page
            if page != nil { next.section = TasksMenu.settings }
        case let .approvalAsked(ask):
            next.approval = ask
        case let .approvalAnswered(completed):
            if completed { next.approval?.answered = true } else { next.approval?.refused = true }
        case let .refused(words):
            next.refusal = words
        case let .settingsChanged(settings):
            next.settings = settings
        case let .permission(permission):
            next.permission = permission
        case let .scheduled(plan):
            next.scheduled = plan
        case let .tapped(path):
            next.tapped = path
        case let .usage(usage):
            next.usage = usage
        case let .offered(version):
            next.offered = version
        case .problemDismissed:
            next.problem = nil
            next.refusal = nil
        }
        rekind(&next, from: previous)
        openTapped(&next)
        if next.records != previous.records || next.settings != previous.settings || next.selection != previous.selection
            || next.filter != previous.filter || next.pending != previous.pending || next.declarations != previous.declarations
            || next.picked != previous.picked || next.linked.link?.scopes != previous.linked.link?.scopes
        {
            next.revision += 1
        }
        return next
    }

    /// The data link over the kinds the collections now read, once the
    /// settings are read (before, a collection turned on would read as off,
    /// and its records be dropped). What is shown or open stays where its
    /// record is still read.
    private func rekind(_ state: inout TasksState, from previous: TasksState) {
        guard state.settingsLinked.loaded,
            state.declarations != previous.declarations || state.settingsLinked.records != previous.settingsLinked.records
                || state.linked.link?.scopes != previous.linked.link?.scopes || state.settingsLinked.loaded != previous.settingsLinked.loaded
        else { return }
        let model = model(state, calendar: Calendar(identifier: .gregorian))
        let kinds = model.dataKinds.isEmpty ? TasksKinds.all : model.dataKinds
        guard kinds != linked.kinds(in: state.linked) else { return }
        state.linked = linked.apply(.rekinded(kinds), to: state.linked)
        let read = { (path: String) in RecordReference(path: path).map { kinds.contains($0.kind) } ?? false }
        if let open = state.openTask, !read(open) { state.openTask = nil }
        for (path, _) in state.pending where !read(path) { state.pending[path] = nil }
        switch state.selection {
        case let .group(path)? where !read(path): state.selection = state.section == TasksMenu.lists ? nil : .needsYou
        case let .ungrouped(kind)?, let .missing(kind)?, let .collection(kind)?:
            if !kinds.contains(kind) { state.selection = state.section == TasksMenu.lists ? nil : .needsYou }
        default: break
        }
        if case let .group(path)? = state.filter, !read(path) { state.filter = nil }
    }

    /// An approval asked for, answered by the scopes the link reports once
    /// the owner went through the page: granted where they now reach what it
    /// asked, else turned down.
    private func answer(_ state: inout TasksState, link: LinkSnapshot) {
        guard var ask = state.approval, ask.answered, !ask.refused, link.scopes != ask.held else { return }
        let grant = Self.grant(link)
        let granted = ask.scopes.allSatisfy { grant.covers($0) }
        if granted {
            state.approval = nil
        } else {
            ask.refused = true
            state.approval = ask
        }
    }

    /// A tapped reminder's task, once its record is there: the Lists section
    /// on its group, the task open.
    private func openTapped(_ state: inout TasksState) {
        guard let path = state.tapped, let record = state.records[path] else { return }
        state.section = TasksMenu.lists
        let model = model(state, calendar: Calendar(identifier: .gregorian))
        switch model.tasks[path]?.group {
        case let .record(group)?: state.selection = .group(group)
        case .none?: state.selection = .ungrouped(record.kind)
        case .missing?: state.selection = .missing(record.kind)
        case nil: state.selection = .collection(record.kind)
        }
        state.openTask = path
        state.tapped = nil
    }
}

extension EditorSave {
    /// A save on its way whose write has one of `ids`.
    func writes(_ ids: Set<UUID>) -> Bool {
        if case let .sending(entries) = self { !entries.isDisjoint(with: ids) } else { false }
    }
}
