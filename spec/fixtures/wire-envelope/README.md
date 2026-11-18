# WebSocket Envelope 宽容解码

本目录记录当前 iOS `ServerEvent.decode` 的一个窄切片：共享 `Envelope` 的标量键、原始 JSON 键、`mesh` 与 `fleet` 的宽容边界，以及 `hello`、`inbox`、`items`、`fleet`、`screen`、`sessions_list` 和未知事件的路由条件。规则来自阅读 Apple 源码，不由 Rust 或其他被测实现生成；本次没有在 iOS、模拟器或真机上执行 Swift。

本目录独立于 `spec/fixtures/wire/` 的 Go golden 样本，不修改现有 17 条 continuity transport 用例。#8955 的诊断投影不改变客户端行为；#8995 随后把同一批源码推导的共享标量 / raw JSON 规则接入 production `persona_core::transport::decode_frame`。production 对合法其他事件仍返回 `Frame::Other`，不会变成完整 `ServerEvent` 路由实现。

## JSON 结构与比较

`frames.json` 顶层包含 `schema_version: 1`、`operation: "server_event_envelope"`、`profile: "ios_current"` 和 `cases`。每条用例包含：

- 唯一的 snake_case `name`、一句话 `description`、可定位到 Swift 类型或函数的 `source`，以及 `derived: true`；
- `input`：直接交给 Envelope 投影的 JSON。重复键用例必须按文件中的原始字节读取，不得先转成会覆盖重复键的对象；
- `expected`：完整的投影结果，按用例中的键和值比较，不比较 Rust JSON 序列化顺序。

`expected.outcome` 有三种：

- `accepted`：`ServerEvent.decode` 返回事件；`event` 是路由后的事件种类，其余键是本项目明确支持的字段投影。`items` 固定投影其数组、会话标识和连续性字段；`hello` 投影 `features`，以及明确出现的 hello 字段；`fleet` 投影计数、宽容字段和用例涉及的 item；`screen` 投影文本和状态。
- `ignored`：Envelope 合法但事件类型未知，`type` 保留原始字符串，不构造其他事件。
- `dropped`：Envelope 或事件必需键不满足要求。`key` 只记录协议键名，用于定位规则；不是 Foundation 的错误文本，也不是对错误优先级或诊断文案的承诺。嵌套 Item 和 Fleet 的错误归属顶层 `items` / `fleet`，没有键名的 JSON 根错误为 `null`。

## 规则与来源

| 规则 | Swift 来源 | 用例 |
| --- | --- | --- |
| `type` 是必需字符串，未知类型在 Envelope 成功后忽略。 | `apple/Shared/LiveConnection.swift`: `ServerEvent.Envelope.type`, `ServerEvent.decode` guard / default | `missing_type_is_dropped`, `null_type_is_dropped`, `numeric_type_is_dropped`, `unknown_type_is_ignored_after_valid_envelope` |
| 共享 Envelope 在事件 switch 前完整解码；事件未使用的已知键类型错误也会丢弃整条事件。 | 同上：`Envelope`, `ServerEvent.decode` guard | `unknown_type_still_checks_known_keys`, `unknown_type_wrong_backlog_drops_event`, `inbox_shared_scalars_check_even_when_unused`, `hello_wrong_from_seq_drops_event`, `hello_rows_must_be_integer`, `inbox_wrong_items_type_drops_event`, `items_wrong_other_event_key_drops_event` |
| 未知键忽略；可选键缺失或为 null 等价；重复键保留第一个值。 | 同上：`Envelope`；`native/app-core/crates/persona-core/src/transport/seen.rs` 对 swift-foundation `KeyedContainer.stringify` 的引用 | `hello_unknown_field_is_ignored`, `hello_features_null_defaults_to_empty`, `hello_features_keep_first_duplicate`, `items_duplicate_type_keeps_first` |
| hello 的 `features` 缺省为空，build 等数字键仍为整数，字符串键不自动转换。 | 同上：`Envelope.features`, `build`, `publicURL`; `decode case "hello"` | `hello_defaults_to_empty_features`, `hello_wrong_features_type_drops_event`, `hello_wrong_build_type_drops_event`, `hello_null_optional_scalars_are_absent`, `hello_public_url_is_not_rewritten` |
| `manager`、`account`、`args` 是原始 `JSONValue`，接受其支持的 JSON 形状，普通小数、有限大整数和嵌套小数按 Double 读取。 | 同上：`Envelope.manager`, `account`, `args`; `apple/Packages/PersonaWire/Sources/PersonaWire/JSONValue.swift`: `JSONValue.init(from:)` | `hello_raw_manager_accepts_mixed_json`, `hello_raw_account_boolean_is_not_string`, `items_raw_args_accepts_object`, `inbox_raw_json_fractional_number_is_accepted`, `inbox_raw_json_finite_large_integer_is_accepted` |
| `mesh` 或 `mesh.hubs` 失败只丢失 mesh；hub 的 `name` / `online` / `self` / `tls_port` 等宽容字段单独失败，合法 hubs 保序。 | 同上：`MeshField.init(from:)`; `apple/Shared/Hubs.swift`: `MeshHub.init(from:)` | `mesh_non_object_is_lenient`, `mesh_bad_hubs_element_drops_only_hubs`, `mesh_valid_hubs_keep_identity`, `mesh_lenient_hub_field_keeps_identity`, `mesh_lenient_scalar_fields_keep_identity`, `mesh_wrong_outer_key_type_drops_event` |
| Fleet 的 stats / look 单独宽容，loads / slots / item background / progress 失败只丢局部字段，但 `package` 非字符串会丢整个 look；根对象、必需 item id 和严格标量字段仍能拒绝整个 Envelope，未知 phase / kind 使用 iOS 默认值，item.since 是 Double。 | `apple/Shared/FleetState.swift`: `FleetState.init(from:)`, `Item.init(from:)`, `Stats.init(from:)`, `Phase.init(from:)`, `Kind.init(from:)`, `FleetLook` | `inbox_fleet_tolerates_bad_stats`, `inbox_fleet_bad_required_scalar_drops_event`, `inbox_fleet_missing_item_id_drops_event`, `inbox_fleet_bad_look_drops_only_look`, `inbox_fleet_bad_package_drops_whole_look`, `inbox_fleet_phase_and_kind_are_lenient`, `inbox_fleet_non_string_kind_large_integer_defaults_to_screen`, `inbox_fleet_lenient_item_fields_keep_event`, `inbox_fleet_lenient_slots_keep_event`, `inbox_fleet_bad_loads_drops_only_loads`, `inbox_fleet_fractional_since_is_a_double`, `inbox_fleet_string_since_drops_event` |
| items / screen 需要完整 SessionKey；items 的数组和标志有默认值；screen 的未知状态降级为 unknown。 | `apple/Shared/LiveConnection.swift`: `decode case "items"`, `"screen"`; `apple/Shared/Models.swift`: `SessionStatus.init(from:)` | `items_defaults_optional_fields`, `items_null_array_and_flags_default`, `items_missing_machine_id_drops_event`, `items_missing_session_id_drops_event`, `items_empty_session_ids_are_kept`, `items_unprojected_number_never_panics`, `screen_missing_session_id_drops_event`, `screen_unknown_status_falls_back_to_unknown`, `screen_null_status_falls_back_to_unknown`, `screen_numeric_status_drops_event` |
| 事件级必需字段与共享 Envelope 的可选字段不同。 | 同上：`decode case "fleet"`, `"sessions_list"` | `fleet_requires_fleet_payload`, `fleet_valid_payload_projects_counts`, `sessions_list_requires_revision`, `sessions_list_keeps_revision` |

