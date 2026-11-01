import WorkoutsCore
import SwiftUI

/// The grid's column widths (DESIGN.md §7.6). A grid is bounded tabular
/// data, so fixed columns are right for it; PREVIOUS takes what is left.
/// Widths grow with Dynamic Type up to 16 %, the most a group member's card
/// on a 393 pt phone holds, and from xxLarge the PREVIOUS column moves under
/// its row, so the value fields never squeeze. The grid's text stops at AX1.
struct GridLayout: Equatable {
    let scale: CGFloat
    let stacksPrevious: Bool

    init(_ size: DynamicTypeSize) {
        scale = switch size {
        case ...DynamicTypeSize.large: 1
        case .xLarge: 1.08
        default: 1.16
        }
        stacksPrevious = size >= .xxLarge
    }

    let spacing: CGFloat = 6
    var set: CGFloat { 36 * scale }
    var value: CGFloat { 62 * scale }
    /// The TIME cell holds its play button and the duration.
    var time: CGFloat { 112 * scale }
    var rpe: CGFloat { 44 * scale }
    var tick: CGFloat { 44 * scale }
    /// Every tappable cell is at least this tall: the 44 pt minimum target.
    var cellHeight: CGFloat { 44 * scale }

    /// A column's width; nil for PREVIOUS, which fills.
    func width(_ column: GridColumn) -> CGFloat? {
        switch column {
        case .set: set
        case .previous: nil
        case .load, .addedLoad, .assistance, .reps, .distance: value
        case .time: time
        case .rpe: rpe
        case .tick: tick
        }
    }
}

/// An exercise's set grid: the column titles, then a row per set.
struct SetGridView: View {
    let card: ExerciseCard
    var focus: FocusState<CoverModel.Field?>.Binding

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        let layout = GridLayout(dynamicTypeSize)
        VStack(spacing: 4) {
            titles(layout)
            ForEach(card.rows) { row in
                SetRowView(row: row, card: card, layout: layout, focus: focus)
                    .id(row.id)
            }
        }
        // Bounded tabular cells: past AX1 their numbers would clip and the
        // row would outgrow the card, so the grid's text stops growing there.
        .dynamicTypeSize(...DynamicTypeSize.accessibility1)
    }

    private func titles(_ layout: GridLayout) -> some View {
        HStack(spacing: layout.spacing) {
            ForEach(Array(zip(card.columns, card.columnTitles)), id: \.0) { column, title in
                if column == .previous {
                    if layout.stacksPrevious {
                        Spacer(minLength: 0)
                    } else {
                        Text(title).frame(maxWidth: .infinity, alignment: .leading)
                    }
                } else if column == .tick {
                    Image(systemName: "checkmark")
                        .frame(width: layout.tick)
                        .accessibilityHidden(true)
                } else {
                    Text(title)
                        .frame(width: layout.width(column))
                }
            }
        }
        .font(.caption.weight(.medium))
        .foregroundStyle(.secondary)
        .lineLimit(1)
        .minimumScaleFactor(0.7)
        .padding(.horizontal, 6)
        .accessibilityHidden(true)
    }
}
