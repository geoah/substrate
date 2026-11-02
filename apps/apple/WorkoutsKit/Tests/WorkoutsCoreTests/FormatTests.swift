import Foundation
@testable import WorkoutsCore
import Testing

// DESIGN §10.1 FormatTests: every formatter example in §4.13 and research
// 03 §12, plus the labels and headers the screens read from the same place.

private func d(_ text: String) -> Decimal { Decimal(string: text, locale: Locale(identifier: "en_US_POSIX"))! }

@Suite("Formatting, DESIGN §4.13 and 03 §12")
struct FormatTests {
    @Test("Durations: m:ss under an hour, h:mm:ss from an hour, Off for a rest of 0")
    func durations() {
        #expect(Format.duration(53) == "0:53")
        #expect(Format.duration(90) == "1:30")
        #expect(Format.duration(62) == "1:02")
        #expect(Format.duration(0) == "0:00")
        #expect(Format.duration(599) == "9:59")
        #expect(Format.duration(3599) == "59:59")
        #expect(Format.duration(3600) == "1:00:00")
        #expect(Format.duration(3725) == "1:02:05")
        #expect(Format.duration(-5) == "0:00")
        #expect(Format.rest(0) == "Off")
        #expect(Format.rest(90) == "1:30")
        #expect(Format.overtime(5) == "+0:05")
        #expect(Format.durationRange(45, 90) == "0:45–1:30")
        #expect(Format.minutes(46 * 60 + 28) == "46 min")
        #expect(Format.minutes(72 * 60) == "1 h 12 min")
        #expect(Format.minutes(2 * 3600) == "2 h")
        #expect(Format.minutes(45) == "45 s")
    }

    @Test("Loads: at most two decimals, trailing zeros removed, a space, the unit")
    func loads() {
        #expect(Format.mass(.kg(35)) == "35 kg")
        #expect(Format.mass(.kg(d("92.5"))) == "92.5 kg")
        #expect(Format.mass(.kg(d("1.25"))) == "1.25 kg")
        #expect(Format.mass(.lb(190)) == "190 lb")
        #expect(Format.mass(.kg(d("35.00"))) == "35 kg")
        #expect(Format.mass(.kg(d("83.9146"))) == "83.91 kg")
        #expect(Format.mass(22.5, unit: .kg) == "22.5 kg")
        #expect(Format.mass(185.0000255, unit: .lb) == "185 lb")
        #expect(Format.number(d("0")) == "0")
        #expect(Format.number(-0.001) == "0")
    }

    @Test("Bodyweight: BW, an added load BW + 2.5 kg, assistance BW − 30 kg (U+2212)")
    func bodyweight() {
        #expect(Format.load(SetValues(), mode: .bodyweight) == "BW")
        #expect(Format.load(SetValues(load: .kg(d("2.5"))), mode: .bodyweightplus) == "BW + 2.5 kg")
        #expect(Format.load(SetValues(load: .kg(0)), mode: .bodyweightplus) == "BW")
        #expect(Format.load(SetValues(assistance: .kg(30)), mode: .assisted) == "BW \u{2212} 30 kg")
        #expect(Format.load(SetValues(assistance: .lb(60)), mode: .assisted) == "BW − 60 lb")
        #expect(Format.load(SetValues(load: .kg(35)), mode: nil) == nil)
    }

