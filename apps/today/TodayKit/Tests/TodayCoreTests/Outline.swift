import Foundation

@testable import TodayCore

/// A pane as indented text: what a reader checks a moment's order by.
func outline(_ pane: PanePresentation) -> String {
    var out: [String] = ["ground: \(pane.ground.rawValue), lead: \(pane.lead ?? "-")"]
    out.append(
        "strip: "
            + pane.strip.entries.map { "\($0.name)[\($0.stripe.flag?.rawValue ?? "-")\($0.stripe.suggested ? "?" : "")] \($0.suggestion ?? $0.line ?? "")" }
            .joined(separator: " | "))
    for item in pane.items {
        switch item {
        case let .night(night):
            out.append(
                "night\(night.leads ? "*" : ""): \(night.line) [\(night.groups.map { "\($0.label): \($0.items.map(\.name).joined(separator: ","))" }.joined(separator: "; "))]"
            )
        case let .earlier(earlier):
            out.append(
                "earlier: \(earlier.count) - "
                    + earlier.entries.map { entry in
                        switch entry {
                        case let .row(row): "\(row.title) (\(row.line ?? row.opening ?? ""))"
                        case let .meeting(meeting): "\(meeting.end) \(meeting.title)"
                        case let .line(line): "\(line.time) \(line.text)"
                        }
                    }.joined(separator: " / "))
        case let .happening(h):
            out.append(
                "happening: \(h.time ?? "") \(h.line) {\(h.groups.map { "\($0.name):\($0.items.count)" }.joined(separator: ","))}")
        case let .row(row):
            out.append(
                "row[\(row.state.rawValue)] \(row.id): \(row.title) | \(row.line ?? row.opening ?? "") | \(row.stripe.flag?.rawValue ?? "-")\(row.stripe.suggested ? "?" : "") | pills=\(row.chips.pills.map(\.word))"
            )
        case let .meeting(m):
            let tie = m.tie.map { " tied\($0.second ? "+" : "")\($0.toChoose ? "?" : "")" } ?? ""
            out.append(
                "meeting\(m.leads ? "*" : "")\(m.live ? "!" : "")\(m.off ? " off" : "")\(tie): \(m.start)-\(m.end) \(m.title) \(m.until ?? "") | \(m.line ?? "")"
            )
        case let .parked(p): out.append("parked: \(p.what) \(p.rest) [\(p.rows.map(\.id).joined(separator: ", "))]")
        case let .unfinished(u): out.append("unfinished: \(u.line)")
        case let .note(n): out.append("note: \(n.text)")
        }
    }
    return out.joined(separator: "\n")
}
