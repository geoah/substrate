#if DEBUG
import WorkoutsCore
import SwiftUI

// Every moment of the Rows design (DESIGN.md §7.6a), each a session driven
// through the engine by `PreviewData.RowsMoment` and drawn by the cover in
// the Rows layout. The fix and the keypad open with `PreviewData.rowsScreen`.

/// The session cover in Rows at one moment.
private struct RowsPreview: View {
    let moment: PreviewData.RowsMoment
    let app: AppModel
    let session: SessionController

    init(_ moment: PreviewData.RowsMoment) {
        self.moment = moment
        app = PreviewData.app(layout: .rows)
        session = PreviewData.rowsSession(app, moment)
    }

    var body: some View {
        SessionCover(rowsScreen: PreviewData.rowsScreen(session, moment))
            .environment(app)
            .environment(session)
            .workoutsHaptics()
    }
}

#Preview("Rows 1, set 2 open") { RowsPreview(.setTwoOpen) }
#Preview("Rows 2, one rep fewer") { RowsPreview(.oneRepFewer) }
#Preview("Rows 3, superset mid-round") { RowsPreview(.superset) }
#Preview("Rows 4, resting after set 2") { RowsPreview(.resting) }
#Preview("Rows 5, chest press drop") { RowsPreview(.drop) }
#Preview("Rows 6, plank running") { RowsPreview(.plankRunning) }
#Preview("Rows 6b, plank ready") { RowsPreview(.plankReady) }
#Preview("Rows 7, RPE 8.5 in one tap") { RowsPreview(.rpeTapped) }
#Preview("Rows 8, fixing set 1") { RowsPreview(.fixing) }
#Preview("Rows 9, out of order") { RowsPreview(.outOfOrder) }
#Preview("Rows 10, myo-reps") { RowsPreview(.myoReps) }
#Preview("Rows 11, keypad") { RowsPreview(.keypad) }
#Preview("Rows 12, warm-up open") { RowsPreview(.warmupOpen) }
#Preview("Rows 13, dark") { RowsPreview(.setTwoOpen).preferredColorScheme(.dark) }
#Preview("Rows, every set done") { RowsPreview(.allDone) }
#Preview("Rows, empty session") { RowsPreview(.empty) }
#Preview("Rows, large text") { RowsPreview(.setTwoOpen).dynamicTypeSize(.xxxLarge) }
#Preview("Rows, accessibility text") { RowsPreview(.resting).dynamicTypeSize(.accessibility2) }
#endif
