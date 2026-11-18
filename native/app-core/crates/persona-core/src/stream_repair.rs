//! Bounded fence, table, final-block-marker, inline, math-end and plain-open-fence projection of Apple's
//! `StreamRepair`.
//!
//! MathMarkdown's `display` predicate is included only for end reporting; formula rewriting and rendering are not
//! implemented. The plain-fence projection takes the caller's drawn-info predicate as explicit input.

use serde::{Deserialize, Serialize};
use unicode_general_category::GeneralCategory;
use unicode_segmentation::UnicodeSegmentation;

#[derive(Debug, Clone, Deserialize)]
pub struct Input {
    pub text: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct Projection {
    pub repaired: String,
    pub end: &'static str,
}

#[derive(Debug, Clone, Deserialize)]
pub struct PlainFenceInput {
    pub text: String,
    /// Exact info strings for which the caller draws a picture. This replaces Apple's `drawn` closure.
    pub drawn: Vec<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct PlainFenceProjection {
    pub text: String,
}

#[derive(Debug, Clone)]
struct Fence {
    character: char,
    count: usize,
    line: usize,
}

#[derive(Debug, Clone)]
struct Table {
    header: usize,
    divided: bool,
}

fn trimmed(line: &str) -> &str {
    line.trim_matches([' ', '\t', '\r'])
}

fn opening(line: &str) -> Option<(char, usize)> {
    let character = line.chars().next()?;
    if !matches!(character, '`' | '~') {
        return None;
    }
    let count = line.chars().take_while(|next| *next == character).count();
    if count < 3 || character == '`' && line.chars().skip(count).any(|next| next == '`') {
        return None;
    }
    Some((character, count))
}

fn closes(fence: &Fence, line: &str) -> bool {
    let count = line.chars().take_while(|next| *next == fence.character).count();
    count >= fence.count && line.chars().skip(count).all(|next| matches!(next, ' ' | '\t'))
}

fn without_indent_so_far(text: &str) -> Option<&str> {
    let trimmed = text.trim_end_matches([' ', '\t']);
    if trimmed.len() != text.len() && (trimmed.is_empty() || trimmed.ends_with('\n')) {
        Some(trimmed)
    } else {
        None
    }
}

fn columns(line: &str) -> usize {
    let mut result = 0;
    for character in line.chars() {
        match character {
            ' ' => result += 1,
            '\t' => result += 4 - result % 4,
            _ => return result,
        }
    }
    result
}

fn indent(line: &str) -> &str {
    let end = line
        .char_indices()
        .find_map(|(index, character)| (!matches!(character, ' ' | '\t')).then_some(index))
        .unwrap_or(line.len());
    &line[..end]
}

fn cell_texts(line: &str) -> Vec<&str> {
    let mut parts = Vec::new();
    let mut start = 0;
    let mut characters = line.char_indices().peekable();
    while let Some((index, character)) = characters.next() {
        if character == '\\' {
            characters.next();
        } else if character == '|' {
            parts.push(&line[start..index]);
            start = index + 1;
        }
    }
    parts.push(&line[start..]);
    if line.starts_with('|') && !parts.is_empty() {
        parts.remove(0);
    }
    if line.ends_with('|') && line.len() > 1 && !parts.is_empty() {
        parts.pop();
    }
    parts
}

fn cells(line: &str) -> usize {
    cell_texts(line).len()
}

fn is_divider(line: &str) -> bool {
    // Foundation `.whitespaces`: Unicode Zs plus tab and ASCII space, not the ASCII newline characters, NEL or
    // line/paragraph separators. Unlike Rust `trim`, this excludes most `char::is_whitespace` scalars.
    let cells = cell_texts(line);
    !cells.is_empty()
        && cells.iter().all(|cell| {
            let mut value = cell.trim_matches(|character: char| {
                character == '\t'
                    || unicode_general_category::get_general_category(character)
                        == unicode_general_category::GeneralCategory::SpaceSeparator
            });
            if let Some(shorter) = value.strip_prefix(':') {
                value = shorter;
            }
            if let Some(shorter) = value.strip_suffix(':') {
                value = shorter;
            }
            !value.is_empty() && value.chars().all(|character| character == '-')
        })
}

fn is_delimiterish(line: &str) -> bool {
    !line.is_empty()
        && line.chars().all(|character| "|-: \t".contains(character))
        && (line.contains('-') || line.contains('|'))
}

fn is_closed_pipe_row(line: &str) -> bool {
    line.len() >= 2 && line.starts_with('|') && line.ends_with('|') && cells(line) > 0
}

fn is_heading(line: &str) -> bool {
    let count = line.chars().take_while(|character| *character == '#').count();
    if !(1..=6).contains(&count) {
        return false;
    }
    line[count..].is_empty() || line[count..].starts_with([' ', '\t'])
}

fn starts_list_item(line: &str) -> bool {
    let Some(first) = line.chars().next() else {
        return false;
    };
    if "-+*".contains(first) {
        return line.len() == 1 || line[1..].starts_with([' ', '\t']);
    }
    let digits = line.bytes().take_while(u8::is_ascii_digit).count();
    if !(1..=9).contains(&digits) {
        return false;
    }
    let rest = &line[digits..];
    if !rest.starts_with(['.', ')']) {
        return false;
    }
    rest.len() == 1 || rest[1..].starts_with([' ', '\t'])
}

fn starts_non_table_block(line: &str) -> bool {
    starts_list_item(line) || line.starts_with('>') || is_heading(line)
}

fn content_column(line: &str) -> usize {
    let value = trimmed(line);
    let marker = if value.starts_with(['-', '+', '*']) {
        1
    } else {
        value.bytes().take_while(u8::is_ascii_digit).count() + 1
    };
    let gap = value[marker..]
        .chars()
        .take_while(|character| *character == ' ')
        .count();
    columns(line) + marker + if (1..=4).contains(&gap) { gap } else { 1 }
}

struct Scan<'a> {
    lines: Vec<&'a str>,
    ends_with_newline: bool,
    fence: Option<Fence>,
    math_open: bool,
    closed_last: Option<&'static str>,
    block_start: usize,
}

impl<'a> Scan<'a> {
    fn new(text: &'a str) -> Self {
        // Apple has no synthetic empty line after a final newline.
        let lines = text.split_terminator('\n').collect::<Vec<_>>();
        let ends_with_newline = text.ends_with('\n');
        let mut fence: Option<Fence> = None;
        let mut math_end: Option<&str> = None;
        let mut closed_last = None;
        let mut block_start = 0;
        for (index, line) in lines.iter().enumerate() {
            let value = trimmed(line);
            if let Some(open) = &fence {
                if closes(open, value) {
                    fence = None;
                    block_start = index + 1;
                    closed_last = (index + 1 == lines.len()).then_some("code");
                }
                continue;
            }
            if let Some(end) = math_end {
                if value == end {
                    math_end = None;
                    block_start = index + 1;
                    closed_last = (index + 1 == lines.len()).then_some("math");
                }
                continue;
            }
            if let Some((character, count)) = opening(value) {
                fence = Some(Fence {
                    character,
                    count,
                    line: index,
                });
                block_start = index;
                continue;
            }
            if columns(line) < 4 && matches!(value, "$$" | "\\[") {
                math_end = Some(if value == "$$" { "$$" } else { "\\]" });
                block_start = index;
                continue;
            }
            if value.is_empty() {
                block_start = index + 1;
            }
        }
        Self {
            lines,
            ends_with_newline,
            fence,
            math_open: math_end.is_some(),
            closed_last,
            block_start,
        }
    }

