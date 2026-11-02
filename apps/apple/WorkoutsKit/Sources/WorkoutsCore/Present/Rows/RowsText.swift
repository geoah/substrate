import Foundation

/// The Rows layout's words for values (§4.13's rules, written as sentences):
/// a load always carries its unit, a `last` reference never does (it sits
/// beside a value that shows it), reps read `16 reps` or `11 per side`, a
/// hold `0:58 hold`. Changes against last time use a true minus (U+2212).
enum RowsText {
    static let times = "\u{00D7}"
    static let minus = "\u{2212}"

    // MARK: - Values

    /// A set as its row shows it: `110 kg × 8`, `16 reps`, `11 per side`,
    /// `BW + 10 kg × 8`, `0:58 hold`, `10 kg for 0:55`, `20 kg for 40 m`,
    /// `500 m in 2:05`. `repsHint` writes the reps `≈15`; `placeholder` stands
    /// in for missing reps (`25 kg to failure`).
    static func values(_ values: SetValues, measurement: MeasurementType, perSide: Bool, repsHint: Bool = false,
                       placeholder: String? = nil) -> String
    {
        let reps = values.reps.map { repsHint ? "\u{2248}\($0)" : "\($0)" }
        switch measurement {
        case .weightreps:
            return joined(values.load.map(Format.mass), reps, placeholder: placeholder)
        case .weightedbodyweightreps, .bodyweightassistedreps:
            let load = Format.load(values, mode: measurement.loadMode)
            return load == "BW" ? repsWords(reps, perSide: perSide, placeholder: placeholder)
                : joined(load, reps, placeholder: placeholder)
        case .repsonly, .bodyweightreps:
            return repsWords(reps, perSide: perSide, placeholder: placeholder)
        case .duration:
            return values.duration.map { "\(Format.duration($0)) hold" } ?? ""
        case .weightduration, .distanceduration, .shortdistanceweight:
            return Format.set(values, measurement: measurement)
        }
    }

    private static func joined(_ load: String?, _ reps: String?, placeholder: String?) -> String {
        switch (load, reps) {
        case let (load?, reps?): "\(load) \(times) \(reps)"
        case let (load?, nil): placeholder.map { "\(load) \($0)" } ?? load
        case let (nil, reps?): "\(times) \(reps)"
        case (nil, nil): placeholder ?? ""
        }
    }

    private static func repsWords(_ reps: String?, perSide: Bool, placeholder: String?) -> String {
        guard let reps else { return placeholder ?? "" }
        if perSide { return "\(reps) per side" }
        return reps == "1" ? "1 rep" : "\(reps) reps"
    }

    /// Last time's set beside a pending row, no units: `105 × 10 @ 9.5`, `15`,
    /// `0:53`, `BW + 10 × 8`, `10 for 0:55`. Its RPE only when it was a rating.
    static func compact(_ values: SetValues, measurement: MeasurementType, perSide: Bool) -> String {
        func number(_ mass: Mass?) -> String? { mass.map { Format.number($0.value) } }
        var text: String
        switch measurement {
        case .weightreps:
            text = [number(values.load), values.reps.map(String.init)].compactMap(\.self).joined(separator: " \(times) ")
        case .weightedbodyweightreps:
            let load = values.load.flatMap { $0.value > 0 ? "BW + \(Format.number($0.value))" : nil }
            text = [load, values.reps.map(String.init)].compactMap(\.self).joined(separator: " \(times) ")
        case .bodyweightassistedreps:
            let load = values.assistance.flatMap { $0.value > 0 ? "BW \(minus) \(Format.number($0.value))" : nil }
            text = [load, values.reps.map(String.init)].compactMap(\.self).joined(separator: " \(times) ")
        case .repsonly, .bodyweightreps:
            text = values.reps.map(String.init) ?? ""
        case .duration:
            text = values.duration.map(Format.duration) ?? ""
        case .weightduration:
            text = [number(values.load), values.duration.map(Format.duration)].compactMap(\.self).joined(separator: " for ")
        case .distanceduration:
            text = [values.distance.map(Format.length), values.duration.map(Format.duration)].compactMap(\.self)
                .joined(separator: " in ")
        case .shortdistanceweight:
            text = [number(values.load), values.distance.map(Format.length)].compactMap(\.self).joined(separator: " for ")
        }
        if let rpe = values.rpe { text += text.isEmpty ? "@ \(Format.rpe(rpe))" : " @ \(Format.rpe(rpe))" }
        return text
    }

