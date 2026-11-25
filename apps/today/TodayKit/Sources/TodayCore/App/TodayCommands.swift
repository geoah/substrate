import Foundation
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// What each command means, as events: the writes it makes (through the
/// outbox), the draft it changes, the note the device keeps. A choice that
/// takes effect at once writes here; a draft above a Save writes nothing
/// until its Save.
public extension TodayApp {
    func events(for command: TodayCommand, in context: Enrichment<TodayApp>) -> [Event] {
        let state = context.state
        let calendar = Self.calendar(context.environment.timeZone, context.environment.locale)
        let now = context.now
        let day = TodayPresenter.day(state, now: now, calendar: calendar)
        let engine = DayEngine(day)
        let writes = TodayWrites(day: day, mint: { context.mint() })
        // The day's notes: today's as the device holds them, else a new day's.
        var notes = state.notes?.day == day.today ? day.notes : Self.rolled(state.notes, at: now, today: day.today)
        var drafts = state.drafts
        var out: [Event] = []

        /// A write goes to the outbox only where the repository holds its kind.
        func wrote(_ write: LocalWrite) {
            let entries = write.entries.filter { $0.kind == TodayKinds.patchRequest || day.discovery.holds($0.kind) }
            guard !entries.isEmpty else { return }
            out.append(.records(.wrote(LocalWrite(entries: entries, replacing: write.replacing))))
        }

        /// What the assistant suggested for a record, taken or turned down
        /// by what the owner chose.
        func settle(_ suggestion: Suggestion?, accept: Bool) {
            guard let suggestion else { return }
            wrote(writes.decide(suggestion, accept: accept))
            out.append(.decided([suggestion.id]))
        }

        func ask(_ subject: MossSubject, _ text: String) {
            let question = text.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !question.isEmpty else { return }
            let exchange = Exchange(
                id: context.mint().uuidString.lowercased(), subject: subject, scope: subject.scope(on: day.today), question: question, at: now)
            out.append(.asked(exchange, message: Assistant.message(subject, question, day: day), context: Assistant.context(subject, day: day)))
        }

        /// Words as a task at once: one name on one line, where the
        /// assistant would have kept their lines.
        func add(_ text: String) {
            let typed = text.split(whereSeparator: \.isNewline).map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
                .joined(separator: " ")
            guard !typed.isEmpty else { return }
            let id = writes.writes.newID()
            wrote(writes.add(Words.cap(typed), id: id))
            notes.added.append(id)
        }

        /// Each suggested task that has an answer, the owner's or the
        /// assistant's, is written with it; one with none is left as it
        /// is. "Other" is written as the owner's words (`today/correction`)
        /// and they go to the assistant about that task; with no words it
        /// stays as it is, and so does its pick.
        func answer(_ tasks: [TaskItem], words: [String: String]) {
            var counts = notes.suggestedSaved
            while counts.count < SuggestedAnswer.allCases.count { counts.append(0) }
            for task in tasks {
                guard let answer = engine.pending(task), let index = SuggestedAnswer.allCases.firstIndex(of: answer) else { continue }
                if answer == .other {
                    let said = (words[task.id] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                    guard !said.isEmpty, engine.keepsCorrections else { continue }
                    wrote(writes.correction(task, said, id: writes.writes.newID()))
                    ask(.task(task.id), said)
                } else {
                    wrote(writes.answer(task, answer))
                    if answer == .done { notes.suggestedDone = (notes.suggestedDone ?? []).union([task.id]) }
                }
                counts[index] += 1
                drafts.suggested[task.id] = nil
            }
            notes.suggestedSaved = counts
        }

        func move(_ task: TaskItem, option: String) {
            guard let choice = engine.whenOptions.first(where: { $0.id == option }) else { return }
            wrote(writes.move(task, to: choice.notBefore))
        }

        switch command {
        // MARK: Routines
        case let .log(row, done):
            guard let routine = engine.routine(row) else { break }
            for occurrence in engine.open(routine) { wrote(writes.log(occurrence, done: done)) }
            if state.permission == .notAsked { out.append(.permissionWanted) }
        case let .logOccurrence(id, done):
            guard let occurrence = day.occurrences.first(where: { $0.id == id }) else { break }
            wrote(writes.log(occurrence, done: done))
        case let .unlog(id):
            wrote(writes.unlog(id))
            // A line of yesterday's taken back is open again: the row's answer is no longer whole.
            if let row = engine.yesterdaysRow(of: id) {
                notes.yesterday["y-" + row.id] = nil
                notes.yesterdayAt["y-" + row.id] = nil
            }
        case let .unlogRow(row):
            guard let routine = engine.routine(row) else { break }
            for occurrence in routine.occurrences where day.logs[occurrence.id] != nil { wrote(writes.unlog(occurrence.id)) }
        case let .later(row):
            guard let routine = engine.routine(row) else { break }
            let count = engine.open(routine).count
            out.append(
                .later(
                    ReminderPlan(
                        id: "today.later." + row, title: routine.isDose && !routine.title.lowercased().contains("medic") ? "Medication" : routine.title,
                        body: count == 1 ? "1 item" : "\(count) items", fireAt: now.addingTimeInterval(3600), row: row)))

        // MARK: What was not logged yesterday
        case let .answerYesterday(item, option):
            guard let left = engine.yesterdayLeft.first(where: { $0.id == item }), left.answered != option, case let .routine(routine) = left.kind
            else { break }
            // An answer logs what is still open, and leaves a line ticked by itself as it is. With nothing open the
            // choice moves: every log yesterday's row was answered with today is changed.
            let open = engine.open(routine)
            for occurrence in open.isEmpty ? engine.late(routine) : open { wrote(writes.log(occurrence, done: option == "took")) }
            notes.yesterday[item] = option
            // The first answer's time stays: it is what "Chosen at" says, and what finds its logs.
            if notes.yesterdayAt[item] == nil { notes.yesterdayAt[item] = now }

        // MARK: The night and what is happening
        case .readNight:
            wrote(writes.preference(["nightReadAt": TodayWrites.stamp(now)]))
        case let .gotIt(project):
            for digest in day.digests where project == nil || digest.key == project {
                let items = project == nil ? engine.dayItems(digest).filter { engine.isNew($0, in: digest) } : digest.items
                guard let latest = items.map(\.at).max() else { continue }
                let through = max(latest, min(digest.until ?? latest, now))
                guard through > (day.marks[digest.key] ?? .distantPast) else { continue }
                wrote(writes.mark(digest.key, through: through))
            }
        case let .answerNight(project, option):
            notes.night[project] = option
            // The task is the thing the option's line named: the night's first item on the project.
            if option == "take", let digest = day.digests.first(where: { $0.key == project }), let text = engine.nightItems(digest).first?.text ?? digest.line {
                var properties: JSONObject = ["name": .string(String(text.prefix(140)))]
                if let list = day.list(project) { properties["list"] = TodayWrites.reference(list.path) }
                let id = context.mint().uuidString.lowercased()
                wrote(writes.writes.create(TodayKinds.task, id, properties: properties))
                notes.added.append(id)
            }

        // MARK: Suggested tasks
        case let .suggest(id, answer):
            drafts.suggested[id] = answer
        case let .saveSuggested(ids, words):
            let before = notes.suggestedSaved
            answer(engine.proposed.filter { ids.contains($0.id) }, words: words)
            // From here a new suggestion is a row of its own, while few wait.
            if notes.suggestedSaved != before { notes.suggestedSavedAt = now }
        case let .showSuggested(group):
            drafts.suggestedShown.insert(group)

        // MARK: The plan
        case let .planPick(id):
            guard let task = day.task(id) else { break }
            var plan = drafts.plan ?? PlanDraft()
            plan.picked[id] = !engine.ticked(task)
            drafts.plan = plan
        case let .planFlag(id, flag):
            var plan = drafts.plan ?? PlanDraft()
            plan.flags[id] = flag
            drafts.plan = plan
        case let .planOverlap(id, option):
            var plan = drafts.plan ?? PlanDraft()
            plan.overlaps[id] = option
            drafts.plan = plan
        case let .planSend(id):
            var plan = drafts.plan ?? PlanDraft()
            plan.sends[id] = !(plan.sends[id] ?? false)
            drafts.plan = plan
        case let .planLeft(id, option):
            guard engine.leftTasks.contains(where: { $0.id == id }), DayEngine.leftOptions.contains(where: { $0.id == option }) else { break }
            var plan = drafts.plan ?? PlanDraft()
            // The option chosen again takes the choice back: none is chosen for the owner.
            plan.left[id] = plan.left[id] == option ? nil : option
            drafts.plan = plan
        case .planReset:
            drafts.plan = nil
        case .planSave:
            let picked = engine.pickedForToday
            let left = engine.leftTasks.filter { engine.leftChoice($0) != nil }
            guard !picked.isEmpty || !left.isEmpty else { break }
            // Where each thing yesterday left goes: today, tomorrow, or closed as not done.
            for task in left {
                guard let option = engine.leftChoice(task) else { continue }
                var change: JSONObject = [:]
                switch option {
                case "today":
                    change["dueDay"] = .string(day.today.description)
                    if day.discovery.taskMoves, task.notBefore != nil { change["notBefore"] = .null }
                case "tomorrow":
                    change["dueDay"] = .string(day.today.adding(days: 1, in: calendar).description)
                    if day.discovery.taskMoves {
                        change["notBefore"] = TodayWrites.stamp(day.startOfTomorrow)
                        change["deferrals"] = .number(Decimal(task.deferrals + 1))
                    }
                case "drop": change = ["status": "abandoned"]
                default: break
                }
                if !change.isEmpty { wrote(writes.change(task, change)) }
                notes.yesterday[DayEngine.leftID(task)] = option
                notes.yesterdayAt[DayEngine.leftID(task)] = now
            }
            // What was put on today and is unticked leaves today: its place is taken back.
            for task in engine.candidates where engine.placedToday(task) && !engine.ticked(task) { wrote(writes.move(task, to: nil)) }
            // A flag the assistant suggests on a task that exists starts
            // selected: it is taken where its task is ticked and the owner
            // picked no other, and turned down where the owner picked another.
            for task in engine.candidates {
                guard let suggestion = engine.flagSuggestion(task.path), let suggested = suggestion.flag else { continue }
                if let pick = drafts.plan?.flags[task.id] {
                    settle(suggestion, accept: pick == suggested)
                } else if engine.ticked(task) {
                    settle(suggestion, accept: true)
                    wrote(writes.flag(task, suggested))
                }
            }
            for (id, flag) in (drafts.plan?.flags ?? [:]).sorted(by: { $0.key < $1.key }) {
                guard let task = day.task(id), flag != task.shownFlag || task.isProposed else { continue }
                wrote(writes.flag(task, flag))
            }
            var overlaps = day.plan?.overlaps ?? []
            for overlap in engine.overlaps {
                guard let chosen = drafts.plan?.overlaps[overlap.id] else { continue }
                let paths = overlap.meetings.map(\.path)
                overlaps.removeAll { Set($0.events) == Set(paths) }
                overlaps.append(PlanRecord.Overlap(events: paths, chosen: chosen, at: now))
            }
            // A message goes only if its row was ticked: the assistant is
            // asked to send it, and nothing else is.
            var sends = day.plan?.sends ?? []
            for index in sends.indices where sends[index].sentAt == nil {
                sends[index].ticked = drafts.plan?.sends[sends[index].id] ?? false
                if sends[index].ticked {
                    let name = sends[index].toName ?? sends[index].to.flatMap(day.person)?.name ?? "the person it is for"
                    ask(.day, "Send this message to \(name), as I ticked it in today's plan: \(sends[index].text)")
                }
            }
            var change: JSONObject = [
                "picked": .array(picked.map { TodayWrites.reference($0.path) }), "savedAt": TodayWrites.stamp(now),
                "overlaps": TodayWrites.encode(overlaps),
            ]
            if !sends.isEmpty { change["sends"] = TodayWrites.encode(sends) }
            wrote(writes.plan(change))
            drafts.plan = nil

        // MARK: An overlap, outside the plan
        case let .decideOverlap(id, option):
            guard let overlap = engine.overlaps.first(where: { $0.id == id }) else { break }
            wrote(writes.plan(["overlaps": writes.overlaps(replacing: overlap, chosen: option, kept: day.plan?.overlaps ?? [])]))

        // MARK: A task
        case let .setFlag(id, flag):
            guard let task = day.task(id) else { break }
            // The order follows the flag only once its list is closed.
            if drafts.held[id] == nil, !drafts.heldNone.contains(id) {
                if let had = task.shownFlag { drafts.held[id] = had } else { drafts.heldNone.insert(id) }
            }
            let suggestion = engine.flagSuggestion(task.path)
            settle(suggestion, accept: suggestion?.flag == flag)
            wrote(writes.flag(task, flag))
            notes.urgentTaken[id] = nil
            notes.urgentLeft[id] = nil
        case .settleFlags:
            drafts.held = [:]
            drafts.heldNone = []
        case let .setWhen(id, option), let .moveTo(id, option):
            guard let task = day.task(id) else { break }
            move(task, option: option)
        case let .setProject(id, project):
            guard let task = day.task(id) else { break }
            let suggestion = engine.listSuggestion(task.path)
            settle(suggestion, accept: suggestion?.list == project)
            wrote(writes.project(task, project))
        case let .done(id):
            guard let task = day.task(id) else { break }
            wrote(writes.status(task, .done))
            notes.doneAt = (notes.doneAt ?? [:]).merging([id: now]) { _, new in new }
        case let .reopen(id):
            guard let task = day.task(id) else { break }
            wrote(writes.status(task, .open))
            notes.doneAt?[id] = nil

        // MARK: Urgent
        case let .urgent(id, choice):
            guard let task = day.task(id), task.urgent else { break }
            switch choice {
            case .takeNow:
                notes.urgentTaken[id] = now
                notes.urgentLeft[id] = nil
            case .leave:
                notes.urgentLeft[id] = now
                notes.urgentTaken[id] = nil
            case .notUrgent:
                wrote(writes.notUrgent(task))
                notes.urgentTaken[id] = nil
                notes.urgentLeft[id] = nil
            }

        // MARK: Priorities
        case let .setPriority(id, flag):
            guard let list = day.list(id) else { break }
            let suggestion = engine.flagSuggestion(list.path)
            settle(suggestion, accept: suggestion?.flag == flag)
            wrote(writes.priority(list, flag))
        case let .togglePriority(id):
            guard let list = day.list(id) else { break }
            let suggestion = engine.flagSuggestion(list.path)
            if list.flag == nil {
                settle(suggestion, accept: suggestion != nil)
                wrote(writes.priority(list, suggestion?.flag ?? .medium))
            } else {
                settle(suggestion, accept: false)
                wrote(writes.priority(list, nil))
            }
        case let .removePriority(id):
            guard let list = day.list(id) else { break }
            settle(engine.flagSuggestion(list.path), accept: false)
            wrote(writes.priority(list, nil))

        // MARK: The end of the day
        case let .carry(id, option):
            drafts.carry[id] = option
        case .saveWrap:
            var moves: [JSONValue] = []
            for task in engine.carrying {
                let pick = drafts.carry[task.id] ?? notes.carried[task.id] ?? "tomorrow"
                switch pick {
                case "drop":
                    wrote(writes.status(task, .abandoned))
                    moves.append(.object(["task": .string(task.path), "to": "dropped"]))
                case "nextweek":
                    let monday = engine.whenOptions.first { $0.id == "nextweek" }?.notBefore ?? day.startOfTomorrow
                    wrote(writes.move(task, to: monday))
                    moves.append(.object(["task": .string(task.path), "to": .string(CivilDay(monday, in: calendar).description)]))
                default:
                    wrote(writes.move(task, to: day.startOfTomorrow))
                    moves.append(.object(["task": .string(task.path), "to": .string(day.today.adding(days: 1, in: calendar).description)]))
                }
                notes.carried[task.id] = pick
            }
            wrote(writes.plan(["wrapSavedAt": TodayWrites.stamp(now), "moves": .array(moves)]))
            drafts.carry = [:]

        // MARK: Add or ask
        case let .addOrAsk(text):
            ask(.day, text)
        case let .addUnanswered(id):
            guard let exchange = day.exchanges.first(where: { $0.id == id && $0.subject == .day && $0.state == .failed }) else { break }
            add(exchange.question)
            out.append(.kept(id))
        case let .ask(subject, text):
            ask(subject, text)
        case let .chatThread(scope, thread):
            guard !scope.isEmpty, !thread.isEmpty, state.threads[scope]?.thread != thread else { break }
            out.append(.threadKept(scope: scope, thread: thread, at: now))
        case let .closeAnswer(id):
            out.append(.answerClosed(id))
        case .stopAnswer:
            out.append(.stopped)
        case let .send(id):
            notes.sent[id] = now

        // MARK: Settings
        case let .instruction(id, on):
            guard let instruction = day.instructions.first(where: { $0.id == id }), instruction.active != on else { break }
            wrote(writes.instruction(instruction, ["active": .bool(on)]))
        case let .rewriteInstruction(id, text):
            let words = text.trimmingCharacters(in: .whitespacesAndNewlines)
            guard let instruction = day.instructions.first(where: { $0.id == id }), !words.isEmpty, words != instruction.text else { break }
            wrote(writes.instruction(instruction, ["text": .string(words)]))
        case let .settings(row):
            if let change = TodaySettings.change(row) { wrote(writes.preference(change)) }

        // MARK: The app
        case .looked, .foreground:
            if command == .foreground { out.append(.records(.foregrounded)) }
            out.append(.looked(at: now, today: day.today, zone: context.environment.timeZone.identifier))
            // The last look is kept in the repository too, no more often
            // than every five minutes: the assistant prepares nothing until
            // the preference exists, and reads the owner's time zone there.
            let zone = context.environment.timeZone.identifier
            if state.notesRead, day.discovery.holds(TodayKinds.preference),
                !day.preference.exists || day.preference.timezone != zone || now.timeIntervalSince(day.preference.lastActiveAt ?? .distantPast) > 300
            {
                wrote(writes.preference(["lastActiveAt": TodayWrites.stamp(now), "timezone": .string(zone)]))
            }
        case let .opened(route):
            out.append(.pageOpened(route))
        case .refresh:
            out.append(.records(.refreshAsked))
        case .clearSaved:
            out.append(.records(.clearAsked))
        case let .offered(version):
            out.append(.offered(version))
        case .dismissProblem:
            out.append(.problemDismissed)
        case .notificationsAsked:
            out.append(.permissionWanted)
        }

        if drafts != state.drafts { out.append(.drafted(drafts)) }
        if notes != state.notes, notes != Self.rolled(state.notes, at: now, today: day.today) || state.notes?.day != day.today {
            out.append(.noted(notes))
        }
        return out
    }
}
