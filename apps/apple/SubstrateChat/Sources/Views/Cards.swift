import SwiftUI
import SubstrateKit

/// The agent's questions, inline in the thread that asked them. Answering is
/// one patch with the version the owner read; the engine checks every
/// selection against the stored option values. Prompts and labels are the
/// model's and render as plain text; what is sent is the stored values.
struct InteractionCard: View {
    @Environment(ChatStore.self) private var store
    let id: String
    @State private var record: Record?
    @State private var picked: [String: Set<String>] = [:]
    @State private var busy: String?
    @State private var problem: String?

    private struct Question: Identifiable {
        let id: String
        let prompt: String
        let options: [(value: String, label: String, description: String?)]
        let multi: Bool
    }

    private var questions: [Question] {
        (record?["questions"]?.array ?? []).compactMap { q in
            guard let id = q["id"]?.text, let prompt = q["prompt"]?.text else { return nil }
            let options = (q["options"]?.array ?? []).compactMap { o -> (String, String, String?)? in
                guard let value = o["value"]?.text else { return nil }
                return (value, o["label"]?.text ?? value, o["description"]?.text)
            }
            return Question(id: id, prompt: prompt, options: options, multi: q["multi"]?.bool == true)
        }
    }

    private var answered: [String: Set<String>] {
        var out: [String: Set<String>] = [:]
        for a in record?["answers"]?.array ?? [] {
            if let q = a["question"]?.string { out[q] = Set(a["selected"]?.array?.compactMap(\.string) ?? []) }
        }
        return out
    }

