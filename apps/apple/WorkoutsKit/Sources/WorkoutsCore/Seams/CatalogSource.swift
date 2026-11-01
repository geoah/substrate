import Foundation

/// Exercises and routines. V1: the bundled JSON plus `local-exercises.json`
/// (`BundledCatalog`). V2: substrate lists, cached on disk, refreshed from the
/// change feed.
public protocol CatalogSource: Sendable {
    func load() async throws -> Catalog
    /// A new catalogue whenever the source changes. V1 yields only after a
    /// local exercise is added.
    func updates() -> AsyncStream<Catalog>
}
