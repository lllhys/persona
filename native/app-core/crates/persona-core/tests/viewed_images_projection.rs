use std::fs;
use std::path::Path;

use persona_core::viewed_images::{Input, project};
use serde_json::{Map, Value, json};

fn viewed_image_id(image: Value) -> String {
    let path = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../../spec/fixtures/lines/work.json");
    let document: Value = serde_json::from_slice(&fs::read(path).unwrap()).unwrap();
    let mut input = document["cases"]
        .as_array()
        .unwrap()
        .iter()
        .find(|case| case["name"] == "tool_images_belong_to_call")
        .unwrap()["input"]
        .clone();
    let item = input["items"]
        .as_array_mut()
        .unwrap()
        .iter_mut()
        .find(|item| item["kind"] == "image" && item["tool_use_id"] == "call_a")
        .unwrap();
    item["image"] = image;
    let input: Input = serde_json::from_value(input).unwrap();
    let viewed = project(&input).expect("the tool image produces a viewed line");
    assert_eq!(viewed.image_ids.len(), 1);
    viewed.image_ids.into_iter().next().unwrap()
}

fn inline_image(data: &str, size: Option<i64>, width: Option<i64>, height: Option<i64>) -> Value {
    let mut image = Map::new();
    image.insert("mime".into(), json!("image/png"));
    image.insert("data".into(), json!(data));
    for (key, value) in [("size", size), ("width", width), ("height", height)] {
        if let Some(value) = value {
            image.insert(key.into(), json!(value));
        }
    }
    Value::Object(image)
}

#[test]
fn absent_size_derives_the_same_identity_as_explicit_size() {
    let derived = inline_image("iVBORw0KGgo=", None, Some(16), Some(10));
    assert_eq!(viewed_image_id(derived.clone()), "s:image/png:9:16x10");

    let explicit = inline_image("iVBORw0KGgo=", Some(9), Some(16), Some(10));
    assert_eq!(viewed_image_id(derived), viewed_image_id(explicit));
}

#[test]
fn explicit_size_wins_including_zero() {
    let explicit = inline_image("ignored", Some(900), Some(16), Some(10));
    assert_eq!(viewed_image_id(explicit), "s:image/png:900:16x10");

    let zero = inline_image("iVBORw0KGgo=", Some(0), Some(16), Some(10));
    assert_eq!(viewed_image_id(zero), "d:iVBORw0KGgo=12");
}

#[test]
fn empty_data_and_missing_dimensions_keep_data_identity() {
    let empty = inline_image("", None, Some(16), Some(10));
    assert_eq!(viewed_image_id(empty), "d:0");

    for (width, height) in [(None, Some(10)), (Some(16), None), (None, None)] {
        let image = inline_image("iVBORw0KGgo=", None, width, height);
        assert_eq!(viewed_image_id(image), "d:iVBORw0KGgo=12");
    }
}

#[test]
fn supplied_ref_identity_is_not_replaced_by_derived_size() {
    let mut image = inline_image("iVBORw0KGgo=", None, None, None)
        .as_object()
        .unwrap()
        .clone();
    image.insert("ref".into(), json!("https://example.com/image.png"));
    assert_eq!(viewed_image_id(Value::Object(image)), "d:https://example.com/image.png");
}
