import Foundation
import SubstrateRuntime

/// One block of a reply as a row of a keyed list. Its id is its position
/// and a hash of what it holds, so a block that is finished is the same row
/// at every flush after it, and only the last, still being written, is
/// another: a list that crosses as deltas sends that one row, and a view
/// compares the rest by their ids' worth.
public struct MarkdownRow: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var block: MarkdownBlock

    public init(position: Int, block: MarkdownBlock) {
        id = "\(position)-\(String(block.digest, radix: 16))"
        self.block = block
    }
}

public extension Markdown {
    /// About a screen and a half of a phone, in characters: past this much
    /// a reply's next block starts another row of the transcript.
    static let pageWeight = 1500

    /// How often, at most, a reply being written is parsed again. Its
    /// pieces share a window longer than this allows, so the limit is met
    /// only when something else presents the tree in between: the rows then
    /// stay as they were until the instant the next parse may start.
    static let parsesPerSecond = 30.0

    /// A reply's text as rows. `open` says the reply is still being
    /// written: its blocks are the ones it draws now, closed for display
    /// (`parse(writing:)`). A finished reply is parsed as it stands, so what
    /// a stream drew at its end is what its stored row draws.
    static func rows(_ text: String, open: Bool) -> [MarkdownRow] {
        (open ? parse(writing: text) : parse(text)).enumerated().map { MarkdownRow(position: $0, block: $1) }
    }

    /// Where each row of the transcript starts among a reply's blocks: the
    /// first at 0, and another once the blocks before it weigh a page. A
    /// block's place depends on the blocks before it alone, so the pages a
    /// reply has do not move as it is written; a lazy stack then measures
    /// rows of about a screen, never one of twenty.
    static func pages(_ rows: [MarkdownRow]) -> [Int] {
        var starts = [0]
        var weight = 0
        for (index, row) in rows.enumerated() {
            if weight >= pageWeight {
                starts.append(index)
                weight = 0
            }
            weight += row.block.weight
        }
        return starts
    }
}

extension MarkdownBlock {
    /// How much there is to draw, in characters, with something for each
    /// line a list or a table adds.
    var weight: Int {
        switch self {
        case let .paragraph(text), let .heading(_, text): text.utf8.count
        case let .code(_, text, _): text.utf8.count
        case let .quote(blocks): blocks.reduce(0) { $0 + $1.weight }
        case let .list(_, _, items): items.reduce(0) { $0 + 40 + $1.blocks.reduce(0) { $0 + $1.weight } }
        case let .table(header, _, rows): ([header] + rows).reduce(0) { $0 + 40 + $1.reduce(0) { $0 + $1.utf8.count } }
        case .rule: 40
        }
    }

    /// A hash of the block that is the same on every device and at every
    /// launch (FNV-1a, 64 bits), which the language's own is not.
    var digest: UInt64 {
        var hash = FNV()
        feed(&hash)
        return hash.value
    }

    private func feed(_ hash: inout FNV) {
        switch self {
        case let .paragraph(text):
            hash.add(1)
            hash.add(text)
        case let .heading(level, text):
            hash.add(2)
            hash.add(UInt8(truncatingIfNeeded: level))
            hash.add(text)
        case let .code(language, text, closed):
            hash.add(3)
            hash.add(language ?? "")
            hash.add(text)
            hash.add(closed ? 1 : 0)
        case let .quote(blocks):
            hash.add(4)
            for block in blocks { block.feed(&hash) }
            hash.add(0)
        case let .list(ordered, start, items):
            hash.add(ordered ? 5 : 6)
            hash.add(String(start))
            for item in items {
                hash.add(item.checked.map { $0 ? 2 : 1 } ?? 0)
                for block in item.blocks { block.feed(&hash) }
                hash.add(0)
            }
        case let .table(header, alignments, rows):
            hash.add(7)
            for alignment in alignments { hash.add(alignment.rawValue) }
            for row in [header] + rows {
                for cell in row { hash.add(cell) }
                hash.add(0)
            }
        case .rule:
            hash.add(8)
        }
    }
}

private struct FNV {
    var value: UInt64 = 0xcbf2_9ce4_8422_2325

    mutating func add(_ byte: UInt8) {
        value = (value ^ UInt64(byte)) &* 0x0000_0100_0000_01b3
    }

    /// The text, and its end: two texts never read as one.
    mutating func add(_ text: String) {
        for byte in text.utf8 { add(byte) }
        add(0xff)
    }
}
