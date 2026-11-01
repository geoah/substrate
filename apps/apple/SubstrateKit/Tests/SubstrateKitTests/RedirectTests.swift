import Foundation
import Testing
@testable import SubstrateKit

/// An HTTP server on a loopback port of its own, so two of them are two
/// origins: each connection is answered `answer`, and every request that
/// reached it is kept, head and body, for the test to read.
final class LocalServer: @unchecked Sendable {
    let port: UInt16
    private let socket: Int32
    private let answer: String
    private let lock = NSLock()
    private var received: [String] = []

    var origin: URL { URL(string: "http://127.0.0.1:\(port)")! }
    var requests: [String] { lock.withLock { received } }

    init(answer: String) throws {
        let fd = Darwin.socket(AF_INET, SOCK_STREAM, 0)
        guard fd >= 0 else { throw POSIXError(.EIO) }
        var address = sockaddr_in()
        address.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
        address.sin_family = sa_family_t(AF_INET)
        address.sin_addr.s_addr = inet_addr("127.0.0.1")
        let bound = withUnsafePointer(to: &address) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { bind(fd, $0, socklen_t(MemoryLayout<sockaddr_in>.size)) }
        }
        guard bound == 0, listen(fd, 8) == 0 else {
            close(fd)
            throw POSIXError(.EADDRINUSE)
        }
        var length = socklen_t(MemoryLayout<sockaddr_in>.size)
        _ = withUnsafeMutablePointer(to: &address) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { getsockname(fd, $0, &length) }
        }
        socket = fd
        port = UInt16(bigEndian: address.sin_port)
        self.answer = answer
        Thread { [self] in serve() }.start()
    }

    /// Closing the socket ends the accept loop.
    func stop() {
        shutdown(socket, SHUT_RDWR)
        close(socket)
    }

    private func serve() {
        while true {
            let client = accept(socket, nil, nil)
            guard client >= 0 else { return }
            let request = read(client)
            lock.withLock { received.append(request) }
            let bytes = Array(answer.utf8)
            _ = bytes.withUnsafeBufferPointer { write(client, $0.baseAddress, $0.count) }
            close(client)
        }
    }

    /// The head, then as much body as its Content-Length says.
    private func read(_ client: Int32) -> String {
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 4096)
        while true {
            let n = recv(client, &buffer, buffer.count, 0)
            if n <= 0 { break }
            data.append(contentsOf: buffer[0 ..< n])
            guard let end = data.range(of: Data("\r\n\r\n".utf8)) else { continue }
            let head = String(decoding: data[..<end.lowerBound], as: UTF8.self)
            let length = head.split(separator: "\r\n").compactMap { line -> Int? in
                let parts = line.split(separator: ":", maxSplits: 1)
                guard parts.count == 2, parts[0].lowercased() == "content-length" else { return nil }
                return Int(parts[1].trimmingCharacters(in: .whitespaces))
            }.first ?? 0
            if data.count - end.upperBound >= length { break }
        }
        return String(decoding: data, as: UTF8.self)
    }
}

