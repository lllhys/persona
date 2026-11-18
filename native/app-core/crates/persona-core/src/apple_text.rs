//! Character-set compatibility for the current Apple pure projections.
//!
//! Source: `swiftlang/swift-corelibs-foundation`, `CFUniChar.c` (`isWhitespace` and the distinct newline set). This is
//! source evidence, not a claim that these Rust tests executed Apple Foundation.

/// CoreFoundation's whitespace set includes U+2000...U+200B, not Unicode White_Space alone.
pub(crate) fn is_apple_whitespace(character: char) -> bool {
    matches!(
        character,
        ' ' | '\t' | '\u{00a0}' | '\u{1680}' | '\u{2000}'..='\u{200b}' | '\u{202f}' | '\u{205f}' | '\u{3000}'
    )
}

pub(crate) fn trim_whitespaces(text: &str) -> &str {
    text.trim_matches(is_apple_whitespace)
}