    @Test("A set: load × reps with U+00D7, reps only, timed, distance, and RPE as @ 9.5")
    func sets() {
        #expect(Format.set(SetValues(load: .kg(35), reps: 13), measurement: .weightreps) == "35 kg \u{00D7} 13")
        #expect(Format.set(SetValues(reps: 15), measurement: .repsonly) == "× 15")
        #expect(Format.set(SetValues(reps: 10), measurement: .bodyweightreps) == "BW × 10")
        #expect(Format.set(SetValues(load: .kg(d("2.5")), reps: 9), measurement: .weightedbodyweightreps) == "BW + 2.5 kg × 9")
        #expect(Format.set(SetValues(assistance: .kg(30), reps: 12), measurement: .bodyweightassistedreps) == "BW − 30 kg × 12")
        #expect(Format.set(SetValues(duration: 62), measurement: .duration) == "1:02")
        #expect(Format.set(SetValues(load: .kg(10), duration: 62), measurement: .weightduration) == "10 kg for 1:02")
        #expect(Format.set(SetValues(distance: Length(metres: 40)), measurement: .distanceduration) == "40 m")
        #expect(Format.set(SetValues(duration: 125, distance: Length(metres: 500)), measurement: .distanceduration) == "500 m in 2:05")
        #expect(Format.set(SetValues(load: .kg(20), distance: Length(metres: 40)), measurement: .shortdistanceweight)
            == "20 kg for 40 m")
        #expect(Format.set(SetValues(load: .kg(35), reps: 13, rpe: d("9.5")), measurement: .weightreps, rpe: true)
            == "35 kg × 13 @ 9.5")
        #expect(Format.set(SetValues(load: .kg(35), reps: 13, rpe: d("9.5")), measurement: .weightreps) == "35 kg × 13")
        #expect(Format.set(SetValues(reps: 14, rpe: 9), measurement: .bodyweightreps, rpe: true) == "BW × 14 @ 9")
        #expect(Format.set(SetValues(reps: 15), measurement: .bodyweightreps, repsHint: true) == "BW × \u{2248}15")
        #expect(Format.set(SetValues(load: .kg(25)), measurement: .weightreps) == "25 kg")
        #expect(Format.set(SetValues(), measurement: .weightreps) == "")
    }

    @Test("RPE: at most one decimal, trailing zero removed; ranges with an en dash")
    func rpeAndRanges() {
        #expect(Format.rpe(d("9.5")) == "9.5")
        #expect(Format.rpe(8) == "8")
        #expect(Format.rpe(d("8.0")) == "8")
        #expect(Format.rpe(d("8.25")) == "8.3")
        #expect(Format.range(10, 12) == "10\u{2013}12")
        #expect(Format.range(5, 5) == "5")
    }

    @Test("Volume and counts: a comma between thousands, singular for one")
    func volumeAndCounts() {
        #expect(Format.volume(.kg(375)) == "375 kg")
        #expect(Format.volume(.kg(3120)) == "3,120 kg")
        #expect(Format.volume(.kg(1_234_567)) == "1,234,567 kg")
        #expect(Format.volume(.lb(0)) == "0 lb")
        #expect(Format.count(1, "set") == "1 set")
        #expect(Format.count(24, "set") == "24 sets")
        #expect(Format.grouped(-1000) == "-1,000")
    }

    @Test("Dates in en_GB on the session's clock: Tue 27 Oct, 13:30; October 2026")
    func dates() throws {
        let london = try #require(TimeZone(identifier: "Europe/London"))
        let gmt = try #require(DateText.parse("2026-10-27T13:30:00Z"))
        #expect(Format.day(gmt, in: london) == "Tue 27 Oct")
        #expect(Format.dayAndTime(gmt, in: london) == "Tue 27 Oct, 13:30")
        #expect(Format.dayWithYear(gmt, in: london) == "Tue 27 Oct 2026")
        #expect(Format.month(gmt, in: london) == "October 2026")
        // Before the clocks went back, BST: 11:34Z is 12:34 on the wall.
        let bst = try #require(DateText.parse("2026-10-22T11:34:08Z"))
        #expect(Format.time(bst, in: london) == "12:34")
    }

    @Test("A suggested load becomes a Mass of at most two decimals")
    func suggestedMass() {
        #expect(Mass(suggested: 22.5, unit: .kg) == .kg(d("22.5")))
        #expect(Mass(suggested: 83.91000000001, unit: .kg) == .kg(d("83.91")))
        #expect(Mass(suggested: 190, unit: .lb) == .lb(190))
    }
}

@Suite("Labels and grid headers")
struct LabelTests {
    @Test("Grid columns per measurement (§7.6): SET and PREVIOUS first, the tick last, RPE on rep sets")
    func columns() {
        #expect(MeasurementType.weightreps.gridColumns == [.set, .previous, .load, .reps, .rpe, .tick])
        #expect(MeasurementType.repsonly.gridColumns == [.set, .previous, .reps, .rpe, .tick])
        #expect(MeasurementType.bodyweightreps.gridColumns == [.set, .previous, .reps, .rpe, .tick])
        #expect(MeasurementType.weightedbodyweightreps.gridColumns == [.set, .previous, .addedLoad, .reps, .rpe, .tick])
        #expect(MeasurementType.bodyweightassistedreps.gridColumns == [.set, .previous, .assistance, .reps, .rpe, .tick])
        #expect(MeasurementType.duration.gridColumns == [.set, .previous, .time, .tick])
        #expect(MeasurementType.weightduration.gridColumns == [.set, .previous, .load, .time, .tick])
        #expect(MeasurementType.distanceduration.gridColumns == [.set, .previous, .distance, .time, .tick])
        #expect(MeasurementType.shortdistanceweight.gridColumns == [.set, .previous, .load, .distance, .tick])
        #expect(MeasurementType.shortdistanceweight.hasStopwatchBelowRow)
        #expect(MeasurementType.weightreps.gridColumns.map { $0.title(unit: .kg) } == ["SET", "PREVIOUS", "KG", "REPS", "RPE", ""])
        #expect(MeasurementType.weightedbodyweightreps.valueColumns.map { $0.title(unit: .lb) } == ["+LB", "REPS"])
        #expect(MeasurementType.bodyweightassistedreps.valueColumns.map { $0.title(unit: .kg) } == ["\u{2212}KG", "REPS"])
        #expect(MeasurementType.distanceduration.valueColumns.map { $0.title(unit: .kg) } == ["M", "TIME"])
    }

