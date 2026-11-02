import WorkoutsCore
import SwiftUI

/// The rest between sets by exercise class, and for an exercise with none
/// (DESIGN.md §4.7, §7.10). Each value uses the session's rest presets.
struct RestDefaultsView: View {
    @Environment(AppModel.self) private var app
    @State private var wheel: Wheel?

    /// The rest whose wheel is open; `nil` class is the fallback.
    private struct Wheel: Identifiable {
        var restClass: RestClass?
        var id: String { restClass?.rawValue ?? "fallback" }
    }

    var body: some View {
        Form {
            Section {
                ForEach(RestClass.allCases, id: \.self) { restClass in
                    row(restClass.label, restClass: restClass)
                }
                row("An exercise with no class", restClass: nil)
            } footer: {
                Text("A routine's own rests win over these.")
            }
        }
        .navigationTitle("Rest between sets")
        .navigationBarTitleDisplayMode(.inline)
        .sheet(item: $wheel) { wheel in
            RestPickerSheet(title: wheel.restClass?.label ?? "No class", initial: app.settings.setRest[wheel.restClass]) { seconds in
                app.settings.setRest[wheel.restClass] = seconds
            }
        }
    }

    private func row(_ title: String, restClass: RestClass?) -> some View {
        // The class name can be long: it stacks over its value rather than sharing a fixed column.
        VStack(alignment: .leading, spacing: 4) {
            Text(title)
            Menu {
                RestPresetItems(current: app.settings.setRest[restClass], defaultText: nil) { seconds in
                    if let seconds { app.settings.setRest[restClass] = seconds }
                } custom: {
                    wheel = Wheel(restClass: restClass)
                }
            } label: {
                Text(Format.rest(app.settings.setRest[restClass]))
                    .monospacedDigit()
            }
        }
        .accessibilityElement(children: .contain)
    }
}

#if DEBUG
#Preview {
    NavigationStack {
        RestDefaultsView()
    }
    .environment(PreviewData.app())
}
#endif
