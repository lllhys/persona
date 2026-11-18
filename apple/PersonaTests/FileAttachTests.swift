import Foundation
import SwiftUI
import Testing
@testable import Persona

struct FileAttachTests {
    @Test func quoting() {
        #expect(PathInsert.quoted("/tmp/会议纪要 v2-20261029-153012.docx") == "'/tmp/会议纪要 v2-20261029-153012.docx'")
        #expect(PathInsert.quoted("/tmp/it's.txt") == "'/tmp/it'\\''s.txt'")
        #expect(PathInsert.quoted("/var/folders/x/T/plain.pdf") == "/var/folders/x/T/plain.pdf")
    }

    /// #8361: what was typed goes exactly as typed; each file follows it as a tag, never in the words.
    @Test func filesFollowTheWordsAsTags() {
        let files = [AttachedFile(kind: "file", path: "/home/me/.persona/uploads/2026-11-15/q3 report.pdf", name: "q3 report.pdf", size: 2_411_724),
                     AttachedFile(kind: "file", path: "/home/me/.persona/uploads/2026-11-15/a&b.xlsx", name: "a&b.xlsx", size: 86)]
        let typed = "帮我对比这几份季度数据，重点看第三季度的支出"
        let message = AttachedFile.message(typed, files: files)
        #expect(message == typed + "\n\n"
            + #"<persona-file kind="file" path="/home/me/.persona/uploads/2026-11-15/q3 report.pdf" name="q3 report.pdf" size="2411724"/>"# + "\n"
            + #"<persona-file kind="file" path="/home/me/.persona/uploads/2026-11-15/a&amp;b.xlsx" name="a&amp;b.xlsx" size="86"/>"#)
        let back = AttachedFile.parse(message)
        #expect(back.rest == typed)
        #expect(back.files == files)
        #expect(UserBubble.display(message) == typed)
    }

    /// Nothing typed: the message says what to look at, the video or the files.
    @Test func standInSaysWhatToLookAt() {
        let video = AttachedFile(path: "/tmp/v.mov", name: "v.mov", size: 1)
        let file = AttachedFile(kind: "file", path: "/tmp/a.pdf", name: "a.pdf", size: 1)
        #expect(AttachedFile.standIn([video]) == "请看视频")
        #expect(AttachedFile.standIn([file]) == "请看附件")
        #expect(AttachedFile.standIn([video, file]) == "请看附件")
        #expect(QueuedDraft.standIns.contains(AttachedFile.standIn([file])))
        #expect(video.playable && AttachedFile(kind: "audio", path: "/r.m4a", name: "r.m4a", size: 1).playable && !file.playable)
    }

    /// Kept for good where the hub knows how; a health conversation's reports keep their own place; an older hub the
    /// temp directory.
    @Test func whereFilesGo() {
        #expect(FileAttachments.destination(nil, features: ["file_upload", "upload_attachment"]) == "attachment")
        #expect(FileAttachments.destination(nil, features: ["file_upload"]) == nil)
        #expect(FileAttachments.destination(nil, features: []) == "attachment") // not in yet: tried, refused by an older hub
        #expect(FileAttachments.refusedDest(APIError(status: 400, message: "unknown dest")))
        #expect(!FileAttachments.refusedDest(APIError(status: 413, message: "too large")))
        #expect(FileAttachments.destination("health", features: ["file_upload", "upload_attachment"]) == "health")
    }

    /// More than four cards fold to three and a row for the rest; four or fewer show whole.
    @Test func bubbleCardsFold() {
        let files = (1...8).map { AttachedFile(kind: "file", path: "/tmp/\($0).pdf", name: "\($0).pdf", size: 1) }
        #expect(AttachedFileCards.shown(Array(files.prefix(4)), all: false).count == 4)
        #expect(AttachedFileCards.shown(Array(files.prefix(5)), all: false).map(\.name) == ["1.pdf", "2.pdf", "3.pdf"])
        #expect(AttachedFileCards.shown(files, all: true).count == 8)
    }

