#if os(iOS)
import SwiftUI
import UIKit

// The text editor of a file (#5829, docs/apple/ios.md "Editing a file"): the system's text view, so typing, selection,
// undo, dictation, find and replace (`UIFindInteraction`) and VoiceOver are the system's own, with line numbers beside
// it and the viewer's colors (`SyntaxLexer`, `SyntaxStyle`). TextKit 1: its line fragments give the gutter where each line
// starts. The colors are worked out off the main thread after a pause in typing and put on the lines on screen only.

/// Each line's colors for a text: where the lines start (UTF-16) and their tokens (UTF-16, from the line's start).
nonisolated struct EditorColoring: Sendable {
    struct Token: Sendable {
        var start: Int
        var length: Int
        var kind: SyntaxKind
    }

    /// Where each line starts, and one more: the end plus one.
    var starts: [Int]
    var tokens: [[Token]]

    static let empty = EditorColoring(starts: [0, 1], tokens: [[]])

    var lineCount: Int { starts.count - 1 }

    /// The line a UTF-16 offset is on.
    func line(at offset: Int) -> Int {
        var lo = 0, hi = lineCount - 1
        while lo < hi {
            let mid = (lo + hi + 1) / 2
            if starts[mid] <= offset { lo = mid } else { hi = mid - 1 }
        }
        return lo
    }

    /// Lexes text a line at a time (`SyntaxLexer`, the viewer's colors); with no language only the line starts.
    static func make(_ text: String, language: SyntaxLanguage?) -> EditorColoring {
        let bytes = Array(text.utf8)
        var starts = [0], tokens: [[Token]] = []
        let lexer = language.map(SyntaxLexer.init)
        var state = SyntaxState()
        var raw: [SyntaxToken] = []
        var at16 = 0
        bytes.withUnsafeBufferPointer { b in
            var lineStart = 0
            var i = 0
            while i <= b.count {
                if i == b.count || b[i] == 10 {
                    let end = i
                    // UTF-16 offset of each byte of the line (ASCII: the same)
                    var map: [Int] = []
                    var ascii = true
                    for j in lineStart..<end where b[j] >= 0x80 { ascii = false; break }
                    if !ascii {
                        map.reserveCapacity(end - lineStart + 1)
                        var u = 0
                        for j in lineStart..<end {
                            map.append(u)
                            let c = b[j]
                            if c & 0xC0 != 0x80 { u += c >= 0xF0 ? 2 : 1 }
                        }
                        map.append(u)
                    }
                    var line: [Token] = []
                    if let lexer, end - lineStart <= CodeDocument.maxLineBytes {
                        raw.removeAll(keepingCapacity: true)
                        lexer.line(b, lineStart, end, &state, &raw)
                        line.reserveCapacity(raw.count)
                        for t in raw {
                            let s = Int(t.start), e = Int(t.start + t.length)
                            let a = ascii ? s : map[min(s, map.count - 1)], z = ascii ? e : map[min(e, map.count - 1)]
                            if z > a { line.append(Token(start: a, length: z - a, kind: t.kind)) }
                        }
                    }
                    tokens.append(line)
                    at16 += ascii ? end - lineStart : (map.last ?? 0)
                    at16 += 1 // the newline
                    starts.append(at16)
                    lineStart = end + 1
                }
                i += 1
            }
        }
        return EditorColoring(starts: starts, tokens: tokens)
    }
}

/// What the page reads from the editor (its text when saving) and tells it.
@MainActor final class CodeEditorHandle {
    fileprivate weak var view: CodeTextView?
    var text: String { view?.text ?? "" }
    func dismissKeyboard() { view?.resignFirstResponder() }
}

struct CodeEditorView: UIViewRepresentable {
    let initial: String
    let language: SyntaxLanguage?
    let handle: CodeEditorHandle
    var onChange: () -> Void

    func makeUIView(context: Context) -> CodeTextView {
        let v = CodeTextView(language: language, text: initial)
        v.onChange = onChange
        handle.view = v
        return v
    }

    func updateUIView(_ view: CodeTextView, context: Context) {
        view.onChange = onChange
    }
}

/// The editor: a text view with a gutter of line numbers and colors on the lines in view.
final class CodeTextView: UITextView, UITextViewDelegate {
    var onChange: (() -> Void)?
    private let language: SyntaxLanguage?
    private let gutter = CodeGutterView()
    /// The colors and line starts for the text as of `colored`; `version` counts the edits.
    private var coloring = EditorColoring.empty
    private var version = 0
    private var colored = -1
    private var lexTask: Task<Void, Never>?
    /// Lines whose colors are on: none after an edit until the next coloring.
    private var painted = IndexSet()
    private var indent = "    "