未列为用例的接受规则不能被本切片推断：其他事件的完整关联值、其他嵌套模型、subscription 预路由、`stamped` 和 URL 的 Foundation 完整等价性不在范围内。

## 回放与证据边界

Rust 和真实 host Kotlin/JNI 使用同一份未修改的 `frames.json`，检查精确 case 清单和完整 `expected` 对象。Rust 回放从源码读取 `input` 的 `RawValue`，Kotlin 用测试专用 cursor 取同一 `input` 的原始文本；两者都不会先解析再序列化输入。

#8995 另有 production 回放（`persona-core/tests/transport_envelope_production.rs`、Android `TransportBindingFixtureTest`、ArkTS `transport.test.mjs` 和独立 C11 ABI 客户端），均走 `decode_frame` / `Action.WirePayload`，而不是用 `wire_envelope` 诊断投影代替 production。它使用本目录相同的 dropped key 期望，不由 Rust 生成。

production 只覆盖共享 Envelope 标量、字符串数组、数组/对象形状和原始 JSONValue 规则。`items_unprojected_number_never_panics` 是诊断 Value 投影的特例，不约束 production 保留原始 Item 的行为。`inbox_fleet_*` 的 dropped 用例属于 Fleet 内部模型；`fleet_requires_fleet_payload`、`screen_missing_session_id_drops_event`、`sessions_list_requires_revision` 属于非 items 的事件级路由，均不是本 issue 的 production 范围。其余非 items 的 accepted / ignored 输入必须为 `Frame::Other`，不会将未实现的事件误当成已支持。C11 精确清单覆盖 20 条适用的 dropped 用例及 `expected.key`；新增共享标量拒绝用例若未放入生产或 C 回放，测试会失败。

```sh
cargo test --manifest-path native/app-core/Cargo.toml -p persona-core --test wire_envelope_projection
cd android && ./gradlew :core:rustcore:test --tests 'persona.core.rustcore.WireEnvelopeBindingFixtureTest'
scripts/ci/core.sh
```

期望值手工来自上述 Swift 源码；`derived: true` 不表示已在 Apple 运行时执行。host JNI 结果不证明 Swift、NAPI、HAP、生产 Android adapter、模拟器、真机或性能行为。现有 transport 的数字写法、编码和未读嵌套结构差异保持不变；#8995 仅把共享 Envelope 标量 / raw JSON 校验接入 continuity 解码器。

## 范围外与新增用例

范围外：`machines`、`sessions` / `upsert` 的完整 `Machine` / `Session` 模型，`interaction`、`update`、`operation`、`auth`、`item`、`view`、`plan`、`badge` 的完整嵌套模型，以及 `args`、`account`、`action`、`answered_by`、`reason`、`answers`、`resolution`、`manager`、`changed` 中极端浮点数与 Foundation `Double` 的完整等价性。这些字段只做本切片明确需要的形状或原始 JSON 检查，不能宣称已覆盖完整 Swift 类型。

Item 的投影使用现有 `Item::from_wire` 标量规则，不新增其 `image`、`task`、`compact`、`injected`、`model_refusal` 等未读嵌套结构的接受结论；fleet 完整 rows 模型同理。整数仍使用现有 transport 的整数字面量规则，不引入 `8.0` / `8e0` 的兼容变更。

新增用例时先阅读当前 Swift 实现，手工写输入与完整预期，补 source 与规则表，并同步 Rust / Kotlin 的精确清单。不得调用核心生成期望后回填。