    fn prefix_before(&self, line: usize) -> String {
        let mut prefix = self.lines[..line].join("\n");
        if line > 0 {
            prefix.push('\n');
        }
        prefix
    }

    fn continues_lazily(&self, index: usize) -> bool {
        for previous in (self.block_start..index).rev() {
            let value = trimmed(self.lines[previous]);
            if is_heading(value) {
                return false;
            }
            if value.starts_with('>') {
                return true;
            }
            if starts_list_item(value) {
                return columns(self.lines[index]) < content_column(self.lines[previous]);
            }
        }
        false
    }

    /// The table at the end, following Apple's `Scan.table`: a matching delimiter/header pair, or an unfinished
    /// delimiter beneath a closed pipe header. A lazy continuation of a list item or quote is not a table header.
    fn table(&self) -> Option<Table> {
        let last = self.lines.len() - 1;
        if self.block_start > last {
            return None;
        }
        let mut index = last;
        while index > self.block_start && !starts_non_table_block(trimmed(self.lines[index])) {
            let value = trimmed(self.lines[index]);
            let head = trimmed(self.lines[index - 1]);
            if value.contains('|')
                && is_divider(value)
                && !head.is_empty()
                && !is_delimiterish(head)
                && !starts_non_table_block(head)
                && cells(head) == cells(value)
                && !self.continues_lazily(index - 1)
            {
                return Some(Table {
                    header: index - 1,
                    divided: true,
                });
            }
            index -= 1;
        }
        let header = if self.ends_with_newline {
            last
        } else {
            last.checked_sub(1)?
        };
        if header < self.block_start {
            return None;
        }
        let head = trimmed(self.lines[header]);
        if !head.starts_with('|')
            || is_delimiterish(head)
            || self.continues_lazily(header)
            || header != self.block_start && trimmed(self.lines[header - 1]).starts_with('|')
        {
            return None;
        }
        let valid = if header == last {
            is_closed_pipe_row(head)
        } else {
            is_delimiterish(trimmed(self.lines[last]))
        };
        valid.then_some(Table { header, divided: false })
    }

