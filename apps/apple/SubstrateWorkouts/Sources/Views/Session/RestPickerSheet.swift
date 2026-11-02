import WorkoutsCore
import SwiftUI

/// The rest presets (DESIGN.md §7.6): Off, 0:30 to 3:00, the default, and
/// "Custom…", which opens the wheel. The current value carries a check.
/// Used by the Rest chip, the exercise and group menus, a set's "Rest after
/// this set…", and Settings.
struct RestPresetItems: View {
    /// The value in force, for the check.
    let current: Seconds?
    /// "Default (2:00)"; nil leaves the default out (Settings, which sets the defaults).
    let defaultText: String?
    let select: (Seconds?) -> Void
    let custom: () -> Void

    var body: some View {
        ForEach(SlotRestOption.presets, id: \.self) { seconds in
            Button {
                select(seconds)
            } label: {
                if current == seconds {
                    Label(Format.rest(seconds), systemImage: "checkmark")
                } else {
                    Text(Format.rest(seconds))
                }
            }
        }
        if let defaultText {
            Button(defaultText) { select(nil) }
        }
        Button("Custom…", action: custom)
    }
}

/// "Custom…": a wheel in 5 s steps up to 10:00, and Done (§7.6). A `.medium`
/// sheet with the system's glass.
struct RestPickerSheet: View {
    let title: String
    let initial: Seconds
    let done: (Seconds) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var seconds: Seconds = 0

    private static let values = Array(stride(from: 0, through: 600, by: 5))

    var body: some View {
        NavigationStack {
            Picker(title, selection: $seconds) {
                ForEach(Self.values, id: \.self) { value in
                    Text(Format.rest(value))
                        .monospacedDigit()
                        .tag(value)
                }
            }
            .pickerStyle(.wheel)
            .labelsHidden()
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", role: .cancel) { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") {
                        done(seconds)
                        dismiss()
                    }
                }
            }
        }
        .presentationDetents([.medium])
        .onAppear { seconds = Self.values.min { abs($0 - initial) < abs($1 - initial) } ?? 0 }
    }
}

#if DEBUG
#Preview {
    Color.clear.sheet(isPresented: .constant(true)) {
        RestPickerSheet(title: "Rest", initial: 90) { _ in }
    }
}
#endif