    var body: some View {
        let record = record ?? store.cardRecords[id]
        let state = record?["state"]?.string ?? "pending"
        // Until the record is read the card claims no state: a resolved one
        // must not flash as waiting.
        let pending = record != nil && state == "pending"
        VStack(alignment: .leading, spacing: 12) {
            Label(record == nil ? "Questions" : pending ? "A few questions before it goes on"
                : (state == "answered" ? "You answered" : "You dismissed these"),
                systemImage: "questionmark.bubble")
                .font(.subheadline.weight(.semibold))
            if record == nil {
                ProgressView().controlSize(.small)
            }
            ForEach(questions) { q in
                VStack(alignment: .leading, spacing: 6) {
                    Text(q.prompt).font(.subheadline)
                    ForEach(q.options, id: \.value) { option in
                        let chosen = pending ? picked[q.id, default: []].contains(option.value)
                            : answered[q.id, default: []].contains(option.value)
                        Button {
                            toggle(q, option.value)
                        } label: {
                            HStack(alignment: .firstTextBaseline, spacing: 8) {
                                Image(systemName: q.multi ? (chosen ? "checkmark.square.fill" : "square")
                                    : (chosen ? "largecircle.fill.circle" : "circle"))
                                    .foregroundStyle(chosen ? Color.accentColor : .secondary)
                                VStack(alignment: .leading, spacing: 1) {
                                    Text(option.label)
                                    if let d = option.description {
                                        Text(d).font(.caption).foregroundStyle(.secondary)
                                    }
                                }
                                Spacer(minLength: 0)
                            }
                            .padding(.vertical, 4)
                            .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                        .disabled(!pending || busy != nil)
                    }
                }
            }
            if let problem {
                Text("That didn't go through: \(problem)").font(.footnote).foregroundStyle(.red)
            }
            // Answering needs the version just read, never a cached one.
            if pending, self.record != nil {
                HStack {
                    Button {
                        resolve(answer: true)
                    } label: {
                        if busy == "answer" { ProgressView().controlSize(.small) } else { Text("Answer") }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(busy != nil || !questions.allSatisfy { !(picked[$0.id]?.isEmpty ?? true) })
                    Button("Dismiss") { resolve(answer: false) }
                        .buttonStyle(.bordered)
                        .disabled(busy != nil)
                }
            }
        }
        .card(tint: pending ? .orange : nil)
        .task(id: id) { await load() }
    }

    private func load() async {
        do {
            record = try await store.api.get(Kinds.interaction, id)
            if let record { store.remember(card: record) }
        } catch {
            if isCancellation(error) || store.handle(error) { return }
            problem = explain(error, server: store.session.server)
        }
    }

    private func toggle(_ q: Question, _ value: String) {
        if q.multi {
            picked[q.id, default: []].formSymmetricDifference([value])
        } else {
            picked[q.id] = [value]
        }
    }

    private func resolve(answer: Bool) {
        guard let record else { return }
        busy = answer ? "answer" : "dismiss"
        problem = nil
        let properties: [String: Any] = answer
            ? ["state": "answered", "answers": questions.map { ["question": $0.id, "selected": Array(picked[$0.id] ?? [])] }]
            : ["state": "dismissed"]
        Task {
            defer { busy = nil }
            do {
                self.record = try await store.api.patch(Kinds.interaction, id, properties: properties, ifVersion: record.version)
                // The resolution wrote into the thread and resumed the agent.
                try? await store.refreshCards()
                await store.refreshThreads()
            } catch {
                if store.handle(error) { return }
                problem = (error as? ServerError)?.status == 409
                    ? "The questions changed since you read them. Check them again."
                    : explain(error, server: store.session.server)
                await load()
            }
        }
    }
}

/// A suggested change: the record, each property before and after, Apply and
/// Dismiss. The decision carries the request's version, so a change slipped
/// under it is refused, and a refusal means read again, never retry. Once
/// decided it is the change that was applied, or the one that was not.
///
/// What it shows comes from the transcript first (the `propose` call and the
/// decision the substrate wrote), so it draws at once and whatever the token
/// may read; the request, read when it can be, adds the version to decide
/// with, and the target its name and its values before.
struct ProposalCard: View {
    @Environment(ChatStore.self) private var store
    let id: String
    /// The `propose` call that made it.
    var call: ToolCall?
    /// The decision the transcript recorded.
    var outcome: DecisionNotice?
    @State private var request: Record?
    @State private var target: Record?
    @State private var busy: String?
    @State private var problem: String?

    /// The request as last read here, or by the store's list of the
    /// suggestions waiting, which carries its version too.
    private var held: Record? { request ?? store.cardRecords[id] ?? store.requests.first { $0.id == id } }
    private var asked: (change: RecordChange, rationale: String?)? { call.flatMap(proposedChange) }

    private var decision: String {
        held?["decision"]?.string ?? outcome.map { $0.accepted ? "accepted" : "rejected" } ?? "proposed"
    }

    private var op: String { held?["op"]?.string ?? outcome?.op ?? asked?.change.op.rawValue ?? "patch" }

    private var targetPath: String? { held?["target"]?.referencePath ?? outcome?.target }

    private var targetKind: String? {
        held?["targetKind"]?.text ?? targetPath.map { $0.split(separator: "/").dropLast().joined(separator: "/") }
            ?? asked?.change.kind
    }

    private var targetID: String? {
        held?["targetId"]?.text ?? held?["target"]?.referenceID ?? outcome?.target?.split(separator: "/").last.map(String.init)
            ?? asked?.change.id
    }

    /// What the change writes: the owner's adjusted values where they applied
    /// their own, else the agent's.
    private var values: [String: JSONValue] {
        if decision == "accepted" {
            if let adjusted = held?["adjustedDiff"]?["properties"]?.object ?? outcome?.adjustedValues { return adjusted }
        }
        return held?["diff"]?["properties"]?.object ?? asked?.change.values ?? [:]
    }

    private var change: RecordChange {
        RecordChange(op: op, kind: targetKind, id: targetID, title: recordTitle(target.map { .object($0.properties) }),
                     values: values)
    }

    var body: some View {
        switch decision {
        case "proposed":
            proposed
        case "accepted":
            ChangePanel(change: change, tense: .done, badge: .applied, note: held?["rationale"]?.text ?? asked?.rationale,
                        tint: .green)
                .task(id: id) { await load() }
        default:
            ChangePanel(change: change, tense: .suggested, badge: .dismissed,
                        note: held?["rationale"]?.text ?? asked?.rationale, struck: true)
                .task(id: id) { await load() }
        }
    }

    private var proposed: some View {
        VStack(alignment: .leading, spacing: 10) {
            ChangeHeader(change: change, tense: .suggests)
            if let rationale = held?["rationale"]?.text ?? asked?.rationale {
                Text(rationale).font(.footnote).foregroundStyle(.secondary)
            }
            ForEach(values.keys.sorted(), id: \.self) { key in
                VStack(alignment: .leading, spacing: 3) {
                    Text(propertyLabel(key)).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                    if op == "patch", let before = target?[key] {
                        RecordValueView(before, struck: true).font(.footnote).lineLimit(4)
                    }
                    RecordValueView(values[key] ?? .null).font(.footnote).lineLimit(6)
                }
            }
            if let problem {
                Text("That didn't go through: \(problem)").font(.footnote).foregroundStyle(.red)
            }
            if !store.canDecide {
                // Without `decide:` the request is not the app's to decide.
                Text("Substrate Chat wasn't approved to apply suggestions. Decide this one in the console.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                Link(destination: store.console("change-requests/\(API.segment(id))")) {
                    Label("Open in the Console", systemImage: "arrow.up.right.square")
                }
                .font(.footnote.weight(.semibold))
            } else {
                HStack {
                    Button {
                        decide(accept: true)
                    } label: {
                        if busy == "accept" { ProgressView().controlSize(.small) } else {
                            Text(op == "create" ? "Add It" : op == "delete" ? "Delete It" : "Apply")
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(op == "delete" ? .red : .accentColor)
                    Button("Dismiss") { decide(accept: false) }
                        .buttonStyle(.bordered)
                }
                .disabled(busy != nil)
            }
        }
        .card(tint: .blue)
        .task(id: id) { await load() }
    }

    private func load() async {
        if store.canDecide {
            do {
                request = try await store.api.get(Kinds.request, id)
                if let request { store.remember(card: request) }
            } catch {
                if isCancellation(error) || store.handle(error) { return }
                // A decided card has nothing to act on: what it shows is the
                // transcript's, and a failed read changes none of it.
                if decision == "proposed" { problem = explain(error, server: store.session.server) }
            }
        }
        if op != "create", let kind = targetKind, let tid = targetID {
            // The target is context, and often out of the token's reach.
            target = try? await store.api.get(kind, tid)
        }
    }

    private func decide(accept: Bool) {
        busy = accept ? "accept" : "reject"
        problem = nil
        let shown = values
        Task {
            defer { busy = nil }
            do {
                // The version decided on is the one whose change is on
                // screen: read now if the card has none, and refuse if what
                // it holds is not what was shown.
                var held = self.held
                if held == nil {
                    held = try await store.api.get(Kinds.request, id)
                    request = held
                    if held?["diff"]?["properties"]?.object.map({ $0 != shown }) ?? false {
                        problem = "It changed since you read it. Check it again."
                        return
                    }
                }
                guard let held else {
                    problem = "This suggestion is no longer there."
                    return
                }
                self.request = try await store.api.patch(Kinds.request, id, properties: ["decision": accept ? "accepted" : "rejected"],
                                                         ifVersion: held.version)
                try? await store.refreshCards()
                await store.refreshThreads()
            } catch {
                if store.handle(error) { return }
                problem = (error as? ServerError)?.status == 409
                    ? "It changed since you read it. Check it again."
                    : explain(error, server: store.session.server)
                await load()
            }
        }
    }
}

extension RecordChange {
    /// This change with a title from the chat where it has none of its own.
    func titled(_ titles: [String: String]) -> RecordChange {
        guard title == nil, let kind, let id, let known = titles["\(kind)/\(id)"] else { return self }
        var out = self
        out.title = known
        return out
    }
}

/// How a change's heading reads: done, still a suggestion, or a suggestion
/// that was not taken.
enum ChangeTense { case done, suggests, suggested }

/// "Changed [Groceries]", "Suggests adding a task": the op's mark, the verb,
/// and the record as its pill where there is one to open.
struct ChangeHeader: View {
    @Environment(\.recordTitles) private var titles
    let change: RecordChange
    let tense: ChangeTense
    var badge: ChangeBadge?

    var body: some View {
        HStack(alignment: .center, spacing: 6) {
            Image(systemName: symbol)
                .frame(width: 20)
            Text(verb)
                .fixedSize()
            // Nothing to open before a suggested record exists, nor after a
            // delete: those say the kind instead.
            if let ref = change.reference, change.op != .delete, !(change.op == .create && tense != .done) {
                RecordPill(ref, title: change.title)
            } else {
                Text(what).lineLimit(1)
            }
            Spacer(minLength: 8)
            if let badge { badge }
        }
        .font(.subheadline.weight(.semibold))
    }

    private var symbol: String {
        switch change.op {
        case .delete: "trash"
        case .create: "plus.square"
        case .put, .patch: "square.and.pencil"
        }
    }

    private var what: String {
        let kind = change.kind.map { kindDisplayName($0).lowercasedFirst } ?? "record"
        return change.titled(titles).title.map { "“\($0)”" } ?? (change.op == .create ? "a new \(kind)" : "a \(kind)")
    }

    private var verb: String {
        switch tense {
        case .done:
            switch change.op {
            case .create: "Added"
            case .put: "Saved"
            case .patch: "Changed"
            case .delete: "Deleted"
            }
        case .suggests, .suggested:
            (tense == .suggests ? "Suggests " : "Suggested ") + {
                switch change.op {
                case .create: "adding"
                case .put: "saving"
                case .patch: "changing"
                case .delete: "deleting"
                }
            }()
        }
    }
}

/// A decided suggestion's outcome, as a capsule.
enum ChangeBadge: View {
    case applied, dismissed

    var body: some View {
        let applied = self == .applied
        Label(applied ? "Applied" : "Dismissed", systemImage: applied ? "checkmark" : "xmark")
            .font(.caption.weight(.semibold))
            .foregroundStyle(applied ? Color.green : Color.secondary)
            .padding(.horizontal, 8).padding(.vertical, 3)
            .background((applied ? Color.green : Color.secondary).opacity(0.15), in: .capsule)
            .fixedSize()
    }
}

/// What a change wrote: the record, then each value it set. A delete has no
/// values; a stamped change from a function names its record alone.
struct ChangePanel: View {
    let change: RecordChange
    var tense: ChangeTense = .done
    var badge: ChangeBadge?
    var note: String?
    var struck = false
    var tint: Color?

    /// Past this many values the panel says how many more there are.
    private let shown = 8

    var body: some View {
        // The heading already names the record.
        let keys = change.values.keys.sorted().filter { key in
            !(["name", "title", "summary", "subject"].contains(key) && change.values[key]?.text == change.title && change.title != nil)
        }
        VStack(alignment: .leading, spacing: 10) {
            ChangeHeader(change: change, tense: tense, badge: badge)
            if let note {
                Text(note).font(.footnote).foregroundStyle(.secondary)
            }
            ForEach(keys.prefix(shown), id: \.self) { key in
                VStack(alignment: .leading, spacing: 3) {
                    Text(propertyLabel(key)).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                    RecordValueView(change.values[key] ?? .null, struck: struck)
                        .font(.footnote)
                        .lineLimit(6)
                }
            }
            if keys.count > shown {
                Text("and \(keys.count - shown) more").font(.caption).foregroundStyle(.secondary)
            }
        }
        .card(tint: tint)
        .accessibilityElement(children: .combine)
    }
}

/// A call's changes that carry no values (a function's, a sub-agent's): one
/// line each, the record as its pill, in one card.
struct ChangeList: View {
    let changes: [RecordChange]
    var wrote: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(changes.prefix(8).enumerated()), id: \.offset) { _, change in
                ChangeHeader(change: change, tense: .done)
                    .font(.footnote)
            }
            if changes.count > 8 {
                Text("and \(changes.count - 8) more").font(.caption).foregroundStyle(.secondary)
            }
            if let wrote {
                Label(wrote, systemImage: "square.stack.3d.up").font(.footnote)
            }
        }
        .card()
    }
}

/// Writes a provider's API key. The key lives in the field until it is sent:
/// never cached, never logged.
struct ProviderKeySheet: View {
    @Environment(ChatStore.self) private var store
    @Environment(\.dismiss) private var dismiss
    let provider: Provider
    @State private var key = ""
    @State private var busy = false
    @State private var problem: String?