    /// The last leaf block: Apple's startsBlock boundary, with a heading ending at its own line.
    fn repair_inlines(&self, text: &str) -> Option<String> {
        let last = self.lines.len() - 1;
        if self.block_start > last {
            return None;
        }
        let mut first = self.block_start;
        for index in (self.block_start..=last).rev() {
            let value = trimmed(self.lines[index]);
            if starts_block(value) {
                first = if is_heading(value) && index < last {
                    index + 1
                } else {
                    index
                };
                break;
            }
        }
        if columns(self.lines[self.block_start]) >= 4
            && !self.lines[..self.block_start]
                .iter()
                .any(|line| starts_list_item(trimmed(line)))
        {
            return None;
        }
        let scalars = text.chars().collect::<Vec<_>>();
        let lo = self.scalar_offset(first);
        let last_lo = self.scalar_offset(last);
        let end = text.trim_end_matches(is_space_like).chars().count();
        if end <= last_lo {
            return None;
        }
        InlineRepair {
            scalars,
            lo,
            last_lo,
            end,
        }
        .repaired()
    }

    fn scalar_offset(&self, line: usize) -> usize {
        self.lines[..line].iter().map(|line| line.chars().count() + 1).sum()
    }

    /// Hold back a half-written last row, or add the header's delimiter with the header's indentation.
    fn repair_table(&self, text: &str) -> Option<String> {
        let table = self.table()?;
        let last = self.lines.len() - 1;
        if table.divided {
            if self.ends_with_newline || last <= table.header + 1 || !trimmed(self.lines[last]).starts_with('|') {
                return None;
            }
            return Some(self.prefix_before(last));
        }
        let head = self.lines[table.header];
        let row = format!("{}|{}", indent(head), " --- |".repeat(cells(trimmed(head))));
        if table.header == last {
            Some(format!("{text}{row}"))
        } else {
            Some(format!("{}{row}", self.prefix_before(last)))
        }
    }
}

/// Apple `Scan.markersOnly`: the unfinished line can still become a list item, heading, quote or rule. A marker
/// followed by real text is not held back, and a line that already ended never reaches this predicate.
fn markers_only(line: &str) -> bool {
    let scalars = line.chars().collect::<Vec<_>>();
    let Some(first) = scalars
        .iter()
        .position(|character| !matches!(character, ' ' | '\t' | '\r'))
    else {
        return false;
    };
    let mut saw = false;
    let mut index = 0;
    while index < scalars.len() {
        let character = scalars[index];
        if matches!(character, ' ' | '\t' | '\r') {
            index += 1;
        } else if "#-=*+_>|`~:[]".contains(character) {
            saw = true;
            index += 1;
        } else if matches!(character, 'x' | 'X') && index > 0 && scalars[index - 1] == '[' {
            index += 1;
        } else if character.is_ascii_digit() {
            let mut end = index;
            while end < scalars.len() && scalars[end].is_ascii_digit() {
                end += 1;
            }
            if end - index > 9 {
                return false;
            }
            if end == scalars.len() {
                if index != first {
                    return false;
                }
            } else if !matches!(scalars[end], '.' | ')') {
                return false;
            }
            saw = true;
            index = (end + 1).min(scalars.len());
        } else {
            return false;
        }
    }
    saw
}

fn hold_markers(text: &str) -> Option<String> {
    if text.ends_with('\n') {
        return None;
    }
    let Some((prefix, last)) = text.rsplit_once('\n') else {
        return markers_only(text).then(String::new);
    };
    markers_only(last).then(|| format!("{prefix}\n"))
}

fn project_repair(text: &str) -> String {
    if text.is_empty() {
        return String::new();
    }
    if let Some(shorter) = without_indent_so_far(text) {
        return project_repair(shorter);
    }
    let scan = Scan::new(text);
    if let Some(open) = &scan.fence {
        let marker = format!(
            "{}{}",
            indent(scan.lines[open.line]),
            open.character.to_string().repeat(open.count)
        );
        if !scan.ends_with_newline {
            let last = scan.lines.len() - 1;
            if open.line == last {
                return scan.prefix_before(last);
            }
            let last_value = trimmed(scan.lines[last]);
            if !last_value.is_empty() && last_value.chars().all(|character| character == open.character) {
                return format!("{}{marker}", scan.prefix_before(last));
            }
        }
        return format!("{text}{}{marker}", if scan.ends_with_newline { "" } else { "\n" });
    }
    if scan.math_open || scan.closed_last.is_some() {
        return text.to_owned();
    }
    if let Some(table) = scan.repair_table(text) {
        return table;
    }
    if scan.table().is_some() || scan.ends_with_newline {
        return text.to_owned();
    }
    hold_markers(text).unwrap_or_else(|| scan.repair_inlines(text).unwrap_or_else(|| text.to_owned()))
}

fn project_end(text: &str) -> &'static str {
    if text.is_empty() {
        return "text";
    }
    if let Some(shorter) = without_indent_so_far(text) {
        return project_end(shorter);
    }
    let scan = Scan::new(text);
    if scan.fence.is_some() {
        return "code";
    }
    if scan.math_open {
        return "math";
    }
    if let Some(closed) = scan.closed_last {
        if !scan.ends_with_newline {
            return closed;
        }
    } else if !scan.ends_with_newline && scan.lines.last().is_some_and(|line| is_display_math(trimmed(line))) {
        return "math";
    }
    if scan.table().is_some() { "table" } else { "text" }
}

