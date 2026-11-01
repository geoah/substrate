import SwiftUI

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
            if !store.handle(error) { problem = explain(error, server: store.session.server) }
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
/// under it is refused, and a refusal means read again, never retry.
struct ProposalCard: View {
    @Environment(ChatStore.self) private var store
    let id: String
    /// The decision the transcript recorded: the card is that decision's,
    /// and says so before the request is read.
    var outcome: DecisionNotice?
    @State private var request: Record?
    @State private var target: Record?
    @State private var busy: String?
    @State private var problem: String?

    private var held: Record? { request ?? store.cardRecords[id] }
    private var op: String { held?["op"]?.string ?? "patch" }
    private var decision: String {
        held?["decision"]?.string ?? outcome.map { $0.accepted ? "accepted" : "rejected" } ?? "proposed"
    }

    private var targetKind: String? {
        held?["targetKind"]?.text ?? held?["target"]?.referencePath.map { path in
            path.split(separator: "/").dropLast().joined(separator: "/")
        }
    }

    private var targetID: String? { held?["targetId"]?.text ?? held?["target"]?.referenceID }
    /// What the change writes: the owner's adjusted diff where they applied
    /// one, else the agent's.
    private var proposed: [String: JSONValue] {
        if decision == "accepted", let adjusted = held?["adjustedDiff"]?["properties"]?.object { return adjusted }
        return held?["diff"]?["properties"]?.object ?? [:]
    }

    var body: some View {
        if store.canDecide {
            card
        } else if outcome != nil {
            // Not the app's to read: what was decided is all it says.
            HStack {
                Label("Suggested a change", systemImage: "square.and.pencil")
                    .font(.subheadline.weight(.semibold))
                Spacer()
                badge
            }
            .card(tint: tint)
        } else {
            // Without `decide:` the request is not the app's to read: the
            // console decides it.
            VStack(alignment: .leading, spacing: 10) {
                Label("Suggested a change", systemImage: "square.and.pencil")
                    .font(.subheadline.weight(.semibold))
                Text("Substrate Chat wasn't approved to apply suggestions. Decide this one in the console.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                Link(destination: store.console("change-requests/\(API.segment(id))")) {
                    Label("Open in the Console", systemImage: "arrow.up.right.square")
                }
                .font(.footnote.weight(.semibold))
            }
            .card(tint: .blue)
        }
    }

    private var card: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline) {
                Label(heading, systemImage: op == "delete" ? "trash" : op == "create" ? "plus.square" : "square.and.pencil")
                    .font(.subheadline.weight(.semibold))
                Spacer()
                badge
            }
            if let rationale = held?["rationale"]?.text {
                Text(rationale).font(.footnote).foregroundStyle(.secondary)
            }
            if held == nil { ProgressView().controlSize(.small) }
            ForEach(proposed.keys.sorted(), id: \.self) { key in
                VStack(alignment: .leading, spacing: 2) {
                    Text(propertyLabel(key)).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                    // The target's value is the before only until the change
                    // is applied.
                    if op == "patch", decision == "proposed", let before = target?[key] {
                        Text(words(before)).font(.footnote).strikethrough().foregroundStyle(.secondary)
                    }
                    Text(words(proposed[key] ?? .null))
                        .font(.footnote)
                        .strikethrough(decision == "rejected")
                        .foregroundStyle(decision == "rejected" ? .secondary : .primary)
                }
            }
            if let problem {
                Text("That didn't go through: \(problem)").font(.footnote).foregroundStyle(.red)
            }
            if decision == "proposed", request != nil {
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
        .card(tint: tint)
        .task(id: id) { await load() }
    }

    private var tint: Color? {
        switch decision {
        case "proposed": .blue
        case "accepted": .green
        default: nil
        }
    }

    /// Applied or dismissed, once decided.
    @ViewBuilder private var badge: some View {
        if decision != "proposed" {
            let accepted = decision == "accepted"
            Label(accepted ? "Applied" : "Dismissed", systemImage: accepted ? "checkmark" : "xmark")
                .font(.caption.weight(.semibold))
                .foregroundStyle(accepted ? Color.green : Color.secondary)
                .padding(.horizontal, 8).padding(.vertical, 3)
                .background((accepted ? Color.green : Color.secondary).opacity(0.15), in: .capsule)
                .fixedSize()
        }
    }

    private var heading: String {
        let what = targetKind.map { kindDisplayName($0).lowercasedFirst } ?? "record"
        let verb = decision == "proposed" ? "Suggests" : "Suggested"
        switch op {
        case "create": return "\(verb) adding a \(what)"
        case "delete": return "\(verb) deleting a \(what)"
        default:
            let name = target?["name"]?.text ?? target?["title"]?.text ?? target?["summary"]?.text
            return name.map { "\(verb) changing “\($0)”" } ?? "\(verb) changing a \(what)"
        }
    }

    private func load() async {
        do {
            request = try await store.api.get(Kinds.request, id)
            if let request { store.remember(card: request) }
        } catch {
            if !store.handle(error) { problem = explain(error, server: store.session.server) }
            return
        }
        if op != "create", let kind = targetKind, let tid = targetID {
            // The target is context: a card without it still decides.
            target = try? await store.api.get(kind, tid)
        }
    }

    private func decide(accept: Bool) {
        guard let request else { return }
        busy = accept ? "accept" : "reject"
        problem = nil
        Task {
            defer { busy = nil }
            do {
                self.request = try await store.api.patch(Kinds.request, id, properties: ["decision": accept ? "accepted" : "rejected"],
                                                         ifVersion: request.version)
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

    private func words(_ value: JSONValue) -> String {
        switch value {
        case .null: "Empty"
        case let .string(s): s
        case let .bool(b): b ? "Yes" : "No"
        case let .number(n): n.rounded() == n ? String(Int(n)) : String(n)
        default: value.referencePath ?? value.jsonText
        }
    }
}

/// `dueAt` → "Due", `assignee` → "Assignee".
func propertyLabel(_ key: String) -> String {
    var words = key.splittingWords().map { $0.lowercased() }
    if words.count > 1, words.last == "at" { words.removeLast() }
    return words.joined(separator: " ").capitalizedFirst
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
