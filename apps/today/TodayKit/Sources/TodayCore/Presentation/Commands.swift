import Foundation
import SubstrateCore
import SubstrateScreens

/// A page. Navigation is the views': a view keeps a path of these and asks
/// the presentation for each (`TodayPresentation.page(_:)`). One level: a
/// page opened from a page takes its place, and back returns.
public enum TodayRoute: Codable, Hashable, Sendable {
    /// The priorities, where a project's flag is set, added or removed.
    case priorities
    /// Every project, the flagged ones first.
    case projects
    /// One project, by its list's id.
    case project(String)
    /// One person, by the contact's id.
    case person(String)
    /// One meeting, by the occurrence's id.
    case meeting(String)
    /// One task, or one suggested task, by its id.
    case task(String)
    /// The plan of the day.
    case plan
    /// The suggested tasks, grouped by project.
    case suggested
}

/// What the assistant is asked about: the thing its box sits under. The
/// record goes with the question as the turn's context.
public enum MossSubject: Codable, Hashable, Sendable {
    case task(String)
    case project(String)
    case person(String)
    case meeting(String)
    /// The day itself: a question typed into "Add or ask".
    case day
}

public extension MossSubject {
    /// What a conversation with the assistant is kept under. A thing has
    /// one conversation for as long as it lives, so a question about a
    /// task a week later follows what was said before; the day has one a
    /// day, in the owner's time zone.
    func scope(on day: CivilDay) -> String {
        switch self {
        case let .task(id): "task:" + id
        case let .project(id): "project:" + id
        case let .person(path): "person:" + path
        case let .meeting(id): "meeting:" + id
        case .day: "day:" + day.description
        }
    }
}

/// The answers a suggested task has, always: the three the assistant may
/// advise, and the owner's own words.
public enum SuggestedAnswer: String, Codable, Hashable, Sendable, CaseIterable {
    /// Make it a task.
    case task
    /// Already done.
    case done
    /// Dismiss.
    case dismiss
    /// Other: the owner says what is wrong, or what to do instead. Offered
    /// only where the repository keeps such words (`today/correction`).
    case other

    /// The three the assistant may advise. It never advises the owner's words.
    public static let advised: [SuggestedAnswer] = [.task, .done, .dismiss]
}

/// What the owner may choose on something urgent. Urgent is never the
/// owner's to put on, only to take off.
public enum UrgentChoice: String, Codable, Hashable, Sendable, CaseIterable {
    /// It becomes what cannot wait: it leads, and the current task waits.
    case takeNow
    /// It stays in what is happening with the urgent pill.
    case leave
    /// The urgent pill comes off and the flag it had comes back.
    case notUrgent
}

/// Everything a view can ask for. A view sends one of these and draws the
/// presentation that follows; it decides nothing itself.
///
/// Three kinds of command, by what the design says of the control:
/// - **A choice that takes effect at once** (a flag, "When", a project,
///   what was not logged yesterday, something urgent, a priority): the
///   write is made when the command arrives.
/// - **A draft above a Save** (`suggest`, the `plan…` commands, `carry`):
///   nothing is written until the card's Save (`saveSuggested`, `planSave`,
///   `saveWrap`).
/// - **An action** (`done`, `log`, `send`, `gotIt`, `readNight`).
public enum TodayCommand: Codable, Equatable, Hashable, Sendable {
    // MARK: Routines

    /// Every open occurrence of a routine row (a group and its slot, or one
    /// series) is logged: `done` true is Taken or Done, false is Skip.
    case log(row: String, done: Bool)
    /// One occurrence of a group, by its id (`<series id>_<slot>`).
    case logOccurrence(String, done: Bool)
    /// A log is taken back: the occurrence is open again.
    case unlog(String)
    /// Every log of a routine row is taken back: the row is open again.
    case unlogRow(String)
    /// Later, from a notification: the row's reminder comes again in an hour.
    case later(row: String)

    // MARK: What was not logged yesterday (a choice, at once)

    /// One routine row yesterday left with nothing logged, and the option
    /// chosen for it (`took`, `skipped`). A task yesterday left is answered
    /// on the plan page (`planLeft`).
    case answerYesterday(item: String, option: String)

    // MARK: The night and what is happening (actions)

    /// The night is read: "Got it, all read".
    case readNight
    /// "Got it" on what is happening: one project's, or everything shown
    /// (nil). It moves the review mark and nothing else.
    case gotIt(project: String?)
    /// The one thing the night left to decide, for a project: an option.
    case answerNight(project: String, option: String)