/// Apple `StreamRepair.plainOpenFence`: strip only the still-open fence's drawn info string. Its Scan trimming is
/// space/tab/CR, while the subsequent info predicate uses Foundation `.whitespacesAndNewlines`.
pub fn plain_open_fence(input: &PlainFenceInput) -> PlainFenceProjection {
    let text = &input.text;
    if !text.contains("```") && !text.contains("~~~") {
        return PlainFenceProjection { text: text.clone() };
    }
    let scan = Scan::new(text);
    let Some(ref open) = scan.fence else {
        return PlainFenceProjection { text: text.clone() };
    };
    let line = scan.lines[open.line];
    let scalars = text.chars().collect::<Vec<_>>();
    let mut run = scan.scalar_offset(open.line);
    let line_end = run + line.chars().count();
    while run < line_end && is_space_like(scalars[run]) {
        run += 1;
    }
    while run < line_end && scalars[run] == open.character {
        run += 1;
    }
    let info = scalars[run..line_end]
        .iter()
        .collect::<String>()
        .trim_matches(is_whitespace_and_newline)
        .to_owned();
    let text = if !info.is_empty() && input.drawn.contains(&info) {
        scalars[..run].iter().collect::<String>() + &scalars[line_end..].iter().collect::<String>()
    } else {
        text.clone()
    };
    PlainFenceProjection { text }
}

