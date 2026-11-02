import Foundation

/// Every number, load, duration, set and date the app shows, in one place
/// (§4.13, research 03 §12): the set grid, PREVIOUS, the rest bar, the Live
/// Activity, notifications, history and summaries all format through it, so
/// one value never reads two ways.
///
/// Output is English and deterministic: decimals with a point, dates in
/// `en_GB` on an explicit time zone (the app is English-only, as the
/// session names it writes are).
public enum Format {
    // MARK: - Numbers

    /// At most `decimals` decimals, rounded half away from zero, trailing zeros
    /// removed, no grouping, never "-0": `92.5`, `35`, `83.91`.
    public static func number(_ value: Decimal, decimals: Int = 2) -> String {
        var input = value
        var rounded = Decimal()
        NSDecimalRound(&rounded, &input, decimals, .plain)
        return DecimalText.format(rounded)
    }

    /// A Double the same way, through its shortest decimal spelling, so 22.5
    /// stays 22.5 and a converted 83.91000000001 reads 83.91.
    public static func number(_ value: Double, decimals: Int = 2) -> String {
        guard value.isFinite else { return "0" }
        return number(decimal(value), decimals: decimals)
    }

    /// A whole number with a comma between thousands: `3,120`.
    public static func grouped(_ value: Int) -> String {
        let digits = String(abs(value))
        var out = ""
        for (index, character) in digits.enumerated() {
            if index > 0, (digits.count - index).isMultiple(of: 3) { out.append(",") }
            out.append(character)
        }
        return value < 0 ? "-" + out : out
    }

    /// `1 set`, `24 sets`: a count and its noun, singular for one.
    public static func count(_ value: Int, _ singular: String, _ plural: String? = nil) -> String {
        "\(value) \(value == 1 ? singular : plural ?? singular + "s")"
    }

    /// A Double as a Decimal through its shortest round-trip spelling.
    static func decimal(_ value: Double) -> Decimal {
        Decimal(string: "\(value)", locale: Locale(identifier: "en_US_POSIX")) ?? Decimal(value)
    }

    // MARK: - Durations

    /// `m:ss` under an hour (`0:53`, `1:30`), `h:mm:ss` from an hour
    /// (`1:02:05`). The same for rests, timed sets, the session clock and
    /// summaries. Negative values read as 0.
    public static func duration(_ seconds: Seconds) -> String {
        let total = max(0, seconds)
        let (hours, minutes, rest) = (total / 3600, total / 60 % 60, total % 60)
        let ss = rest < 10 ? "0\(rest)" : "\(rest)"
        guard hours > 0 else { return "\(minutes):\(ss)" }
        return "\(hours):\(minutes < 10 ? "0\(minutes)" : "\(minutes)"):\(ss)"
    }

    /// A rest: `Off` for 0 (no timer), else a duration.
    public static func rest(_ seconds: Seconds) -> String {
        seconds <= 0 ? "Off" : duration(seconds)
    }

    /// Time past a timed set's target: `+0:05`.
    public static func overtime(_ seconds: Seconds) -> String {
        "+" + duration(seconds)
    }

    /// A session's length in words for summaries: `46 min`, `1 h 12 min`,
    /// `2 h`; under a minute, `45 s`. Minutes are counted down, not rounded.
    public static func minutes(_ seconds: Seconds) -> String {
        let total = max(0, seconds)
        if total < 60 { return "\(total) s" }
        let (hours, minutes) = (total / 3600, total / 60 % 60)
        guard hours > 0 else { return "\(minutes) min" }
        return minutes == 0 ? "\(hours) h" : "\(hours) h \(minutes) min"
    }

    // MARK: - Loads

    /// `35 kg`, `92.5 kg`, `1.25 kg`, `190 lb`: at most two decimals, a space, the unit.
    public static func mass(_ mass: Mass) -> String {
        "\(number(mass.value)) \(mass.unit.rawValue)"
    }

    /// A load the suggestion engine gives in the exercise's unit.
    public static func mass(_ value: Double, unit: MassUnit) -> String {
        "\(number(value)) \(unit.rawValue)"
    }