    init(language: SyntaxLanguage?, text: String) {
        self.language = language
        let storage = NSTextStorage()
        let layout = NSLayoutManager()
        let container = NSTextContainer(size: CGSize(width: 0, height: CGFloat.greatestFiniteMagnitude))
        container.widthTracksTextView = true
        layout.addTextContainer(container)
        layout.allowsNonContiguousLayout = true
        storage.addLayoutManager(layout)
        super.init(frame: .zero, textContainer: container)
        font = SyntaxStyle.font
        typingAttributes = SyntaxStyle.plain
        textStorage.setAttributedString(NSAttributedString(string: text, attributes: SyntaxStyle.plain))
        autocorrectionType = .no
        autocapitalizationType = .none
        smartQuotesType = .no
        smartDashesType = .no
        smartInsertDeleteType = .no
        spellCheckingType = .no
        keyboardDismissMode = .interactive
        alwaysBounceVertical = true
        backgroundColor = .clear
        isFindInteractionEnabled = true
        delegate = self
        indent = Self.indentUnit(text)
        addSubview(gutter)
        gutter.editor = self
        inputAccessoryView = makeKeyBar()
        accessibilityIdentifier = "file-editor"
        relex(text)
    }

    @available(*, unavailable) required init?(coder: NSCoder) { nil }

    // MARK: Gutter

    private var gutterWidth: CGFloat {
        let digits = max(3, String(max(coloring.lineCount, 1)).count)
        return ceil(CGFloat(digits) * ("8" as NSString).size(withAttributes: SyntaxStyle.gutter).width) + Theme.Space.l
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        let w = gutterWidth
        if abs(textContainerInset.left - (w + Theme.Space.s)) > 0.5 {
            textContainerInset = UIEdgeInsets(top: Theme.Space.s, left: w + Theme.Space.s, bottom: Theme.Space.l, right: Theme.Space.s)
        }
        // The gutter covers what is in view only (one the content's height would be a huge drawing), redrawn as it scrolls.
        gutter.frame = CGRect(x: contentOffset.x, y: contentOffset.y, width: w, height: bounds.height)
        gutter.setNeedsDisplay()
        paintVisible()
    }

    /// Line number and top of each line starting in view, for the gutter.
    fileprivate func visibleLineStarts() -> [(number: Int, y: CGFloat, height: CGFloat)] {
        guard coloring.lineCount > 0, colored == version else { return [] }
        let rect = CGRect(x: 0, y: contentOffset.y - textContainerInset.top, width: textContainer.size.width, height: bounds.height)
        let glyphs = layoutManager.glyphRange(forBoundingRect: rect, in: textContainer)
        var out: [(Int, CGFloat, CGFloat)] = []
        let text = textStorage.string as NSString
        layoutManager.enumerateLineFragments(forGlyphRange: glyphs) { frag, _, _, range, _ in
            let c = self.layoutManager.characterIndexForGlyph(at: range.location)
            guard c == 0 || (c <= text.length && text.character(at: c - 1) == 10) else { return }
            out.append((self.coloring.line(at: c) + 1, frag.minY + self.textContainerInset.top - self.contentOffset.y, frag.height))
        }
        // An empty last line (after a final newline) has no fragment of its own: the extra one.
        let extra = layoutManager.extraLineFragmentRect
        if !extra.isEmpty, extra.maxY >= rect.minY, extra.minY <= rect.maxY {
            out.append((coloring.lineCount, extra.minY + textContainerInset.top - contentOffset.y, extra.height))
        }
        return out
    }

    // MARK: Colors

    func textViewDidChange(_ textView: UITextView) {
        version += 1
        painted = []
        onChange?()
        relex(text)
    }

    private func relex(_ text: String) {
        lexTask?.cancel()
        let v = version, lang = language
        let wait = v == 0 ? Duration.zero : .milliseconds(180)
        lexTask = Task { [weak self] in
            try? await Task.sleep(for: wait)
            guard !Task.isCancelled else { return }
            let c = await Task.detached(priority: .userInitiated) { EditorColoring.make(text, language: lang) }.value
            guard let self, !Task.isCancelled, v == self.version else { return }
            self.coloring = c
            self.colored = v
            self.painted = []
            self.setNeedsLayout()
            self.gutter.setNeedsDisplay()
        }
    }

    /// Colors the lines in view (and a screen around them) that are not colored yet for this text.
    private func paintVisible() {
        guard language != nil, colored == version, coloring.lineCount > 0 else { return }
        let rect = CGRect(x: 0, y: contentOffset.y - textContainerInset.top - bounds.height, width: textContainer.size.width, height: bounds.height * 3)
        let glyphs = layoutManager.glyphRange(forBoundingRect: rect, in: textContainer)
        let chars = layoutManager.characterRange(forGlyphRange: glyphs, actualGlyphRange: nil)
        let first = coloring.line(at: chars.location), last = coloring.line(at: max(chars.location, NSMaxRange(chars) - 1))
        let todo = IndexSet(integersIn: first...last).subtracting(painted)
        guard !todo.isEmpty else { return }
        let length = textStorage.length
        textStorage.beginEditing()
        for i in todo where i < coloring.lineCount {
            let start = coloring.starts[i], end = min(coloring.starts[i + 1] - 1, length)
            guard end >= start, start <= length else { continue }
            textStorage.setAttributes(SyntaxStyle.plain, range: NSRange(location: start, length: end - start))
            for t in coloring.tokens[i] where start + t.start + t.length <= end {
                textStorage.setAttributes(SyntaxStyle.attributes(t.kind), range: NSRange(location: start + t.start, length: t.length))
            }
        }
        textStorage.endEditing()
        painted.formUnion(todo)
    }