fn is_whitespace_and_newline(character: char) -> bool {
    matches!(character, '\t'..='\r')
        || unicode_general_category::get_general_category(character) == GeneralCategory::SpaceSeparator
        || matches!(character, '\u{0085}' | '\u{2028}' | '\u{2029}')
}

/// Apple `MathMarkdown.display`: a trimmed line containing exactly one formula. Its inline scanner uses Swift
/// `Character` indices, so this separate predicate walks extended graphemes rather than reusing scalar offsets.
fn is_display_math(line: &str) -> bool {
    for (open, close) in [("$$", "$$"), ("\\[", "\\]")] {
        if line.graphemes(true).count() > open.len() + close.len()
            && let Some(tex) = line.strip_prefix(open).and_then(|rest| rest.strip_suffix(close))
            && !tex.contains(close)
            && !tex.trim_matches(is_math_whitespace).is_empty()
        {
            return true;
        }
    }
    let mut spans = inline_math_spans(line);
    let Some((0, end)) = spans.next() else {
        return false;
    };
    end == line.len() && spans.next().is_none()
}

fn inline_math_spans(line: &str) -> impl Iterator<Item = (usize, usize)> {
    let graphemes = line.grapheme_indices(true).collect::<Vec<_>>();
    let mut spans = Vec::new();
    let mut index = 0;
    let mut tick = 0;
    while index < graphemes.len() && spans.len() < 2 {
        let (start, character) = graphemes[index];
        let next = graphemes.get(index + 1).map_or(line.len(), |(offset, _)| *offset);
        if character == "`" {
            let count = (index..graphemes.len())
                .take_while(|next| graphemes[*next].1 == "`")
                .count();
            tick = if tick == 0 {
                count
            } else if tick == count {
                0
            } else {
                tick
            };
            index += count;
            continue;
        }
        if tick > 0 {
            index += 1;
            continue;
        }
        if character == "\\" && index + 1 < graphemes.len() {
            if graphemes[index + 1].1 == "("
                && let Some(close) = find_math_pair(&graphemes, index + 2, "\\", ")")
            {
                let end = graphemes.get(close + 2).map_or(line.len(), |(offset, _)| *offset);
                spans.push((start, end));
                index = close + 2;
            } else {
                index += 2;
            }
            continue;
        }
        if character == "$"
            && index + 1 < graphemes.len()
            && graphemes[index + 1].1 != "$"
            && !is_math_character_whitespace(graphemes[index + 1].1.chars().next().unwrap_or(' '))
            && let Some(span) = closing_inline_dollar(&graphemes, index)
            && plausible_math(&line[next..graphemes[span].0])
        {
            let end = graphemes.get(span + 1).map_or(line.len(), |(offset, _)| *offset);
            spans.push((start, end));
            index = span + 1;
            continue;
        }
        index += 1;
    }
    spans.into_iter()
}

/// Locate the first candidate `$`, applying Apple's escape, close and money rules. A rejected candidate ends the
/// search for this opener rather than letting a later dollar close it.
fn closing_inline_dollar(graphemes: &[(usize, &str)], open: usize) -> Option<usize> {
    let mut index = open + 1;
    while index < graphemes.len() {
        let (_, character) = graphemes[index];
        if character == "\\" {
            index += 2;
            continue;
        }
        if character == "$" {
            let after_digit = graphemes
                .get(index + 1)
                .is_some_and(|(_, next)| next.chars().next().is_some_and(|c| c.is_numeric()));
            let before = graphemes[index - 1].1;
            let valid = !is_math_character_whitespace(before.chars().next().unwrap_or(' '))
                && !after_digit
                && graphemes.get(index + 1).is_none_or(|(_, next)| *next != "$");
            return valid.then_some(index);
        }
        index += 1;
    }
    None
}