    /// One field's number as the editor shows it: `110`, `8`, `1:07`, `40`;
    /// with `withUnit`, `110 kg`, `8 reps`, `1:07`, `40 m`.
    static func fieldText(_ field: ValueField, _ value: Decimal, unit: MassUnit, withUnit: Bool) -> String {
        switch field {
        case .load, .assistance:
            withUnit ? Format.mass(Mass(value: value, unit: unit)) : Format.number(value)
        case .reps:
            withUnit ? (value == 1 ? "1 rep" : "\(Format.number(value)) reps") : Format.number(value)
        case .duration:
            Format.duration(NSDecimalNumber(decimal: value).intValue)
        case .distance:
            withUnit ? Format.length(Length(metres: value)) : Format.number(value)
        }
    }

    /// The word after the editor's big number: `kg`, `reps`, `per side`,
    /// `kg added`, `kg assist`, `m`; nil for a time.
    static func unitText(_ field: ValueField, measurement: MeasurementType, unit: MassUnit, perSide: Bool,
                         value: Decimal?) -> String?
    {
        switch field {
        case .load: measurement == .weightedbodyweightreps ? "\(unit.rawValue) added" : unit.rawValue
        case .assistance: "\(unit.rawValue) assist"
        case .reps: perSide ? "per side" : value == 1 ? "rep" : "reps"
        case .duration: nil
        case .distance: "m"
        }
    }

    /// A step's size on its button, without the sign: `5`, `2.5`, `1`, `0:05`.
    static func stepText(_ field: ValueField, _ step: Decimal) -> String {
        field == .duration ? Format.duration(NSDecimalNumber(decimal: step).intValue) : Format.number(step)
    }

    /// VoiceOver's words for a step: `5 kilograms less` and `5 kilograms more`.
    static func spokenStep(_ field: ValueField, _ step: Decimal, unit: MassUnit) -> (less: String, more: String) {
        let amount = Format.number(step)
        switch field {
        case .load, .assistance:
            return ("\(amount) \(unit.spokenName) less", "\(amount) \(unit.spokenName) more")
        case .reps:
            let noun = step == 1 ? "rep" : "reps"
            return ("\(amount) \(noun) fewer", "\(amount) \(noun) more")
        case .duration:
            let seconds = spokenDuration(NSDecimalNumber(decimal: step).intValue)
            return ("\(seconds) less", "\(seconds) more")
        case .distance:
            return ("\(amount) metres less", "\(amount) metres more")
        }
    }

    /// The field as VoiceOver names it: `load in kilograms`, `reps`.
    static func spokenField(_ field: ValueField, measurement: MeasurementType, unit: MassUnit) -> String {
        let column = measurement.valueColumns.first { $0.field == field } ?? .reps
        return column.spokenName(unit: unit)
    }

    /// ` @ 8`, or nothing.
    static func effortSuffix(_ rpe: Decimal?) -> String {
        rpe.map { " @ \(Format.rpe($0))" } ?? ""
    }

    /// The Log button: `Log 110 kg × 8 @ 8`, `Log warm-up 40 kg × 12`,
    /// `Log A2, 11 per side`, `Log mini-set 3, 3 reps`; `Log` alone with no values.
    static func logTitle(prefix: String, values: String, rpe: Decimal?) -> String {
        guard !values.isEmpty else {
            let bare = prefix.hasSuffix(",") ? String(prefix.dropLast()) : prefix
            return bare + effortSuffix(rpe)
        }
        return "\(prefix) \(values)\(effortSuffix(rpe))"
    }

    // MARK: - Spoken

    /// A set as VoiceOver reads it: `110 kilograms, 8 reps`, `11 reps per side`,
    /// `58 seconds`, `bodyweight plus 10 kilograms, 8 reps`, `20 kilograms, 40 metres`.
    static func spoken(_ values: SetValues, measurement: MeasurementType, perSide: Bool, repsHint: Bool = false,
                       placeholder: String? = nil) -> String
    {
        var parts: [String] = []
        switch measurement.loadMode {
        case .external: if let load = values.load { parts.append(spokenMass(load)) }
        case .bodyweightplus:
            if let load = values.load, load.value > 0 { parts.append("bodyweight plus \(spokenMass(load))") }
        case .assisted:
            if let help = values.assistance, help.value > 0 { parts.append("bodyweight minus \(spokenMass(help))") }
        case .bodyweight, nil: break
        }
        if measurement.recordsReps {
            if let reps = values.reps {
                parts.append("\(repsHint ? "about " : "")\(reps) \(reps == 1 ? "rep" : "reps")\(perSide ? " per side" : "")")
            } else if let placeholder {
                parts.append(placeholder)
            }
        }
        if let distance = values.distance { parts.append("\(Format.number(distance.metres)) metres") }
        if let duration = values.duration, !measurement.recordsReps { parts.append(spokenDuration(duration)) }
        return parts.joined(separator: ", ")
    }

