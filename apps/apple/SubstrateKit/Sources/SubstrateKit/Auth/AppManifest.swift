import Foundation

/// What an app asks a substrate for, built once per run: who it is, the
/// scopes it needs, what its approval sets up, and the server it needs to do
/// any of it. The same value drives the sign-in, the "still set up" check
/// and the review that asks the owner for the difference.
public struct AppManifest: Sendable {
    /// The name the app goes by in every sentence the kit writes.
    public var appName: String
    /// The URL of the app's client ID metadata document, published beside
    /// its release, or a loopback id for a local build.
    public var clientID: String
    public var scopes: [String]
    public var details: [AuthorizationDetail]
    public var requirement: ServerRequirement
    /// The loopback path the authorization redirect comes back to.
    public var redirectPath: String
    public var passwordFallback: PasswordFallback
    /// What a person calls a package, an account kind or a trigger, by its
    /// id, where the word built from the id is not the right one.
    public var names: [String: String]
    /// The labels the kinds the app names declare, by kind reference: the
    /// approval page words a kind by its declared label, which the word
    /// built from its name can miss (`applehealth/export` is "Deliveries").
    public var kindLabels: [String: KindLabel]

    /// When the app signs in with the owner's password instead.
    public enum PasswordFallback: Sendable, Hashable {
        /// Never: a server that cannot do what the app needs is too old.
        case never
        /// Only on a server with no app authorization at all.
        case withoutAppAuthorization
        /// On any server that does not meet the requirement.
        case belowRequirement
    }

    public init(appName: String, clientID: String, scopes: [String], details: [AuthorizationDetail] = [],
                requirement: ServerRequirement = ServerRequirement(), redirectPath: String = "/oauth/callback",
                passwordFallback: PasswordFallback = .never, names: [String: String] = [:],
                kindLabels: [String: KindLabel] = [:])
    {
        self.appName = appName
        self.clientID = clientID
        self.scopes = scopes
        self.details = details
        self.requirement = requirement
        self.redirectPath = redirectPath
        self.passwordFallback = passwordFallback
        self.names = names
        self.kindLabels = kindLabels
    }

    /// The client id a release build is stamped with under `infoKey`, else
    /// `localDefault`: a loopback id, which nothing fetches or verifies.
    public static func clientID(infoKey: String, localDefault: String, bundle: Bundle = .main) -> String {
        let stamped = (bundle.object(forInfoDictionaryKey: infoKey) as? String)?.trimmingCharacters(in: .whitespaces) ?? ""
        return stamped.isEmpty ? localDefault : stamped
    }

    /// The `scope` parameter.
    public var scope: String { scopes.joined(separator: " ") }

    /// The details a server sets up: every one when it lists every type the
    /// app sends, none otherwise. An older server ignores the parameter and
    /// would approve the scopes alone, which the app then reads as set up.
    public func details(for metadata: AuthorizationServerMetadata?) -> [AuthorizationDetail] {
        guard let metadata, details.allSatisfy({ metadata.supports(detailType: $0.type) }) else { return [] }
        return details
    }

    /// The request the app would send now, as the check reads it.
    public func check(for metadata: AuthorizationServerMetadata?) -> AuthorizationCheck {
        AuthorizationCheck(scopes: scopes, details: details(for: metadata))
    }

    /// How the app signs in to a server it probed.
    public func door(for probe: ServerProbe.Result) -> SignInDoor {
        if probe.verdict.isCompatible { return .app }
        switch passwordFallback {
        case .never: return .tooOld
        case .withoutAppAuthorization: return probe.metadata == nil ? .password : .tooOld
        case .belowRequirement: return .password
        }
    }

    /// Whether a password session on a server it probed should move to app
    /// access: the server now meets the requirement.
    public func canMoveToAppAccess(_ probe: ServerProbe.Result) -> Bool { probe.verdict.isCompatible }

    // MARK: Names

    /// A package by what a person calls it: "Apple Health".
    public func packageName(_ id: String) -> String {
        names[id] ?? packageDisplayName(String(id.split(separator: "/").last ?? Substring(id)))
    }

    /// The package a trigger or an account kind belongs to, among the ones
    /// the app asks for: the kind's own package, or the longest package
    /// word a trigger id starts with (`applehealth-webhook` is
    /// `applehealth`'s).
    func owningPackage(kind: String? = nil, trigger: String? = nil) -> String? {
        let packages = details.compactMap(\.package)
        if let kind {
            let prefix = kind.split(separator: "/").prefix(2).joined(separator: "/")
            return packages.first { $0 == prefix }
        }
        if let trigger {
            return packages.filter { trigger.hasPrefix(String($0.split(separator: "/").last ?? "")) }
                .max { $0.count < $1.count }
        }
        return nil
    }

    /// "Apple Health account": the package's name and the kind's label.
    func accountWords(kind: String) -> String {
        if let named = names[kind] { return named }
        let label = (kindLabels[kind]?.singular ?? kindDisplayName(kind)).lowercasedFirst
        guard let package = owningPackage(kind: kind) else { return label }
        return "\(packageName(package)) \(label)"
    }

    /// "Apple Health webhook".
    func webhookWords(trigger: String) -> String {
        if let named = names[trigger] { return named }
        guard let package = owningPackage(trigger: trigger) else { return "\(trigger) webhook" }
        return "\(packageName(package)) webhook"
    }

    /// The package name a trigger's deliveries are for, where the app asks
    /// for its package.
    func webhookPackageName(trigger: String) -> String? {
        owningPackage(trigger: trigger).map(packageName)
    }
}

/// A kind's declared label, as its declaration's `label` gives it.
public struct KindLabel: Sendable, Hashable {
    public var singular: String
    public var plural: String

    public init(singular: String, plural: String) {
        self.singular = singular
        self.plural = plural
    }
}

/// Which door a server takes for an app.
public enum SignInDoor: Sendable, Hashable {
    /// The authorization flow, in the browser.
    case app
    /// The owner's password, on a server the app falls back for.
    case password
    /// Neither: the server needs an update first.
    case tooOld
}