fn find_math_pair(graphemes: &[(usize, &str)], start: usize, first: &str, second: &str) -> Option<usize> {
    (start..graphemes.len().saturating_sub(1))
        .find(|index| graphemes[*index].1 == first && graphemes[*index + 1].1 == second)
}

/// Foundation `.whitespaces` contains Unicode Zs and tab, unlike the inline scanner's broader
/// `Character.isWhitespace` or Rust's `char::is_whitespace`. Keep these two Apple predicates separate.
fn is_math_whitespace(character: char) -> bool {
    character == '\t' || unicode_general_category::get_general_category(character) == GeneralCategory::SpaceSeparator
}

fn is_math_character_whitespace(character: char) -> bool {
    character.is_whitespace()
}

fn is_math_wide(character: char) -> bool {
    matches!(character as u32, 0x3000..=0x9fff | 0xff00..=0xffef)
}

fn plausible_math(tex: &str) -> bool {
    tex.graphemes(true).count() <= 400 && (!tex.chars().any(is_math_wide) || tex.contains("\\text"))
}

fn starts_block(line: &str) -> bool {
    starts_non_table_block(line) || line.starts_with('|')
}

fn is_space_like(character: char) -> bool {
    matches!(character, ' ' | '\t' | '\r' | '\n')
        || unicode_general_category::get_general_category(character)
            == unicode_general_category::GeneralCategory::SpaceSeparator
}

fn is_markdown_punctuation(character: char) -> bool {
    if character.is_ascii() {
        return "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~".contains(character);
    }
    use unicode_general_category::GeneralCategory;
    matches!(
        unicode_general_category::get_general_category(character),
        GeneralCategory::ConnectorPunctuation
            | GeneralCategory::DashPunctuation
            | GeneralCategory::OpenPunctuation
            | GeneralCategory::ClosePunctuation
            | GeneralCategory::InitialPunctuation
            | GeneralCategory::FinalPunctuation
            | GeneralCategory::OtherPunctuation
    )
}

/// Apple's InlineRepair works on a Unicode-scalar array. Keep its indices and control flow here, then collect the
/// scalar slices back into UTF-8. In particular, the same-line destination finder is not nested or escape-aware.
struct InlineRepair {
    scalars: Vec<char>,
    lo: usize,
    last_lo: usize,
    end: usize,
}

#[derive(Debug)]
struct Opener {
    character: char,
    count: usize,
    at: usize,
}

struct InlineCode {
    closer: String,
}

#[derive(Clone, Copy)]
struct InlineLink {
    cut: usize,
    text: (usize, usize),
}

impl InlineRepair {
    fn run(&self, character: char, index: usize) -> usize {
        let mut end = index;
        while end < self.end && self.scalars[end] == character {
            end += 1;
        }
        end - index
    }

    fn line_end(&self, start: usize) -> usize {
        (start..self.end)
            .find(|index| self.scalars[*index] == '\n')
            .unwrap_or(self.end)
    }

    fn find(&self, first: char, second: Option<char>, start: usize) -> Option<usize> {
        let stop = self.line_end(start);
        (start..stop).find(|index| {
            self.scalars[*index] == first
                && second.is_none_or(|next| *index + 1 < stop && self.scalars[*index + 1] == next)
        })
    }

    fn find_run(&self, character: char, count: usize, start: usize) -> Option<usize> {
        let mut index = start;
        while index < self.end {
            if self.scalars[index] != character {
                index += 1;
                continue;
            }
            let n = self.run(character, index);
            if n == count {
                return Some(index);
            }
            index += n;
        }
        None
    }

    fn closing_dollar(&self, start: usize) -> Option<usize> {
        let stop = self.line_end(start);
        let mut index = start;
        while index < stop {
            if self.scalars[index] == '\\' {
                index += 2;
                continue;
            }
            if self.scalars[index] == '$' {
                let before_digit = index + 1 < stop && self.scalars[index + 1].is_ascii_digit();
                return (!is_space_like(self.scalars[index - 1]) && !before_digit).then_some(index);
            }
            index += 1;
        }
        None
    }