    // MARK: Suggested tasks (a draft, then Save)

    /// The owner's pick for one suggested task. Nothing takes effect.
    case suggest(String, SuggestedAnswer)
    /// Save, under a project's suggestions on their page or under one
    /// drawn alone: the ones it draws, by id, and nothing it does not draw
    /// is answered. `words` is what was typed under "Other", by task: the
    /// view's until now, as every typed text is.
    case saveSuggested([String], words: [String: String])
    /// The rest of a project's suggestions are drawn: the page shows six
    /// of a group until this. The group's id is the list's, or `none`.
    case showSuggested(group: String)

    // MARK: The plan (a draft, then Save)

    /// A tick row of the plan page is toggled.
    case planPick(String)
    /// A flag chosen on the plan page: written to its task on Save.
    case planFlag(String, Flag)
    /// An overlap's option, on the plan page.
    case planOverlap(String, option: String)
    /// A message row is ticked or unticked. Never ticked in advance.
    case planSend(String)
    /// Where a task yesterday left goes (`today`, `tomorrow`, `drop`), on
    /// the plan page; the option chosen again takes the choice back.
    case planLeft(String, option: String)
    /// The draft is dropped: the page shows what the assistant suggests again.
    case planReset
    /// Save the plan.
    case planSave

    // MARK: An overlap, outside the plan (a choice, at once)

    case decideOverlap(String, option: String)

    // MARK: A task (choices, at once; and its one action)

    /// High, Medium or Low. On something urgent, choosing a flag takes
    /// urgent off. Choosing the flag the assistant suggests accepts its
    /// request; choosing another rejects it.
    case setFlag(task: String, Flag)
    /// A flag list was closed: the order of the day follows the flags now.
    case settleFlags
    /// "When": one option of the task's list.
    case setWhen(task: String, option: String)
    /// The task's project: a list's id, or nil for none.
    case setProject(task: String, project: String?)
    /// Done.
    case done(String)
    /// A finished task is open again.
    case reopen(String)
    /// Where an unfinished thing flagged High goes, when the assistant asks.
    case moveTo(task: String, option: String)

    // MARK: Urgent (a choice, at once)

    case urgent(task: String, UrgentChoice)

    // MARK: Priorities (choices, at once)

    /// A project's flag, on the priorities page, the strip or its own page.
    case setPriority(project: String, Flag)
    /// A project joins the priorities, or leaves them when it is there
    /// (the square rows of "Add a project").
    case togglePriority(project: String)
    /// Remove, on the priorities page.
    case removePriority(project: String)

    // MARK: The end of the day (a draft, then Save)

    /// Where one unfinished thing goes: an option of its list.
    case carry(task: String, option: String)
    /// Save and close the day.
    case saveWrap

    // MARK: Add or ask, and the assistant

    /// What was typed or dictated into the field. All of it goes to the
    /// assistant, which adds a task where the words are one and answers
    /// where they ask.
    case addOrAsk(String)
    /// The words of an exchange about the day that the assistant could not
    /// take become a task at once, so they are not lost: by the exchange's
    /// id. Its card says so.
    case addUnanswered(String)
    /// A question or an instruction about one thing, from its box.
    case ask(MossSubject, String)
    /// The chat's screen says the thread a conversation is in (`ChatDoor`):
    /// kept under its scope, so whatever is asked there next, from that
    /// screen or from a box, continues it.
    case chatThread(scope: String, thread: String)
    /// The answer in place is closed.
    case closeAnswer(String)
    /// The answer still being written is stopped.
    case stopAnswer

    // MARK: Messages

    /// Send, under a message that is ready: the message's id.
    case send(String)

    // MARK: Settings (choices, at once)

    /// One of the assistant's standing instructions is turned on or off.
    case instruction(String, on: Bool)
    /// Its text is rewritten.
    case rewriteInstruction(String, text: String)
    /// A row of the app's own settings: when the assistant prepares, and
    /// how long an urgent mark nobody checked waits.
    case settings(SettingsCommand)

    // MARK: The app

    /// The owner looked: the app came forward or the pane was opened.
    case looked
    /// A page is on the screen: sent once each time one appears. It writes
    /// nothing. A project's page has the records its timeline was written
    /// from read for their links, a few at a time.
    case opened(TodayRoute)
    case refresh
    /// The app came to the front: what the system may have changed is read
    /// again.
    case foreground
    case clearSaved
    /// The updater found a newer build, or none.
    case offered(String?)
    case dismissProblem
    /// Notifications were allowed or refused when the app asked.
    case notificationsAsked
}
