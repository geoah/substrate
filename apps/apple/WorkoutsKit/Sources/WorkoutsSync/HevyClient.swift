import Foundation
import WorkoutsCore

/// Hevy's public API, read with an account's API key (Hevy Pro: hevy.com →
/// Settings → Developer). Read only: the import never writes to Hevy. Pages
/// are read to the end; a 429 waits and asks again.
public struct HevyClient: Sendable {
    public static let base = URL(string: "https://api.hevyapp.com/v1/")!

    public var apiKey: String
    public var session: URLSession

    public init(apiKey: String, session: URLSession = .shared) {
        self.apiKey = apiKey.trimmingCharacters(in: .whitespacesAndNewlines)
        self.session = session
    }

    /// The account's workouts, routines and routine folders, and the
    /// templates of the exercises they name that `known` does not map (the
    /// account's own custom exercises). `progress` hears each step.
    public func export(known: Set<String>, progress: @escaping @Sendable (String) -> Void = { _ in }) async throws -> HevyAPI.Export {
        progress("Reading workouts")
        let workouts: [HevyAPI.Workout] = try await pages("workouts", key: "workouts", pageSize: 10, progress: progress)
        progress("Reading routines")
        let routines: [HevyAPI.Routine] = try await pages("routines", key: "routines", pageSize: 10)
        let folders: [HevyAPI.Folder] = try await pages("routine_folders", key: "routine_folders", pageSize: 10)
        let named = Set(workouts.flatMap { $0.exercises.map(\.exerciseTemplateId) } + routines.flatMap { $0.exercises.map(\.exerciseTemplateId) })
        var templates: [String: HevyAPI.Template] = [:]
        for id in named.subtracting(known).sorted() {
            progress("Reading exercise \(templates.count + 1) of \(named.subtracting(known).count)")
            templates[id] = try await get("exercise_templates/\(id)")
        }
        return HevyAPI.Export(workouts: workouts, routines: routines, folders: folders, templates: templates)
    }

    func pages<T: Decodable>(_ path: String, key: String, pageSize: Int, progress: (@Sendable (String) -> Void)? = nil) async throws -> [T] {
        var out: [T] = []
        var page = 1
        while true {
            let data = try await fetch(path, query: [URLQueryItem(name: "page", value: String(page)), URLQueryItem(name: "pageSize", value: String(pageSize))])
            let decoder = HevyAPI.decoder
            decoder.userInfo[Page<T>.key] = key
            let answer = try decoder.decode(Page<T>.self, from: data)
            out += answer.items
            progress?("Reading \(key.replacingOccurrences(of: "_", with: " ")): page \(page) of \(max(answer.pageCount, 1))")
            guard page < answer.pageCount else { return out }
            page += 1
        }
    }

    func get<T: Decodable>(_ path: String) async throws -> T {
        try await HevyAPI.decoder.decode(T.self, from: fetch(path, query: []))
    }

    private func fetch(_ path: String, query: [URLQueryItem]) async throws -> Data {
        var components = URLComponents(url: Self.base.appendingPathComponent(path), resolvingAgainstBaseURL: false)!
        if !query.isEmpty { components.queryItems = query }
        var request = URLRequest(url: components.url!)
        request.setValue(apiKey, forHTTPHeaderField: "api-key")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        for attempt in 0 ..< 4 {
            let (data, response) = try await session.data(for: request)
            let status = (response as? HTTPURLResponse)?.statusCode ?? 0
            switch status {
            case 200 ..< 300:
                return data
            case 401, 403:
                throw HevyError.keyRefused
            case 429 where attempt < 3:
                try await Task.sleep(for: .seconds(2 << attempt))
            default:
                throw HevyError.answered(status, String(decoding: data.prefix(300), as: UTF8.self))
            }
        }
        throw HevyError.answered(429, "Hevy is limiting requests; try again in a minute.")
    }

    /// `{"page": 1, "page_count": 3, "<key>": [...]}`, the list under the key
    /// the endpoint names.
    struct Page<T: Decodable>: Decodable {
        static var key: CodingUserInfoKey { CodingUserInfoKey(rawValue: "hevy.page.key")! }

        var pageCount: Int
        var items: [T]

        struct Key: CodingKey {
            var stringValue: String
            var intValue: Int? { nil }
            init(stringValue: String) { self.stringValue = stringValue }
            init?(intValue _: Int) { nil }
        }

        init(from decoder: any Decoder) throws {
            let container = try decoder.container(keyedBy: Key.self)
            // Snake case is converted before keys are matched.
            pageCount = try container.decodeIfPresent(Int.self, forKey: Key(stringValue: "pageCount")) ?? 1
            let name = (decoder.userInfo[Self.key] as? String) ?? ""
            let camel = name.split(separator: "_").enumerated().map { $0 == 0 ? String($1) : $1.capitalized }.joined()
            items = try container.decodeIfPresent([T].self, forKey: Key(stringValue: camel)) ?? []
        }
    }
}

public enum HevyError: LocalizedError, Equatable, Sendable {
    case keyRefused
    case answered(Int, String)

    public var errorDescription: String? {
        switch self {
        case .keyRefused: "Hevy refused the API key. Copy it again from hevy.com → Settings → Developer (it needs Hevy Pro)."
        case let .answered(status, text): "Hevy answered \(status): \(text)"
        }
    }
}
