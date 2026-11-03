import Foundation
@testable import WorkoutsCore
import Testing

// The routine page's and the Stats tab's numbers, over the three example
// sessions: Day 1 on Thu 22 Oct and Mon 26 Oct, Fully Body Collection #1 on
// Tue 27 Oct, read on Fri 30 Oct in London with weeks from Monday.

private func d(_ text: String) -> Decimal { Decimal(string: text, locale: Locale(identifier: "en_US_POSIX"))! }

private enum Stats {
    static let now = DateText.parse("2026-10-30T13:42:00Z")!
    static let dayOne = RoutineID("day-1-strength-foundation")
    static let fullBody = RoutineID("fully-body-collection-1")

    static var settings: PresenterSettings {
        var calendar = Progression.calendar
        calendar.firstWeekday = 2
        return PresenterSettings(calendar: calendar)
    }

    static func history() throws -> [HistoryEntry] {
        try ExampleFixtures.sessions("sessions")
            .sorted { $0.session.at > $1.session.at }
            .map { HistoryEntry(records: $0, local: HistoryLocal(health: HealthSaveState(status: .saved))) }
    }

    static func present(_ scope: StatsScope, _ period: StatsPeriod, catalog: Catalog? = nil) throws -> StatsPresentation {
        StatsPresenter.present(try history(), catalog: try catalog ?? ExampleFixtures.catalog(), scope: scope, period: period,
                               now: now, settings: settings)
    }

    static func session(on day: String) throws -> SessionRecords {
        try #require(ExampleFixtures.sessions("sessions").first { DateText.format($0.session.at).hasPrefix(day) })
    }

    static func total(_ stats: StatsPresentation, _ label: String) throws -> StatsTotal {
        try #require(stats.totals.first { $0.label == label })
    }

    static func exercise(_ stats: StatsPresentation, _ id: ExerciseID) throws -> StatsExercise {
        try #require(stats.exercises.flatMap(\.exercises).first { $0.id == id })
    }
}

