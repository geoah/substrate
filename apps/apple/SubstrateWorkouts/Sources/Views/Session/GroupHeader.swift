import WorkoutsCore
import SwiftUI

/// A group's header row (DESIGN.md §7.6): "Superset A: 2 rounds, 1:00 after
/// each round", and a menu for the round rest, the kind, the pairing and
/// "Ungroup all".
struct GroupHeaderView: View {
    let group: GroupHeader

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(group.title)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(GroupPalette.color(group.colorIndex))
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityAddTraits(.isHeader)
            Menu {
                GroupMenuItems(group: group)
            } label: {
                Image(systemName: "ellipsis")
                    .font(.body.weight(.semibold))
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(.rect)
            }
            .accessibilityLabel("\(group.kind.label) \(group.key.uppercased()) options")
        }
        .padding(.horizontal, 4)
    }
}

/// A group's menu items (DESIGN.md §7.6): the round rest, the kind, the
/// pairing and "Ungroup all". The Table's group header and the Rows block's
/// `…` open the same items.
struct GroupMenuItems: View {
    let group: GroupHeader

    @Environment(SessionController.self) private var session
    @Environment(CoverModel.self) private var cover

    var body: some View {
        Menu("Rest after each round…", systemImage: "timer") {
            RestPresetItems(current: group.roundRest, defaultText: "Default") { seconds in
                session.run(RestTarget.roundRest(group: group.key).command(seconds))
            } custom: {
                cover.sheet = .rest(.roundRest(group: group.key))
            }
        }
        Menu("Kind", systemImage: "square.stack") {
            ForEach(GroupKind.allCases, id: \.self) { kind in
                Button {
                    session.run(.setGroup(SessionEvent.SetGroup(group: group.key, kind: kind)))
                } label: {
                    if kind == group.kind { Label(kind.label, systemImage: "checkmark") } else { Text(kind.label) }
                }
            }
        }
        Menu("Pairing", systemImage: "arrow.left.arrow.right") {
            Button {
                session.run(.setGroup(SessionEvent.SetGroup(group: group.key, pairing: .cleared)))
            } label: {
                if group.pairing == nil { Label("None", systemImage: "checkmark") } else { Text("None") }
            }
            ForEach(Pairing.allCases, id: \.self) { pairing in
                Button {
                    session.run(.setGroup(SessionEvent.SetGroup(group: group.key, pairing: .set(pairing))))
                } label: {
                    if pairing == group.pairing { Label(pairing.label, systemImage: "checkmark") } else { Text(pairing.label) }
                }
            }
        }
        Divider()
        Button("Ungroup all", systemImage: "xmark.circle", role: .destructive) {
            session.run(.ungroupAll(group: group.key))
        }
    }
}
