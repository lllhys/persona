import Foundation

/// The text Share › Persona sends (#2282): a web page as its title and link (from Safari, the page's own words through
/// `PersonaShare/SharePage.js`), the selected text quoted after them, then whatever else was shared, each piece once.
/// Pure, so PersonaTests can hold it; the extension (`ShareModel.loadItems`) gathers the pieces.
nonisolated enum ShareText {
    /// A page as Safari's preprocessing script read it (`document.title`, `document.URL`, the selection).
    struct Page: Equatable, Sendable {
        var title: String
        var url: String
        var selection: String = ""
    }

    /// Where Safari puts the object the script handed to `completionFunction`, in the `com.apple.property-list` item.
    static let resultsKey = "NSExtensionJavaScriptPreprocessingResultsKey"
    /// The most of a selection that is sent: a page selected whole stays a link, not a copy of the page.
    static let maxSelection = 4000

    /// The page in the property list Safari hands over; nil when it is not one (another app's plist, a page with no URL).
    static func page(_ item: Any?) -> Page? {
        guard let dict = item as? [String: Any], let results = dict[resultsKey] as? [String: Any] else { return nil }
        let url = clean(results["url"])
        guard !url.isEmpty else { return nil }
        var selection = clean(results["selection"])
        if selection.count > maxSelection { selection = String(selection.prefix(maxSelection)) + "…" }
        return Page(title: clean(results["title"]), url: url, selection: selection)
    }

    /// What is sent: the page ("Title\nURL", then the selection quoted), then every other piece in the order it was shared
    /// (each item's own text, `attributedContentText`, then its links and texts as its attachments come), leaving out any
    /// that repeat what is already there (the same link, the title again). Without a page that is the order Share › Persona
    /// always sent: another app's text before its link stays before it.
    static func compose(page: Page?, pieces: [String] = []) -> String {
        var parts: [String] = []
        var seen: Set<String> = []
        func add(_ s: String) {
            let s = s.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !s.isEmpty, seen.insert(key(s)).inserted else { return }
            parts.append(s)
        }
        if let page {
            let title = page.title.trimmingCharacters(in: .whitespacesAndNewlines)
            var block = key(title) == key(page.url) || title.isEmpty ? page.url : title + "\n" + page.url
            if !page.selection.isEmpty, key(page.selection) != key(title) {
                block += "\n\n" + quoted(page.selection)
            }
            parts.append(block)
            for s in [title, page.url, page.selection] where !s.isEmpty { seen.insert(key(s)) }
        }
        for s in pieces { add(s) }
        return parts.joined(separator: "\n")
    }

    /// `text` as a Markdown quote, each line marked.
    static func quoted(_ text: String) -> String {
        text.split(separator: "\n", omittingEmptySubsequences: false).map { "> " + $0 }.joined(separator: "\n")
    }

    /// The same piece told apart from another only by spaces or a link's final slash counts as the same.
    private static func key(_ s: String) -> String {
        var s = s.trimmingCharacters(in: .whitespacesAndNewlines)
        while s.hasSuffix("/"), s.contains("://") { s.removeLast() }
        return s
    }

    private static func clean(_ value: Any?) -> String {
        (value as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
    }
}