    var body: some View {
        NavigationStack {
            if store.canWriteKeys { form } else { console }
        }
        .frame(minWidth: 380, minHeight: 260)
    }

    /// An app never writes a provider's key: the owner adds it in the console,
    /// on the provider's own record.
    private var console: some View {
        Form {
            Section {
                Text("Agents run on your own key, sealed on the provider's record. Add it in the console, where only you can write it.")
                Link(destination: store.console("data/\(Kinds.provider)/\(API.segment(provider.id))")) {
                    Label("Open \(provider.name) in the Console", systemImage: "arrow.up.right.square")
                }
            } header: {
                Text("\(provider.name) API key")
            } footer: {
                Text("Its completions go to \(provider.endpoint).")
            }
        }
        .formStyle(.grouped)
        .navigationTitle("Add API Key")
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .toolbar {
            ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
        }
    }

    private var form: some View {
            Form {
                Section {
                    SecureField("API key", text: $key)
                        .textContentType(.password)
                        #if os(iOS)
                        .textInputAutocapitalization(.never)
                        #endif
                        .autocorrectionDisabled()
                } header: {
                    Text("\(provider.name) API key")
                } footer: {
                    Text("Agents run on your own key. It is sealed on the provider's record on \(store.session.server.host() ?? "your server") and sent to \(provider.endpoint).")
                }
                if let problem {
                    Section { ProblemLine(message: problem) }
                }
            }
            .formStyle(.grouped)
            .navigationTitle("Add API Key")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }
                        .disabled(key.trimmingCharacters(in: .whitespaces).isEmpty || busy)
                }
            }
    }

    private func save() {
        busy = true
        problem = nil
        let value = key.trimmingCharacters(in: .whitespacesAndNewlines)
        Task {
            defer { busy = false }
            do {
                try await store.setKey(value, provider: provider.id)
                key = ""
                dismiss()
            } catch {
                if store.handle(error) { return }
                problem = explain(error, server: store.session.server)
            }
        }
    }
}
