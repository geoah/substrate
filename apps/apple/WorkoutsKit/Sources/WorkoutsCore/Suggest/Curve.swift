import Foundation

/// The load-to-reps curve the suggestion engine and the e1RM trend share:
/// the RTS percentage chart in `n = reps + 10 − RPE`, with an Epley-shaped
/// tail past `n = 16` (research 03 §1.5 and §10.1).
public enum RPECurve {
    /// Fraction of the one-rep maximum at `n = 1 + 0.5 × i`, as published (03 §1.4).
    public static let points: [Double] = [
        1.000, 0.978, 0.955, 0.939, 0.922, 0.907, 0.892, 0.878, 0.863, 0.850,
        0.837, 0.824, 0.811, 0.799, 0.786, 0.774, 0.762, 0.751, 0.739, 0.723,
        0.707, 0.694, 0.680, 0.667, 0.653, 0.640, 0.626, 0.613, 0.599, 0.586, 0.572,
    ]

    /// The tail's constant: `0.572 × 46`, so the tail meets the chart at `n = 16`.
    static let tailA = 0.572 * 46.0

    /// The fraction of the one-rep maximum a set of effort `n` is done at.
    public static func p(_ n: Double) -> Double {
        if n <= 1 { return 1 }
        if n <= 16 {
            let i = Int(((n - 1) * 2).rounded(.down)) // 0...30
            let a = 1 + 0.5 * Double(i)
            if i == 30 || n == a { return points[i] }
            return points[i] + (points[i + 1] - points[i]) * (n - a) / 0.5
        }
        return tailA / (30 + n)
    }

    /// The inverse of `p`: the effort `n` a fraction `q` of the one-rep maximum allows.
    public static func pInv(_ q: Double) -> Double {
        if q >= 1 { return 1 }
        if q >= 0.572 {
            for i in 0 ..< 30 where points[i] >= q && q >= points[i + 1] {
                return 1 + 0.5 * Double(i) + 0.5 * (points[i] - q) / (points[i] - points[i + 1])
            }
        }
        // A load of 0 (or a NaN) has no finite answer; cap it so the reps stay an Int.
        guard q > 0 else { return maxEffort }
        return min(tailA / q - 30, maxEffort)
    }

    /// Effort beyond which `pInv` stops counting; far past anything a set reaches.
    static let maxEffort = 1_000_000.0

    /// Predicted reps at `load` for an e1RM `e1RM` and a target RPE `rpe` (03 §10.6).
    public static func repsAt(_ load: Double, e1RM: Double, rpe: Double) -> Int {
        guard e1RM > 0 else { return 0 }
        let value = (pInv(load / e1RM) - (10 - rpe) + 1e-6).rounded(.down)
        return value.isFinite ? max(0, Int(value)) : 0
    }
}
