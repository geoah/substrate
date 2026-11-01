import Foundation

/// An app's request in the words the console's approval page uses for it
/// (web/console's app-scopes.ts and app-setup.ts): a scope as what the app
/// may do, a setup row as what approving does on the server.
public extension AppManifest {
    /// One scope in words: a kind the app names by its declared label, the
    /// kinds a glob reaches among those the app labels, and a webhook's by
    /// the package it delivers to, the server's (`package`) where a check
    /// named it, else the one the app's detail names.
    func scopeWords(_ scope: String, package: String? = nil) -> String {
        let (verb, pattern) = ScopeWords.split(scope)
        if verb == "webhook" {
            return ScopeWords.webhookSentence(trigger: pattern, packageName: (package ?? deliversTo(trigger: pattern)).map(packageName))
        }
        if verb == "read" || verb == "write" {
            let kinds = labelledKinds(reachedBy: verb, pattern)
            if !kinds.isEmpty { return ScopeWords.sentence(scope, kinds: kinds) }
        }
        return ScopeWords.sentence(scope)
    }

    /// The kinds the app labels that a read or write pattern reaches, by
    /// reference, as the approval page lists the kinds a scope reaches.
    private func labelledKinds(reachedBy verb: String, _ pattern: String) -> [ScopeKind] {
        let grants = Grants(["\(verb):\(pattern)"])
        return kindLabels.compactMap { identity, label -> ScopeKind? in
            let reached = verb == "write" ? grants.writes(identity, purpose: label.purpose ?? "primary")
                : grants.reads(identity, purpose: label.purpose ?? "primary")
            return reached ? ScopeKind(identity: identity, plural: label.plural, purpose: label.purpose) : nil
        }
        .sorted { $0.identity < $1.identity }
    }

    /// What a first sign-in asks the owner for: every scope, the webhook
    /// deliveries an approved webhook adds, and every row set up.
    func plannedChanges() -> [Change] {
        var changes = scopes.map { Change(kind: .scope, words: scopeWords($0)) }
        changes += details.compactMap { detail in
            guard detail.type == AuthorizationDetail.Types.webhook, let trigger = detail.trigger else { return nil }
            return Change(kind: .scope, words: scopeWords("webhook:\(trigger)"))
        }
        changes += details.compactMap { setupChange($0) }
        return changes
    }

    /// What a review asks the owner for the request the app sends now: the
    /// changes a check found, and each scope the owner left out of the last
    /// approval, which the request asks for again.
    func review(_ status: AppStatus?, session: Session) -> [Change] {
        (status?.changes ?? []) + leftOut(by: session).map { Change(kind: .scope, words: scopeWords($0), leftOut: true) }
    }

    /// The changes a check found against what the app holds, and what the
    /// server itself lacks to make them: a package the catalog ships below
    /// the version the app needs is a server update, not an approval.
    func changes(from result: AuthorizationCheckResult, session: Session?) -> (changes: [Change], serverMissing: [String]) {
        let delivering = Dictionary(result.setup.compactMap { row in row.webhook.flatMap { w in w.package.map { (w.trigger, $0) } } }) { a, _ in a }
        var changes = result.missingScopes.map { Change(kind: .scope, words: scopeWords($0)) }
        // A webhook the token does not hold reads `unknown` in the check, so
        // the session's own scopes say whether delivering to it was approved.
        if let grants = session?.grants {
            changes += details.compactMap { detail in
                guard detail.type == AuthorizationDetail.Types.webhook, let trigger = detail.trigger,
                      !grants.delivers(trigger) else { return nil }
                return Change(kind: .scope, words: scopeWords("webhook:\(trigger)", package: delivering[trigger]))
            }
        }
        var serverMissing: [String] = []
        for (index, row) in result.setup.enumerated() {
            let detail = index < details.count ? details[index] : AuthorizationDetail(type: row.type)
            if let block = row.blocked {
                if block.reason == AuthorizationSetup.Block.Reasons.serverUpdate, let package = row.package {
                    serverMissing.append("install \(packageName(package.id)) version \(package.required) or later")
                } else if var change = setupChange(detail, row: row) {
                    change.detail = block.problem.isEmpty ? change.detail : block.problem
                    change.block = block
                    changes.append(change)
                }
            } else if AuthorizationSetup.Actions.needsApproval(row.action), let change = setupChange(detail, row: row) {
                changes.append(change)
            }
            // `none` is in place, `unknown` is out of the token's sight, and
            // an action this kit does not know asks nothing either.
        }
        return (changes, serverMissing)
    }

    /// One setup row in words. `row` is the server's plan where a check gave
    /// one; without it the row is a first sign-in's, all to do.
    private func setupChange(_ detail: AuthorizationDetail, row: AuthorizationSetup? = nil) -> Change? {
        let action = row?.action
        switch detail.type {
        case AuthorizationDetail.Types.package:
            guard let id = detail.package ?? row?.package?.id else { return nil }
            let name = packageName(id)
            guard let package = row?.package else { return Change(kind: .install, words: "Install \(name)") }
            switch action {
            case AuthorizationSetup.Actions.upgrade:
                return Change(kind: .upgrade, words: "Update \(name)", detail: versionWords(package))
            case AuthorizationSetup.Actions.none:
                return Change(kind: .install, words: "\(name) is installed")
            default:
                return Change(kind: .install, words: "Install \(name)", detail: package.shipped > 0 ? "Version \(package.shipped)" : nil)
            }
        case AuthorizationDetail.Types.account:
            guard let kind = detail.kind ?? row?.account?.kind else { return nil }
            if action == AuthorizationSetup.Actions.none {
                return Change(kind: .account, words: "The \(accountWords(kind: kind)) this app uses is in place")
            }
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
            let what = webhookWords(trigger: trigger, package: row?.webhook?.package)
            if action == AuthorizationSetup.Actions.none { return Change(kind: .webhook, words: "The \(what) is on") }
            return Change(kind: .webhook, words: "Turn on the \(what)")
        default:
            return nil
        }
    }

    /// An upgrade's versions: from the one held to the one the server ships,
    /// and the one the app needs where that is another.
    private func versionWords(_ package: AuthorizationSetup.Package) -> String {
        let to = package.shipped > 0 ? package.shipped : package.required
        let from = "Version \(package.held) to \(to)"
        return package.shipped > 0 && package.shipped != package.required ? "\(from) (needs \(package.required) or later)" : from
    }
}