    /// Picked files upload side by side, become cards with the size the machine reports, go with the message, and
    /// a queued message taken back puts at most `most` back.
    @MainActor @Test func filesUploadAndGo() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/machines/m1/files") { req in
            let name = req.query["name"] ?? "x"
            return Data(#"{"path":"/tmp/\#(name)","name":"\#(name)","size":7}"#.utf8)
        }
        let model = AppModel()
        model.apiForTests = hub.client
        let dir = FileManager.default.temporaryDirectory.appending(path: "file-attach-test-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: dir) }
        let urls = try ["a.pdf", "b.xlsx", "c.zip", "d.docx"].map { name in
            let url = dir.appending(path: name)
            try Data("1234567".utf8).write(to: url)
            return url
        }
        let files = FileAttachments()
        files.start(urls, machine: MachineRef(group: "", id: "m1"), model: model)
        #expect(files.items.count == 4)
        #expect(files.items.filter { $0.step == .uploading }.count == FileAttachments.together)
        for _ in 0..<200 where files.busy { try await Task.sleep(for: .milliseconds(20)) }
        #expect(!files.busy)
        #expect(files.ready.map(\.name) == ["a.pdf", "b.xlsx", "c.zip", "d.docx"])
        #expect(files.ready.allSatisfy { $0.kind == "file" && $0.size == 7 && $0.path.hasPrefix("/tmp/") })
        #expect(hub.sent("POST", "/api/v1/machines/m1/files").count == 4)
        #expect(FileManager.default.fileExists(atPath: urls[0].path)) // the picked file is the user's: never deleted
        files.sent()
        #expect(files.items.isEmpty)
        let back = (1...12).map { AttachedFile(kind: "file", path: "/tmp/\($0)", name: "\($0)", size: 1) }
        let left = files.restore(back)
        #expect(files.items.count == FileAttachments.most && left.count == 2 && files.ready.count == FileAttachments.most)
    }

    @Test func selectionFromAnEarlierText() {
        let long = "a much longer draft than the one now"
        let sel = TextSelection(insertionPoint: long.endIndex)
        #expect(PathInsert.range(sel, in: "short") == 5..<5)
        #expect(PathInsert.range(sel, in: "") == 0..<0)
        #expect(PathInsert.range(nil, in: "x") == nil)
        let mid = "比较和的区别"
        #expect(PathInsert.range(TextSelection(insertionPoint: mid.index(mid.startIndex, offsetBy: 2)), in: mid) == 2..<2)
    }

    @Test func messages() {
        let big = APIError(status: 413, message: "文件超过 200 MB", fields: ["code": "too_large"])
        #expect(FileUploads.message(big, name: "a.zip") == "「a.zip」超过 200 MB，发不了")
        let old = APIError(status: 409, message: "MacBook 上的 personad 太旧，不能接收文件，需要重新部署", fields: ["code": "node_outdated"])
        #expect(FileUploads.message(old, name: "a.zip") == old.message)
        #expect(FileUploads.message(APIError(status: 404, message: "404 page not found"), name: "a.zip")?.contains("hub 版本太旧") == true)
        #expect(FileUploads.message(URLError(.cancelled), name: "a.zip") == nil)
        #expect(FileUploads.message(CancellationError(), name: "a.zip") == nil)
    }

    @Test func stagesACopy() async throws {
        let src = FileManager.default.temporaryDirectory.appending(path: "stage-src-\(UUID().uuidString).txt")
        try Data("hello".utf8).write(to: src)
        defer { try? FileManager.default.removeItem(at: src) }
        let copy = try await FileUploads.stage(src)
        defer { try? FileManager.default.removeItem(at: copy.deletingLastPathComponent()) }
        #expect(copy != src)
        #expect(copy.lastPathComponent == src.lastPathComponent)
        #expect(try Data(contentsOf: copy) == Data("hello".utf8))
    }
}