    static func spokenMass(_ mass: Mass) -> String {
        "\(Format.number(mass.value)) \(mass.unit.spokenName)"
    }

    /// `1 minute 2 seconds`, `58 seconds`, `2 minutes`.
    static func spokenDuration(_ seconds: Seconds) -> String {
        let (minutes, rest) = (seconds / 60, seconds % 60)
        let m = minutes == 0 ? nil : "\(minutes) \(minutes == 1 ? "minute" : "minutes")"
        let s = rest == 0 && minutes > 0 ? nil : "\(rest) \(rest == 1 ? "second" : "seconds")"
        return [m, s].compactMap(\.self).joined(separator: " ")
    }

    // MARK: - Changes against last time

    /// A done set against last time's, field by field in the grid's order:
    /// `+5 kg`, `−2 reps`, `+0:05`, `+5 m`. A gain is more load (less
    /// assistance), more reps, a longer hold or distance, or a faster time over
    /// a distance. Equal everywhere: `as last time`.
    static func changes(_ today: SetValues, last: SetValues, measurement: MeasurementType, unit: MassUnit)
        -> [RowsChange]
    {
        var result: [RowsChange] = []
        for column in measurement.valueColumns {
            guard let field = column.field else { continue }
            switch field {
            case .load, .assistance:
                let pick: (SetValues) -> Mass? = { field == .load ? $0.load : $0.assistance }
                guard let now = pick(today), let then = pick(last) else { continue }
                let diff = (now.converted(to: unit).value - then.converted(to: unit).value).roundedTo(2)
                guard diff != 0 else { continue }
                let gain = field == .assistance ? diff < 0 : diff > 0
                result.append(RowsChange(text: signed(Format.number(abs(diff)), diff > 0) + " \(unit.rawValue)", isGain: gain))
            case .reps:
                guard let now = today.reps, let then = last.reps, now != then else { continue }
                let diff = now - then
                result.append(RowsChange(text: signed("\(abs(diff))", diff > 0) + (abs(diff) == 1 ? " rep" : " reps"),
                                         isGain: diff > 0))
            case .duration:
                guard let now = today.duration, let then = last.duration, now != then else { continue }
                let diff = now - then
                // Over a distance, less time is the gain.
                let gain = measurement == .distanceduration ? diff < 0 : diff > 0
                result.append(RowsChange(text: signed(Format.duration(abs(diff)), diff > 0), isGain: gain))
            case .distance:
                guard let now = today.distance?.metres, let then = last.distance?.metres, now != then else { continue }
                let diff = now - then
                result.append(RowsChange(text: signed(Format.number(abs(diff)), diff > 0) + " m", isGain: diff > 0))
            }
        }
        return result.isEmpty ? [sameAsLastTime] : result
    }

    static let sameAsLastTime = RowsChange(text: "as last time", isGain: false)
    static let firstTime = RowsChange(text: "first time", isGain: false)

    static func signed(_ magnitude: String, _ positive: Bool) -> String {
        (positive ? "+" : minus) + magnitude
    }

    /// VoiceOver's words for the changes: `5 kilograms more and 2 reps fewer than last time`.
    static func spokenChanges(_ changes: [RowsChange], unit: MassUnit) -> String {
        if changes == [sameAsLastTime] { return "The same as last time" }
        if changes == [firstTime] { return "First time" }
        let words = changes.map { change -> String in
            let up = change.text.hasPrefix("+")
            let body = String(change.text.dropFirst())
            if body.hasSuffix(" \(unit.rawValue)") {
                let amount = body.dropLast(unit.rawValue.count + 1)
                return "\(amount) \(unit.spokenName) \(up ? "more" : "less")"
            }
            if body.hasSuffix(" reps") || body.hasSuffix(" rep") { return "\(body) \(up ? "more" : "fewer")" }
            if body.hasSuffix(" m") { return "\(body.dropLast(2)) metres \(up ? "more" : "less")" }
            let seconds = body.split(separator: ":").reduce(0) { $0 * 60 + (Int($1) ?? 0) }
            return "\(spokenDuration(seconds)) \(up ? "longer" : "shorter")"
        }
        let list = words.count <= 1 ? words.joined()
            : words.dropLast().joined(separator: ", ") + " and " + (words.last ?? "")
        return list.capitalizedFirst + " than last time"
    }
}
