import Foundation
import SwiftUI
import Testing
import UIKit
@testable import Persona

/// The composer's @ list (#2319): the token before the caret, what a pick leaves in the draft, and the rows in the
/// web's order (web/src/lib/mentions.test.ts covers the same rules).
@MainActor
struct MentionMenuTests {
    private func session(_ id: String, title: String, cwd: String = "/Users/me/code/app", agent: String = "claude",
                         assistant: Bool = false, updated: TimeInterval, last: String = "") -> Session {
        let json = """
        {"machine_id":"m","session_id":"\(id)","title":"\(title)","cwd":"\(cwd)","status":"idle","state":"idle",
         "agent":"\(agent)","assistant":\(assistant),"last_text":"\(last)",
         "updated_at":"\(ISO8601DateFormatter().string(from: Date(timeIntervalSince1970: updated)))"}
        """
        do { return try JSON.decoder.decode(Session.self, from: Data(json.utf8)) } catch { fatalError("bad fixture: \(error)") }
    }

    private func tool(_ seq: Int, _ input: [String: JSONValue]) -> Item {
        Item(id: "\(seq)#0", seq: seq, ts: Date(timeIntervalSince1970: TimeInterval(seq)), role: "assistant", kind: .toolUse,
             text: nil, toolUseID: "t\(seq)", toolName: "Read", toolInput: .object(input), isError: nil, image: nil)
    }

    private func entry(_ name: String, dir: Bool = false) -> FileEntry { FileEntry(name: name, dir: dir, size: 0) }

    // MARK: - The token

    @Test func aTokenStartsAtTheStartOrAfterWhitespace() {
        #expect(MentionToken.at("@", caret: 1) == MentionToken(start: 0, end: 1, query: ""))
        #expect(MentionToken.at("look at @src", caret: 12) == MentionToken(start: 8, end: 12, query: "src"))
        #expect(MentionToken.at("first line\n@Read", caret: 16)?.query == "Read")
        #expect(MentionToken.at("看一下 @文件", caret: 7)?.query == "文件")
        // An e-mail address, a second @, or a space after the word: no list.
        #expect(MentionToken.at("me@example", caret: 10) == nil)
        #expect(MentionToken.at("@a@b", caret: 4) == nil)
        #expect(MentionToken.at("@src ", caret: 5) == nil)
        #expect(MentionToken.at("no mention", caret: 10) == nil)
    }

    @Test func theTokenIsWhereTheCaretIs() {
        // The caret right after the query, with more text after it.
        #expect(MentionToken.at("@src and more", caret: 4) == MentionToken(start: 0, end: 4, query: "src"))
        // Within the word: only what is before the caret is the query.
        #expect(MentionToken.at("@src", caret: 2)?.query == "s")
        // Offsets count UTF-16: an emoji before the token takes two.
        #expect(MentionToken.at("👋 @a", caret: 5) == MentionToken(start: 3, end: 5, query: "a"))
        // An offset inside the emoji is no caret, and one past the end is none either.
        #expect(MentionToken.at("👋", caret: 1) == nil)
        #expect(MentionToken.at("@a", caret: 9) == nil)
    }

    @Test func aSelectedRangeIsNotTyping() {
        let text = "see @src"
        #expect(MentionToken.at(text, selection: .cursor(utf16: 8))?.query == "src")
        #expect(MentionToken.at(text, selection: nil)?.query == "src") // none reported yet: the end
        let range = TextSelection(range: text.index(text.startIndex, offsetBy: 4)..<text.endIndex)
        #expect(MentionToken.at(text, selection: range) == nil)
    }

    @Test func markedTextHoldsTheList() {
        let view = UITextView(frame: CGRect(x: 0, y: 0, width: 300, height: 44))
        view.insertText("@")
        #expect(!TypedText.composing(view))
        view.setMarkedText("wen", selectedRange: NSRange(location: 3, length: 0))
        #expect(TypedText.composing(view))
        view.unmarkText()
        #expect(!TypedText.composing(view))
        #expect(!TypedText.composing(nil))
    }

    // MARK: - What a pick leaves

    @Test func aPickReplacesTheToken() {
        let token = MentionToken(start: 4, end: 9, query: "Prov")
        let file = token.replacing(in: "see @Prov now", with: "@WeatherWidget/Provider.swift ")
        #expect(file.text == "see @WeatherWidget/Provider.swift now") // the space after the token is not doubled
        #expect(file.caret == 34)
        let folder = token.replacing(in: "see @Prov", with: "@Sources/")
        #expect(folder.text == "see @Sources/")
        #expect(folder.caret == folder.text.utf16.count)
        // A session: the token goes, the quote carries it.
        let session = token.replacing(in: "see @Prov", with: "")
        #expect(session.text == "see ")
        #expect(session.caret == 4)
    }

