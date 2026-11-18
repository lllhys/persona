import Foundation
import Testing
@testable import Persona

@MainActor
struct FileTextPreviewTests {
    private func hub(_ fields: [String: Any]) throws -> FakeHub {
        let hub = FakeHub()
        let data = try JSONSerialization.data(withJSONObject: ["path": "/tmp/task.output", "name": "task.output",
                                                              "size": 24, "truncated": false, "is_dir": false].merging(fields) { _, new in new })
        hub.on("GET", "/api/v1/machines/test/file") { _ in data }
        return hub
    }

    private func page(_ hub: FakeHub, stream: Bool = false) -> FileViewerModel {
        FileViewerModel(ref: FileRef(machineID: "test", path: "/tmp/task.output", cwd: nil), api: hub.client, stream: stream)
    }

    @Test func omittedEmptyTextDoesNotBecomeBinary() async throws {
        let hub = try hub(["size": 0, "mime": "text/plain; charset=utf-8"])
        let model = page(hub)
        await model.load()
        #expect(model.kind == .empty)
        #expect(model.text == "")
        #expect(model.error == nil)
        #expect(hub.sent("GET", "/api/v1/machines/test/file/download").isEmpty)
    }

    @Test(arguments: ["task.output", "task.log", "task", "task.unknown"])
    func textDoesNotNeedAnExtensionAllowlist(_ name: String) async throws {
        let model = page(try hub(["name": name, "text": "Command output\nAll checks passed.\n", "mime": "text/plain; charset=utf-8"]))
        await model.load()
        #expect(model.kind == .code)
        #expect(model.text == "Command output\nAll checks passed.\n")
        #expect(model.code?.lineCount == 2)
    }

    @Test(arguments: ["task.output", "task.log", "task"])
    func inlineUTF8BytesAreText(_ name: String) async throws {
        let text = "Command output\nAll checks passed.\n"
        let model = page(try hub(["name": name, "mime": "application/octet-stream", "data": Data(text.utf8).base64EncodedString()]))
        await model.load()
        #expect(model.kind == .code)
        #expect(model.text == text)
    }

    @Test func textMIMEWithoutInlinePayloadDownloadsText() async throws {
        let hub = try hub(["mime": "Text/Plain; charset=utf-8", "stream": true])
        hub.on("GET", "/api/v1/machines/test/file/download") { _ in Data("Command output\n".utf8) }
        let model = page(hub, stream: true)
        await model.load()
        #expect(model.kind == .code)
        #expect(model.text == "Command output\n")
        #expect(hub.sent("GET", "/api/v1/machines/test/file/download").count == 1)
    }

    @Test(arguments: ["task.output", "task.log", "task", "task.md", "task.json", "task.pdf"])
    func allEmptyFilesHaveAnEmptyState(_ name: String) async throws {
        let model = page(try hub(["name": name, "size": 0, "text": "", "mime": "application/octet-stream"]))
        await model.load()
        #expect(model.kind == .empty)
        #expect(model.code == nil)
        #expect(model.preview == nil)
    }

    @Test func zeroSizeDirectoryIsStillADirectory() async throws {
        let model = page(try hub(["size": 0, "is_dir": true, "entries": []]))
        await model.load()
        #expect(model.kind == .directory)
    }

    @Test func unknownStreamIsSniffedFromABoundedPreview() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/machines/test/file") { req in
            let fields: [String: Any] = ["path": "/tmp/task.output", "name": "task.output", "size": 10_000_000,
                                        "mime": "application/octet-stream", "truncated": true, "is_dir": false,
                                        "stream": req.query["stream"] == "1"]
            var response = fields
            if req.query["stream"] != "1" {
                response["text"] = "First page\n"
                response["next"] = 11
            }
            return try JSONSerialization.data(withJSONObject: response)
        }
        let model = page(hub, stream: true)
        await model.load()
        #expect(model.kind == .code)
        #expect(model.next == 11)
        #expect(model.text == "First page\n")
        #expect(hub.sent("GET", "/api/v1/machines/test/file/download").isEmpty)
        #expect(hub.sent("GET", "/api/v1/machines/test/file").count == 2)
    }

    @Test func textDownloadFailureIsRecoverable() async throws {
        let hub = try hub(["mime": "text/plain"])
        let model = page(hub)
        await model.load()
        #expect(model.file == nil)
        #expect(model.error != nil)
    }

    @Test func binaryBytesStayUnsupported() async throws {
        let model = page(try hub(["mime": "application/octet-stream", "data": Data([0xff, 0xfe, 0]).base64EncodedString()]))
        await model.load()
        #expect(model.kind == .binary)
        #expect(model.text == nil)
    }

    @Test func unicodeAndANSIAreStillText() async throws {
        let text = "\u{1b}[32m检查通过 · 完了\u{1b}[0m\n"
        let model = page(try hub(["data": Data(text.utf8).base64EncodedString()]))
        await model.load()
        #expect(model.kind == .code)
        #expect(model.text == text)
    }

    @Test func binaryControlBytesDoNotBecomePlainText() async throws {
        let model = page(try hub(["data": Data("PK\u{3}\u{4}".utf8).base64EncodedString()]))
        await model.load()
        #expect(model.kind == .binary)
    }

    @Test func textNamedAsMediaRemainsText() async throws {
        let model = page(try hub(["name": "task.mp4", "mime": "text/plain", "data": Data("Command output".utf8).base64EncodedString()]))
        await model.load()
        #expect(model.kind == .code)
        #expect(model.video == nil)
    }
}
