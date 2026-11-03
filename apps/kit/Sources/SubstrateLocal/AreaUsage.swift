/// What one area of a repository's folder holds on the device, for Storage.
public struct AreaUsage: Codable, Hashable, Sendable {
    public var area: FolderArea
    public var bytes: Int
    public var files: Int

    public init(area: FolderArea, bytes: Int, files: Int) {
        self.area = area
        self.bytes = bytes
        self.files = files
    }
}
