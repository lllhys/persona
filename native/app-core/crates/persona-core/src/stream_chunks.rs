//! Pure projection of Apple's `StreamChunks.split(_:)` and its `MarkdownFence` dependency.
//!
//! This does not repair Markdown, calculate visual spacing, schedule rendering or establish streaming performance.
//! The independent expected objects live in `spec/fixtures/stream-chunks/split.json`; none are generated here.

use crate::apple_text::trim_whitespaces;
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Deserialize)]
pub struct Input {
    pub text: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct Chunks {
    pub done: Vec<String>,
    pub tail: String,
}

#[derive(Debug, Clone, Copy)]
struct Fence {
    character: char,
    count: usize,
}

impl Fence {
    /// Follows `MarkdownFence.opening`; `StreamChunks` has already trimmed the candidate line.
    fn opening(line: &str) -> Option<Self> {
        let character = line.chars().next()?;
        if !matches!(character, '`' | '~') {
            return None;
        }
        let count = line.chars().take_while(|next| *next == character).count();
        if count < 3 {
            return None;
        }
        let suffix = &line[line.char_indices().nth(count).map_or(line.len(), |(offset, _)| offset)..];
        if character == '`' && suffix.contains('`') {
            return None;
        }
        Some(Self { character, count })
    }

    /// Follows `MarkdownFence.isClosed(by:)`, including its literal space/tab suffix rule.
    fn is_closed_by(self, line: &str) -> bool {
        let count = line.chars().take_while(|next| *next == self.character).count();
        if count < self.count {
            return false;
        }
        line.chars()
            .skip(count)
            .all(|character| matches!(character, ' ' | '\t'))
    }
}

/// Cuts finished blocks exactly where the current Apple splitter does, without normalizing original text.
pub fn split(input: &Input) -> Chunks {
    let lines: Vec<&str> = input.text.split('\n').collect();
    let mut done = Vec::new();
    let mut start = 0;
    let mut fence: Option<Fence> = None;
    for (index, line) in lines.iter().enumerate() {
        let trimmed = trim_whitespaces(line);
        if let Some(open) = fence {
            if open.is_closed_by(trimmed) {
                fence = None;
            }
            continue;
        }
        if let Some(open) = Fence::opening(trimmed) {
            fence = Some(open);
            continue;
        }
        if !trimmed.is_empty() || index <= start || index + 1 >= lines.len() {
            continue;
        }
        let next = lines[index + 1];
        if next.is_empty() || next.starts_with([' ', '\t']) {
            continue;
        }
        done.push(lines[start..index].join("\n"));
        start = index + 1;
    }
    Chunks {
        done,
        tail: lines[start..].join("\n"),
    }
}

#[cfg(test)]
mod tests {
    use super::Fence;

    #[test]
    fn fence_tables_match_the_direct_apple_assertions() {
        for (line, expected) in [
            ("```swift", Some(('`', 3))),
            ("````md", Some(('`', 4))),
            ("~~~", Some(('~', 3))),
            ("``code``", None),
            ("```code```", None),
            ("~~~ a`b", Some(('~', 3))),
            ("text", None),
        ] {
            let actual = Fence::opening(line).map(|fence| (fence.character, fence.count));
            assert_eq!(actual, expected, "opening {line:?}");
        }
        let four = Fence::opening("````md").unwrap();
        for (line, expected) in [
            ("```", false),
            ("```html", false),
            ("~~~~", false),
            ("````", true),
            ("`````", true),
        ] {
            assert_eq!(four.is_closed_by(line), expected, "four-backtick closer {line:?}");
        }
        let three = Fence::opening("```").unwrap();
        for (line, expected) in [("```js", false), ("```", true), ("```  ", true)] {
            assert_eq!(three.is_closed_by(line), expected, "three-backtick closer {line:?}");
        }
    }
}