    /// A volume load: whole units with a comma between thousands, `3,120 kg`.
    public static func volume(_ mass: Mass) -> String {
        var input = mass.value
        var rounded = Decimal()
        NSDecimalRound(&rounded, &input, 0, .plain)
        return "\(grouped(NSDecimalNumber(decimal: rounded).intValue)) \(mass.unit.rawValue)"
    }

    /// A distance in metres: `40 m`, `1.5 m`.
    public static func length(_ length: Length) -> String {
        "\(number(length.metres)) m"
    }

    /// The load part of a set as the lifter sets it (03 §12): `35 kg`; `BW`,
    /// `BW + 2.5 kg`, `BW − 30 kg` (U+2212) in the bodyweight modes; nil
    /// when the exercise moves no load or the load is not known.
    public static func load(_ values: SetValues, mode: LoadMode?) -> String? {
        switch mode {
        case nil: nil
        case .external: values.load.map(mass)
        case .bodyweight: "BW"
        case .bodyweightplus: values.load.map { $0.value > 0 ? "BW + \(mass($0))" : "BW" } ?? "BW"
        case .assisted: values.assistance.map { $0.value > 0 ? "BW \u{2212} \(mass($0))" : "BW" } ?? "BW"
        }
    }

    // MARK: - Sets

    /// One set as text (§4.13): `35 kg × 13`, reps only `× 15`, `BW × 10`;
    /// a timed set `1:02`, with a load `10 kg for 1:02`; a distance `40 m`,
    /// with a load `20 kg for 40 m`; a distance and a time `500 m in 2:05`.
    /// With `rpe`, a logged RPE follows as ` @ 9.5` (PREVIOUS and history).
    /// `repsHint` writes the reps as a hint, `≈15` (a `rirstop` set, §5.3).
    public static func set(_ values: SetValues, measurement: MeasurementType, rpe showRPE: Bool = false,
                           repsHint: Bool = false) -> String
    {
        let load = load(values, mode: measurement.loadMode)
        // A plain "BW" before a time or a distance says nothing.
        let realLoad = load == "BW" ? nil : load
        var text: String
        switch measurement {
        case .weightreps, .repsonly, .bodyweightreps, .weightedbodyweightreps, .bodyweightassistedreps:
            let reps = values.reps.map { repsHint ? "\u{2248}\($0)" : "\($0)" }
            switch (load, reps) {
            case let (load?, reps?): text = "\(load) \u{00D7} \(reps)"
            case let (load?, nil): text = load
            case let (nil, reps?): text = "\u{00D7} \(reps)"
            case (nil, nil): text = ""
            }
        case .duration, .weightduration:
            let time = values.duration.map(duration)
            switch (realLoad, time) {
            case let (load?, time?): text = "\(load) for \(time)"
            case let (load?, nil): text = load
            case let (nil, time?): text = time
            case (nil, nil): text = ""
            }
        case .distanceduration:
            // The spec names no form for a distance with a time; "in" reads as a pace.
            let distance = values.distance.map(length)
            let time = values.duration.map(duration)
            switch (distance, time) {
            case let (distance?, time?): text = "\(distance) in \(time)"
            case let (distance?, nil): text = distance
            case let (nil, time?): text = time
            case (nil, nil): text = ""
            }
        case .shortdistanceweight:
            let distance = values.distance.map(length)
            switch (realLoad, distance) {
            case let (load?, distance?): text = "\(load) for \(distance)"
            case let (load?, nil): text = load
            case let (nil, distance?): text = distance
            case (nil, nil): text = ""
            }
        }
        if showRPE, let rpe = values.rpe {
            text += text.isEmpty ? "@ \(self.rpe(rpe))" : " @ \(self.rpe(rpe))"
        }
        return text
    }

    /// RPE with at most one decimal, trailing zero removed: `9.5`, `8`.
    public static func rpe(_ value: Decimal) -> String {
        number(value, decimals: 1)
    }

    /// An RPE range with an en dash, `7–8`; one value, `8`, when both ends
    /// match or only one is given.
    public static func rpeRange(_ lower: Decimal?, _ upper: Decimal?) -> String? {
        switch (lower, upper) {
        case let (lower?, upper?): lower == upper ? rpe(upper) : "\(rpe(lower))\u{2013}\(rpe(upper))"
        case let (one?, nil), let (nil, one?): rpe(one)
        case (nil, nil): nil
        }
    }

