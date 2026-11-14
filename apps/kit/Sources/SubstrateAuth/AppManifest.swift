import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice

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
    /// This build's version, sent as `software_version` on the authorization
    /// request (RFC 7591's name) and as `softwareVersion` in the set up
    /// check: the server records it on the token beside the floors of the
    /// `package` details, and holds an upgrade that would break this build
    /// while it is connected (decision 0213). At most 64 printable
    /// characters; the default is the bundle's short version and build.
    public var softwareVersion: String
    /// What the server must be able to do: the requirement the app names,
    /// with every scope verb its scopes use and every detail type its
    /// required details use added, so a server that cannot take the request
    /// never meets it. An optional detail is left out for such a server.
    public var requirement: ServerRequirement {
        get {
            var needed = named
            for verb in scopes.map({ ScopeWords.split($0).verb }) where !verb.isEmpty && !needed.scopeVerbs.contains(verb) {
                needed.scopeVerbs.append(verb)
            }
            for type in details.filter({ !$0.optional }).map(\.type) where !needed.detailTypes.contains(type) {
                needed.detailTypes.append(type)
            }
            return needed
        }
        set { named = newValue }
    }

    private var named: ServerRequirement
    /// The path on the loopback address the authorization redirect comes
    /// back to (RFC 8252), for a build without a claimed redirect.
    public var loopbackPath: String
    /// The https redirect a release claims through its associated domain,
    /// `https://<site host>/apps/<app>/callback`: the system hands the
    /// callback only to the signed app the domain's association file names,
    /// which a loopback port cannot promise. Used only beside an https
    /// client id, whose document lists it.
    public var claimedRedirect: URL?
    public var passwordFallback: PasswordFallback
    /// What a person calls a package, an account kind or a trigger, by its
    /// id, where the word built from the id is not the right one.
    public var names: [String: String]
    /// The labels the kinds the app names declare, by kind reference: the
    /// approval page words a kind by its declared label, which the word
    /// built from its name can miss (`applehealth/export` is "Deliveries"),
    /// and names the kinds a glob reaches by theirs.
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

    public init(
        appName: String, clientID: String, scopes: [String], details: [AuthorizationDetail] = [],
        requirement: ServerRequirement = ServerRequirement(), loopbackPath: String = "/oauth/callback",
        claimedRedirect: URL? = nil, passwordFallback: PasswordFallback = .never, names: [String: String] = [:],
        kindLabels: [String: KindLabel] = [:], softwareVersion: String = AppManifest.softwareVersion()
    ) {
        self.appName = appName
        self.clientID = clientID
        self.scopes = scopes
        self.details = details
        self.softwareVersion = Self.softwareVersion(softwareVersion)
        named = requirement
        self.loopbackPath = loopbackPath
        self.claimedRedirect = claimedRedirect
        self.passwordFallback = passwordFallback
        self.names = names
        self.kindLabels = kindLabels
    }

    /// The build's version as the server records it: the bundle's
    /// `CFBundleShortVersionString` and, in parentheses, its `CFBundleVersion`
    /// where the two differ ("0.113.1 (20261021.153012)"); a bundle stamped
    /// with neither is "unknown".
    public static func softwareVersion(bundle: Bundle = .main) -> String {
        let short = (bundle.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String)?.trimmingCharacters(in: .whitespaces) ?? ""
        let build = (bundle.object(forInfoDictionaryKey: "CFBundleVersion") as? String)?.trimmingCharacters(in: .whitespaces) ?? ""
        switch (short.isEmpty, build.isEmpty) {
        case (false, false): return softwareVersion(short == build ? short : "\(short) (\(build))")
        case (false, true): return softwareVersion(short)
        case (true, false): return softwareVersion(build)
        case (true, true): return "unknown"
        }
    }

    /// The most the server stores: 64 characters, each printable and not a
    /// line break, so a stamped value never refuses the request it rides on.
    static let softwareVersionLimit = 64

    static func softwareVersion(_ text: String) -> String {
        let printable = text.unicodeScalars.filter { !CharacterSet.controlCharacters.contains($0) && !CharacterSet.newlines.contains($0) }
        let cut = String(String.UnicodeScalarView(printable)).trimmingCharacters(in: .whitespaces)
        let trimmed = cut.count > softwareVersionLimit ? String(cut.prefix(softwareVersionLimit)) : cut
        return trimmed.isEmpty ? "unknown" : trimmed
    }

    /// The client id a release build is stamped with under `infoKey`, else
    /// `localDefault`: a loopback id, which nothing fetches or verifies.
    public static func clientID(infoKey: String, localDefault: String, bundle: Bundle = .main) -> String {
        let stamped = (bundle.object(forInfoDictionaryKey: infoKey) as? String)?.trimmingCharacters(in: .whitespaces) ?? ""
        return stamped.isEmpty ? localDefault : stamped
    }

    /// The claimed redirect a release build is stamped with under `infoKey`:
    /// an https URL with a host and a path, else nil, which signs in through
    /// the loopback receiver.
    public static func claimedRedirect(infoKey: String, bundle: Bundle = .main) -> URL? {
        let stamped = (bundle.object(forInfoDictionaryKey: infoKey) as? String)?.trimmingCharacters(in: .whitespaces) ?? ""
        return claimedRedirect(stamped)
    }

    /// `text` where it is a callback a release may claim: an https URL with
    /// a host and a path, and no query, fragment or user.
    public static func claimedRedirect(_ text: String) -> URL? {
        guard let url = URL(string: text), url.scheme?.lowercased() == "https",
            let host = url.host(percentEncoded: false), !host.isEmpty,
            url.path(percentEncoded: false).count > 1, url.query(percentEncoded: true) == nil,
            url.fragment(percentEncoded: true) == nil, url.user(percentEncoded: true) == nil
        else { return nil }
        return url
    }

    /// The claimed redirect this build signs in through: its own, beside an
    /// https client id. A local build's loopback id has no document to list
    /// one, so it keeps the loopback receiver.
    public var signInRedirect: URL? { isLocal ? nil : claimedRedirect }

    /// The manifest asking for `more` beside its own scopes, each once: an
    /// app that asks for a kind only when the owner chooses it (Tasks'
    /// collections) asks through the same request and the same review.
    public func asking(_ more: [String]) -> AppManifest {
        var out = self
        for scope in more where !out.scopes.contains(scope) { out.scopes.append(scope) }
        return out
    }

    /// The scopes `session` holds beyond the manifest's: ones an earlier
    /// request added (`asking`), which a request sent again must carry, or
    /// approving it would take them back.
    public func beyond(_ session: Session) -> [String] {
        let local = { ScopeWords.localize($0, authority: session.repository) }
        let own = Set(scopes.map(local))
        return (session.scopes ?? []).filter { !own.contains(local($0)) }
    }

    /// The `scope` parameter.
    public var scope: String { scopes.joined(separator: " ") }

    /// Whether the client id is a loopback one, which the server never
    /// matches to an earlier approval: every approval page is a first.
    public var isLocal: Bool { URL(string: clientID)?.scheme?.lowercased() != "https" }

    /// Whether the approval page lists what `session` holds under Already
    /// allowed: an app session whose access has not ended, approved for this
    /// released client id (or kept by a build that named none). The page
    /// folds nothing an ended approval held.
    public func showsAlreadyAllowed(_ session: Session, at now: Date = Date()) -> Bool {
        !isLocal && session.isApp && !session.hasEnded(at: now) && (session.clientID.map { $0 == clientID } ?? true)
    }

    /// Whether a server sets up every detail the app sends. One that does not
    /// would ignore the parameter and approve the scopes alone, which the app
    /// would then read as set up, so its requirement fails and it never gets
    /// the request.
    public func takesDetails(_ metadata: AuthorizationServerMetadata?) -> Bool {
        let required = details.filter { !$0.optional }
        return required.isEmpty || (metadata.map { m in required.allSatisfy { m.supports(detailType: $0.type) } } ?? false)
    }

    /// The details the request sends to a server: every one it requires,
    /// and each optional one whose type the server lists.
    public func details(for metadata: AuthorizationServerMetadata?) -> [AuthorizationDetail] {
        details.filter { !$0.optional || metadata?.supports(detailType: $0.type) == true }
    }

    /// The scopes the owner left out of the last approval: the ones its
    /// request asked for that the token does not hold. Never a reason to ask
    /// again; a review offers them back.
    public func leftOut(by session: Session) -> [String] {
        guard let held = session.scopes, let asked = session.requestedScopes else { return [] }
        let local = { ScopeWords.localize($0, authority: session.repository) }
        let holds = Set(held.map(local))
        let askedThen = Set(asked.map(local))
        return scopes.filter { askedThen.contains(local($0)) && !holds.contains(local($0)) }
    }

    /// The request the check asks about for `session`: every detail, every
    /// scope but those the owner left out of the last approval, and this
    /// build's version. A scope a later build adds is asked about.
    public func check(for session: Session, metadata: AuthorizationServerMetadata? = nil) -> AuthorizationCheck {
        let leftOut = Set(leftOut(by: session))
        return AuthorizationCheck(
            scopes: scopes.filter { !leftOut.contains($0) }, details: details(for: metadata),
            softwareVersion: softwareVersion)
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

    /// "Apple Health account": the name of the package the kind belongs to,
    /// and the kind's label.
    func accountWords(kind: String) -> String {
        if let named = names[kind] { return named }
        let label = (kindLabels[kind]?.singular ?? kindDisplayName(kind)).lowercasedFirst
        let parts = kind.split(separator: "/")
        guard parts.count == 3 else { return label }
        return "\(packageName(parts.prefix(2).joined(separator: "/"))) \(label)"
    }

    /// "Apple Health webhook", by the package its deliveries reach where it
    /// is known: the server's, else the one the app's detail names.
    func webhookWords(trigger: String, package: String?) -> String {
        if let named = names[trigger] { return named }
        guard let package = package ?? deliversTo(trigger: trigger) else { return "\(trigger) webhook" }
        return "\(packageName(package)) webhook"
    }

    /// The package the app's own webhook detail says a trigger delivers to.
    func deliversTo(trigger: String) -> String? {
        details.first { $0.type == AuthorizationDetail.Types.webhook && $0.trigger == trigger }?.deliversTo
    }
}

/// A kind's declared label, as its declaration's `label` gives it, and its
/// declared purpose where it is not `primary`: a sentence names the primary
/// kinds a glob reaches by their labels and counts the rest.
public struct KindLabel: Sendable, Hashable {
    public var singular: String
    public var plural: String
    public var purpose: String?

    public init(singular: String, plural: String, purpose: String? = nil) {
        self.singular = singular
        self.plural = plural
        self.purpose = purpose
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
