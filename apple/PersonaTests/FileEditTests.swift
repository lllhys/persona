import Foundation
import Testing
@testable import Persona

/// Editing a text file (#5829): what can be edited, line endings and the byte order mark kept, the colors' line starts
/// and UTF-16 offsets, the indentation the Tab key inserts, and the save: over the version opened only, a copy beside it.
@MainActor
struct FileEditTests {
    @Test func whatCanBeEdited() {
        #expect(FileEditLimit.check(text: "a", size: 1, truncated: false, next: 0) == .editable)
        #expect(FileEditLimit.check(text: nil, size: 1, truncated: false, next: 0) == .notText)
        #expect(FileEditLimit.check(text: "a", size: FileEditLimit.maxBytes + 1, truncated: false, next: 0) == .tooLarge)
        #expect(FileEditLimit.check(text: "a", size: 10, truncated: true, next: 0) == .partial)
        #expect(FileEditLimit.check(text: "a", size: 10, truncated: false, next: 5) == .partial)
        #expect(FileEditLimit.editable.reason == nil && FileEditLimit.tooLarge.reason != nil)
    }

    @Test func lineEndingsAndMarkGoBackAsTheyWere() {
        let crlf = FileEditText(file: "\u{FEFF}a\r\nb\r\n")
        #expect(crlf.bom && crlf.crlf && crlf.body == "a\nb\n")
        #expect(crlf.file("a\nb\nc\n") == "\u{FEFF}a\r\nb\r\nc\r\n")
        let lf = FileEditText(file: "a\nb")
        #expect(!lf.bom && !lf.crlf && lf.file("x\ny") == "x\ny")
        // Mixed: left as it is, CRs and all.
        let mixed = FileEditText(file: "a\r\nb\nc")
        #expect(!mixed.crlf && mixed.body == "a\r\nb\nc" && mixed.file(mixed.body) == "a\r\nb\nc")
        #expect(FileEditText.sha256("hello") == "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824")
    }

    @Test func coloringKnowsWhereLinesStartInUTF16() {
        let text = "let a = \"é\"\n// 注释\nlet 😀 = 1"
        let c = EditorColoring.make(text, language: SyntaxLanguage.detect(name: "a.swift", firstLine: nil))
        #expect(c.lineCount == 3)
        let ns = text as NSString
        #expect(c.starts[1] == ns.range(of: "//").location)
        #expect(c.starts[2] == ns.range(of: "let 😀").location)
        #expect(c.line(at: 0) == 0 && c.line(at: c.starts[1]) == 1 && c.line(at: ns.length - 1) == 2)
        // The string on line 1 ends where its closing quote is, in UTF-16.
        let string = c.tokens[0].first { $0.kind == .string }
        #expect(string.map { ns.substring(with: NSRange(location: $0.start, length: $0.length)) } == "\"é\"")
        // The comment covers the whole second line (CJK is one UTF-16 unit, three UTF-8 bytes).
        let comment = c.tokens[1].first { $0.kind == .comment }
        #expect(comment.map { ns.substring(with: NSRange(location: c.starts[1] + $0.start, length: $0.length)) } == "// 注释")
        // A final newline makes an empty last line.
        #expect(EditorColoring.make("a\n", language: nil).lineCount == 2)
        #expect(EditorColoring.make("", language: nil).lineCount == 1)
    }

    @Test func tabInsertsTheFilesIndentation() {
        #expect(CodeTextView.indentUnit("a\n\tb\n\tc\n  d") == "\t")
        #expect(CodeTextView.indentUnit("a\n  b\n    c") == "  ")
        #expect(CodeTextView.indentUnit("a\n    b\n        c") == "    ")
        #expect(CodeTextView.indentUnit("one line") == "    ")
    }

    private func editModel(_ hub: FakeHub, text: String = "a\r\nb\r\n", revision: String? = "r1") -> FileEditModel {
        let file = FileContent(path: "/w/a.txt", name: "a.txt", size: Int64(text.utf8.count), mime: "text/plain", text: text, data: nil,
                               truncated: false, isDir: false, revision: revision)
        return FileEditModel(ref: FileRef(machineID: "m1", path: "/w/a.txt", cwd: "/w"), file: file, language: nil, api: hub.client)
    }

    private func body(_ req: LocalRequest) throws -> [String: Any] {
        try #require(JSONSerialization.jsonObject(with: req.body ?? Data()) as? [String: Any])
    }

    @Test func savesOverTheVersionOpenedWithItsLineEndings() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/machines/m1/files/write", json: #"{"path":"/w/a.txt","size":7,"revision":"r2"}"#)
        let edit = editModel(hub)
        edit.dirty = true
        #expect(await edit.save("a\nb\nc\n") == .saved)
        #expect(!edit.dirty)
        let sent = try body(try #require(hub.sent("POST", "/api/v1/machines/m1/files/write").first))
        #expect(sent["text"] as? String == "a\r\nb\r\nc\r\n")
        #expect(sent["revision"] as? String == "r1")
        #expect(sent["sha256"] as? String == FileEditText.sha256("a\r\nb\r\n"))
        #expect(sent["force"] == nil && sent["create"] == nil)
        // The next save checks against the version just written.
        _ = await edit.save("x\n")
        let again = try body(try #require(hub.sent("POST", "/api/v1/machines/m1/files/write").last))
        #expect(again["revision"] as? String == "r2")
    }

    @Test func aChangedFileIsNotOverwrittenUnlessAsked() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/machines/m1/files/write") { req in
            let b = (try? JSONSerialization.jsonObject(with: req.body ?? Data()) as? [String: Any]) ?? [:]
            if b["force"] as? Bool == true { return Data(#"{"path":"/w/a.txt","size":1,"revision":"r9"}"#.utf8) }
            throw APIError(status: 409, message: "changed", fields: ["code": "file_changed", "revision": "r5", "mtime": "2026-11-12T10:00:00.5Z"])
        }
        let edit = editModel(hub)
        guard case .changed(let at) = await edit.save("x") else { Issue.record("not refused"); return }
        #expect(at != nil)
        #expect(await edit.save("x", force: true) == .saved)
        let forced = try body(try #require(hub.sent("POST", "/api/v1/machines/m1/files/write").last))
        #expect(forced["force"] as? Bool == true)
    }

    @Test func aCopyTakesTheFirstFreeName() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/machines/m1/files/write") { req in
            let b = (try? JSONSerialization.jsonObject(with: req.body ?? Data()) as? [String: Any]) ?? [:]
            if (b["path"] as? String)?.hasSuffix("a 2.txt") == true { throw APIError(status: 409, message: "taken", fields: ["code": "exists", "name": "a 2.txt"]) }
            return Data(#"{"path":"/w/a 3.txt","size":1,"revision":"r1"}"#.utf8)
        }
        let edit = editModel(hub)
        let result = await edit.saveCopy("x")
        #expect((try? result.get()) == "a 3.txt")
        let sent = hub.sent("POST", "/api/v1/machines/m1/files/write").compactMap { try? body($0) }
        #expect(sent.map { $0["path"] as? String } == ["/w/a 2.txt", "/w/a 3.txt"])
        #expect(sent.allSatisfy { $0["create"] as? Bool == true })
    }
}