/// A redirect is never followed: what a request carries (a bearer token, a
/// code and its verifier, a token to revoke) reaches the server the owner
/// chose and no other origin, and the caller reads the 3xx as a refusal.
@Suite(.timeLimit(.minutes(1))) struct Redirects {
    /// Two origins: `elsewhere` answers as a server would, and `chosen`
    /// sends every request on to it with a 307, which keeps the method and
    /// the body.
    func origins() throws -> (chosen: LocalServer, elsewhere: LocalServer) {
        let elsewhere = try LocalServer(answer: "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}")
        let chosen = try LocalServer(answer: "HTTP/1.1 307 Temporary Redirect\r\nLocation: \(elsewhere.origin.absoluteString)/taken\r\n"
            + "Content-Length: 0\r\nConnection: close\r\n\r\n")
        return (chosen, elsewhere)
    }

    /// The kit's session and one the app made itself: the task refuses the
    /// redirect either way.
    @Test(arguments: [true, false])
    func theTransportAnswersTheRedirectItself(kitSession: Bool) async throws {
        let (chosen, elsewhere) = try origins()
        defer {
            chosen.stop()
            elsewhere.stop()
        }
        let transport = kitSession ? URLSessionTransport.shared : URLSessionTransport(session: URLSession(configuration: .ephemeral))
        var request = URLRequest(url: chosen.origin.appending(path: "oauth/token"))
        request.httpMethod = "POST"
        request.setValue("Bearer sub_x", forHTTPHeaderField: "Authorization")
        request.httpBody = Data("code=c&code_verifier=v".utf8)
        let (_, response) = try await transport.data(for: request)
        #expect(response.statusCode == 307)
        #expect(chosen.requests.count == 1)
        #expect(chosen.requests.first?.contains("code_verifier=v") == true)
        #expect(elsewhere.requests.isEmpty)
    }

    /// A stream an app reads from a session the kit made refuses it too.
    @Test func aStreamFromTheKitsSessionRefusesIt() async throws {
        let (chosen, elsewhere) = try origins()
        defer {
            chosen.stop()
            elsewhere.stop()
        }
        var request = URLRequest(url: chosen.origin.appending(path: "api/v1/changes"))
        request.setValue("Bearer sub_x", forHTTPHeaderField: "Authorization")
        let (_, response) = try await URLSessionTransport.makeSession(timeout: 10).bytes(for: request)
        #expect((response as? HTTPURLResponse)?.statusCode == 307)
        #expect(elsewhere.requests.isEmpty)
    }

    @Test func theExchangeRefusesARedirect() async throws {
        let (chosen, elsewhere) = try origins()
        defer {
            chosen.stop()
            elsewhere.stop()
        }
        let metadata = AuthorizationServerMetadata(issuer: chosen.origin, scopesSupported: ["read:*", "write:*"],
                                                   authorizationDetailsTypesSupported: AuthorizationDetail.Types.all)
        let oauth = OAuthClient(manifest: syncManifest(), metadata: metadata, transport: URLSessionTransport.shared)
        await #expect(throws: Redirected(status: 307, location: "\(elsewhere.origin.absoluteString)/taken")) {
            _ = try await oauth.exchange(code: "c", redirect: "http://127.0.0.1:1/oauth/callback", pkce: PKCE(verifier: "v"))
        }
        #expect(chosen.requests.count == 1)
        #expect(elsewhere.requests.isEmpty)
    }

    @Test func aRevocationRefusesARedirect() async throws {
        let (chosen, elsewhere) = try origins()
        defer {
            chosen.stop()
            elsewhere.stop()
        }
        let app = Session(server: chosen.origin, repository: "ada.example.com", secret: "sub_app_x", clientID: "http://127.0.0.1/kit",
                          scopes: ["read:*"], revocation: chosen.origin.appending(path: "oauth/revoke"))
        await #expect(throws: Redirected.self) { try await app.revoke(transport: URLSessionTransport.shared) }
        let password = Session(server: chosen.origin, repository: "ada.example.com", secret: "sub_password", tokenID: "tok_1")
        await #expect(throws: Redirected.self) { try await password.revoke(transport: URLSessionTransport.shared) }
        #expect(chosen.requests.count == 2)
        #expect(elsewhere.requests.isEmpty)
    }

    @Test func anAuthorizedRequestRefusesARedirect() async throws {
        let (chosen, elsewhere) = try origins()
        defer {
            chosen.stop()
            elsewhere.stop()
        }
        let client = SubstrateClient(server: chosen.origin, token: "sub_x", transport: URLSessionTransport.shared)
        do {
            try await client.send("GET", "api/v1/records")
            Issue.record("a redirect answered as the server")
        } catch let redirect as Redirected {
            #expect(redirect.target == "127.0.0.1")
            #expect(explain(redirect, app: "Substrate Sync", server: chosen.origin).contains("which Substrate Sync never follows"))
        }
        #expect(chosen.requests.first?.contains("Authorization: Bearer sub_x") == true)
        #expect(elsewhere.requests.isEmpty)
    }
}
