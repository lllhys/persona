//! Pure question-answer construction following Apple's `QuestionForm` and `QuestionOption.display`.
//!
//! This is the `ios_current` answer-construction contract, not the UI's timed paging, review presentation, transport
//! state or a proposed unified approval policy. A produced resolution is a request to send, not delivery evidence.

use std::collections::{BTreeMap, BTreeSet};

use crate::apple_text::is_apple_whitespace;

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Deserialize)]
pub struct Input {
    pub interaction: Interaction,
    pub operation: Operation,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Interaction {
    pub kind: String,
    #[serde(default)]
    pub questions: Vec<Question>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Question {
    pub question: String,
    #[serde(default)]
    pub multi_select: bool,
    #[serde(default)]
    pub options: Vec<QuestionOption>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct QuestionOption {
    pub label: String,
}

#[derive(Debug, Clone, Copy, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum Action {
    PickOption,
    Advance,
    Submit,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Operation {
    pub action: Action,
    #[serde(default)]
    pub page: usize,
    #[serde(default)]
    pub picks: BTreeMap<String, BTreeSet<String>>,
    #[serde(default)]
    pub other: BTreeMap<String, String>,
    #[serde(default)]
    pub sending: bool,
    pub option_index: Option<usize>,
}

/// What one click leaves behind: the draft the next click starts from (`page`, `picks`, `other`) and, when this click
/// sends the answers, the resolution.
///
/// The draft follows the current iOS `QuestionForm` entry for entry (#9238). An accepted single choice replaces its
/// question's picks and leaves that question's Other entry as an empty string; the key stays, as in iOS's
/// `other[q.question] = ""`. An accepted multiple choice only toggles its label. Advance and Submit hand back the picks
/// and Other entries they were given. A client that reads a missing entry and an empty one alike, as iOS does wherever
/// it reads one, can store the draft as it is. One that tests the key itself, as Android's form does to draw its Other
/// field, must drop an emptied entry first.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize)]
pub struct Transition {
    pub allowed: bool,
    pub page: usize,
    pub picks: BTreeMap<String, BTreeSet<String>>,
    pub other: BTreeMap<String, String>,
    pub resolution: Option<Answers>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct Answers {
    pub answers: BTreeMap<String, String>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct Display {
    pub text: String,
    pub recommended: bool,
}

fn trim_display(text: &str) -> &str {
    text.trim_matches(is_apple_whitespace)
}

fn trim_answer(text: &str) -> &str {
    text.trim_matches(|character| {
        is_apple_whitespace(character)
            || matches!(
                character,
                '\n' | '\u{000b}' | '\u{000c}' | '\r' | '\u{0085}' | '\u{2028}' | '\u{2029}'
            )
    })
}

/// Display-only suffix removal. The submitted answer always retains the original option label.
pub fn display(label: &str) -> Display {
    // i18n-ignore: exact suffixes written by the agent in protocol option labels.
    for suffix in [" (Recommended)", "(Recommended)", "（推荐）", "(推荐)"] {
        if let Some(text) = label.strip_suffix(suffix) {
            return Display {
                text: trim_display(text).to_owned(),
                recommended: true,
            };
        }
    }
    Display {
        text: label.to_owned(),
        recommended: false,
    }
}

fn answer(
    question: &Question,
    picks: &BTreeMap<String, BTreeSet<String>>,
    other: &BTreeMap<String, String>,
) -> Option<String> {
    let custom = other
        .get(&question.question)
        .map(|text| trim_answer(text))
        .unwrap_or_default();
    if !custom.is_empty() {
        return Some(custom.to_owned());
    }
    let picks = picks.get(&question.question)?;
    if picks.is_empty() {
        return None;
    }
    Some(
        question
            .options
            .iter()
            .map(|option| &option.label)
            .filter(|label| picks.contains(*label))
            .cloned()
            .collect::<Vec<_>>()
            .join(", "),
    )
}

/// Applies one of the three operations in the existing answer fixture driver. Invalid pages/options are rejected
/// rather than indexed, since those inputs are outside the current Apple replay's contract.
///
/// Only a pick edits the draft ([`Transition`]). Advance and Submit never apply the single-choice rule: a question
/// holding both a pick and typed text still answers with the typed text and keeps both, as iOS does.
pub fn project(input: &Input) -> Option<Transition> {
    if input.interaction.kind != "question" {
        return None;
    }
    let questions = &input.interaction.questions;
    let operation = &input.operation;
    let mut next = Transition {
        allowed: false,
        page: operation.page,
        picks: operation.picks.clone(),
        other: operation.other.clone(),
        resolution: None,
    };
    if operation.sending {
        return Some(next);
    }
    if operation.action == Action::PickOption {
        let question = questions.get(operation.page)?;
        let label = question.options.get(operation.option_index?)?.label.clone();
        next.allowed = true;
        let selected = next.picks.entry(question.question.clone()).or_default();
        if question.multi_select {
            if !selected.remove(&label) {
                selected.insert(label);
            }
            return Some(next);
        }
        selected.clear();
        selected.insert(label);
        next.other.insert(question.question.clone(), String::new());
        if questions.len() > 1 {
            // The current UI animates this later. The existing synchronous projection does not model that delay.
            next.page = operation.page.saturating_add(1).min(questions.len() - 1);
            return Some(next);
        }
    } else {
        let question = questions.get(operation.page)?;
        if answer(question, &next.picks, &next.other).is_none() {
            return Some(next);
        }
        next.allowed = true;
        let all_answered = questions
            .iter()
            .all(|question| answer(question, &next.picks, &next.other).is_some());
        if operation.action == Action::Advance && operation.page + 1 < questions.len() && !all_answered {
            next.page += 1;
            return Some(next);
        }
    }

    let missing = questions
        .iter()
        .position(|question| answer(question, &next.picks, &next.other).is_none());
    if let Some(missing) = missing {
        next.page = missing;
        return Some(next);
    }
    let answers = questions
        .iter()
        .map(|question| {
            answer(question, &next.picks, &next.other)
                .map(|answer| (question.question.clone(), answer))
                .ok_or(())
        })
        .collect::<Result<BTreeMap<_, _>, _>>()
        .ok()?;
    next.resolution = Some(Answers { answers });
    Some(next)
}
