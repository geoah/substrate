// What a TestFlight release reads and keeps in App Store Connect, with the
// API key a release signs with.
//
//   swift scripts/appstoreconnect.swift records <bundle-id>…
//
// One line per bundle id that has an App Store Connect record, its bundle id
// and the record's Apple ID. A bundle id with no record prints nothing, and
// the caller names it; the API cannot create a record, so a new app's is made
// once in App Store Connect by hand.
//
//   swift scripts/appstoreconnect.swift profiles <dir> <sha1,…> <bundle-id>…
//
// One App Store profile per bundle id (registered first where it is not),
// signed by the team's Apple Distribution certificate whose SHA-1 is one of
// <sha1,…> (an identity in this machine's keychain), written to
// <dir>/<uuid>.mobileprovision; one line per
// bundle id, its bundle id and the profile's UUID. An active profile of that
// name and certificate is reused, and one the bundle id's capabilities have
// since invalidated is replaced. The key may not use the cloud-managed
// distribution certificate, so an export signs with these, by hand.
//
// The key is SUBSTRATE_APPLE_ASC_KEY_PATH (the AuthKey_….p8),
// SUBSTRATE_APPLE_ASC_KEY_ID and SUBSTRATE_APPLE_ASC_ISSUER_ID, as for
// xcodebuild. Its token is signed here (ES256) and lives ten minutes.
import CryptoKit
import Foundation

func fail(_ message: String) -> Never {
    FileHandle.standardError.write(Data("appstoreconnect: \(message)\n".utf8))
    exit(1)
}

func environment(_ name: String) -> String {
    guard let value = ProcessInfo.processInfo.environment[name], !value.isEmpty else {
        fail("set \(name)")
    }
    return value
}

func base64URL(_ data: Data) -> String {
    data.base64EncodedString()
        .replacingOccurrences(of: "+", with: "-")
        .replacingOccurrences(of: "/", with: "_")
        .replacingOccurrences(of: "=", with: "")
}

func token() -> String {
    let path = environment("SUBSTRATE_APPLE_ASC_KEY_PATH")
    let id = environment("SUBSTRATE_APPLE_ASC_KEY_ID")
    let issuer = environment("SUBSTRATE_APPLE_ASC_ISSUER_ID")
    guard let pem = try? String(contentsOfFile: path, encoding: .utf8) else { fail("cannot read \(path)") }
    let key: P256.Signing.PrivateKey
    do { key = try P256.Signing.PrivateKey(pemRepresentation: pem) } catch { fail("\(path) is no P-256 private key: \(error)") }
    let now = Int(Date().timeIntervalSince1970)
    let header = try! JSONSerialization.data(withJSONObject: ["alg": "ES256", "kid": id, "typ": "JWT"], options: .sortedKeys)
    let claims = try! JSONSerialization.data(
        withJSONObject: ["iss": issuer, "iat": now, "exp": now + 600, "aud": "appstoreconnect-v1"], options: .sortedKeys)
    let input = base64URL(header) + "." + base64URL(claims)
    // JWT's ES256 is the raw r‖s, not the DER CryptoKit's derRepresentation is.
    let signature = try! key.signature(for: Data(input.utf8)).rawRepresentation
    return input + "." + base64URL(signature)
}

let bearer = token()

/// One call; a status other than `expect` stops the run with Apple's answer.
@discardableResult
func call(_ method: String, _ path: String, query: [String: String] = [:], body: Any? = nil, expect: Int = 200) async throws -> [String: Any] {
    var url = URLComponents(string: "https://api.appstoreconnect.apple.com" + path)!
    if !query.isEmpty { url.queryItems = query.sorted { $0.key < $1.key }.map { URLQueryItem(name: $0.key, value: $0.value) } }
    var request = URLRequest(url: url.url!)
    request.httpMethod = method
    request.setValue("Bearer \(bearer)", forHTTPHeaderField: "Authorization")
    if let body {
        request.httpBody = try JSONSerialization.data(withJSONObject: body)
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
    }
    let (data, response) = try await URLSession.shared.data(for: request)
    let status = (response as? HTTPURLResponse)?.statusCode ?? 0
    guard status == expect else {
        fail("App Store Connect answered \(status) to \(method) \(path): \(String(decoding: data, as: UTF8.self))")
    }
    if data.isEmpty { return [:] }
    return (try JSONSerialization.jsonObject(with: data)) as? [String: Any] ?? [:]
}

func rows(_ answer: [String: Any]) -> [[String: Any]] { answer["data"] as? [[String: Any]] ?? [] }
func attributes(_ row: [String: Any]) -> [String: Any] { row["attributes"] as? [String: Any] ?? [:] }