    @Test func thePlusMenuTypesAnAtThatOpensTheList() {
        let empty = MentionToken.typing(in: "", caret: 0)
        #expect(empty.text == "@")
        #expect(MentionToken.at(empty.text, caret: empty.caret)?.query == "")
        let after = MentionToken.typing(in: "look at", caret: 7)
        #expect(after.text == "look at @")
        #expect(after.caret == 9)
        #expect(MentionToken.typing(in: "look ", caret: 5).text == "look @")
        let middle = MentionToken.typing(in: "foobar", caret: 3)
        #expect(middle.text == "foo @bar")
        #expect(MentionToken.at(middle.text, caret: middle.caret)?.query == "")
    }

    // MARK: - The rows

    @Test func theChatsFilesNewestFirstOnce() {
        let items = [tool(1, ["file_path": .string("/a/old.swift")]), tool(2, ["notebook_path": .string("/a/n.ipynb")]),
                     tool(3, ["file_path": .string("/a/old.swift")]), tool(4, ["pattern": .string("x")])]
        #expect(MentionRows.usedPaths(items) == ["/a/old.swift", "/a/n.ipynb"])
        let many = (1...20).map { tool($0, ["file_path": .string("/a/f\($0)")]) }
        #expect(MentionRows.usedPaths(many).count == 12)
        #expect(MentionRows.usedPaths(many).first == "/a/f20")
        // The prompt bar's file rows read the same paths.
        #expect(PromptItems.files(items, cwd: "/a").map(\.insert) == ["@old.swift ", "@n.ipynb "])
    }

    @Test func aSlashBrowsesThatFolder() {
        let cwd = "/Users/me/code/app/"
        #expect(MentionRows.browseTarget("src", cwd: cwd) == nil)
        let rel = MentionRows.browseTarget("src/Vi", cwd: cwd)
        #expect(rel?.dir == "/Users/me/code/app/src")
        #expect(rel?.typed == "src/")
        #expect(rel?.filter == "Vi")
        #expect(MentionRows.browseTarget("/", cwd: cwd)?.dir == "/")
        #expect(MentionRows.browseTarget("/etc/ho", cwd: cwd)?.dir == "/etc")
        #expect(MentionRows.browseTarget("~/", cwd: nil)?.dir == "~")
        #expect(MentionRows.browseTarget("~/code/", cwd: nil)?.dir == "~/code")
        // A relative path with no folder to start from (a new conversation): nothing to list.
        #expect(MentionRows.browseTarget("src/", cwd: nil) == nil)
        #expect(MentionRows.folder(for: "", cwd: cwd) == cwd)
        #expect(MentionRows.folder(for: "src/", cwd: cwd) == "/Users/me/code/app/src")
        #expect(MentionRows.folder(for: "", cwd: "") == nil)
    }

    @Test func filesThenTheFolderThenSessions() {
        let cwd = "/Users/me/code/app"
        let used = ["/Users/me/code/app/Sources/Store.swift", "/Users/me/code/app/README.md", "/tmp/out.log"]
        let listing = [entry("README.md"), entry(".gitignore"), entry("Sources", dir: true), entry("Package.swift"), entry("docs", dir: true)]
        let sessions = [session("s1", title: "Blog", cwd: "/Users/me/code/blog", updated: 5),
                        session("self", title: "This chat", updated: 9),
                        session("pa", title: "Persona", assistant: true, updated: 8),
                        session("s2", title: "Orders", cwd: "/home/me/shop", agent: "codex", updated: 7)]
        let rows = MentionRows.rows(query: "", cwd: cwd, used: used, listing: listing, sessions: sessions,
                                    except: SessionKey(machineID: "m", sessionID: "self"))
        #expect(rows.map(\.name) == ["Store.swift", "README.md", "out.log", "Sources/", "docs/", "Package.swift", "Orders", "Blog"])
        let usedNote = String(localized: "Used in this session"), folderNote = String(localized: "Folder"), topNote = String(localized: "Session folder")
        #expect(rows.map(\.detail) == ["Sources", usedNote, "/tmp", folderNote, folderNote, topNote, "Codex · shop", "Claude · blog"])
        #expect(rows[0].kind == .file(insert: "@Sources/Store.swift ", folder: false))
        #expect(rows[2].kind == .file(insert: "@/tmp/out.log ", folder: false))
        #expect(rows[3].kind == .file(insert: "@Sources/", folder: true))
        #expect(rows[5].kind == .file(insert: "@Package.swift ", folder: false))
        #expect(rows[6].kind == .session(SessionKey(machineID: "m", sessionID: "s2")))
    }