    // MARK: Keyboard bar

    private func makeKeyBar() -> UIView {
        let bar = UIInputView(frame: CGRect(x: 0, y: 0, width: 0, height: Theme.Size.tapTarget), inputViewStyle: .keyboard)
        func button(_ symbol: String, _ label: String, _ id: String, _ action: @escaping () -> Void) -> UIButton {
            var config = UIButton.Configuration.plain()
            config.image = UIImage(systemName: symbol)
            config.baseForegroundColor = .label
            let b = UIButton(configuration: config, primaryAction: UIAction { _ in action() })
            b.accessibilityLabel = label
            b.accessibilityIdentifier = id
            b.widthAnchor.constraint(greaterThanOrEqualToConstant: Theme.Size.tapTarget).isActive = true
            return b
        }
        let find = button(Theme.Icon.search, String(localized: "Find and Replace"), "editor.find") { [weak self] in
            self?.findInteraction?.presentFindNavigator(showingReplace: true)
        }
        let undo = button(Theme.Icon.undo, String(localized: "Undo"), "editor.undo") { [weak self] in self?.undoManager?.undo() }
        let redo = button(Theme.Icon.redo, String(localized: "Redo"), "editor.redo") { [weak self] in self?.undoManager?.redo() }
        let tab = button(Theme.Icon.indent, String(localized: "Indent"), "editor.tab") { [weak self] in
            guard let self else { return }
            self.insertText(self.indent)
        }
        var brackets = UIButton.Configuration.plain()
        brackets.title = "{ }"
        brackets.baseForegroundColor = .label
        let pairs = UIButton(configuration: brackets)
        pairs.menu = UIMenu(children: ["{}", "()", "[]", "<>", "\"\"", "''", "``"].map { pair in
            UIAction(title: pair) { [weak self] _ in
                guard let self else { return }
                self.insertText(pair)
                if let r = self.selectedTextRange, let back = self.position(from: r.start, offset: -1) {
                    self.selectedTextRange = self.textRange(from: back, to: back)
                }
            }
        })
        pairs.showsMenuAsPrimaryAction = true
        pairs.accessibilityLabel = String(localized: "Brackets and Quotes")
        let hide = button(Theme.Icon.dismissKeyboard, String(localized: "Hide Keyboard"), "editor.hide") { [weak self] in self?.resignFirstResponder() }
        let stack = UIStackView(arrangedSubviews: [find, undo, redo, tab, pairs, UIView(), hide])
        stack.axis = .horizontal
        stack.spacing = Theme.Space.xs
        stack.translatesAutoresizingMaskIntoConstraints = false
        bar.addSubview(stack)
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: bar.layoutMarginsGuide.leadingAnchor),
            stack.trailingAnchor.constraint(equalTo: bar.layoutMarginsGuide.trailingAnchor),
            stack.topAnchor.constraint(equalTo: bar.topAnchor),
            stack.bottomAnchor.constraint(equalTo: bar.bottomAnchor),
        ])
        return bar
    }

    /// The file's indentation: a tab when its lines start with tabs more than spaces, else its smallest indent of
    /// spaces (2 or 4), 4 when it has none.
    nonisolated static func indentUnit(_ text: String) -> String {
        var tabs = 0, spaces = 0, smallest = Int.max
        for line in text.split(separator: "\n", omittingEmptySubsequences: true).prefix(2000) {
            if line.first == "\t" { tabs += 1; continue }
            let n = line.prefix { $0 == " " }.count
            if n > 0, n < line.count {
                spaces += 1
                smallest = min(smallest, n)
            }
        }
        if tabs > spaces { return "\t" }
        return String(repeating: " ", count: smallest == Int.max ? 4 : min(max(smallest, 2), 8))
    }
}

/// Line numbers beside the editor, for the lines in view.
final class CodeGutterView: UIView {
    weak var editor: CodeTextView?

    override init(frame: CGRect) {
        super.init(frame: frame)
        isOpaque = false
        isUserInteractionEnabled = false
        contentMode = .redraw
        isAccessibilityElement = false
    }

    @available(*, unavailable) required init?(coder: NSCoder) { nil }

    override func draw(_ rect: CGRect) {
        guard let editor else { return }
        let w = bounds.width - Theme.Space.s
        for line in editor.visibleLineStarts() {
            let s = NSAttributedString(string: String(line.number), attributes: SyntaxStyle.gutter)
            let size = s.size()
            s.draw(at: CGPoint(x: w - size.width, y: line.y + (line.height - size.height) / 2))
        }
    }
}
#endif
