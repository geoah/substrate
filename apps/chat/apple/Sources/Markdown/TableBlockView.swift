import ChatKit
import SubstrateKit
import SwiftUI

/// The sizes a table is drawn with, at the text's size.
struct TableMetrics {
    /// The space inside a cell, and the first and last column's to the
    /// table's edge.
    static let cellInset = EdgeInsets(top: 7, leading: 10, bottom: 7, trailing: 10)
    static let edgeInset: CGFloat = 12
    static let radius: CGFloat = 12
    /// Between two cards, and between two lines of one.
    static let cardSpacing: CGFloat = 8
    static let lineSpacing: CGFloat = 6
    /// A hairline between rows, and beside the held column.
    static let rule: CGFloat = 0.5
}

/// A reply's table: at its natural width where that fits, its wide columns
/// wrapped where that makes it fit, and otherwise scrolled sideways with
/// its first column held in place. At the accessibility text sizes each row
/// is a card, each header beside its value. How it is laid out is ChatKit's
/// to say (`TableLayout`), from the widths this measures; a cell is inline
/// Markdown, a record's pill included, and a column keeps the alignment its
/// Markdown gave it.
struct TableBlockView: View {
    @Environment(\.missingRecords) private var missing
    @Environment(\.dynamicTypeSize) private var size
    let header: [String]
    let alignments: [MarkdownAlignment]
    let rows: [[String]]
    /// The narrowest a column is wrapped to and the widest one is drawn at
    /// while the table scrolls, with the text's size.
    @ScaledMetric(relativeTo: .subheadline) private var least = TableLayout.Metrics().least
    @ScaledMetric(relativeTo: .subheadline) private var widest = TableLayout.Metrics().widest
    /// What the scroll view says of itself: its width, and whether the
    /// table is wider.
    @State private var scroll = Scrolled()

    struct Scrolled: Equatable {
        var room: CGFloat = 0
        var scrolls = false
    }

    private var metrics: TableLayout.Metrics { TableLayout.Metrics(least: least, widest: widest) }

    var body: some View {
        if TableLayout.plan(natural: [], room: 0, accessibilitySize: size.isAccessibilitySize).mode == .cards {
            cards
        } else {
            grid
        }
    }

    // MARK: The grid

    private var grid: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            TableGrid(columns: header.count, room: scroll.room, metrics: metrics) {
                ForEach(Array(header.enumerated()), id: \.offset) { column, cell in
                    self.cell(cell, column: column, row: 0)
                        .font(.subheadline.weight(.semibold))
                        .accessibilityAddTraits(.isHeader)
                }
                ForEach(Array(rows.enumerated()), id: \.offset) { row, cells in
                    ForEach(Array(cells.enumerated()), id: \.offset) { column, cell in
                        self.cell(cell, column: column, row: row + 1)
                            .font(.subheadline)
                    }
                }
            }
            // The fill is the table's own, as wide as its columns: one
            // narrower than its room is a card of its own width, and one
            // that scrolls is rounded where the scroll view cuts it.
            .background(ChatColours.tableFill.color, in: .rect(cornerRadius: TableMetrics.radius))
        }
        .scrollBounceBehavior(.basedOnSize, axes: .horizontal)
        .onScrollGeometryChange(for: Scrolled.self) { geometry in
            Scrolled(room: geometry.containerSize.width, scrolls: geometry.contentSize.width > geometry.containerSize.width + 0.5)
        } action: { _, now in
            if now != scroll { scroll = now }
        }
        .clipShape(.rect(cornerRadius: TableMetrics.radius))
    }

    /// One cell, filling its place in the grid.
    private func cell(_ text: String, column: Int, row: Int) -> some View {
        let first = column == 0
        return inline(text, missing: missing)
            .multilineTextAlignment(textAlignment(column))
            .padding(TableMetrics.cellInset)
            .padding(.leading, first ? TableMetrics.edgeInset - TableMetrics.cellInset.leading : 0)
            .padding(.trailing, column == header.count - 1 ? TableMetrics.edgeInset - TableMetrics.cellInset.trailing : 0)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: Alignment(horizontal: alignment(column), vertical: .top))
            // A line above every row but the header's first, the header's
            // own a little stronger.
            .overlay(alignment: .top) {
                if row > 0 { Rectangle().fill(Color.secondary.opacity(row == 1 ? 0.35 : 0.18)).frame(height: TableMetrics.rule) }
            }
            .modifier(HeldColumn(held: first, scrolls: scroll.scrolls))
    }

    /// A cell of the first column of a table that scrolls: held at the
    /// scroll view's leading edge while the rest scroll under it. It is
    /// moved back by as far as the table has scrolled, in the drawing
    /// alone, so nothing is laid out again while it scrolls; it has the
    /// table's fill, to cover what passes under it, and a line at its
    /// edge. Any other cell, and every cell of a table that fits, is left
    /// as it is: a table that does not scroll pays nothing for this.
    private struct HeldColumn: ViewModifier {
        let held: Bool
        let scrolls: Bool

        func body(content: Content) -> some View {
            if held, scrolls {
                content
                    .background(ChatColours.tableFill.color)
                    .overlay(alignment: .trailing) {
                        Rectangle().fill(Color.secondary.opacity(0.3)).frame(width: TableMetrics.rule)
                    }
                    .visualEffect { content, proxy in
                        content.offset(x: max(0, -proxy.frame(in: .scrollView(axis: .horizontal)).minX))
                    }
                    .zIndex(1)
            } else {
                content
            }
        }
    }

    // MARK: Cards

    /// One card for each row: each header beside its value, the header's
    /// column as wide as the widest header and no more than its share.
    private var cards: some View {
        VStack(alignment: .leading, spacing: TableMetrics.cardSpacing) {
            // A table with no row yet (its header alone, as one being
            // written is for a moment) is one card of its headers.
            ForEach(Array((rows.isEmpty ? [[]] : rows).enumerated()), id: \.offset) { _, cells in
                TableGrid(columns: 2, room: 0, metrics: metrics, shares: true) {
                    ForEach(Array(header.enumerated()), id: \.offset) { column, name in
                        inline(name, missing: missing)
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(.quietText)
                            .padding(.trailing, TableMetrics.cellInset.trailing)
                            .padding(.vertical, TableMetrics.lineSpacing / 2)
                            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
                        inline(column < cells.count ? cells[column] : "", missing: missing)
                            .font(.subheadline)
                            .padding(.vertical, TableMetrics.lineSpacing / 2)
                            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
                    }
                }
                .padding(TableMetrics.edgeInset)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(ChatColours.tableFill.color, in: .rect(cornerRadius: TableMetrics.radius))
                // A card is read in its order: each header, then its value.
                .accessibilityElement(children: .contain)
            }
        }
    }

    private func alignment(_ column: Int) -> HorizontalAlignment {
        switch column < alignments.count ? alignments[column] : .leading {
        case .leading: .leading
        case .center: .center
        case .trailing: .trailing
        }
    }

    private func textAlignment(_ column: Int) -> TextAlignment {
        switch column < alignments.count ? alignments[column] : .leading {
        case .leading: .leading
        case .center: .center
        case .trailing: .trailing
        }
    }
}