    fn url_end(&self, index: usize) -> Option<usize> {
        let scheme = ["https://", "http://"].into_iter().find(|scheme| {
            let chars = scheme.chars().collect::<Vec<_>>();
            index + chars.len() <= self.end
                && self.scalars[index..index + chars.len()]
                    .iter()
                    .zip(chars)
                    .all(|(actual, expected)| actual.to_lowercase().to_string() == expected.to_string())
        })?;
        let mut end = index + scheme.len();
        while end < self.end
            && !is_space_like(self.scalars[end])
            && self.scalars[end].is_ascii()
            && !"<>]`".contains(self.scalars[end])
        {
            end += 1;
        }
        Some(end)
    }

    fn flanking(&self, character: char, index: usize, count: usize) -> (bool, bool) {
        let previous = (index > self.lo).then(|| self.scalars[index - 1]);
        let next = (index + count < self.end).then(|| self.scalars[index + count]);
        let previous_space = previous.is_none_or(is_space_like);
        let next_space = next.is_none_or(is_space_like);
        let previous_punctuation = previous.is_some_and(is_markdown_punctuation);
        let next_punctuation = next.is_some_and(is_markdown_punctuation);
        let left = !next_space && (!next_punctuation || previous_space || previous_punctuation);
        let right = !previous_space && (!previous_punctuation || next_space || next_punctuation);
        if character == '_' {
            (
                left && (!right || previous_punctuation),
                right && (!left || next_punctuation),
            )
        } else {
            (left, right)
        }
    }

    fn followed_by_text(&self, start: usize, cut: usize, code: Option<&InlineCode>, link: Option<InlineLink>) -> bool {
        if code.is_some() {
            return true;
        }
        fn text(scalars: &[char], start: usize, end: usize) -> bool {
            start < end
                && scalars[start..end]
                    .iter()
                    .any(|c| !is_space_like(*c) && !"*_~`".contains(*c))
        }
        if let Some(link) = link {
            return text(&self.scalars, start, link.cut) || text(&self.scalars, start.max(link.text.0), link.text.1);
        }
        text(&self.scalars, start, cut)
    }

