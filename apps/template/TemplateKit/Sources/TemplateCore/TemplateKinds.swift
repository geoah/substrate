/// Every kind name the app reads or writes. Nothing else in the code spells
/// one. The package is published under `substrate.reamde.dev`, so the names
/// are the same in every repository (package/README.md).
public enum TemplateKinds {
    public static let authority = "substrate.reamde.dev"
    public static let packageID = "\(authority)/template"
    public static let item = "\(packageID)/item"
    /// Every kind, as a list read and the watch name them.
    public static let all = [item]
}