func records(_ bundles: [String]) async throws {
    for bundle in bundles {
        let apps = rows(try await call("GET", "/v1/apps", query: ["filter[bundleId]": bundle, "fields[apps]": "bundleId"]))
        // The filter matches more than the one id; only an exact one is its record.
        if let app = apps.first(where: { attributes($0)["bundleId"] as? String == bundle }) {
            print("\(bundle) \(app["id"] as! String)")
        }
    }
}

func profiles(_ dir: String, _ hashes: Set<String>, _ bundles: [String]) async throws {
    // The team's distribution certificate whose key this machine holds.
    let certificates = rows(
        try await call(
            "GET", "/v1/certificates",
            query: [
                "filter[certificateType]": "DISTRIBUTION,IOS_DISTRIBUTION", "fields[certificates]": "certificateContent,displayName",
                "limit": "200",
            ]))
    let certificate = certificates.first { row in
        guard let content = attributes(row)["certificateContent"] as? String, let der = Data(base64Encoded: content) else { return false }
        let sha1 = Insecure.SHA1.hash(data: der).map { String(format: "%02X", $0) }.joined()
        return hashes.contains(sha1)
    }
    guard let certificate, let certificateID = certificate["id"] as? String else {
        fail("no Apple Distribution certificate of the team is in this keychain; import the one in the release secrets")
    }
    for bundle in bundles {
        let ids = rows(try await call("GET", "/v1/bundleIds", query: ["filter[identifier]": bundle, "fields[bundleIds]": "identifier"]))
        var bundleID = ids.first(where: { attributes($0)["identifier"] as? String == bundle })?["id"] as? String
        if bundleID == nil {
            // An extension with no capability of its own is signed in
            // development by the team's wildcard profile, so no build ever
            // registered it. Named from its labels past the organisation's
            // reverse domain, "Substrate Sync Share", in place of Xcode's "XC …".
            let label = bundle.split(separator: ".").dropFirst(2).map { $0.prefix(1).uppercased() + $0.dropFirst() }
            let body: [String: Any] = [
                "data": [
                    "type": "bundleIds",
                    "attributes": ["identifier": bundle, "name": label.joined(separator: " "), "platform": "UNIVERSAL"],
                ]
            ]
            bundleID = ((try await call("POST", "/v1/bundleIds", body: body, expect: 201))["data"] as? [String: Any])?["id"] as? String
            FileHandle.standardError.write(Data("appstoreconnect: registered \(bundle)\n".utf8))
        }
        guard let bundleID else { fail("\(bundle) could not be registered with the team") }
        let name = "\(bundle) App Store \(certificateID)"
        let existing = rows(
            try await call(
                "GET", "/v1/bundleIds/\(bundleID)/profiles",
                query: [
                    "fields[profiles]": "name,profileType,profileState,uuid,profileContent", "limit": "200",
                ])
        ).filter { attributes($0)["name"] as? String == name }
        var profile = existing.first { attributes($0)["profileState"] as? String == "ACTIVE" }
        if profile == nil {
            // An invalidated profile keeps its name, which a new one needs.
            for stale in existing { try await call("DELETE", "/v1/profiles/\(stale["id"] as! String)", expect: 204) }
            let body: [String: Any] = [
                "data": [
                    "type": "profiles",
                    "attributes": ["name": name, "profileType": "IOS_APP_STORE"],
                    "relationships": [
                        "bundleId": ["data": ["type": "bundleIds", "id": bundleID]],
                        "certificates": ["data": [["type": "certificates", "id": certificateID]]],
                    ],
                ]
            ]
            profile = (try await call("POST", "/v1/profiles", body: body, expect: 201))["data"] as? [String: Any]
        }
        guard let profile, let uuid = attributes(profile)["uuid"] as? String,
            let content = attributes(profile)["profileContent"] as? String, let data = Data(base64Encoded: content)
        else { fail("App Store Connect gave \(bundle) no usable profile") }
        try data.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(uuid).mobileprovision"))
        print("\(bundle) \(uuid)")
    }
}

var arguments = Array(CommandLine.arguments.dropFirst())
switch arguments.first {
case "records" where arguments.count > 1:
    try await records(Array(arguments.dropFirst()))
case "profiles" where arguments.count > 3:
    let hashes = Set(arguments[2].split(separator: ",").map { $0.uppercased() })
    try await profiles(arguments[1], hashes, Array(arguments.dropFirst(3)))
default:
    fail("usage: appstoreconnect.swift records <bundle-id>… | profiles <dir> <sha1,…> <bundle-id>…")
}