@Suite("Stats presenter")
struct StatsPresenterTests {
    @Test("Each period is whole days to the end of today; the period before is as many days")
    func windows() {
        let calendar = Stats.settings.calendar
        func range(_ period: StatsPeriod) -> String {
            StatsPresenter.range(period.window(now: Stats.now, calendar: calendar), calendar: calendar)
        }
        #expect(range(.week) == "Sat 24 Oct \u{2013} Fri 30 Oct")
        #expect(range(.month) == "Wed 30 Sep \u{2013} Fri 30 Oct")
        // 13 and 26 weeks from a Monday; 12 months from the first.
        #expect(range(.threeMonths) == "Mon 3 Aug \u{2013} Fri 30 Oct")
        #expect(range(.sixMonths) == "Mon 4 May \u{2013} Fri 30 Oct")
        #expect(range(.year) == "Sat 1 Nov 2025 \u{2013} Fri 30 Oct 2026")
        let week = StatsPeriod.week.window(now: Stats.now, calendar: calendar)
        #expect(StatsPresenter.range(StatsPeriod.previous(week, calendar: calendar), calendar: calendar)
            == "Sat 17 Oct \u{2013} Fri 23 Oct")
        // The clocks went back on 25 October: still seven days, not 7 × 24 hours.
        #expect(week.duration == 7 * 86400 + 3600)
    }

    @Test("One bar per day, week or month of the period, empty ones included")
    func volumeBars() throws {
        let week = try Stats.present(.all, .week)
        #expect(week.volume.count == 7)
        #expect(week.volume.map(\.sessions) == [0, 0, 1, 1, 0, 0, 0])
        #expect(week.volume[2].label == "Mon 26 Oct")
        #expect(week.volume[0].text == "No sessions")
        let monday = try Stats.session(on: "2026-10-26")
        let volume = StatsPresenter.sessionVolume(monday, exercises: try ExampleFixtures.catalog().exercises, settings: Stats.settings)
        #expect(volume > 0)
        #expect(week.volume[2].value == volume)
        #expect(week.volume[2].text == "\(Format.grouped(Int(volume))) kg, 1 session")
        #expect(try Stats.present(.all, .month).volume.count == 31)
        let quarter = try Stats.present(.all, .threeMonths)
        #expect(quarter.volume.count == 13)
        #expect(quarter.volume.last?.label == "Week of Mon 26 Oct")
        #expect(quarter.volume.last?.sessions == 2)
        #expect(try Stats.present(.all, .sixMonths).volume.count == 26)
        let year = try Stats.present(.all, .year)
        #expect(year.volume.map(\.label).first == "November 2025")
        #expect(year.volume.last?.label == "October 2026")
        #expect(year.volume.last?.sessions == 3)
        #expect(year.volume.reduce(0) { $0 + $1.value } == quarter.volume.reduce(0) { $0 + $1.value })
    }

    @Test("The totals of a week, against the week before")
    func totals() throws {
        let stats = try Stats.present(.all, .week)
        #expect(stats.range == "Sat 24 Oct \u{2013} Fri 30 Oct")
        #expect(stats.comparison == "Compared with Sat 17 Oct \u{2013} Fri 23 Oct")
        #expect(stats.totals.map(\.label) == ["Sessions", "Volume", "Working sets", "Time per session"])
        // A week's count is its rate: no "a week" under it.
        #expect(try Stats.total(stats, "Sessions").detail == nil)
        let sessions = try Stats.total(stats, "Sessions")
        #expect(sessions.value == "2")
        #expect(sessions.change == StatsChange(direction: .up, text: "+1", spoken: "Up 1 on the period before"))

        let exercises = try ExampleFixtures.catalog().exercises
        let (thursday, monday, tuesday) = try (Stats.session(on: "2026-10-22"), Stats.session(on: "2026-10-26"),
                                               Stats.session(on: "2026-10-27"))
        let volume = [monday, tuesday].reduce(0.0) { $0 + StatsPresenter.sessionVolume($1, exercises: exercises, settings: Stats.settings) }
        let before = StatsPresenter.sessionVolume(thursday, exercises: exercises, settings: Stats.settings)
        #expect(try Stats.total(stats, "Volume").value == "\(Format.grouped(Int(volume))) kg")
        let percent = Format.number(abs((volume / before - 1) * 100).rounded(), decimals: 0)
        #expect(try Stats.total(stats, "Volume").change?.text == "\(volume > before ? "+" : "\u{2212}")\(percent)%")
        let sets = Counting.workingSets(monday.sets) + Counting.workingSets(tuesday.sets)
        #expect(try Stats.total(stats, "Working sets").value == "\(sets)")

        // 51:33 less a 4:00 pause, and 46:28.
        #expect(try Stats.total(stats, "Time per session").value == "47 min")
        // Thursday ran 46:56: the same to the minute.
        #expect(try Stats.total(stats, "Time per session").change?.text == "Same")
    }

    @Test("A routine's stats count only the sessions started from it")
    func routineScope() throws {
        let stats = try Stats.present(.routine(Stats.dayOne), .month)
        #expect(stats.scopeTitle == "Day 1 \u{2013} Strength Foundation")
        #expect(stats.hasSessions)
        #expect(try Stats.total(stats, "Sessions").value == "2")
        // Nothing in the month before: no session to compare, no percentage of nothing.
        #expect(try Stats.total(stats, "Sessions").change?.text == "+2")
        #expect(try Stats.total(stats, "Volume").change == nil)
        #expect(stats.sessions.map(\.title) == ["Day 1 \u{2013} Strength Foundation", "Day 1 \u{2013} Strength Foundation"])
        #expect(stats.sessions.map(\.date) == ["Mon 26 Oct, 12:31", "Thu 22 Oct, 12:34"])
        // In the routine's order.
        let routine = try #require(try ExampleFixtures.catalog().routines[Stats.dayOne])
        let order = stats.exercises.flatMap(\.exercises).map(\.id)
        #expect(order == routine.slots.map(\.exercise).filter(order.contains))
        #expect(order.first == "leg-press-machine")
        #expect(StatsPresenter.entries(try Stats.history(), scope: .routine(Stats.dayOne)).count == 2)
    }

    @Test("An exercise's trend over the period: the best, the change, the best set, volume and sets")
    func exerciseProgress() throws {
        let month = try Stats.exercise(try Stats.present(.routine(Stats.dayOne), .month), "leg-press-machine")
        #expect(month.metric == "Estimated 1RM")
        // 22 Oct: 100 kg × 10 at RPE 8 gives 147.1 kg; 26 Oct: 105 kg × 9 at RPE 8 gives 148.5 kg.
        #expect(month.points.map(\.text) == ["147.1 kg", "148.5 kg"])
        #expect(month.best == "148.5 kg")
        #expect(month.change == nil)
        #expect(month.trendSummary == "Estimated 1RM, 147.1 kg on Thu 22 Oct to 148.5 kg on Mon 26 Oct, 2 sessions")
        #expect(month.facts.map(\.label) == ["Best set", "Working sets"])
        // Monday's first set, its RPE pre-filled and so no rating (shown without one).
        #expect(month.facts.first?.value == "105 kg \u{00D7} 9")

        // The week: Monday's 148.5 kg against Thursday's 147.1 kg.
        let week = try Stats.exercise(try Stats.present(.routine(Stats.dayOne), .week), "leg-press-machine")
        #expect(week.points.map(\.text) == ["148.5 kg"])
        #expect(week.change == StatsChange(direction: .up, text: "+1.4 kg", spoken: "Up 1.4 kg on the period before"))
        #expect(week.trendSummary == "Estimated 1RM 148.5 kg on Mon 26 Oct, 1 session")

        let all = try Stats.present(.all, .week)
        let plank = try Stats.exercise(all, "plank")
        #expect(plank.metric == "Longest hold" && plank.best == "1:02")
        #expect(!plank.facts.contains { $0.label == "Volume" })
        // Every exercise of the week has a row, none twice.
        let ids = all.exercises.flatMap(\.exercises).map(\.id)
        #expect(Set(ids).count == ids.count)
        let sets = try [Stats.session(on: "2026-10-26"), Stats.session(on: "2026-10-27")].flatMap(\.sets)
        let done = Set(sets.filter { $0.role == .working }.map(\.exercise))
        #expect(Set(ids) == done)
        // An exercise done only as a warm-up has no row.
        var warmupOnly = try Stats.history()
        let first = try #require(warmupOnly.first)
        var records = first.records
        records.sets = records.sets.map { set in
            var set = set
            if set.exercise == "plank" { set.role = .warmup }
            return set
        }
        warmupOnly[0] = HistoryEntry(records: records, local: first.local)
        let without = StatsPresenter.present(warmupOnly, catalog: try ExampleFixtures.catalog(), scope: .all, period: .week, now: Stats.now,
                                             settings: Stats.settings)
        #expect(!without.exercises.flatMap(\.exercises).contains { $0.id == "plank" })
    }

    @Test("A record is a best above every earlier session's, in any routine; a first session beats nothing")
    func records() throws {
        let week = try Stats.present(.all, .week)
        let legPress = week.records.filter { $0.exercise == "leg-press-machine" }
        #expect(legPress.map(\.kind) == ["Estimated 1RM", "Heaviest load"])
        let e1RM = try #require(legPress.first)
        #expect(e1RM.value == "148.5 kg" && e1RM.previous == "147.1 kg" && e1RM.date == "Mon 26 Oct")
        #expect(e1RM.session == (try Stats.session(on: "2026-10-26")).session.id)
        // Fully Body's exercises were new on Tuesday, except the leg press and the seated row.
        #expect(!week.records.contains { $0.exercise == "plank" || $0.exercise == "chest-press-machine" })
        #expect(week.records.map(\.at) == week.records.map(\.at).sorted(by: >))
        // A routine's records still beat sessions of every routine.
        #expect(try Stats.present(.routine(Stats.fullBody), .week).records.allSatisfy { $0.session != e1RM.session })
        #expect(try Stats.present(.all, .week).records.count == week.records.count)
        // The month holds Thursday too, which beat nothing.
        #expect(try Stats.present(.all, .month).records.allSatisfy { $0.date != "Thu 22 Oct" })
    }

    @Test("Working sets per muscle group, weighted by share, and per muscle where the exercise gives shares")
    func muscles() throws {
        var catalog = try ExampleFixtures.catalog()
        catalog.movements["chest-press"] = Movement(id: "chest-press", name: "Chest Press",
                                                    muscles: [MuscleShare(.pecsternal, 1), MuscleShare(.deltfront, d("0.5")),
                                                              MuscleShare(.tricepslateral, d("0.5"))])
        catalog.exercises["chest-press-machine"]?.movement = "chest-press"
        catalog.exercises["iso-lateral-chest-press-machine"]?.movement = "chest-press"
        catalog.exercises["leg-press-machine"]?.muscles = [MuscleShare(.vasti, 1), MuscleShare(.glutemax, d("0.5")),
                                                           MuscleShare(.adductors, d("0.25"))]
        catalog.resolveMovements()
        let stats = try Stats.present(.all, .week, catalog: catalog)

        let sets = try [Stats.session(on: "2026-10-26"), Stats.session(on: "2026-10-27")].flatMap(\.sets)
        let groups = Counting.setsPerMuscle(sets, exercises: catalog.exercises)
        #expect(stats.muscles.map(\.muscle).sorted { $0.rawValue < $1.rawValue }
            == groups.filter { $0.value > 0 }.keys.sorted { $0.rawValue < $1.rawValue })
        #expect(stats.muscles.map(\.sets) == stats.muscles.map(\.sets).sorted(by: >))
        for group in stats.muscles {
            #expect(group.sets == groups[group.muscle]?.double)
        }
        func working(_ id: ExerciseID) -> Int { Counting.workingSets(sets.filter { $0.exercise == id }) }
        let chest = try #require(stats.muscles.first { $0.muscle == .chest })
        let pressSets = working("chest-press-machine") + working("iso-lateral-chest-press-machine")
        #expect(chest.muscles.map(\.label) == ["Chest, mid and lower (sternal pectoralis)"])
        #expect(chest.muscles.first?.sets == Double(pressSets))
        let quads = try #require(stats.muscles.first { $0.muscle == .quadriceps })
        #expect(quads.muscles.map(\.muscle) == [.vasti])
        #expect(quads.muscles.first?.text == "\(working("leg-press-machine")) sets")
        let adductors = try #require(stats.muscles.first { $0.muscle == .adductors })
        #expect(adductors.muscles.first?.sets == Double(working("leg-press-machine")) * 0.25)
        // A group no exercise of the week gives shares for lists no muscles.
        #expect(try #require(stats.muscles.first { $0.muscle == .hamstrings }).muscles.isEmpty)

        // Two equipment versions of one movement are one group, each with its own trend.
        let press = try #require(stats.exercises.first { $0.title == "Chest Press" })
        #expect(press.detail == "2 equipment versions")
        #expect(Set(press.exercises.map(\.id)) == ["chest-press-machine", "iso-lateral-chest-press-machine"])
        #expect(stats.exercises.filter { $0.exercises.contains { $0.id == "chest-press-machine" } }.count == 1)
        #expect(stats.exercises.filter { $0.title == nil }.allSatisfy { $0.exercises.count == 1 })
    }

    @Test("A routine never done, and a period with no session")
    func empty() throws {
        let never = try Stats.present(.routine("day-3"), .year)
        #expect(!never.hasSessions && never.isEmpty)
        #expect(never.scopeTitle == "Day 3")
        #expect(try Stats.total(never, "Sessions").value == "0")
        #expect(try Stats.total(never, "Sessions").change == nil)
        #expect(try Stats.total(never, "Time per session").value == "None")
        #expect(never.balance.isEmpty && never.lastSession == nil)
        #expect(never.consistency?.value == "0 of \(never.weeks.count)" && never.consistency?.streak == nil)
        #expect(never.exercises.isEmpty && never.records.isEmpty && never.muscles.isEmpty && never.sessions.isEmpty)

        // A week after the last session: the routine has sessions, the week none.
        let later = StatsPresenter.present(try Stats.history(), catalog: try ExampleFixtures.catalog(), scope: .routine(Stats.dayOne),
                                           period: .week, now: Stats.now.addingTimeInterval(9 * 86400), settings: Stats.settings)
        #expect(later.hasSessions && later.isEmpty)
        #expect(try Stats.total(later, "Sessions").change?.text == "\u{2212}1")
        #expect(later.sessions.count == 2)
    }

    @Test("The routine page's header: the folder and what the plan holds")
    func summary() throws {
        let catalog = try ExampleFixtures.catalog()
        let routine = try #require(catalog.routines[Stats.dayOne])
        let summary = StatsPresenter.summary(routine, history: try Stats.history(), settings: Stats.settings)
        #expect(summary.folder == "Full Body 3x Week (Back-Friendly)")
        let working = routine.slots.reduce(0) { $0 + $1.sets.count { $0.role == .working } }
        #expect(summary.facts == [LabelledValue("Exercises", "5"), LabelledValue("Sets per session", "\(working)"),
                                  LabelledValue("Last done", "Mon 26 Oct")])
        let never = StatsPresenter.summary(try #require(catalog.routines["day-3"]), history: try Stats.history(), settings: Stats.settings)
        #expect(never.facts.map(\.label) == ["Exercises", "Sets per session"])
    }

    @Test("Sessions per week, weeks trained and the weeks in a row")
    func consistency() throws {
        // Thursday 22 Oct, then Monday and Tuesday of the week of 26 Oct.
        let week = try Stats.present(.all, .week)
        #expect(week.consistency == nil)
        #expect(week.weeks.map(\.label) == ["Week of Mon 19 Oct", "Week of Mon 26 Oct"])
        #expect(week.weeks.map(\.sessions) == [0, 2])

        let quarter = try Stats.present(.all, .threeMonths)
        #expect(quarter.weeks.count == 13)
        #expect(quarter.weeks.map(\.sessions).suffix(2) == [1, 2])
        #expect(quarter.weeks.last?.text == "2 sessions" && quarter.weeks.first?.text == "No sessions")
        let consistency = try #require(quarter.consistency)
        #expect(consistency.weeksTrained == 2 && consistency.weeks == 13 && consistency.value == "2 of 13")
        // 3 sessions over 89 days.
        #expect(consistency.perWeek == "0.2")
        #expect(consistency.streak == "2 weeks")
        #expect(consistency.sentence == "Trained in 2 of 13 weeks, 0.2 sessions a week. 2 weeks in a row.")

        // A month's count comes with its rate: 3 sessions over 31 days.
        let month = try Stats.present(.all, .month)
        #expect(try Stats.total(month, "Sessions").detail == "0.7 a week")

        // A week with no session yet breaks nothing; a week missed does.
        let history = try Stats.history().map(\.records)
        let calendar = Stats.settings.calendar
        #expect(StatsPresenter.streakWeeks(history, now: Stats.now.addingTimeInterval(4 * 86400), calendar: calendar) == 2)
        #expect(StatsPresenter.streakWeeks(history, now: Stats.now.addingTimeInterval(11 * 86400), calendar: calendar) == 0)
    }

    @Test("Push against pull over the upper body, and upper body against lower body and core")
    func balance() throws {
        var catalog = try ExampleFixtures.catalog()
        catalog.movements["chest-press"] = Movement(id: "chest-press", name: "Chest Press", force: .push)
        catalog.movements["leg-press"] = Movement(id: "leg-press", name: "Leg Press", force: .push)
        catalog.exercises["chest-press-machine"]?.movement = "chest-press"
        catalog.exercises["leg-press-machine"]?.movement = "leg-press"
        let stats = try Stats.present(.all, .week, catalog: catalog)
        #expect(stats.balance.map(\.id) == ["push-pull", "body"])
        let sets = try [Stats.session(on: "2026-10-26"), Stats.session(on: "2026-10-27")].flatMap(\.sets)
            .filter { $0.role == .working && $0.continues == nil }
        let pushPull = try #require(stats.balance.first)
        #expect(pushPull.parts.map(\.label) == ["Push", "Pull"])
        // The leg press pushes, but on the legs: left out.
        let push = sets.count { set in
            let exercise = catalog.exercises[set.exercise]
            return exercise.map { StatsPresenter.topGroup($0).map(StatsPresenter.upperBody.contains) == true
                && StatsPresenter.force(of: $0, catalog: catalog) == .push } ?? false
        }
        #expect(pushPull.parts[0].sets == Double(push))
        #expect(!sets.filter { $0.exercise == "leg-press-machine" }.isEmpty)
        // The seated row has no movement: its upper back reads as a pull.
        #expect(StatsPresenter.force(of: try #require(catalog.exercises["seated-row-machine"]), catalog: catalog) == .pull)
        let percents: [Int] = pushPull.parts.compactMap { Int($0.percent.dropLast()) }
        #expect(percents.reduce(0, +) == 100)
        #expect(pushPull.sentence.hasPrefix("Push \(pushPull.parts[0].percent), "))

        let body = try #require(stats.balance.last)
        #expect(body.parts.map(\.label).prefix(2) == ["Upper body", "Lower body"])
        let groups = Counting.setsPerMuscle(sets, exercises: catalog.exercises)
        let lower = groups.filter { StatsPresenter.lowerBody.contains($0.key) }.reduce(0.0) { $0 + $1.value.double }
        #expect(body.parts[1].sets == lower)
        #expect(abs(body.parts.reduce(0) { $0 + $1.share } - 1) < 1e-9)

        #expect(StatsPresenter.roundedPercents([1.0 / 3, 1.0 / 3, 1.0 / 3]) == [34, 33, 33])
        #expect(StatsPresenter.roundedPercents([0.5, 0.5]) == [50, 50])
        #expect(StatsPresenter.roundedPercents([0, 0]) == [0, 0])
    }

    @Test("The newest session against the one before it of its routine")
    func lastSession() throws {
        let routine = try #require(try Stats.present(.routine(Stats.dayOne), .week).lastSession)
        let (thursday, monday) = try (Stats.session(on: "2026-10-22"), Stats.session(on: "2026-10-26"))
        #expect(routine.session == monday.session.id && routine.previous == thursday.session.id)
        #expect(routine.title == "Mon 26 Oct" && routine.against == "Against Thu 22 Oct")
        #expect(routine.totals.map(\.label) == ["Time", "Volume", "Working sets"])
        // Monday's 51:33 less a 4:00 pause against Thursday's 46:56.
        #expect(routine.totals[0].value == "47 min")
        #expect(routine.totals[0].change == StatsChange(direction: .up, text: "+1 min", spoken: "Up 1 min on the session before"))
        #expect(routine.totals[2].change?.spoken.hasSuffix("the session before") ?? true)
        // Only the exercises with a best on both days count.
        let (better, shared) = try Self.better(monday, than: thursday)
        #expect(shared > 0 && shared <= 5)
        #expect(routine.exercises == "Better on \(better) of \(Format.count(shared, "exercise"))")
        // Every routine: Tuesday's Fully Body is its routine's first, so nothing to compare.
        #expect(try Stats.present(.all, .week).lastSession == nil)
        // Without Tuesday, Monday's Day 1 against Thursday's, named.
        let earlier = try Stats.history().filter { $0.records.session.routine == Stats.dayOne }
        let all = StatsPresenter.present(earlier, catalog: try ExampleFixtures.catalog(), scope: .all, period: .week, now: Stats.now,
                                         settings: Stats.settings)
        #expect(all.lastSession?.title == "Mon 26 Oct, Day 1 \u{2013} Strength Foundation")
    }

    /// How many exercises of `today` beat `before`'s best by more than 1%,
    /// of those with a best on both days.
    static func better(_ today: SessionRecords, than before: SessionRecords) throws -> (Int, Int) {
        let catalog = try ExampleFixtures.catalog()
        let (now, then) = (StatsPresenter.byExercise([today]), StatsPresenter.byExercise([before]))
        var (better, shared) = (0, 0)
        for (id, entries) in now {
            guard let exercise = catalog.exercises[id], let earlier = then[id] else { continue }
            let config = SuggestionInputs.config(for: exercise, defaultUnit: .kg)
            guard let a = ExercisePresenter.trend(exercise, sessions: entries, config: config).points.map(\.value).max(),
                  let b = ExercisePresenter.trend(exercise, sessions: earlier, config: config).points.map(\.value).max(),
                  b > 0 else { continue }
            shared += 1
            if a > b * 1.01 { better += 1 }
        }
        return (better, shared)
    }

    @Test("A change is signed, said in words, and the same when it rounds to nothing")
    func changes() {
        #expect(StatsPresenter.count(3, 5) == StatsChange(direction: .down, text: "\u{2212}2", spoken: "Down 2 on the period before"))
        #expect(StatsPresenter.count(0, 0) == nil)
        #expect(StatsPresenter.percent(110, 100)?.text == "+10%")
        #expect(StatsPresenter.percent(100.2, 100)?.direction == .same)
        #expect(StatsPresenter.percent(5, 0) == nil)
        #expect(StatsPresenter.metricChange(-5, metric: .longestHold, unit: .kg)?.text == "\u{2212}0:05")
        #expect(StatsPresenter.metricChange(1, metric: .mostReps, unit: .kg)?.text == "+1 rep")
        #expect(StatsPresenter.metricChange(0.04, metric: .oneRepMax, unit: .lb)?.text == "Same")
        #expect(StatsPresenter.metricChange(3, metric: .none, unit: .kg) == nil)
        #expect(StatsPresenter.setsText(1) == "1 set")
        #expect(StatsPresenter.setsText(d("0.5")) == "0.5 sets")
    }
}
