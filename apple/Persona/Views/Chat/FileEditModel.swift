import CryptoKit
import Foundation

// Editing a text file from the preview (#5829, docs/apple/ios.md "Editing a file"): UTF-8 text up to 1 MB opens in the
// editor; Save writes it back only while the file on the computer is still the version opened (its revision, or the same
// bytes), else the user chooses: save a copy, take the new version, or overwrite it anyway. Line endings (LF or CRLF) and
// a byte order mark are kept as the file had them; the editor works in LF.

/// Whether a file the viewer read can be edited, and why not.
nonisolated enum FileEditLimit: Equatable, Sendable {
    case editable
    case tooLarge
    case notText
    case partial

    /// The editor takes text files up to this (the web's editor too).
    static let maxBytes: Int64 = 1 << 20

    static func check(text: String?, size: Int64, truncated: Bool, next: Int64) -> FileEditLimit {
        guard text != nil else { return .notText }
        if size > maxBytes { return .tooLarge }
        if truncated || next > 0 { return .partial }
        return .editable
    }

    /// Why Edit is off, in the menu's words; nil when it is on.
    var reason: String? {
        switch self {
        case .editable: nil
        case .tooLarge: String(localized: "Too large to edit here (over 1 MB)")
        case .notText: String(localized: "Only text files can be edited")
        case .partial: String(localized: "Open the whole file to edit it")
        }
    }
}

/// The text as the editor shows it and as it goes back to the file.
nonisolated struct FileEditText: Equatable, Sendable {
    /// What the editor holds: LF line endings, no byte order mark.
    var body: String
    var crlf: Bool
    var bom: Bool

    init(file text: String) {
        var t = text
        bom = t.hasPrefix("\u{FEFF}")
        if bom { t.removeFirst() }
        // CRLF only when every line break is one: a mixed file is left as it is, its CRs shown and kept.
        let crlfs = t.components(separatedBy: "\r\n").count - 1
        let lfs = t.components(separatedBy: "\n").count - 1
        crlf = crlfs > 0 && crlfs == lfs
        body = crlf ? t.replacingOccurrences(of: "\r\n", with: "\n") : t
    }

    /// The file's text for an edited body: its line endings and mark put back.
    func file(_ edited: String) -> String {
        (bom ? "\u{FEFF}" : "") + (crlf ? edited.replacingOccurrences(of: "\n", with: "\r\n") : edited)
    }

    /// The hex SHA-256 of a text's UTF-8 (the node's check that a file is still the one opened).
    static func sha256(_ text: String) -> String {
        SHA256.hash(data: Data(text.utf8)).map { String(format: "%02x", $0) }.joined()
    }
}

/// One editing session of a file: what was opened, the save and its conflicts.
@MainActor @Observable
final class FileEditModel {
    enum Outcome: Equatable {
        case saved
        /// The file changed on the computer; nothing was written.
        case changed(at: Date?)
        case failed(String)
    }

    let ref: FileRef
    let name: String
    let language: SyntaxLanguage?
    let original: FileEditText
    /// The text as opened (the file's own), for the hash check.
    @ObservationIgnored private let opened: String
    @ObservationIgnored private var revision: String?
    @ObservationIgnored private let api: APIClient?
    private(set) var saving = false
    var dirty = false

    init(ref: FileRef, file: FileContent, language: SyntaxLanguage?, api: APIClient?) {
        self.ref = ref
        name = file.name
        self.language = language
        opened = file.text ?? ""
        original = FileEditText(file: opened)
        revision = file.revision
        self.api = api
    }

    /// Writes edited (the editor's body) back: only over the version opened unless force.
    func save(_ edited: String, force: Bool = false) async -> Outcome {
        guard let api else { return .failed(String(localized: "Not connected to the hub [App Model 93d955]")) }
        saving = true
        defer { saving = false }
        do {
            let out = try await api.writeFile(machine: ref.machineID, path: ref.path, cwd: ref.cwd ?? "", text: original.file(edited),
                                              revision: revision, sha256: FileEditText.sha256(opened), force: force)
            revision = out.revision
            dirty = false
            return .saved
        } catch let e as APIError where e.code == "file_changed" {
            return .changed(at: Self.date(e.fields["mtime"]))
        } catch {
            return .failed(FolderEdits.message(error))
        }
    }

    /// A time as the hub writes it (RFC 3339, with or without fractions of a second).
    nonisolated static func date(_ s: String?) -> Date? {
        guard let s else { return nil }
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f.date(from: s) ?? ISO8601DateFormatter().date(from: s)
    }

    /// Saves edited beside the file as "name 2.ext" (the first free one); returns its name.
    func saveCopy(_ edited: String) async -> Result<String, Error> {
        guard let api else { return .failure(URLError(.notConnectedToInternet)) }
        saving = true
        defer { saving = false }
        let dir = (ref.path as NSString).deletingLastPathComponent
        let ext = (name as NSString).pathExtension
        let stem = ext.isEmpty ? name : (name as NSString).deletingPathExtension
        for i in 2..<100 {
            let copy = ext.isEmpty ? "\(stem) \(i)" : "\(stem) \(i).\(ext)"
            do {
                _ = try await api.writeFile(machine: ref.machineID, path: (dir as NSString).appendingPathComponent(copy), cwd: ref.cwd ?? "",
                                            text: original.file(edited), create: true)
                dirty = false
                return .success(copy)
            } catch let e as APIError where e.code == "exists" {
                continue
            } catch {
                return .failure(error)
            }
        }
        return .failure(URLError(.cannotCreateFile))
    }
}
