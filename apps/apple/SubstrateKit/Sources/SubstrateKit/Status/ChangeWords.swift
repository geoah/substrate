import Foundation

/// An app's request in the words the console's approval page uses for it
/// (web/console's app-scopes.ts and app-setup.ts): a scope as what the app
/// may do, a setup row as what approving does on the server.
public extension AppManifest {
    /// One scope in words: a kind the app names by its declared label, and a
    /// webhook's by the package it delivers for where the app asks for that
    /// package.
    func scopeWords(_ scope: String) -> String {
        let (verb, pattern) = ScopeWords.split(scope)
        if verb == "webhook" { return ScopeWords.webhookSentence(trigger: pattern, packageName: webhookPackageName(trigger: pattern)) }
        if verb == "read" || verb == "write", let label = kindLabels[pattern] {
            return ScopeWords.sentence(scope, kinds: [ScopeKind(identity: pattern, plural: label.plural)])
        }
        return ScopeWords.sentence(scope)
    }

    /// What a first sign-in asks the owner for: every scope, the webhook
    /// deliveries an approved webhook adds, and every row set up.
    func plannedChanges(for metadata: AuthorizationServerMetadata?) -> [Change] {
        let details = details(for: metadata)
        var changes = scopes.map { Change(kind: .scope, words: scopeWords($0)) }
        changes += details.compactMap { detail in
            guard detail.type == AuthorizationDetail.Types.webhook, let trigger = detail.trigger else { return nil }
            return Change(kind: .scope, words: ScopeWords.webhookSentence(trigger: trigger, packageName: webhookPackageName(trigger: trigger)))
        }
        changes += details.compactMap { setupChange($0) }
        return changes
    }

    /// The changes a check found against what the app holds, and what the
    /// server itself lacks to make them: a package the catalog ships below
    /// the version the app needs is a server update, not an approval.
    func changes(from result: AuthorizationCheckResult, session: Session?,
                 metadata: AuthorizationServerMetadata?) -> (changes: [Change], serverMissing: [String])
    {
        let details = details(for: metadata)
        var changes = result.missingScopes.map { Change(kind: .scope, words: scopeWords($0)) }
        // A webhook the token does not hold reads `unknown` in the check, so
        // the session's own scopes say whether delivering to it was approved.
        if let grants = session?.grants {
            changes += details.compactMap { detail in
                guard detail.type == AuthorizationDetail.Types.webhook, let trigger = detail.trigger,
                      !grants.delivers(trigger) else { return nil }
                return Change(kind: .scope, words: ScopeWords.webhookSentence(trigger: trigger,
                                                                              packageName: webhookPackageName(trigger: trigger)))
            }
        }
        var serverMissing: [String] = []
        for (index, row) in result.setup.enumerated() {
            let detail = index < details.count ? details[index] : nil
            switch row.action {
            case AuthorizationSetup.Actions.blocked:
                if let package = row.package, package.shipped < package.required {
                    serverMissing.append("install \(packageName(package.id)) version \(package.required) or later")
                } else if let change = setupChange(detail ?? AuthorizationDetail(type: row.type), row: row) {
                    changes.append(Change(kind: change.kind, words: change.words,
                                          detail: row.problem ?? "Open your substrate to see what is in the way.", blocked: true))
                }
            case let action where AuthorizationSetup.Actions.needsApproval(action):
                if let change = setupChange(detail ?? AuthorizationDetail(type: row.type), row: row) { changes.append(change) }
            default:
                // `none` is in place, `unknown` is out of the token's sight,
                // and an action this kit does not know asks nothing either.
                continue
            }
        }
        return (changes, serverMissing)
    }

    /// One setup row in words. `row` is the server's plan where a check gave
    /// one; without it the row is a first sign-in's, all to do.
    private func setupChange(_ detail: AuthorizationDetail, row: AuthorizationSetup? = nil) -> Change? {
        switch detail.type {
        case AuthorizationDetail.Types.package:
            guard let id = detail.package ?? row?.package?.id else { return nil }
            let name = packageName(id)
            if let row, let package = row.package, row.action == AuthorizationSetup.Actions.upgrade
                || (row.action == AuthorizationSetup.Actions.blocked && package.held > 0)
            {
                return Change(kind: .upgrade, words: "Update \(name)",
                              detail: "Version \(package.held) to \(max(package.required, detail.version ?? 0))")
            }
            return Change(kind: .install, words: "Install \(name)")
        case AuthorizationDetail.Types.account:
            guard let kind = detail.kind ?? row?.account?.kind else { return nil }
            let properties = detail.properties ?? row?.account?.properties ?? [:]
            let id = detail.id ?? row?.account?.id ?? ""
            let nameKey = ["displayName", "name", "title"].first { properties[$0]?.text?.isEmpty == false }
            let name = nameKey.flatMap { properties[$0]?.text } ?? id
            let rest = properties.keys.filter { $0 != nameKey }.sorted().compactMap { key -> String? in
                guard let value = properties[key], value != .null else { return nil }
                return "\(propertyLabel(key)): \(value.text ?? value.jsonText)"
            }
            return Change(kind: .account, words: "Create the \(accountWords(kind: kind)) “\(name)”",
                          detail: rest.isEmpty ? nil : rest.joined(separator: ", "))
        case AuthorizationDetail.Types.webhook:
            guard let trigger = detail.trigger ?? row?.webhook?.trigger else { return nil }
            return Change(kind: .webhook, words: "Turn on the \(webhookWords(trigger: trigger))")
        default:
            return nil
        }
    }
}
