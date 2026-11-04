import Foundation
import SubstrateDevice
import SubstrateTesting
import Testing

#if canImport(Network)
import SubstrateDeviceApple
#endif

@Suite("Device services", .timeLimit(.minutes(10)))
struct DeviceServiceTests {
    @Test func scriptedConnectivityAnswersNowThenEachChange() async {
        let connectivity = ScriptedConnectivity()
        var paths = connectivity.paths().makeAsyncIterator()
        #expect(await paths.next() == NetworkPath(online: true))
        connectivity.set(NetworkPath(online: true, expensive: true))
        #expect(await paths.next() == NetworkPath(online: true, expensive: true))
        connectivity.set(.offline)
        #expect(await paths.next() == .offline)
        #expect(await connectivity.current() == .offline)
    }

    @Test func temporaryFoldersAreTwoAndMadeOnDemand() throws {
        let folders = TemporaryFolders()
        #expect(try folders.data() != folders.cache())
        #expect(FileManager.default.fileExists(atPath: try folders.data().path))
    }

    #if canImport(Network)
    @Test func thePathMonitorAnswers() async {
        // Whatever the machine's network, it answers once and stops.
        _ = await PathMonitor().current()
    }
    #endif

    #if canImport(Darwin)
    @Test func theApplicationsDataFolderIsKeptOutOfBackups() throws {
        let folders = ApplicationFolders(name: "SubstrateKitTests-\(UUID().uuidString)")
        let data = try folders.data()
        defer { try? FileManager.default.removeItem(at: data) }
        #expect(try data.resourceValues(forKeys: [.isExcludedFromBackupKey]).isExcludedFromBackup == true)
        try? FileManager.default.removeItem(at: folders.cache())
    }
    #endif
}