    fn repaired(&self) -> Option<String> {
        let mut openers: Vec<Opener> = Vec::new();
        let mut brackets: Vec<(usize, bool)> = Vec::new();
        let mut mixed = Vec::new();
        let mut code = None;
        let mut link = None;
        let mut stopped = false;
        let mut cut = self.end;
        let mut index = self.lo;
        'scan: while index < self.end {
            let character = self.scalars[index];
            match character {
                '\\' => {
                    if index + 1 >= self.end {
                        index += 1;
                        continue;
                    }
                    if self.scalars[index + 1] == '(' {
                        if let Some(end) = self.find('\\', Some(')'), index + 2) {
                            index = end + 2;
                            continue;
                        }
                        if index >= self.last_lo {
                            stopped = true;
                            break 'scan;
                        }
                    }
                    index += 2;
                }
                '`' => {
                    let count = self.run(character, index);
                    if let Some(end) = self.find_run(character, count, index + count) {
                        index = end + count;
                        continue;
                    }
                    if index >= self.last_lo && index + count < self.end && !is_space_like(self.scalars[index + count])
                    {
                        code = Some(InlineCode {
                            closer: "`".repeat(count),
                        });
                        stopped = true;
                        break 'scan;
                    }
                    index += count;
                }
                '$' => {
                    if index + 1 < self.end && self.scalars[index + 1] == '$' {
                        if let Some(end) = self.find('$', Some('$'), index + 2) {
                            index = end + 2;
                            continue;
                        }
                        if index >= self.last_lo {
                            stopped = true;
                            break 'scan;
                        }
                        index += 2;
                        continue;
                    }
                    if index + 1 < self.end
                        && !is_space_like(self.scalars[index + 1])
                        && !self.scalars[index + 1].is_ascii_digit()
                    {
                        if let Some(end) = self.closing_dollar(index + 1) {
                            index = end + 1;
                            continue;
                        }
                        if index >= self.last_lo {
                            stopped = true;
                            break 'scan;
                        }
                    }
                    index += 1;
                }
                'h' | 'H' => {
                    if (index == self.lo || !self.scalars[index - 1].is_ascii_alphanumeric())
                        && let Some(end) = self.url_end(index)
                    {
                        index = end;
                        continue;
                    }
                    index += 1;
                }
                '<' => {
                    if let Some(end) = self.find('>', None, index + 1)
                        && !self.scalars[index + 1..end].iter().any(|c| is_space_like(*c))
                    {
                        index = end + 1;
                        continue;
                    }
                    index += 1;
                }
                '[' => {
                    brackets.push((index, index > self.lo && self.scalars[index - 1] == '!'));
                    index += 1;
                }
                ']' => {
                    let Some((at, image)) = brackets.pop() else {
                        index += 1;
                        continue;
                    };
                    if index + 1 < self.end && self.scalars[index + 1] == '(' {
                        if let Some(end) = self.find(')', None, index + 2) {
                            index = end + 1;
                            continue;
                        }
                        if at >= self.last_lo {
                            link = Some(InlineLink {
                                cut: if image { at - 1 } else { at },
                                text: (at + 1, index),
                            });
                            stopped = true;
                            break 'scan;
                        }
                        index += 2;
                        continue;
                    }
                    if image && index + 1 == self.end && at >= self.last_lo {
                        link = Some(InlineLink {
                            cut: at - 1,
                            text: (at + 1, index),
                        });
                        stopped = true;
                        break 'scan;
                    }
                    index += 1;
                }
                '*' | '_' | '~' => {
                    let count = self.run(character, index);
                    if character == '~' && count != 2 {
                        if count == 1
                            && index + count == self.end
                            && index >= self.last_lo
                            && openers.iter().any(|opener| opener.character == '~')
                        {
                            cut = index;
                        }
                        index += count;
                        continue;
                    }
                    if count >= 3 {
                        mixed.push(character);
                        index += count;
                        continue;
                    }
                    let (open, close) = self.flanking(character, index, count);
                    let mut left = count;
                    if close {
                        while left > 0 {
                            let Some(last) = openers.iter().rposition(|opener| opener.character == character) else {
                                break;
                            };
                            let used = if left >= 2 && openers[last].count >= 2 { 2 } else { 1 };
                            openers[last].count -= used;
                            left -= used;
                            openers.truncate(last + 1);
                            if openers[last].count == 0 {
                                openers.remove(last);
                            }
                        }
                    }
                    if left > 0 && open {
                        openers.push(Opener {
                            character,
                            count: left,
                            at: index + count - left,
                        });
                    }
                    index += count;
                }
                _ => index += 1,
            }
        }
        if !stopped && let Some(&(at, _)) = brackets.iter().rev().find(|(at, image)| *image && *at >= self.last_lo) {
            link = Some(InlineLink {
                cut: at - 1,
                text: (at + 1, self.end),
            });
        }

        let mut closers = code
            .as_ref()
            .map_or_else(String::new, |code: &InlineCode| code.closer.clone());
        for opener in openers.iter().rev() {
            if opener.character == '_' {
                continue;
            }
            if opener.at < self.last_lo
                || mixed.contains(&opener.character)
                || openers
                    .iter()
                    .filter(|other| other.character == opener.character && other.count == opener.count)
                    .count()
                    != 1
                || !self.followed_by_text(opener.at + opener.count, cut, code.as_ref(), link)
            {
                break;
            }
            closers.push_str(&opener.character.to_string().repeat(opener.count));
        }
        if let Some(link) = link {
            let mut result = self.slice(..link.cut);
            result.extend(self.scalars[link.text.0..link.text.1].iter());
            result.push_str(&closers);
            return Some(result);
        }
        if closers.is_empty() {
            return None;
        }
        let mut result = self.slice(..cut);
        result.push_str(&closers);
        Some(result)
    }

    fn slice(&self, range: std::ops::RangeTo<usize>) -> String {
        self.scalars[range].iter().collect()
    }
}

/// Repairs the named bounded slices, including Apple's math end classification.
pub fn project(input: &Input) -> Projection {
    Projection {
        repaired: project_repair(&input.text),
        end: project_end(&input.text),
    }
}