    @Test("The RPE sheet's words, half steps 6 to 10 (§7.6), and the CR10 words (§7.7)")
    func effortWords() {
        #expect(RPEScaleText.values.count == 9)
        #expect(RPEScaleText.headline(d("8.5")) == "Hard")
        #expect(RPEScaleText.reserve(d("8.5")) == "Could do 1 more rep, maybe 2")
        #expect(RPEScaleText.headline(10) == "Maximum effort")
        #expect(RPEScaleText.reserve(10) == "Could not do more reps")
        #expect(RPEScaleText.headline(d("6.5")) == "Moderate effort")
        #expect(RPEScaleText.reserve(6) == "Could do 4 or more reps")
        #expect(RPEScaleText.headline(d("5.5")) == nil)
        #expect(RPEScaleText.values.allSatisfy { RPEScaleText.headline($0) != nil && RPEScaleText.reserve($0) != nil })
        #expect(SessionRPEText.word(1) == "very, very easy")
        #expect(SessionRPEText.word(6) == "hard")
        #expect(SessionRPEText.word(8) == "very hard")
        #expect(SessionRPEText.word(10) == "maximal")
        #expect(SessionRPEText.word(0) == nil)
    }

    @Test("The kind files' labels")
    func kindLabels() {
        #expect(MeasurementType.weightreps.label == "Weight and reps")
        #expect(MeasurementType.allCases.map(\.label).count == 9)
        #expect(Equipment.plateloaded.label == "Plate-loaded machine")
        #expect(GroupKind.triset.label == "Tri-set")
        #expect(GroupKind.paired.label == "Paired sets")
        #expect(Pairing.upperlower.label == "Upper with lower body")
        #expect(Muscle.upperback.label == "Upper back")
        #expect(Muscle.chest.label == "Chest")
        #expect(Laterality.unilateral.label == "One side at a time")
        #expect(SetKind.myomatch.label == "Myo-rep match")
        #expect(RestSlot.blockRest.label == "Rest before next exercise")
        #expect(RestSlot.sideSwitch.label == "Switch sides")
        #expect(groupMemberLabel(group: "a", position: 2) == "A2")
    }

    @Test("Labels of records (§4.10): W, 1, 2, 2a for the 27 October chest press; 1L, 1R for the split squat")
    func recordLabels() throws {
        let session = try EngineFixtures.session("sessions", EngineFixtures.fullyBody)
        let chest = session.sets.filter { $0.slot == "chest-press" }.sorted { $0.order < $1.order }
        let labels = SetLabel.make(records: chest)
        #expect(chest.map { labels[$0.id]?.short } == ["W", "1", "2", "2a"])
        #expect(chest.map { labels[$0.id]?.spoken } == ["warm-up", "set 1", "set 2", "set 2, drop 1"])
        let split = session.sets.filter { $0.slot == "bulgarian-split-squat" }.sorted { $0.order < $1.order }
        let sides = SetLabel.make(records: split)
        #expect(split.map { sides[$0.id]?.short } == ["1L", "1R", "2L", "2R"])
        #expect(split.map { sides[$0.id]?.spoken }.first == "set 1, left side")
        // A myo chain: the head is 2, its mini-sets M.
        let day1 = try EngineFixtures.session("sessions", EngineFixtures.day1First)
        let raise = day1.sets.filter { $0.slot == "lateral-raise" }.sorted { $0.order < $1.order }
        let myo = SetLabel.make(records: raise)
        #expect(raise.map { myo[$0.id]?.short } == ["1", "2", "M", "M", "M", "M"])
        #expect(raise.map { myo[$0.id]?.spoken }.last == "set 2, mini-set 4")
    }
}
