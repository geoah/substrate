import Foundation

/// The suggestion's explanation strings and the value formats they use
/// (research 03 §12, DESIGN §4.13).
struct SuggestText: Sendable {
    var unit: MassUnit
    var mode: LoadMode03

    /// A load as the lifter sets it: `35 kg`; `BW`, `BW + 2.5 kg`, `BW − 30 kg`
    /// in bodyweight modes; nil when there is no load to show.
    func load(_ setting: Double?) -> String? {
        switch mode {
        case .none:
            return nil
        case .external:
            return setting.map(mass)
        case .bodyweightAdded:
            guard let setting, setting > 1e-9 else { return "BW" }
            return "BW + \(mass(setting))"
        case .bodyweightAssisted:
            guard let setting, setting > 1e-9 else { return "BW" }
            return "BW \u{2212} \(mass(setting))"
        }
    }

    /// A rep set: `35 kg × 13`, `BW × 10`, reps only `× 15`, or the load alone.
    func set(load setting: Double?, reps: Int?) -> String {
        switch (load(setting), reps) {
        case let (load?, reps?): "\(load) \u{00D7} \(reps)"
        case let (load?, nil): load
        case let (nil, reps?): "\u{00D7} \(reps)"
        case (nil, nil): ""
        }
    }

    /// A timed set: `1:02`, or with a load `10 kg for 1:02`.
    func timed(load setting: Double?, duration: Int?) -> String {
        let time = duration.map(Self.duration) ?? ""
        guard let setting, setting > 1e-9, let load = load(setting) else { return time }
        return duration == nil ? load : "\(load) for \(time)"
    }

    /// `{prev}`: the reference set, then ` at RPE {e}` only when rated, or
    /// `, assuming RPE {e}` when the set carried an RPE that is no rating (a
    /// pre-fill, a copy), then ` ({days} days ago)` only past 28 days.
    func previous(_ set: DoneSet, setting: Double?, rpe: EffectiveRPE, days: Int? = nil) -> String {
        var text = set.kind == .timed
            ? timed(load: setting, duration: set.duration)
            : self.set(load: setting, reps: set.effectiveReps)
        if rpe.logged {
            text += " at RPE \(Self.rpe(rpe.value))"
        } else if set.unratedRPE != nil {
            text += ", assuming RPE \(Self.rpe(rpe.value))"
        }
        if let days, days > 28 { text += " (\(days) days ago)" }
        return text
    }

    /// `35 kg`, `92.5 kg`, `1.25 kg`, `190 lb`.
    func mass(_ value: Double) -> String {
        "\(Self.number(value, decimals: 2)) \(unit.rawValue)"
    }

    /// RPE with at most one decimal: `9.5`, `8`.
    static func rpe(_ value: Double) -> String { number(value, decimals: 1) }

    /// A fraction as a percentage with at most two decimals: `20`, `12.5`.
    static func percent(_ fraction: Double) -> String { number(fraction * 100, decimals: 2) }

    /// `m:ss` under an hour, `h:mm:ss` from an hour.
    static func duration(_ seconds: Int) -> String {
        let total = max(0, seconds)
        let (hours, minutes, rest) = (total / 3600, total / 60 % 60, total % 60)
        let ss = rest < 10 ? "0\(rest)" : "\(rest)"
        guard hours > 0 else { return "\(minutes):\(ss)" }
        return "\(hours):\(minutes < 10 ? "0\(minutes)" : "\(minutes)"):\(ss)"
    }

    /// At most `decimals` decimals, trailing zeros removed, never `-0`.
    static func number(_ value: Double, decimals: Int) -> String {
        let scale = pow(10, Double(decimals))
        let rounded = (value * scale).rounded() / scale + 0
        var text = String(format: "%.\(decimals)f", rounded)
        guard text.contains(".") else { return text }
        while text.hasSuffix("0") { text.removeLast() }
        if text.hasSuffix(".") { text.removeLast() }
        return text
    }
}
