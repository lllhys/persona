//! The rendered viewed-image leaf of `ViewedImages.of`, from the existing lines input. This is not the whole
//! `ChatLine` projection: FoldImage.ref synthesis, UI loading and actual tool execution stay outside this slice.

use std::collections::BTreeSet;

use serde::{Deserialize, Serialize};
use serde_json::Value;
use unicode_segmentation::UnicodeSegmentation;

#[derive(Debug, Clone, Deserialize)]
pub struct Input {
    pub items: Vec<Item>,
    pub line_id: Option<String>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct Item {
    pub id: String,
    pub seq: i64,
    pub role: String,
    pub kind: String,
    pub tool_use_id: Option<String>,
    pub tool_name: Option<String>,
    pub tool_input: Option<Value>,
    pub is_error: Option<bool>,
    pub image: Option<ImagePayload>,
    pub task: Option<Task>,
    pub fold: Option<Fold>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct Fold {
    #[serde(default)]
    pub tools: i64,
    #[serde(default)]
    pub thoughts: i64,
    #[serde(default)]
    pub through_seq: i64,
    #[serde(default)]
    pub through_id: String,
    #[serde(default)]
    pub until: Option<String>,
    #[serde(default)]
    pub images: Option<Vec<FoldImage>>,
}

/// An image a fold says its hidden work looked at. Only its identity is projected here: `ref` is supplied by the
/// caller's existing `ChatStore.withImageRefs` boundary, not reconstructed from a session key in this pure leaf.
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct FoldImage {
    pub item_id: Option<String>,
    pub tool_use_id: String,
    pub tool: String,
    pub source: String,
    pub path: Option<String>,
    pub machine: Option<String>,
    pub mime: Option<String>,
    pub size: Option<i64>,
    pub width: Option<i64>,
    pub height: Option<i64>,
    #[serde(rename = "ref")]
    pub ref_: Option<String>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct Task {
    pub kind: String,
    pub status: Option<String>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct ImagePayload {
    pub data: String,
    #[serde(default)]
    pub mime: String,
    #[serde(rename = "ref")]
    pub ref_: Option<String>,
    pub size: Option<i64>,
    pub width: Option<i64>,
    pub height: Option<i64>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ViewedLine {
    pub id: String,
    pub image_ids: Vec<String>,
    pub is_input: bool,
    #[serde(rename = "type")]
    pub kind: String,
}

fn source(input: &Value) -> Option<String> {
    input
        .get("file_path")
        .and_then(Value::as_str)
        .filter(|path| !path.is_empty())
        .map(str::to_owned)
}

fn picture(path: &str) -> bool {
    let extension = path
        .rsplit_once('.')
        .map(|(_, extension)| extension.to_ascii_lowercase());
    extension.is_some_and(|extension| {
        [
            "png", "jpg", "jpeg", "gif", "webp", "heic", "heif", "bmp", "tif", "tiff",
        ]
        .contains(&extension.as_str())
    })
}

fn size_key(mime: &str, size: Option<i64>, width: Option<i64>, height: Option<i64>) -> Option<String> {
    if let (Some(size), Some(width), Some(height)) = (size, width, height)
        && size > 0
        && width > 0
        && height > 0
    {
        return Some(format!("s:{mime}:{size}:{width}x{height}"));
    }
    None
}

fn key(path: Option<&str>, image: &ImagePayload) -> Option<String> {
    if let Some(path) = path {
        return Some(format!("p:{path}"));
    }
    let size = image.size.or_else(|| {
        if image.data.is_empty() {
            None
        } else {
            Some(image.data.graphemes(true).count() as i64 * 3 / 4)
        }
    });
    if let Some(key) = size_key(&image.mime, size, image.width, image.height) {
        return Some(key);
    }
    let identity = image.ref_.as_deref().unwrap_or_else(|| {
        // ImagePayload.identity is only truncated for inline data, never for a supplied ref.
        &image.data
    });
    if image.ref_.is_some() {
        return Some(format!("d:{identity}"));
    }
    let prefix = identity.graphemes(true).take(64).collect::<String>();
    Some(format!("d:{prefix}{}", identity.graphemes(true).count()))
}

fn fold_key(image: &FoldImage) -> String {
    if let Some(path) = image.path.as_deref() {
        return format!("p:{path}");
    }
    size_key(
        image.mime.as_deref().unwrap_or_default(),
        image.size,
        image.width,
        image.height,
    )
    .unwrap_or_else(|| format!("i:{}", image.item_id.as_deref().unwrap_or(&image.tool_use_id)))
}

/// Projects the viewed strip from the first tool group in the first represented turn. Every call/fold step in that
/// group contributes its images in order; later turns have separate viewed lines and are outside this leaf.
pub fn project(input: &Input) -> Option<ViewedLine> {
    let turn_input: crate::turns::Input = serde_json::from_value(serde_json::json!({"items": input.items})).ok()?;
    let turns = crate::turns::project(&turn_input);
    let (owner, groups) = turns.as_array()?.iter().find_map(|turn| {
        let mut groups = Vec::new();
        let mut has_user = false;
        for id in turn["ordered_ids"].as_array()?.iter().filter_map(Value::as_str) {
            let is_user = input.items.iter().any(|item| {
                item.id == id
                    && item.role == "user"
                    && matches!(item.kind.as_str(), "text" | "image")
                    && item.tool_use_id.is_none()
            });
            if is_user {
                if has_user {
                    break;
                }
                has_user = true;
            }
            if let Some(group) = id.strip_prefix("tools-").or_else(|| {
                input
                    .items
                    .iter()
                    .any(|item| item.id == id && item.kind == "tool_use")
                    .then_some(id)
            }) {
                groups.push(group.to_owned());
            }
        }
        (!groups.is_empty()).then_some((turn, groups))
    })?;
    let owner = input
        .line_id
        .as_deref()
        .or_else(|| owner.get("user_ids")?.as_array()?.first()?.as_str())
        .or_else(|| owner.get("id")?.as_str())?;

    let mut results: std::collections::BTreeMap<&str, Vec<&Item>> = std::collections::BTreeMap::new();
    let mut images: std::collections::BTreeMap<&str, Vec<&ImagePayload>> = std::collections::BTreeMap::new();
    for item in &input.items {
        let Some(id) = item.tool_use_id.as_deref() else {
            continue;
        };
        match item.kind.as_str() {
            "tool_result" => results.entry(id).or_default().push(item),
            "image" => {
                if let Some(image) = &item.image {
                    images.entry(id).or_default().push(image);
                }
            }
            _ => {}
        }
    }
    let mut image_ids = Vec::new();
    let mut seen = BTreeSet::new();
    let mut add = |id: String| {
        if seen.insert(id.clone()) {
            image_ids.push(id);
        }
    };
    let mut represented = false;
    let mut remaining = groups;
    while let Some(group) = remaining.first().cloned() {
        let first = input
            .items
            .iter()
            .position(|item| (item.kind == "tool_use" || item.kind == "fold") && item.id == group);
        let Some(first) = first else {
            remaining.remove(0);
            continue;
        };
        let mut work = Vec::new();
        for item in &input.items[first..] {
            if matches!(item.kind.as_str(), "tool_use" | "fold" | "thinking") {
                if matches!(item.kind.as_str(), "tool_use" | "fold") {
                    work.push(item);
                }
                continue;
            }
            if item.kind == "tool_result" || item.kind == "image" && item.tool_use_id.is_some() {
                // Results and result images are absorbed by their call rather than ending the run.
                continue;
            }
            break;
        }
        let count = remaining
            .iter()
            .skip(1)
            .position(|next| work.first().is_some_and(|item| item.id == *next))
            .map_or(1, |next| next + 2);
        remaining.drain(..count);
        for item in work {
            if item.kind == "fold" {
                let fold = item;
                represented = true;
                for image in fold
                    .fold
                    .as_ref()
                    .and_then(|fold| fold.images.as_ref())
                    .into_iter()
                    .flatten()
                {
                    add(fold_key(image));
                }
                continue;
            }
            let Some(id) = item.tool_use_id.as_deref() else {
                continue;
            };
            represented = true;
            let is_read = item.tool_name.as_deref() == Some("Read");
            let path = is_read
                .then(|| source(&item.tool_input.clone().unwrap_or(Value::Null)))
                .flatten();
            let calls_images = images.get(id).into_iter().flatten().copied().collect::<Vec<_>>();
            let result = results.get(id).and_then(|items| items.last());
            let agent = results.get(id).and_then(|items| {
                items
                    .iter()
                    .rev()
                    .find_map(|item| item.task.as_ref().filter(|task| task.kind == "agent"))
            });
            let running = agent.map_or(result.is_none(), |task| {
                !matches!(
                    task.status.as_deref(),
                    Some("completed" | "failed" | "error" | "killed" | "stopped")
                )
            });
            let error = result.is_some_and(|item| item.is_error == Some(true))
                || agent.is_some_and(|task| matches!(task.status.as_deref(), Some("failed" | "error")));
            if !calls_images.is_empty() {
                let file = (calls_images.len() == 1).then_some(path.as_deref()).flatten();
                for image in calls_images {
                    if let Some(key) = key(file, image) {
                        add(key);
                    }
                }
            } else if !running && !error && path.as_deref().is_some_and(picture) {
                add(format!("p:{}", path?));
            }
        }
    }
    if !represented || image_ids.is_empty() {
        return None;
    }
    Some(ViewedLine {
        id: format!("vi-{owner}"),
        image_ids,
        is_input: false,
        kind: "viewed".into(),
    })
}
