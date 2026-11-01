import WorkoutsCore
import SwiftUI
import UIKit

/// The cover's header (DESIGN.md §7.6): three labelled stats, Duration,
/// Volume and Working sets (warm-ups never move it, so the label says so).
/// Duration's clock ticks with no app code; a tap pauses or resumes it. At
/// the largest text sizes the three stack.
struct SessionHeaderView: View {
    let header: SessionHeaderStats
    let togglePause: () -> Void

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .top, spacing: 12) { stats }
            VStack(alignment: .leading, spacing: 10) { stats }
        }
        .padding(.horizontal, 4)
    }

    @ViewBuilder
    private var stats: some View {
        Button(action: togglePause) {
            stat(SessionHeaderStats.durationTitle) {
                if header.isPaused {
                    Text(header.durationText)
                        .foregroundStyle(.orange)
                } else {
                    SessionClockText(clockStart: header.clockStart, pausedAt: header.clockPausedAt)
                        .foregroundStyle(.tint)
                }
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        // The label is the stat's own, its title and the clock, so the time is read.
        .accessibilityValue(header.isPaused ? "paused" : "")
        .accessibilityHint(header.isPaused ? "Resumes the session clock" : "Pauses the session clock")
        stat(SessionHeaderStats.volumeTitle) { Text(header.volumeText) }
        stat(SessionHeaderStats.setsTitle) { Text(header.setsText) }
    }

    private func stat(_ label: String, @ViewBuilder value: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .font(.footnote)
                .foregroundStyle(.secondary)
            value()
                .font(.title3.weight(.semibold).monospacedDigit())
                .lineLimit(1)
                .minimumScaleFactor(0.7)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

/// "Alerts are off for Workouts. Rest ends will not notify." with a way to
/// the system settings (§7.6, §9.6). Quiet: content, not glass.
/// The journal could not be written (a full disk): ticks are kept in memory
/// only until a write succeeds, so the owner hears it now, not at Finish.
struct NotSavingBanner: View {
    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.orange)
                .accessibilityHidden(true)
            Text("Changes are not being saved on this iPhone. Its storage may be full; free some space to keep this session.")
                .font(.footnote)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(12)
        .cardBackground(cornerRadius: 12)
    }
}

struct AlertsOffBanner: View {
    @Environment(\.openURL) private var openURL

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Image(systemName: "bell.slash")
                .foregroundStyle(.orange)
                .accessibilityHidden(true)
            Text("Alerts are off for Workouts. Rest ends will not notify.")
                .font(.footnote)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button("Settings") {
                if let url = URL(string: UIApplication.openNotificationSettingsURLString) { openURL(url) }
            }
            .font(.footnote.weight(.semibold))
            .buttonStyle(.bordered)
        }
        .padding(12)
        .cardBackground(cornerRadius: 12)
    }
}
