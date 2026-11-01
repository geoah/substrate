import WorkoutsCore
import SwiftUI

/// A routine before it starts (DESIGN.md §7.3): each exercise's planned sets
/// in words, groups with their bracket and A1, A2 labels, the rests that
/// differ from the defaults, tempo and notes. Start sits in the bottom
/// toolbar, the system's glass; the tab bar steps aside on this pushed
/// page, or its floating glass would lie over Start.
struct RoutinePreview: View {
    let routineID: RoutineID

    @Environment(AppModel.self) private var app

    var body: some View {
        if let catalog = app.catalog, let routine = catalog.routines[routineID] {
            let preview = RoutinePresenter.preview(routine, catalog: catalog, history: app.historyIndex,
                                                   settings: app.presenterSettings)
            list(preview)
                .navigationTitle(preview.name)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar(.hidden, for: .tabBar)
                .toolbar {
                    ToolbarItem(placement: .bottomBar) {
                        StartSessionButton(request: .routine(routineID)) {
                            Text("Start routine")
                                .font(.body.weight(.semibold))
                                .frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.glassProminent)
                    }
                }
        } else {
            ContentUnavailableView("Routine not found", systemImage: "list.bullet.clipboard")
        }
    }

    private func list(_ preview: RoutinePreviewPresentation) -> some View {
        List {
            if preview.notes != nil || preview.lastDone != nil {
                Section {
                    VStack(alignment: .leading, spacing: 6) {
                        if let notes = preview.notes {
                            Text(notes)
                                .font(.subheadline)
                        }
                        if let lastDone = preview.lastDone {
                            Text(lastDone)
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
            }
            ForEach(preview.blocks) { block in
                Section {
                    ForEach(block.slots) { slot in
                        SlotLines(slot: slot, color: block.header == nil ? nil : GroupPalette.color(block.colorIndex))
                    }
                } header: {
                    if let header = block.header {
                        Text(header)
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(GroupPalette.color(block.colorIndex))
                            .textCase(nil)
                    }
                }
            }
            if !preview.unreadable.isEmpty {
                Section("Could not be read") {
                    ForEach(preview.unreadable, id: \.self) { line in
                        Text(line)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
        .listStyle(.insetGrouped)
    }
}

/// One exercise of the preview: the name (with its group label), then one
/// line per kind of planned set, then its notes.
private struct SlotLines: View {
    let slot: RoutinePreviewSlot
    let color: Color?

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            if let color {
                RoundedRectangle(cornerRadius: 2)
                    .fill(color)
                    .frame(width: 4)
                    .accessibilityHidden(true)
            }
            VStack(alignment: .leading, spacing: 4) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    if let label = slot.groupLabel {
                        Text(label)
                            .font(.subheadline.weight(.bold))
                            .foregroundStyle(color ?? .secondary)
                    }
                    Text(slot.name)
                        .font(.headline)
                }
                ForEach(Array(slot.lines.enumerated()), id: \.offset) { _, line in
                    Text(line)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                if let notes = slot.notes {
                    Text(notes)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .italic()
                }
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }
}

#if DEBUG
#Preview {
    let app = PreviewData.app()
    NavigationStack {
        RoutinePreview(routineID: "fully-body-collection-1")
    }
    .environment(app)
    .environment(PreviewData.noSession(app))
}
#endif
