import SwiftUI
import UIKit
import WorkoutsCore

/// A label stacked over its value, never side by side in fixed columns
/// (the repository's UI rule; §7.9's facts, the Finish summary).
struct LabelledValueView: View {
    let label: String
    let value: String

    init(_ labelled: LabelledValue) {
        label = labelled.label
        value = labelled.value
    }

    init(label: String, value: String) {
        self.label = label
        self.value = value
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .font(.subheadline)
                .foregroundStyle(.secondary)
            Text(value)
                .font(.body)
                .monospacedDigit()
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}
