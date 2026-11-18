import Foundation
import Testing
@testable import Persona

struct CacheBudgetTests {
    /// A folder with `count` files of `kb` KiB each, the first the oldest.
    private func folder(count: Int, kb: Int) throws -> (URL, [URL]) {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        var files: [URL] = []
        for i in 0..<count {
            let f = dir.appending(path: "f\(i)")
            try Data(repeating: UInt8(i), count: kb << 10).write(to: f)
            let date = Date(timeIntervalSince1970: 1_000_000 + Double(i) * 60)
            try FileManager.default.setAttributes([.modificationDate: date], ofItemAtPath: f.path)
            files.append(f)
        }
        return (dir, files)
    }

    private func left(_ files: [URL]) -> [Int] {
        files.indices.filter { FileManager.default.fileExists(atPath: files[$0].path) }
    }

    @Test func underTheLimitNothingIsRemoved() throws {
        let (dir, files) = try folder(count: 4, kb: 64)
        defer { try? FileManager.default.removeItem(at: dir) }
        let total = StorageUsage.size(of: dir)
        #expect(CacheBudget.trim(dir, limit: total, target: 0) == 0)
        #expect(left(files) == [0, 1, 2, 3])
    }

    @Test func overTheLimitTheOldestGoUntilTheTargetIsMet() throws {
        let (dir, files) = try folder(count: 10, kb: 64)
        defer { try? FileManager.default.removeItem(at: dir) }
        let one = StorageUsage.size(of: files[0])
        let total = StorageUsage.size(of: dir)
        let removed = CacheBudget.trim(dir, limit: total - 1, target: one * 6)
        #expect(left(files) == [4, 5, 6, 7, 8, 9]) // the four oldest went, the newest six fit the target
        #expect(removed == one * 4)
        #expect(StorageUsage.size(of: dir) <= one * 6)
    }

    @Test func aMissingFolderIsNotAnError() {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        #expect(CacheBudget.trim(dir, limit: 0, target: 0) == 0)
    }

    @Test func budgetsLeaveRoomBetweenLimitAndTarget() {
        for f in CacheBudget.folders { #expect(f.target < f.limit) }
        #expect(CacheBudget.webLimit > 0)
    }
}
