import Charts
import WorkoutsCore
import SwiftUI

/// The trend (DESIGN.md §7.9): a point per session joined by straight
/// segments, a deload's point hollow. Straight, not smoothed: a curve would
/// draw values between sessions that no session had. Tapping a point opens
/// that session. Content, not glass.
struct E1RMChart: View {
    let trend: ExerciseTrend
    let points: [TrendPoint]
    let select: (SessionID) -> Void

    var body: some View {
        Chart(points) { point in
            LineMark(x: .value("Date", point.date), y: .value(trend.title, point.value))
                .foregroundStyle(.tint)
                .interpolationMethod(.linear)
            PointMark(x: .value("Date", point.date), y: .value(trend.title, point.value))
                .symbol {
                    if point.hollow {
                        Circle().strokeBorder(Color.accentColor, lineWidth: 2).frame(width: 9, height: 9)
                    } else {
                        Circle().fill(Color.accentColor).frame(width: 9, height: 9)
                    }
                }
                .accessibilityLabel(Format.day(point.date, in: .current))
                .accessibilityValue(point.hollow ? "\(point.text), deload" : point.text)
        }
        .chartYScale(domain: .automatic(includesZero: false))
        .chartYAxis {
            AxisMarks { value in
                AxisGridLine()
                AxisValueLabel {
                    if let number = value.as(Double.self) { Text(axisText(number)) }
                }
            }
        }
        .chartOverlay { proxy in
            GeometryReader { geometry in
                Rectangle()
                    .fill(.clear)
                    .contentShape(.rect)
                    .onTapGesture { location in
                        if let point = nearest(to: location, proxy: proxy, geometry: geometry) { select(point.session) }
                    }
            }
        }
    }

    /// The point nearest a tap, by date.
    private func nearest(to location: CGPoint, proxy: ChartProxy, geometry: GeometryProxy) -> TrendPoint? {
        guard let frame = proxy.plotFrame else { return nil }
        let x = location.x - geometry[frame].origin.x
        guard let date: Date = proxy.value(atX: x) else { return nil }
        return points.min { abs($0.date.timeIntervalSince(date)) < abs($1.date.timeIntervalSince(date)) }
    }

    private func axisText(_ value: Double) -> String {
        switch trend.metric {
        case .oneRepMax: Format.mass(value, unit: trend.unit)
        case .longestHold: Format.duration(Int(value.rounded()))
        case .mostReps, .none: Format.number(value, decimals: 0)
        }
    }
}