    /// A plan's effort target: `RPE 8`, `RPE 7–8`, `2 reps in reserve`,
    /// `2–3 reps in reserve`; nil when it names none.
    public static func effort(rpeMin: Decimal?, rpeMax: Decimal?, rirMin: Decimal?, rirMax: Decimal?) -> String? {
        if let range = rpeRange(rpeMin, rpeMax) { return "RPE \(range)" }
        guard let range = rpeRange(rirMin, rirMax) else { return nil }
        let one = (rirMin ?? rirMax) == (rirMax ?? rirMin) && (rirMin ?? rirMax) == 1
        return "\(range) \(one ? "rep" : "reps") in reserve"
    }

    /// What a percentage load is of, in words: `one-rep max`, `training max`.
    public static func loadReference(_ reference: LoadReference) -> String {
        switch reference {
        case .onerm: "one-rep max"
        case .e1rm: "estimated one-rep max"
        case .trainingmax: "training max"
        case .bodymass: "body mass"
        }
    }

    /// A range with an en dash, `10–12`; one number when both ends match.
    public static func range(_ lower: Int, _ upper: Int) -> String {
        lower == upper ? "\(lower)" : "\(lower)\u{2013}\(upper)"
    }

    /// A range of seconds: `0:45–1:30`.
    public static func durationRange(_ lower: Seconds, _ upper: Seconds) -> String {
        lower == upper ? duration(lower) : "\(duration(lower))\u{2013}\(duration(upper))"
    }

    // MARK: - Dates

    /// `Tue 27 Oct`.
    public static func day(_ date: Date, in zone: TimeZone) -> String {
        formatted(date, "EEE d MMM", zone)
    }

    /// `Tue 27 Oct 2026`.
    public static func dayWithYear(_ date: Date, in zone: TimeZone) -> String {
        formatted(date, "EEE d MMM yyyy", zone)
    }

    /// `Tue 27 Oct, 13:30`.
    public static func dayAndTime(_ date: Date, in zone: TimeZone) -> String {
        "\(day(date, in: zone)), \(time(date, in: zone))"
    }

    /// `13:30`, on the 24-hour clock.
    public static func time(_ date: Date, in zone: TimeZone) -> String {
        formatted(date, "HH:mm", zone)
    }

    /// `October 2026`, a History section.
    public static func month(_ date: Date, in zone: TimeZone) -> String {
        formatted(date, "LLLL yyyy", zone)
    }

    private static func formatted(_ date: Date, _ pattern: String, _ zone: TimeZone) -> String {
        dateFormatters.string(from: date, pattern: pattern, zone: zone)
    }

    /// A formatter costs about 0.2 ms to make, and History formats hundreds
    /// of dates, so one is kept per pattern and zone.
    private static let dateFormatters = DateFormatterCache()
}

/// `DateFormatter`s by pattern and zone, used under a lock, so `Format`
/// stays callable from any thread.
private final class DateFormatterCache: @unchecked Sendable {
    // Guards `formatters` and every use of one; `@unchecked Sendable` rests on it.
    private let lock = NSLock()
    private var formatters: [String: DateFormatter] = [:]

    func string(from date: Date, pattern: String, zone: TimeZone) -> String {
        lock.withLock {
            let key = "\(pattern)|\(zone.identifier)"
            let formatter = formatters[key] ?? {
                let formatter = DateFormatter()
                formatter.locale = Locale(identifier: "en_GB")
                formatter.calendar = Calendar(identifier: .gregorian)
                formatter.timeZone = zone
                formatter.dateFormat = pattern
                formatters[key] = formatter
                return formatter
            }()
            return formatter.string(from: date)
        }
    }
}

public extension Mass {
    /// A load the suggestion engine gives as a Double in `unit`, kept to two
    /// decimals as the formats show it.
    init(suggested value: Double, unit: MassUnit) {
        var input = Format.decimal(value)
        var rounded = Decimal()
        NSDecimalRound(&rounded, &input, 2, .plain)
        self.init(value: rounded, unit: unit)
    }
}