/// A table's cells laid out in columns: its subviews are the cells, row by
/// row. Each column's natural width is its widest cell on one line; what
/// width each is drawn at is `TableLayout`'s plan for the room, and a row
/// is as tall as its tallest cell at that width. A cell is proposed its
/// whole place, so one that fills it draws its lines to the grid's.
struct TableGrid: Layout {
    let columns: Int
    /// The width the table has; zero where it is not known yet, or where
    /// the layout is proposed it (`shares`).
    let room: CGFloat
    let metrics: TableLayout.Metrics
    /// The columns share the width proposed, however little: a card's two.
    var shares = false

    struct Plan {
        var widths: [CGFloat] = []
        var heights: [CGFloat] = []
        /// What it was made for: the room, or the width proposed.
        var width: CGFloat?
        var size: CGSize { CGSize(width: widths.reduce(0, +), height: heights.reduce(0, +)) }
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout Plan) -> CGSize {
        cache = plan(shares ? proposal.width : room, subviews)
        return cache.size
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout Plan) {
        // Made again where it was made for another width: the last size
        // asked for need not be the one it is placed at.
        let width = shares ? bounds.width : room
        if cache.widths.isEmpty || cache.width != width { cache = plan(width, subviews) }
        var y = bounds.minY
        for (row, height) in cache.heights.enumerated() {
            var x = bounds.minX
            for (column, width) in cache.widths.enumerated() {
                let index = row * columns + column
                guard index < subviews.count else { break }
                subviews[index].place(at: CGPoint(x: x, y: y), anchor: .topLeading, proposal: ProposedViewSize(width: width, height: height))
                x += width
            }
            y += height
        }
    }

    func makeCache(subviews: Subviews) -> Plan { Plan() }

    /// - Parameter width: the room for the table, or for a card's two
    ///   columns the width they share; nil or zero where none is set.
    private func plan(_ width: CGFloat?, _ subviews: Subviews) -> Plan {
        guard columns > 0, !subviews.isEmpty else { return Plan() }
        var natural = [Double](repeating: 0, count: columns)
        for (index, cell) in subviews.enumerated() {
            natural[index % columns] = max(natural[index % columns], Double(cell.sizeThatFits(.unspecified).width.rounded(.up)))
        }
        let widths: [Double]
        let room = Double(width ?? 0)
        if shares {
            widths = room > 0 && natural.reduce(0, +) > room ? TableLayout.shared(natural, room: room) : natural
        } else {
            widths = TableLayout.plan(natural: natural, room: room, accessibilitySize: false, metrics: metrics).widths
        }
        var heights: [CGFloat] = []
        for start in stride(from: 0, to: subviews.count, by: columns) {
            var height: CGFloat = 0
            for column in 0 ..< columns where start + column < subviews.count {
                height = max(height, subviews[start + column].sizeThatFits(ProposedViewSize(width: widths[column], height: nil)).height)
            }
            heights.append(height.rounded(.up))
        }
        return Plan(widths: widths.map { CGFloat($0) }, heights: heights, width: width)
    }
}
