//! A byte-preserving supervision edit, using one non-recursive JSON lexical scan.

use std::borrow::Cow;

const SUPERVISION_TRUE: &str = "\"supervision_user\":true";
const SUPERVISION_TRUE_COMMA: &str = "\"supervision_user\":true,";

#[derive(Clone, Copy, PartialEq, Eq)]
enum Selected {
    Item,
    Fold,
    FoldSupervision,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Expect {
    FirstValueOrEnd,
    Value,
    FirstMemberOrEnd,
    Member,
    Colon,
    AfterValue,
}

struct Scope {
    open: usize,
    close: u8,
    expect: Expect,
    has_members: bool,
}

struct Edit {
    start: usize,
    end: usize,
    replacement: &'static str,
}

/// Returns the closing quote's position plus one. The boolean says whether the string contains an escape.
fn quoted_end(bytes: &[u8], start: usize) -> Option<(usize, bool)> {
    if bytes.get(start) != Some(&b'"') {
        return None;
    }
    let mut escaped = false;
    let mut i = start + 1;
    while i < bytes.len() {
        match bytes[i] {
            b'"' => return Some((i + 1, escaped)),
            b'\\' => {
                escaped = true;
                i += 2;
            }
            0..=0x1f => return None,
            _ => i += 1,
        }
    }
    None
}

fn key_name(json: &str, start: usize, end: usize, escaped: bool) -> Option<Cow<'_, str>> {
    if escaped {
        serde_json::from_str(&json[start..end]).ok().map(Cow::Owned)
    } else {
        Some(Cow::Borrowed(&json[start + 1..end - 1]))
    }
}

fn skip_whitespace(bytes: &[u8], mut i: usize) -> usize {
    while bytes.get(i).is_some_and(|b| matches!(b, b' ' | b'\t' | b'\n' | b'\r')) {
        i += 1;
    }
    i
}