    @Test func typingFilters() {
        let cwd = "/Users/me/code/app"
        let listing = [entry("README.md"), entry("Sources", dir: true), entry("docs", dir: true), entry("resources", dir: true)]
        let sessions = [session("s1", title: "Blog", cwd: "/Users/me/code/blog", updated: 5),
                        session("s2", title: "Orders", cwd: "/home/me/shop", agent: "codex", updated: 7)]
        // Names that start with the query first.
        #expect(MentionRows.rows(query: "so", cwd: cwd, used: [], listing: listing, sessions: [], except: nil).map(\.name)
                == ["Sources/", "resources/"])
        // A session matches by its title, or by its agent and folder.
        #expect(MentionRows.rows(query: "BLOG", cwd: cwd, used: [], listing: listing, sessions: sessions, except: nil).map(\.name) == ["Blog"])
        #expect(MentionRows.rows(query: "codex", cwd: cwd, used: [], listing: listing, sessions: sessions, except: nil).map(\.name) == ["Orders"])
        // Hidden files only when asked for.
        let hidden = [entry(".env"), entry("env.sample")]
        #expect(MentionRows.rows(query: "env", cwd: cwd, used: [], listing: hidden, sessions: [], except: nil).map(\.name) == ["env.sample"])
        #expect(MentionRows.rows(query: ".e", cwd: cwd, used: [], listing: hidden, sessions: [], except: nil).map(\.name) == [".env"])
        // A file the chat used is not listed again among the folder's.
        #expect(MentionRows.rows(query: "read", cwd: cwd, used: [cwd + "/README.md"], listing: listing, sessions: [], except: nil).map(\.id)
                == ["used:/Users/me/code/app/README.md"])
    }

    @Test func browsingListsTheFolderAlone() {
        let listing = [entry("main.swift"), entry("App", dir: true), entry("Models", dir: true), entry("app.json")]
        let sessions = [session("s1", title: "App", updated: 5)]
        let rows = MentionRows.rows(query: "Sources/", cwd: "/Users/me/code/app", used: ["/Users/me/code/app/x.swift"],
                                    listing: listing, sessions: sessions, except: nil)
        #expect(rows.map(\.name) == ["App/", "Models/", "main.swift", "app.json"])
        #expect(rows.allSatisfy { $0.detail == "Sources/" })
        #expect(rows[0].kind == .file(insert: "@Sources/App/", folder: true))
        #expect(rows[2].kind == .file(insert: "@Sources/main.swift ", folder: false))
        let filtered = MentionRows.rows(query: "Sources/ap", cwd: "/Users/me/code/app", used: [], listing: listing, sessions: [], except: nil)
        #expect(filtered.map(\.name) == ["App/", "app.json"])
        // Not read yet: no rows (the list says it is reading).
        #expect(MentionRows.rows(query: "Sources/", cwd: "/Users/me/code/app", used: [], listing: nil, sessions: sessions, except: nil).isEmpty)
    }

    @Test func atMostEightSessionsAndFortyRows() {
        let sessions = (1...12).map { session("s\($0)", title: "Task \($0)", updated: TimeInterval($0)) }
        let rows = MentionRows.rows(query: "", cwd: nil, used: [], listing: nil, sessions: sessions, except: nil)
        #expect(rows.count == 8)
        #expect(rows.first?.name == "Task 12")
        let big = (1...60).map { entry("f\($0)") }
        #expect(MentionRows.rows(query: "f/", cwd: "/a", used: [], listing: big, sessions: [], except: nil).count == 40)
    }

    @Test func aPickedSessionIsQuoted() {
        let s = session("s1", title: "Blog", cwd: "/Users/me/code/blog", updated: 5, last: "Done.")
        let ref = PromptItems.quote(s)
        #expect(ref.title == "Blog")
        #expect(ref.url == "persona://session/m/s1")
        #expect(ref.xml.hasPrefix("<persona-context kind=\"context\" title=\"Blog\" url=\"persona://session/m/s1\""))
        #expect(ref.text.contains("/Users/me/code/blog"))
        // The prompt bar's pick quotes the same.
        let source = PromptItems.sessions([s], except: nil)[0]
        #expect(PromptItems.quote(source, in: [s])?.text == ref.text)
    }
}
