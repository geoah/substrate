#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDeviceApple

/// Lays its children out in rows, wrapping where the width runs out.
public struct WrapLayout: Layout {
    var spacing: CGFloat

    public init(spacing: CGFloat = 6) {
        self.spacing = spacing
    }

    /// The layout hugs its widest row, and a parent hands the bounds back
    /// rounded to the screen's pixels: a row measured to fit can be a hair
    /// wider than the bounds it is placed in. Placing breaks a row only
    /// past this much, so the rows placed are the rows the height counted
    /// and no child lands on a row drawn over what stands below.
    static let slack: CGFloat = 1

    /// One row: its children by index with their sizes, and its own size.
    struct Row {
        var items: [(index: Int, size: CGSize)] = []
        var width: CGFloat = 0
        var height: CGFloat = 0
    }

    /// The children in rows. Each is measured within `fit`; a row breaks
    /// where the next child would end past `limit`.
    func rows(_ subviews: Subviews, fit: CGFloat, limit: CGFloat) -> [Row] {
        var rows: [Row] = []
        var row = Row()
        for (index, view) in subviews.enumerated() {
            let size = Self.size(of: view, within: fit)
            let start = row.items.isEmpty ? 0 : row.width + spacing
            if !row.items.isEmpty, start + size.width > limit {
                rows.append(row)
                row = Row()
            }
            row.items.append((index, size))
            row.width = (row.items.count == 1 ? 0 : row.width + spacing) + size.width
            row.height = max(row.height, size.height)
        }
        if !row.items.isEmpty { rows.append(row) }
        return rows
    }

    public func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? .infinity
        let rows = rows(subviews, fit: width, limit: width)
        let widest = rows.map(\.width).max() ?? 0
        let height = rows.map(\.height).reduce(0, +) + spacing * CGFloat(max(0, rows.count - 1))
        return CGSize(width: min(widest.rounded(.up), width), height: height)
    }

    public func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var y = bounds.minY
        for row in rows(subviews, fit: bounds.width, limit: bounds.width + Self.slack) {
            var x = bounds.minX
            for item in row.items {
                subviews[item.index].place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(item.size))
                x += item.size.width + spacing
            }
            y += row.height + spacing
        }
    }

    /// A child at the width it asks for, or at the row's where it asks for
    /// more: a pill whose name is longer than the row truncates inside it
    /// and never runs past the edge.
    private static func size(of view: LayoutSubview, within width: CGFloat) -> CGSize {
        let ideal = view.sizeThatFits(.unspecified)
        guard width.isFinite, ideal.width > width else { return ideal }
        return view.sizeThatFits(ProposedViewSize(width: width, height: nil))
    }
}
#endif