/// Sets the first selected supervision field in the item and in the first fold object. Only the selected value's
/// bytes change; a missing key is inserted immediately after its object's opening brace. Everything else, including
/// duplicate keys, whitespace and escaped key spellings, remains byte for byte unchanged.
pub(super) fn patch(json: &str) -> Option<String> {
    let bytes = json.as_bytes();
    let first = skip_whitespace(bytes, 0);
    if bytes.get(first) != Some(&b'{') {
        // The legacy positional representation has no named fields to patch.
        return None;
    }
    let mut stack = vec![Scope {
        open: first,
        close: b'}',
        expect: Expect::FirstMemberOrEnd,
        has_members: false,
    }];
    let mut edits = Vec::with_capacity(2);
    let mut item_patched = false;
    let mut fold_selected = false;
    let mut fold_scope = None;
    let mut fold_patched = false;
    // The key waiting for its value, and a container value whose closing brace has not arrived yet.
    let mut pending = None;
    let mut container_value: Option<(Selected, usize, usize)> = None;
    let mut i = first + 1;

    while i < bytes.len() {
        let Some(scope) = stack.last() else {
            return (i == bytes.len()).then_some(()).and_then(|()| apply(json, edits));
        };
        if scope.expect == Expect::Colon {
            i = skip_whitespace(bytes, i);
            let value_start = i;
            let selected = pending.take();
            if selected == Some(Selected::Fold) {
                fold_selected = true;
            }
            match *bytes.get(i)? {
                open @ (b'{' | b'[') => {
                    let depth = stack.len() + 1;
                    if selected == Some(Selected::Fold) && open == b'{' {
                        fold_scope = Some(depth);
                    }
                    if matches!(selected, Some(Selected::Item | Selected::FoldSupervision)) {
                        container_value = Some((selected.expect("matched"), value_start, depth));
                    }
                    stack.last_mut().expect("a colon has a parent").expect = Expect::AfterValue;
                    stack.push(Scope {
                        open: i,
                        close: if open == b'{' { b'}' } else { b']' },
                        expect: if open == b'{' {
                            Expect::FirstMemberOrEnd
                        } else {
                            Expect::FirstValueOrEnd
                        },
                        has_members: false,
                    });
                    i += 1;
                    continue;
                }
                b'"' => {
                    let (end, _) = quoted_end(bytes, i)?;
                    i = end;
                }
                _ => {
                    while bytes
                        .get(i)
                        .is_some_and(|b| !matches!(b, b',' | b'}' | b']' | b' ' | b'\t' | b'\n' | b'\r'))
                    {
                        i += 1;
                    }
                    if i == value_start {
                        return None;
                    }
                }
            }
            if let Some(selected @ (Selected::Item | Selected::FoldSupervision)) = selected {
                edits.push(Edit {
                    start: value_start,
                    end: i,
                    replacement: "true",
                });
                match selected {
                    Selected::Item => item_patched = true,
                    Selected::FoldSupervision => fold_patched = true,
                    Selected::Fold => unreachable!(),
                }
            }
            stack.last_mut().expect("a colon has a parent").expect = Expect::AfterValue;
            continue;
        }

        i = skip_whitespace(bytes, i);
        let depth = stack.len();
        let scope = stack.last().expect("nonempty");
        let expect = scope.expect;
        let close = scope.close;
        let b = *bytes.get(i)?;
        match expect {
            Expect::FirstMemberOrEnd | Expect::Member => {
                if b == close && expect == Expect::FirstMemberOrEnd {
                    let scope = stack.pop().expect("nonempty");
                    if let Some((selected, value_start, value_depth)) = container_value
                        && value_depth == depth
                    {
                        edits.push(Edit {
                            start: value_start,
                            end: i + 1,
                            replacement: "true",
                        });
                        match selected {
                            Selected::Item => item_patched = true,
                            Selected::FoldSupervision => fold_patched = true,
                            Selected::Fold => unreachable!(),
                        }
                        container_value = None;
                    }
                    if (depth == 1 && !item_patched) || (fold_scope == Some(depth) && !fold_patched) {
                        edits.push(Edit {
                            start: scope.open + 1,
                            end: scope.open + 1,
                            replacement: if scope.has_members {
                                SUPERVISION_TRUE_COMMA
                            } else {
                                SUPERVISION_TRUE
                            },
                        });
                        if depth == 1 {
                            item_patched = true;
                        } else {
                            fold_patched = true;
                        }
                    }
                    i += 1;
                    continue;
                }
                if b != b'"' {
                    return None;
                }
                let start = i;
                let (end, escaped) = quoted_end(bytes, start)?;
                let key = key_name(json, start, end, escaped)?;
                pending = match (depth, key.as_ref()) {
                    (1, "supervision_user") if !item_patched => Some(Selected::Item),
                    (1, "fold") if !fold_selected => Some(Selected::Fold),
                    (2, "supervision_user") if fold_scope == Some(2) && !fold_patched => {
                        Some(Selected::FoldSupervision)
                    }
                    _ => None,
                };
                stack.last_mut().expect("nonempty").expect = Expect::Colon;
                stack.last_mut().expect("nonempty").has_members = true;
                i = skip_whitespace(bytes, end);
                if bytes.get(i) != Some(&b':') {
                    return None;
                }
                i += 1;
            }
            Expect::FirstValueOrEnd | Expect::Value => {
                if b == close && expect == Expect::FirstValueOrEnd {
                    stack.pop();
                    i += 1;
                    continue;
                }
                let value_start = i;
                match b {
                    b'{' | b'[' => {
                        stack.last_mut().expect("nonempty").expect = Expect::AfterValue;
                        stack.push(Scope {
                            open: i,
                            close: if b == b'{' { b'}' } else { b']' },
                            expect: if b == b'{' {
                                Expect::FirstMemberOrEnd
                            } else {
                                Expect::FirstValueOrEnd
                            },
                            has_members: false,
                        });
                        i += 1;
                    }
                    b'"' => {
                        i = quoted_end(bytes, i)?.0;
                        stack.last_mut().expect("nonempty").expect = Expect::AfterValue;
                    }
                    _ => {
                        while bytes
                            .get(i)
                            .is_some_and(|b| !matches!(b, b',' | b'}' | b']' | b' ' | b'\t' | b'\n' | b'\r'))
                        {
                            i += 1;
                        }
                        if i == value_start {
                            return None;
                        }
                        stack.last_mut().expect("nonempty").expect = Expect::AfterValue;
                    }
                }
            }
            Expect::AfterValue => match b {
                b',' => {
                    let scope = stack.last_mut().expect("nonempty");
                    scope.expect = if scope.close == b'}' {
                        Expect::Member
                    } else {
                        Expect::Value
                    };
                    i += 1;
                }
                b if b == close => {
                    let scope = stack.pop().expect("nonempty");
                    if let Some((selected, value_start, value_depth)) = container_value
                        && value_depth == depth
                    {
                        edits.push(Edit {
                            start: value_start,
                            end: i + 1,
                            replacement: "true",
                        });
                        match selected {
                            Selected::Item => item_patched = true,
                            Selected::FoldSupervision => fold_patched = true,
                            Selected::Fold => unreachable!(),
                        }
                        container_value = None;
                    }
                    if (depth == 1 && !item_patched) || (fold_scope == Some(depth) && !fold_patched) {
                        edits.push(Edit {
                            start: scope.open + 1,
                            end: scope.open + 1,
                            replacement: if scope.has_members {
                                SUPERVISION_TRUE_COMMA
                            } else {
                                SUPERVISION_TRUE
                            },
                        });
                        if depth == 1 {
                            item_patched = true;
                        } else {
                            fold_patched = true;
                        }
                    }
                    i += 1;
                }
                _ => return None,
            },
            Expect::Colon => unreachable!("a colon is handled before the member state"),
        }
    }
    if stack.is_empty() { apply(json, edits) } else { None }
}

fn apply(json: &str, mut edits: Vec<Edit>) -> Option<String> {
    edits.sort_by_key(|edit| (edit.start, edit.end));
    let mut result = String::with_capacity(json.len() + edits.len() * (SUPERVISION_TRUE_COMMA.len() + 1));
    let mut copied = 0;
    for edit in edits {
        if edit.start < copied || edit.end < edit.start || edit.end > json.len() {
            return None;
        }
        result.push_str(&json[copied..edit.start]);
        result.push_str(edit.replacement);
        copied = edit.end;
    }
    result.push_str(&json[copied..]);
    Some(result)
}
