import Foundation

/// Regular expression matches that give up at a deadline (#9108). `NSRegularExpression` has no time limit of its own, so
/// a pattern that backtracks on some text keeps its thread at full CPU for as long as that takes. A chat row's path
/// detection (`PathRefs` before #8911) did: in the foreground that is one busy thread, but an app kept running in the
/// background by a recording is killed by iOS for using too much CPU there, and the meeting recording ends with it.
///
/// With `.reportProgress` ICU's matcher calls back now and then during a long match, and the enumeration can be stopped
/// there; what was found before the deadline is kept.
public nonisolated enum BoundedRegex {
    /// How long the matches in one text may take, all patterns together. Ordinary text takes well under a millisecond.
    public static let budget: Duration = .milliseconds(200)

    /// When matching that starts now has to stop.
    public static func deadline(after budget: Duration = budget) -> ContinuousClock.Instant { .now + budget }

    /// The matches of `re` in `range` of `text` (all of it when nil), in order, as far as they got by `deadline`;
    /// `finished` is false when the deadline stopped them.
    public static func matches(_ re: NSRegularExpression, in text: String, range: NSRange? = nil,
                               until deadline: ContinuousClock.Instant) -> (matches: [NSTextCheckingResult], finished: Bool) {
        var found: [NSTextCheckingResult] = []
        var finished = true
        guard ContinuousClock.now < deadline else { return ([], false) }
        let all = range ?? NSRange(location: 0, length: (text as NSString).length)
        re.enumerateMatches(in: text, options: .reportProgress, range: all) { match, _, stop in
            if let match { found.append(match) }
            if ContinuousClock.now >= deadline {
                finished = false
                stop.pointee = true
            }
        }
        return (found, finished)
    }

    /// The first match of `re` in `text`, unless the deadline came first.
    public static func firstMatch(_ re: NSRegularExpression, in text: String, range: NSRange? = nil,
                                  until deadline: ContinuousClock.Instant) -> NSTextCheckingResult? {
        var first: NSTextCheckingResult?
        guard ContinuousClock.now < deadline else { return nil }
        let all = range ?? NSRange(location: 0, length: (text as NSString).length)
        re.enumerateMatches(in: text, options: .reportProgress, range: all) { match, _, stop in
            if let match {
                first = match
                stop.pointee = true
            } else if ContinuousClock.now >= deadline {
                stop.pointee = true
            }
        }
        return first
    }
}
