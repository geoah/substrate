#if canImport(SwiftUI)
import SwiftUI
import SubstrateAuth
import SubstrateScreens

/// One change an approval would make: its mark, its words, and the fact
/// beside it, a blocked one's in full contrast.
public struct ChangeLineView: View {
    public let line: ChangeLine

    public init(_ line: ChangeLine) {
        self.line = line
    }

    public var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Image(systemName: line.symbol.systemName)
                .foregroundStyle(line.tone.color)
                .frame(width: 22)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(line.words)
                    .fixedSize(horizontal: false, vertical: true)
                if let detail = line.detail {
                    Text(detail)
                        .font(.footnote)
                        .foregroundStyle(line.detailStands ? .primary : .secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .combine)
    }
}

/// One change, as `ChangeLine` words it.
public struct ChangeRow: View {
    public let change: Change

    public init(_ change: Change) {
        self.change = change
    }

    public var body: some View {
        ChangeLineView(ChangeLine(change))
    }
}
#endif
