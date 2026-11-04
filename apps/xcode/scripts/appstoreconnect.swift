// The App Store Connect records of the bundle ids named, read with the API
// key a release signs with: one line per bundle id that has a record, its
// bundle id and the record's Apple ID. A bundle id with no record prints
// nothing, and the caller names it; the API cannot create a record, so a new
// app's is made once in App Store Connect by hand.
//
//   swift scripts/appstoreconnect.swift <bundle-id>…
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

struct Apps: Decodable {
    struct App: Decodable {
        struct Attributes: Decodable { var bundleId: String }
        var id: String
        var attributes: Attributes
    }
    var data: [App]
}

let bundles = Array(CommandLine.arguments.dropFirst())
guard !bundles.isEmpty else { fail("usage: appstoreconnect.swift <bundle-id>…") }
let bearer = token()
for bundle in bundles {
    var url = URLComponents(string: "https://api.appstoreconnect.apple.com/v1/apps")!
    url.queryItems = [
        URLQueryItem(name: "filter[bundleId]", value: bundle),
        URLQueryItem(name: "fields[apps]", value: "bundleId"),
    ]
    var request = URLRequest(url: url.url!)
    request.setValue("Bearer \(bearer)", forHTTPHeaderField: "Authorization")
    let (body, response) = try await URLSession.shared.data(for: request)
    let status = (response as? HTTPURLResponse)?.statusCode ?? 0
    guard status == 200 else {
        fail("App Store Connect answered \(status) for \(bundle): \(String(decoding: body, as: UTF8.self))")
    }
    // The filter matches more than the one id; only an exact one is its record.
    let apps = try JSONDecoder().decode(Apps.self, from: body)
    if let app = apps.data.first(where: { $0.attributes.bundleId == bundle }) {
        print("\(bundle) \(app.id)")
    }
}
