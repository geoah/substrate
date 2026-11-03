/// Where the catalogue comes from.
public enum CatalogOrigin: Codable, Hashable, Sendable {
    case bundled
    case server(String)
}
