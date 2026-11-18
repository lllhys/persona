# 对话行折叠夹具

本目录固定当前 iOS 的 `items → ChatRow → ChatBlock/Turn → ChatLine` 行为，供 Android 纯 Kotlin/JUnit 实现和以后的 XCTest 共用。判定依据是现有源码和测试；设计稿尚未实现的状态行、失败文案不作为当前 iOS 的输出。所有消息、标识和时间均为编造数据。

## 文件和执行范围

| 文件 | 覆盖内容 |
| --- | --- |
| `structure.json` | 基本问答、行标识、菜单归属、回顾、中断、压缩通知 |
| `queued.json` | 中途吸收、多消息轮按消息分段、普通图片和被吸收的图片 |
| `wakes.json` | A wake opens a turn of its own (#9140): the answer before it stays, several wakes, a wake after a recap, a live woken turn, pictures after a wake |
| `work.json` | 思考叙述、工具配对、失败、缺结果、工具图片、时间边界 |
| `folds.json` | fold 计数、重复页、扩大范围、展开还原、范围外的结果 |
| `tasks.json` | 后台任务关联、主题、结果文案和后台徽标 |
| `frozen.json` | 按 `(seq, part)` 冻结、冻结时的进行中轮、补入更早历史 |
| `rebuild.json` | 增量与全量重建等价、前插历史、后台突发和替换收敛 |
| `layout.json` | 待处理交互在正文之外、输入框上方的位置契约 |
| `ios/differences.json` | Where current iOS differs from the cases above ("Where current iOS differs") |
| `ios/entries.json` | Current iOS only: the blocks of one transcript entry (text, picture, text; a queued message with pictures) are one message |
| `ios/agent_wakes.json` | Current iOS only: a subagent's tool result that wakes the agent, while pending, after a recap, and opening the window |
| `ios/frozen_cuts.json` | Current iOS only: a frozen view cut inside a tool run, with results after the cut |
| `ios/compactions.json` | Current iOS only: a compaction's continuation mid-turn, idle after the answer, alone, and after `/compact` |
| `ios/supervision.json` | Current iOS only: genuine supervision input (the hub's mark or its exact template) and the same words protected as the user's |

The 61 cases above are the shared corpus, which Android replays too; the 17 cases under `ios/` are current iOS only ("Cases only current iOS asserts"). "Evidence" says how far each case is checked. No case was filled in by guesswork, and none rests on an XCTest assertion that cannot be replayed.

## Evidence

A case is evidence of up to three things, each checked a different way. The replay ("对照和复跑") checks all three on macOS; nothing here is a UI test, a simulator run or a device claim.

| Tier | What it covers | How the replay checks it | Cases |
| --- | --- | --- | --- |
| Current source | The `source` of every case and of every `ios/differences.json` entry: the Apple files, functions and tests it cites. For `verification: "view_source_order"`, also the declaration order in `ChatView.mainFooter` | Each cited file must exist and contain each name cited from it (`check_source`), and a `from_test` must be in one of them; the code the replay runs is extracted from `apple/` by unique markers, so a type or function that is gone fails the build; the footer cases read `mainFooter`, `pendingCards` and `PendingCards` | All 78. The only tier of `single_interaction_in_footer` and `multiple_interactions_in_footer` (`layout.json`), whose lines and turns are empty |
| Static turn projection | `expected.turns`, and `task_index` when there are task notices: `ChatStore.buildRows` → `ChatBlock.build` on `items`, after the `frozen` cut, with no view and no store | Built with the app's code and compared as complete objects. With `check_prefixes`, `ChatStore.rebuild`, the app's incremental path, must also give the same rows and turns for every prefix | 55 corpus cases (all but the 2 footer cases and the 4 `operations` cases), through `ios/differences.json`; all 17 iOS-only cases, each with `check_prefixes` |
| Rendered lines | `expected.lines`: `ChatLines(openWork:).build(blocks, live:)` as an Agent's chat draws them without BeautifulUI | Same run, complete objects | 59 corpus cases (all but the 2 footer cases), through `ios/differences.json`, and all 17 iOS-only cases |

The 4 `operations` cases (`folds.json`, `rebuild.json`) are no static projection: their `checkpoints` hold the turns and lines after `ChatStore` operations on a real store, which the replay runs and waits for. A port of the turn folding alone, with no lines, can use the static tier of the other cases.

What the rendered tier does not cover: `ChatView`'s post-processing of the lines (`ChatLines.splitAnswers`, `sourceRuns`, `hidingCloseout`), BeautifulUI's chip runs, and the supervision and background-round folds of Persona's own chats (`foldSupervision`, `foldBackground`). So in `ios/supervision.json` the claim is the turn split; the lines show the supervision input as the user line an Agent's chat draws, not the folded round of Persona's chat.

## 规则及来源

每行是一条规则，末列给出覆盖用例；用例内的 `source` 保留完整文件和函数名称。

| 规则 | 来源 | 用例 |
| --- | --- | --- |
| 空窗口不生成行，纯问答不补空工作行。 | `ChatLines.build`、`flatLines` | `empty_session`、`plain_exchange` |
| 进行中轮即使还没步骤也保留工作行，只有最后一段可以进行中。 | `ChatLines.flatLines(live:)`、`ChatView.liveTurnID` | `live_empty_work`、`live_reply_stays_after_work`、`live_queued_message_keeps_position` |
| 普通用户文本开新轮，`queued: true` 文本加入当前轮，窗口首条 queued 文本可以独立开轮。 | `ChatBlock.build` | `ordinary_user_starts_next_turn`、`queued_without_existing_turn` |
| 完成轮的多条用户文本和进行中一样按消息分段，每条一个消息行，工作和回复挂在各自下面（#2512 起不再合并）。 | `ChatLines.flatLines`、`segments` | `finished_queued_messages_apart`、`open_queued_messages_work`、`three_users_apart_without_steps` |
| 工作展开只追加独立步骤和收起行，已有消息和工作标题的标识不变。 | `ChatLineTests.workExpansionKeepsExistingIdentities`、`ChatLines.unfold` | `closed_work_identity`、`open_work_identity` |
| 多消息轮里每条消息的菜单归属于它自己那一行。 | `ChatLineTests.messageMenuStillBelongsToItsOriginalRow`、`ChatLine.holds` | `menu_owner_per_message` |
| 每段最后一条助手文本是回复，之前的助手文本作为工作叙述。 | `ChatLines.flatLines`、`workItems` | `open_queued_messages_work`、`trailing_tool_after_answer` |
| 非空 thinking 保留为可展开叙述，独立的空白 thinking 不进入工作行。 | `ChatLines.workItems`、`ThinkingRow`；`docs/lessons/transcript.md` thinking 经验 | `thinking_narration_preserved`、`standalone_empty_thinking_hidden` |
| thinking 在工具前形成独立行，在连续工具之间或末尾进入工具组，组内空 thinking 也保留。 | `ChatStore.buildRows`、`ChatLines.workItems` | `thinking_before_between_after_tools`、`empty_thinking_between_tools_is_retained` |
| 工具按 `tool_use_id` 在整个窗口配结果，最后一个结果生效，孤立结果不另生成行。 | `buildRows` | `late_result_attaches_to_original_call`、`last_result_for_call_wins`、`orphan_tool_result_is_not_a_row` |
| 工具缺结果时 `ToolCall.isRunning` 为真，但不会据此把完成轮的工作行改成进行中。 | `ToolCall.isRunning`、`ChatLines.flatLines(live:)` | `missing_tool_result_is_running` |
| 工具失败只取结果的 `is_error`，不根据输出文字推断。 | `ToolCall.isError` | `failed_tool_counts_known_failure` |
| 带 `tool_use_id` 的图片归工具卡，普通及被吸收的用户图片在消息上方显示。 | `buildRows`、`ChatLines.photos`、`flatLines`、`viewed`；`docs/lessons/transcript.md` 图片两条路 | `tool_images_belong_to_call`、`regular_user_images`、`absorbed_user_images` |
| A notice that woke the idle agent (`wake`) opens a turn of its own (#9140): the turn before it keeps its last reply as its answer, whatever the lengths (until then the woken run stayed in that turn and its latest reply took the answer's place, #638). Each run's last reply is its turn's answer; a reply before it is narration in that turn's work line. | `ChatBlock.build`; `ChatStoreTests.aBackgroundWakeOpensItsOwnExchange`, `theAnswerToANewerQuestionSurvivesLaterWakes` | `multiple_wakes_pick_latest_answer`、`only_last_reply_in_each_wake_is_candidate`、`short_reply_after_wake_is_the_answer`、`long_answer_after_wake` |
| A woken turn draws its notice where a message would be, then one work line for what it did and its last reply. The Android port also lists what the wake led to (`after_wake_ids`) and what came after the turn's last reply (`tail_ids`); neither is a line of its own, and current iOS has no such lists. | `ChatBlock.build`, `ChatLines.flatLines` | `short_reply_after_wake_is_the_answer`、`long_answer_after_wake`、`live_tail_runs_behind_chip` |
| A message typed while the woken agent works, and its pictures, join the woken turn and stay with that message. | `ChatBlock.build`、`ChatRow.isUserInput`；`ChatStoreTests.picturesSentAfterAWakeStayWithTheirMessage` | `photos_after_wake_stay_with_message` |
| 回顾独立于轮。A wake after a recap does not reach back over it: the woken turn follows the recap, and the turn before the recap keeps its answer. | `ChatBlock.build`；`ChatStoreTests.aWakeAfterARecapOpensItsExchangeAfterTheRecap` | `recap_separates_turns`、`wake_after_recap_reopens_turn` |
| 没有唤醒标记的后台通知只算工作过程。A woken turn with no reply keeps its work line under its notice. | `ChatBlock.build` | `notification_without_wake`、`wake_without_any_reply` |
| A turn a wake opened (also at the start of the window) draws the notice first; its work line is known by the wake's id from before its first step (`w-<wake id>`), and it is the live one while the woken agent works. | `ChatLines.flatLines` | `orphan_wake_opens_with_notice`、`live_tail_runs_behind_chip`、`live_answer_after_wake` |
| 无任务字段且含“中断”的通知进入轮尾，压缩通知和任务标题里的“中断”仍属工作过程。 | `ChatBlock.build`；`TaskNoteTests.aNoticeAboutAnInterruptIsNotTheInterrupt` | `interrupt_is_trailing_notice`、`compaction_stays_inside_work`、`task_title_containing_interrupt_is_not_interrupt` |
| fold 按其代表的工具和思考计数，结束时间取 `fold.until`，收起步数按工具数加思考数。 | `ToolStep`、`ChatLines.unfold`；`FoldedWorkTests.aFoldCountsWhatItStandsFor` | `fold_counts_tools_and_thoughts` |
| fold 替换范围内的工作，重复 fold 幂等，扩大范围替换旧 fold，展开还原原步骤。 | `ChatStore.dropFolded`、`ChatStore.unfold`；`FoldedWorkTests.aFoldReplacesTheStepsItCoversAndOpeningItPutsThemBack` | `fold_replaces_extends_and_unfolds` |
| fold 范围外的工具仍接收之后到达的结果。 | `buildRows`；`FoldedWorkTests.stepsOutsideAFoldStay` | `outside_fold_tool_keeps_late_result` |
| 后台通知优先按调用标识关联，再按工具结果公布的任务标识关联。 | `TaskIndex.build`；`TaskNoteTests.noticesFindTheirCalls` | `task_notices_find_calls` |
| 后台任务主题优先用调用描述，Monitor 事件优先用详情首个非空行，找不到调用时用通知标题。 | `TaskNoteText.subject` | `task_notices_find_calls`、`monitor_event_uses_first_detail_line`、`task_without_loaded_call_uses_title` |
| 后台结果与徽标按状态和退出码生成，事件只累计事件数，尚无通知时为后台运行中。 | `TaskNoteText.outcome`、`badge` | `task_notices_find_calls`、`background_running_badge`、`task_status_outcomes` |
| 当前工作行从本段消息或首个步骤算到末条内容，进行中时算到输入的固定时钟，缺起点则隐藏耗时。 | `ChatLines.flatLines`、`workEnd`、`WorkLine.duration` | `timed_work_ends_at_answer`、`missing_start_time_hides_duration`、`missing_start_time_uses_first_step`、`trailing_tool_after_answer` |
| 冻结按数字块序号截取 item 前缀，保留冻结时的进行中轮，更早历史仍能补入。 | `currentLines`、`prefixCount`、`Frozen`；`ux.md` 3.5 | `frozen_snapshot_excludes_newer_items`、`frozen_uses_numeric_block_order`、`older_page_stays_visible_during_freeze` |
| 增量重建须等于全量重建，迟到结果、前插历史和整段替换都不能遗留旧行。 | `ChatStore.rebuild`、`scheduleRebuild`；`RebuildTests` | `incremental_with_late_result_and_older_page`、`background_bursts_and_replacement` |
| 窗口可从助手过程开始，未知类型不生成消息。 | `buildRows`、`ChatBlock.build` | `orphan_assistant`、`unknown_item_is_ignored` |
| 请求面板按给定顺序排在正文外、输入框上方，不给它伪造 transcript 行。 | `ChatView.mainFooter`、`pendingCards`、`PendingCards`、`chatScroll`；`ux.md` 3.2、3.4 | `single_interaction_in_footer`、`multiple_interactions_in_footer` |
| Current iOS only: the blocks of one transcript entry are one message whatever their kind or `queued`; a picture goes after the entry's next text block and is drawn above it. | `ChatStore.picturesAfterTheirText`, `ChatBlock.build` (`sameMessage` by `Item.entry`), `rebuild`; `ChatLinesTests.aPictureAfterItsTextStaysWithItsMessage` | `same_entry_text_image_text_is_one_message`, `queued_entry_text_and_images_join_the_running_turn`, `supervision_round_keeps_same_entry_text_and_images` |
| Current iOS only: a subagent's tool result with `wake` opens an exchange of its own like any wake, also after a recap. It is its call's result, never a notice line or a work step, even when it opens the window. | `buildRows`, `ChatBlock.build`, `SubagentDelivery.isWake`, `ChatLines.flatLines`; `SubagentDeliveryTests.wakesRemainTurnEvidenceWithoutCreatingAParentMessageCard` | `agent_result_wake_opens_its_own_exchange`, `agent_result_wake_pending_keeps_previous_answer`, `agent_result_wake_after_recap_follows_the_recap`, `orphan_agent_result_wake_draws_no_notice` |
| Current iOS only: a frozen view cuts the raw items at `(seq, part)`, inside a tool run too. Results after the cut are not paired, an earlier exchange's call included. | `ChatView.currentLines`, `prefixCount`, `buildRows`; `ChatStoreTests.frozenChatCountsItemsUpToTheSnapshot` | `frozen_cut_between_parallel_calls_leaves_later_results_out`, `frozen_cut_keeps_an_earlier_exchanges_late_result_out` |
| Current iOS only: a compaction's continuation and idle note join its boundary: one work step mid-turn, one divider after the answer or the command; a continuation whose boundary is on an older page is a divider by itself. | `ChatStore.compactions`, `ChatLines.flatLines`; `ChatLinesTests` compaction tests | `compaction_continuation_joins_its_boundary_mid_turn`, `idle_compaction_after_answer_is_one_divider`, `compaction_continuation_alone_is_a_divider`, `manual_compaction_is_a_divider_after_the_command` |
| Current iOS only: supervision input (the hub's mark or its exact template) opens a round that lasts across recaps until the next input, and the user's queued input after it opens another turn. The same words from an app, from another conversation or under the user's sentinel are the user's. | `ChatBlock.build` (`supervisionRound`), `Injected.of`, `MessageSource.protectsUser`; `ChatSupervisionTests` | `queued_supervision_and_next_genuine_input_are_separate_turns`, `protected_supervision_text_stays_the_users_queued_input`, `supervision_round_keeps_same_entry_text_and_images`, `image_only_queued_input_after_supervision_opens_new_turn`, `recap_does_not_end_a_supervision_round` |

## JSON 结构

每个文件是 `{"schema_version": 1, "domain": "lines", "cases": [...]}`。每个用例都有唯一的英文 snake_case `name`、`description`、出处数组 `source`、`input` 和 `expected`。由原测试搬出的场景写 `from_test`，实现边界与缩小后的性质场景写 `derived: true`；两者不混用。

### 输入

`input.items` 使用 [协议的 Item](../../../docs/protocol/protocol.md) 字段名，按 transcript 顺序排列，`id`、`seq`、`role`、`kind` 必填；未给出的可选字段视为 `null`。同一 `seq` 内按 `id` 最后一个 `#` 后的数字排序，不按字符串排序。夹具不负责解析原始 `attachment/queued_command`：它已经被服务端规范化成 `queued: true` 的用户文本及图片。

支持 `text`、`thinking`、`tool_use`、`tool_result`、`notice`、`image`、`recap`、`fold`，未知 `kind` 按现有宽容解码处理。`tool_input`、`image`、`task`、`fold` 的嵌套结构直接采用线上结构；`task` 是 notice 的字段，不是独立 kind。图片只用编造的占位数据，不测试图片解码。

| 可选字段 | 含义与默认值 |
| --- | --- |
| `live_turn_id` | 当前进行中的 Turn ID，缺省为 `null`，对应 iOS 已从会话状态选出的轮，不根据工具结果自行推断 |
| `open_work` | 展开的行 ID 集合，缺省为空，既可以有 `w-…`，也可以有抽屉的 `dw-…`（no client draws a wake drawer any more: iOS since #2996, Android's port emits no `wakes` line） |
| `now` | 固定比较时钟，缺省为 `2026-01-01T00:01:00Z`，只用于工作行时长投影 |
| `menu_queries` | 要传给每行 `holds` 的原消息 ID，允许 `null`，结果按查询顺序排列 |
| `frozen` | `{seq, part, live_turn_id}`，按该位置截取 item 前缀并用冻结时的进行中轮重建 |
| `check_prefixes` | 为真时，对每个 item 前缀验证增量 Rows/Blocks 与全量结果相同，缺省为假 |
| `operations` | 有序的状态操作，存在时 `expected` 改为每次操作后的 `checkpoints` |
| `pending_interaction_ids` | 只用于位置用例的请求标识列表，不参与 items 折叠 |

`operations` 支持 `merge`、`replace`、`unfold` 和 `burst_replace`。前三者的 `items` 分别是新页、完整替换页和展开取回的原步骤；`unfold` 还需 `fold_id`。`burst_replace.batches` 按批合并，每批后让后台任务执行，再立即用 `items` 替换整段记录。每次操作可以覆盖 `open_work`、`live_turn_id`。对照器等待后台 Rows/Blocks 收敛，带明确超时。

### 期望输出

普通用例的 `expected.lines` 按阅读顺序排列，即最早到最新，iOS 倒置列表的渲染顺序不改变这个契约。`expected.turns` 是中间轮结构的核对信息；它有助于区分取答、图片归属与最终渲染规则。操作用例的 `expected.checkpoints` 按操作顺序保存同样结构，并额外列出 `item_ids`。

每行都有 `id`、`type`、`is_input`；提供 `menu_queries` 时还含 `holds` 布尔数组。内容行保留 `text`，其他内容通过 `item_ids` 或步骤里的 `item_id` 回指输入，不复制图片和工具输入等大字段。

| `type` | 内容与结构 |
| --- | --- |
| `user` / `reply` / `thinking` / `notice` / `image` / `recap` | 原条目行；recap 是独立的历史标记，不增加轮间分隔线 |
| `user_images` | `item_ids`，图片组在所属文本上方，ID 为 `img-` 加首图 ID |
| `work` | 过程步骤、计数、时间、展开状态和进行中状态，见下文 |
| `work_step` | `group_id`、`first` 和 `step`，ID 为组 ID 加 `/` 再加步骤 ID |
| `work_end` | `group_id` 和收起步数 `count`，ID 为组 ID 加 `-end` |
| `wakes` | `notice_ids`、`live` 及真正的 `drawer_lines`，ID 为 `wk-` 加 Turn ID。No case has such a line: iOS draws none since #2996, and the Android port emits none. |
| `task_note` | 原通知内容及 `task_note` 投影，包含标签、主题、结果、失败标记和回配的调用 ID |

`work` 的 `steps` 即折叠内容，展开时每个步骤另有一条 `work_step`，两者不重复计数。步骤类型包括 `tool`、`thinking`、`notice`、`task_note`、`narration`、`fold`。工具步骤保存实际结果 ID、结果文本、`running`、`failed`、图片数和可选的 `background_badge`；任务步骤沿用 `TaskNoteText` 的标签与结果；fold 保存代表计数和加载范围。

工作计数的含义：

- `tools`、`thoughts` 是步骤所代表的工具数和思考数，包含 fold 的计数。
- `known_failed_tools`、`unresolved_tools` 是当前可见工具卡中，错误标记为真和缺少结果的数量；`contains_fold` 为真时它们仅是下界，fold 没有提供失败数或缺结果数。
- `summary_full`、`summary_short` 是当前 iOS `WorkSummary` 的完整与简略文案；有 `wake: true` 的通知会让文案里的工具数加一，因此文案中的数字可能和实际 `tools` 不同（since #9140 a wake opens its own turn and is drawn before the work line, so no case has one inside a work line）。
- `live` 完全来自进行中轮和段位置，`open` 来自展开集合；`running` 只描述单张工具卡缺结果。

`start`、`end` 是折叠算法选出的时间戳，`elapsed_seconds` 和 `duration_text` 使用固定时钟按现有 `WorkLine.duration` 计算；这些是编造时间戳的规格值，不是性能测量。缺起点时分别为 `null` 和空字符串。完成段缺结束时间时，现有 View 使用当前时钟；对照器以 `now` 固定这条路径。

`separator_before` 只描述展开卡片内部的结构：首步骤没有分隔线，后续步骤及收起行有分隔线；不规定颜色、线宽、位置等像素。原始 iOS 代码分别来自 `WorkStepView` 和 `lineView` 的 `.workEnd` 分支。

`turns` 包含用户、过程、回复、轮尾、唤醒、抽屉和到达顺序的 ID 列表，以及 Turn 本身的计数和起止时间；`process_ids` 中的工具组使用 `tools-<首步骤 ID>`。Turn 统计发生在 View 的空思考过滤之前，不能直接拿 `thought_count` 当工作行的 `thoughts`。有任务通知时还输出 `task_index`，包含 `started`、通知到调用的 `calls`、描述、开始时间和按调用归组的状态序列。`wake_ids` is the wake that opened the turn (empty when a message did). Current iOS has no `after_wake_ids` since #2996; its turns carry `has_pending_wake`, false on every turn since #9140 (`spec/fixtures/lines/ios/differences.json`).

`layout.json` 额外输出 `footer`：请求 ID 顺序、`placement: "above_composer"`、`part_of_transcript: false`；不把请求交互和新的 `StatusLine` 塞进 transcript 行。

## 与设计文档的差异

- [总纲 2.3、D1、D4](../../../docs/archive/redesign/README.md) 和 [架构调研 A4/A5](../../../docs/archive/redesign/research/persona-ios-arch.md) 指定共享行为夹具，本目录固定的是当前 iOS 输出，没有重构生产代码。
- [UX 3.3](../../../docs/archive/redesign/ux.md) 要求折叠时显示失败数、缺结果记录，且耗时只取记录中的轮次时长；当前 `WorkSummary` 未显示这些计数，`WorkLine` 仍用时间戳差及当前时钟，本夹具保留现状，并另列已知失败和缺结果数供后续使用。
- 协议说空思考不显示，实际 `workItems` 只过滤独立 thinking，已并入 `.tools` 的空思考未经再次过滤；两个空思考用例分别固定这两条路径。
- [UX 3.2/5.1](../../../docs/archive/redesign/ux.md) 的统一状态行尚未进入当前 `ChatView`，本目录只覆盖工作行的 `live`，不提前定义新状态机。
- 总纲第 5 节提到权限卡挂记录末尾，而当前 `mainFooter` 把所有请求面板放在输入区；本夹具采用实际位置，与 UX 3.2/3.4 的当前规则一致。
- Swift 的 `String.count` 数扩展字素簇，Kotlin 实现取答时须注意复合表情，不能直接用 UTF-16 长度代替。

## Where current iOS differs

The corpus is the shared contract: Android's `LinesFixtureTest` replays every case against its own Kotlin port, which keeps `afterWake`, and compares complete objects. Current iOS differs from it in three ways. Since #1015, and by transcript entry since #2112, a user picture in an entry of its own is a message of its own that opens the next turn (`regular_user_images`, `tool_images_belong_to_call`). Since #2112 user pictures are a turn's input, not its process (`absorbed_user_images`, `photos_after_wake_stay_with_message`). Since #2996 there is no wake chip or drawer, so a turn has no `after_wake_ids` but `has_pending_wake`. Since #9140 a wake opens a turn of its own on both clients, so the wake cases agree again: no turn has a wake pending inside it, and none has a tail.

`spec/fixtures/lines/ios/differences.json` pins exactly these differences, each with why and the Apple code and test that assert it. `turn_wake_state` drops `after_wake_ids` from every turn and sets `has_pending_wake`, true only for the turns it lists (none since #9140); an entry of `cases` replaces whole keys of a case's `expected` (`replace`) or single fields of one of its turns (`turn_fields`). The file sits in a subdirectory, so Android's readers, which list only this directory's own `*.json` files, do not take it for a fixture, and the replay leaves it out of the iOS-only cases it reads there. The replay fails on an entry that changes nothing, on one the case passes without, and on one that names a case or turn the corpus does not have, so the file can only shrink. Moving the corpus to current iOS is a follow-up once Android's port follows; until then the corpus values stay as they are.

## Cases only current iOS asserts

The other `ios/*.json` files hold cases derived from current iOS that the shared corpus does not have: same-entry text→picture→text, subagent results that wake the agent (also after a recap), frozen cuts inside a tool run, compaction continuations, and genuine against protected supervision input. They have the corpus's structure with current iOS's turn shape (`has_pending_wake`, no `after_wake_ids` and no `wakes` lines), so `differences.json` does not apply to them. Each one's inputs follow the Apple tests its `source` names; its expected object is what the replay printed on current `apple/`, written down only after it showed what those tests assert. The replay also asserts those claims for each case (who opens which turn, where a wake or a continuation went), so a case written again from a later run cannot drift from them unnoticed. Every one sets `check_prefixes`.

They sit in the subdirectory so that Android does not replay them: `LinesFixtureTest` and `FixtureReader` list only this directory's own `*.json` files, and Harmony's `domain.test.mjs` reads named corpus cases. The Kotlin port keeps the corpus contract (`afterWake`), which these cases do not have. They move into the corpus, as they are, when the corpus moves to current iOS.

## 对照和复跑

The replay has no file of its own in the repository: the Python block below writes what it needs to a temporary directory under `/tmp`. It takes the Swift adapter from this README and extracts types and functions from the Apple sources by unique start and end markers; `apple/Shared/ChatStore.swift`, `apple/Persona/Views/Chat/ViewedImages.swift` and a few small `apple/Shared` files go to `swiftc` whole (those #7895 has moved into PersonaWire come with that package), and no source file is changed. `struct ChatLines` (`apple/Persona/Views/Chat/ChatLines.swift`), `ChatLine`, `WorkItem`, `WorkSummary` and `TaskNoteText` are taken verbatim; the adapter only hosts them and projects what they return, it does not reimplement any folding. The packages these sources import, `PersonaWire`, `PersonaKit` and `BeautifulUI` under `apple/Packages/`, are first compiled from their sources into static modules, nonisolated by default as SwiftPM builds them. The app's sources are compiled in Swift 6 mode with the settings `apple/project.yml` gives the app (`SWIFT_DEFAULT_ACTOR_ISOLATION: MainActor`, `SWIFT_APPROACHABLE_CONCURRENCY: YES`), so `ChatStore` keeps its concurrency. The app's `apple/Shared/Localization/Localizable.xcstrings` is compiled next to the binary, which runs in Simplified Chinese (`-AppleLanguages (zh-Hans)`), as the Apple tests do: the summaries and durations in the fixtures are the zh-Hans words.

The lines are built as an Agent's chat in a style without BeautifulUI draws them (`ChatView.lineBuilder` with only `openWork` set): no supervision or background-round folds, no chip runs, no child sessions, no feed. Only what those options or the views need is replaced, and none of it is compared: empty `Session`, `ChildSessions.Place`, `FeedItem`, `QuickResult` and `PlaceMapResult` types, a `ChildSessionLines.insert` and a `ToolRunSummary.diff` that trap if they are ever reached, the icon name `ToolPresentation.symbol`, and an `AppStyle` whose `usesBeautifulUI` is false (the fixtures pin the plain styles; Liquid Glass is the default). SwiftUI, scrolling, taps, image decoding and the real disk cache are not run; `CFFIXED_USER_HOME` keeps the cache in the temporary directory. The `view_source_order` cases check declaration order only and do not claim a UI test passed: `PendingCards(cards: pendingCards, …)` comes before `Composer(key:` in `ChatView.mainFooter`, `pendingCards` is built from `model.interactions(for: key)`, `PendingCards` draws an `InteractionPanel` for each card, and `ChatLines.swift` never reads interactions. The cards keep the given order while they fit; once they scroll, `PendingCards.urgentFirst` puts permissions first (#926), which these two cases do not cover. The separators are checked against the `first` flag and the view source.

The replay's output differs from the projection the fixtures were written against in three ways, each forced by the refactor:

- `Turn.afterWake` is gone: #2996 removed the wake chip and its drawer. Each turn reports `has_pending_wake` where the corpus has `after_wake_ids`; since #9140 a wake opens a turn of its own, so `Turn.hasPendingWake` and `Turn.tail` are gone too and the adapter writes `false` and an empty list to keep the shape. `wakes` lines and their `drawer_lines` no longer occur.
- The line kinds `ChatLines.build` gained since are projected as `LinesFixtureTest` on Android projects them: `viewed` (`image_ids`), `recaps` (`item_ids`) and `api_error` (`item_id`, `count`, `status`, `reason`); subagents launched one after another share an `agents` line (`group_id`, `call_ids`, `first`, `separator_before`, `alone`). A kind the replay's options cannot produce traps.
- The Swift runner prints what it built for each case, and the Python block compares that with `expected` and lists every difference instead of stopping at the first mismatching case.

The corpus's expected values are not rewritten to match: the replay compares each case with its `expected` after `spec/fixtures/lines/ios/differences.json` is applied ("Where current iOS differs"), and any difference fails it. The iOS-only cases are compared with their `expected` as it is.

四份指定测试的覆盖情况：

| 原测试 | 这里如何覆盖 |
| --- | --- |
| `ChatLineTests` 两个测试 | `structure.json` 固定稳定 ID、展开插入行、输入判定和菜单归属，对照时另检查行 ID 唯一及反转可还原 |
| `RebuildTests.incrementalRebuildEqualsAFullOne` | 对照器读取原测试的 LCG 和 session 生成器，验证 40 个种子、全部 6400 个前缀和每个种子前插历史后的等价性 |
| `RebuildTests.incrementalRebuildIsCheap` | 重现长会话的结果等价断言，不将单次耗时当性能基线，本车道未实测性能 |
| `RebuildTests.realSessionsRebuildIncrementally` | 未读取私有语料；前缀与前插历史规则用编造序列验证，不声称覆盖真实会话内容 |
| `RebuildTests.backgroundRebuildConvergesAfterBurstsAndReplacement` | 夹具保留缩小场景，对照器另用原生成器重现突发和替换，带超时等待收敛 |
| `TaskNoteTests` 两个测试 | `tasks.json` 固定调用关联、事件主题、徽标、退出码和时间文案、中断标题的行为 |
| `FoldedWorkTests` 三个测试 | `folds.json` 实际调用 `merge` 和 `unfold`，逐个检查状态快照和最终步骤 |

Run the Python block from the repository root, for example `` sed -n '/^```python$/,/^```$/{/^```/d;p;}' spec/fixtures/lines/README.md | python3 - ``. It needs macOS with Xcode (Swift 6.2 or later, for `-default-isolation` and the packages' tools version, and `xcstringstool`) and Python 3. Each compile, each fixture file and the property check have a timeout. `-disable-sandbox` only keeps Swift's macro plugins from starting a sandbox of their own; the outer environment's limits still apply.

```python
from pathlib import Path
import subprocess
import copy
import os
import json
import re
import tempfile

root = Path.cwd()
tmp = Path(tempfile.mkdtemp(prefix='spec-lines-', dir='/tmp'))
readme = (root / 'spec/fixtures/lines/README.md').read_text()
(tmp / 'Runner.swift').write_text(readme.split('```swift\n', 1)[1].split('```', 1)[0])


def between(path, start, end):
    text = (root / path).read_text()
    assert text.count(start) == 1, start
    begin = text.index(start)
    finish = text.index(end, begin)
    return text[begin:finish]


def swiftc(*args):
    subprocess.run(['swiftc', '-parse-as-library', '-swift-version', '6', '-module-cache-path', str(tmp / 'module-cache'),
                    '-disable-sandbox', '-I', str(tmp), *args], check=True, timeout=300)


# The packages the app's sources import, each a module of its own, nonisolated by default as SwiftPM builds them.
for package in ['PersonaWire', 'PersonaKit', 'BeautifulUI']:
    sources = sorted(str(p) for p in (root / 'apple/Packages' / package / 'Sources').rglob('*.swift'))
    swiftc('-module-name', package, '-emit-module', '-emit-module-path', str(tmp / (package + '.swiftmodule')),
           '-emit-library', '-static', '-o', str(tmp / ('lib' + package + '.a')), *sources)

model = 'apple/Shared/Models.swift'
view = 'apple/Persona/Views/Chat/ChatView.swift'
lines = 'apple/Persona/Views/Chat/ChatLines.swift'
parts = ['import Foundation\nimport PersonaWire\nimport PersonaKit\nimport BeautifulUI\n']
parts += [between(model, 'nonisolated struct MachineRef:', 'nonisolated struct MachineEngine:'),
          between(model, 'nonisolated enum SessionStatus:', '/// New sources opt in to every operation;'),
          between(model, 'nonisolated struct SessionKey:', 'nonisolated enum InteractionKind:')]
parts += [between(view, 'struct ChatLine: Identifiable {', '/// One step of an open work line,'),
          between(view, 'nonisolated struct WorkSummary: Equatable {', 'struct WorkLine: View, Equatable {')]
parts += [between(lines, 'struct ChatLines {', '/// A file a work line changed'),
          between(lines, 'nonisolated struct ChangedFile:', 'extension ChatLines {')]
parts += ['enum ChatView {\n' + between(view, '    static func prefixCount(', '\n    #if DEBUG') + '\n}\n',
          'enum WorkLine {\n' + between(view, '    static func duration(from start:', '\n}\n\n\n/// What the closeout card') + '\n}\n']
parts += [between('apple/Persona/Views/Chat/TaskNoteRow.swift', 'enum TaskNoteText {', '/// A background task reporting back,'),
          'enum SubagentCard {\n' + between('apple/Persona/Views/Chat/SubagentCard.swift', '    nonisolated static func isAgent(', '    @State private var unfold') + '}\n',
          'enum ToolPresentation {\n' + between('apple/Persona/Views/Chat/ToolViews.swift', '    nonisolated static func baseName(', '    static func symbol(')
          + '    static func symbol(_ name: String) -> String { "" }\n}\n']
parts += ['nonisolated struct APIClient {\n' + between('apple/Shared/APIClient.swift', '    enum MessageVia: String, Codable, Sendable {', '    struct SendBody: Encodable {') + '}\n',
          between('apple/Shared/ChatStore+SendFeedback.swift', 'extension ChatStore {', '    /// Only fresh session changes terminate') + '}\n',
          between('apple/Shared/HubNaming.swift', 'nonisolated enum HubNaming {', '    static func accepts(scheme:') + '}\n',
          between('apple/Shared/NativeStateMigration.swift', 'nonisolated enum LegacyNames {', '/// A non-destructive bridge')]
parts += ['struct RebuildTests {\n' + between('apple/PersonaTests/RebuildTests.swift', '    struct LCG {', '    @Test func incrementalRebuildEqualsAFullOne()') + '\n}\n']
parts += ['''
// Reached only with child sessions, BeautifulUI's chip runs or the assistant's feed, all off here: a call traps.
struct Session {}
enum ChildSessions { struct Place {} }
enum ChildSessionLines {
    static func insert(_ lines: [ChatLine], places: [ChildSessions.Place], sessions: [String: Session], complete: Bool) -> [ChatLine] {
        fatalError("child sessions are not part of the lines fixtures")
    }
}
enum ToolRunSummary { static func diff(_ c: ToolCall) -> (adds: Int, dels: Int) { fatalError("chip runs are off") } }
struct FeedItem { var id: String }
struct QuickResult { var callID: String }
struct PlaceMapResult { var callID: String }
// The fixtures pin the styles without BeautifulUI's components: Liquid Glass, the default, and the others but Beautiful.
nonisolated enum AppStyle { case glass; static let current = AppStyle.glass; var usesBeautifulUI: Bool { false } }
''']
(tmp / 'Extracted.swift').write_text('\n'.join(parts))
# #7895 moves leaf files from apple/Shared into PersonaWire, built above: those come with the package.
wire = root / 'apple/Packages/PersonaWire/Sources/PersonaWire'
shared = ['apple/Shared/%s.swift' % name for name in ['ChatStore', 'TextMemo', 'ChatSendFeedback', 'LiveText', 'CommandAsk',
          'Injected', 'MessageSource', 'ModelFallback', 'Closeout'] if not (wire / (name + '.swift')).exists()]
swiftc('-default-isolation', 'MainActor', '-enable-upcoming-feature', 'NonisolatedNonsendingByDefault',
       '-enable-upcoming-feature', 'InferIsolatedConformances', '-enable-bare-slash-regex',
       *shared, 'apple/Persona/Views/Chat/ViewedImages.swift',
       str(tmp / 'Extracted.swift'), str(tmp / 'Runner.swift'), '-L', str(tmp),
       '-lPersonaWire', '-lPersonaKit', '-lBeautifulUI', '-o', str(tmp / 'check'))
# The app's words, as a phone set to Simplified Chinese reads them.
subprocess.run(['xcrun', 'xcstringstool', 'compile', 'apple/Shared/Localization/Localizable.xcstrings',
                '--output-directory', str(tmp)], check=True, timeout=120)

view_source = (root / view).read_text()
assert '.overlay(alignment: .top) { if !first { WorkCardSlice.separator } }' in view_source
assert '.overlay(alignment: .top) { WorkCardSlice.separator }' in view_source
runtime = tmp / 'runtime'
runtime.mkdir()
env = dict(os.environ, CFFIXED_USER_HOME=str(runtime))
zh = ['-AppleLanguages', '(zh-Hans)']
names = set()
all_cases = {}
failed = []
# Where current iOS differs from the corpus ("Where current iOS differs" above), applied to `expected` before comparing.
overlay_path = root / 'spec/fixtures/lines/ios/differences.json'
overlay = json.loads(overlay_path.read_text())
assert overlay['schema_version'] == 1 and overlay['domain'] == 'lines'
assert overlay_path.read_text() == json.dumps(overlay, ensure_ascii=False, indent=2) + '\n'
wake = overlay.get('turn_wake_state')
pending = {(p['case'], p.get('checkpoint'), p['turn']) for p in (wake['pending'] if wake else [])}
patches = {entry['name']: entry for entry in overlay['cases']}
assert len(patches) == len(overlay['cases'])
seen_pending = set()
# The cases only current iOS asserts ("Cases only current iOS asserts" above): current iOS's shape, compared as they are.
ios_only = sorted(p for p in overlay_path.parent.glob('*.json') if p != overlay_path)
assert ios_only
ios_names = set()


def ios_expected(case, patched=True):
    """The case's `expected` as current iOS draws it. An overlay entry that changes nothing is stale and fails."""
    expected = copy.deepcopy(case['expected'])
    checkpoints = 'checkpoints' in expected
    for k, state in enumerate(expected.get('checkpoints', [expected])):
        for turn in state['turns'] if wake else []:
            assert wake['drop'] in turn and wake['set'] not in turn, 'stale turn_wake_state: ' + case['name']
            del turn[wake['drop']]
            key = (case['name'], k if checkpoints else None, turn['id'])
            turn[wake['set']] = key in pending
            seen_pending.add(key)
    entry = patches.get(case['name']) if patched else None
    if entry:
        assert set(entry) <= {'name', 'why', 'source', 'replace', 'turn_fields'} and entry['why'] and entry['source']
        for key, value in entry.get('replace', {}).items():
            assert key in expected and expected[key] != value, 'stale replace: %s.%s' % (case['name'], key)
            expected[key] = value
        for turn_id, fields in entry.get('turn_fields', {}).items():
            turns = [t for t in expected.get('turns', []) if t['id'] == turn_id]
            assert len(turns) == 1, 'no turn %s in %s' % (turn_id, case['name'])
            for key, value in fields.items():
                assert key in turns[0] and turns[0][key] != value, 'stale turn_fields: %s[%s].%s' % (case['name'], turn_id, key)
                turns[0][key] = value
    return expected


def check_source(sources):
    """Each `path: Name.name / name (note)` a case cites: the file is there, and every name in it."""
    for source in sources:
        path, _, names = source.partition(': ')
        text = (root / path).read_text()
        for name in re.sub(r'\([^)]*\)', '', names).split('/'):
            for word in (name.split() or [''])[0].split('.'):
                assert not re.fullmatch('[A-Za-z_][A-Za-z0-9_]*', word) or word in text, 'stale source: ' + source


check_source(wake['source'] if wake else [])
for entry in overlay['cases']:
    check_source(entry['source'])


def check_lines(lines):
    ids = [line['id'] for line in lines]
    assert len(ids) == len(set(ids))
    assert list(reversed(list(reversed(ids)))) == ids
    for line in lines:
        if 'drawer_lines' in line:
            check_lines(line['drawer_lines'])


def diff(path, want, got, out):
    """Where `got` differs from `want`, exactly (true is not 1)."""
    if type(want) is not type(got):
        out.append('%s: expected %s, actual %s' % (path, json.dumps(want, ensure_ascii=False), json.dumps(got, ensure_ascii=False)))
    elif isinstance(want, dict):
        for key in sorted(set(want) | set(got)):
            if key not in got:
                out.append('%s.%s: missing, expected %s' % (path, key, json.dumps(want[key], ensure_ascii=False)))
            elif key not in want:
                out.append('%s.%s: not expected, actual %s' % (path, key, json.dumps(got[key], ensure_ascii=False)))
            else:
                diff(path + '.' + key, want[key], got[key], out)
    elif isinstance(want, list):
        ids = [[x.get('id') if isinstance(x, dict) else None for x in side] for side in (want, got)]
        if None in ids[0] + ids[1]:
            if len(want) != len(got):
                out.append('%s: expected %s, actual %s' % (path, json.dumps(want, ensure_ascii=False), json.dumps(got, ensure_ascii=False)))
                return
            pairs = list(zip(range(len(want)), want, got))
        else:
            if ids[0] != ids[1]:
                out.append('%s: ids expected %s, actual %s' % (path, ids[0], ids[1]))
            pairs = [(x, want[i], got[ids[1].index(x)]) for i, x in enumerate(ids[0]) if x in ids[1]]
        for i, w, g in pairs:
            diff('%s[%s]' % (path, i), w, g, out)
    elif want != got:
        out.append('%s: expected %s, actual %s' % (path, json.dumps(want, ensure_ascii=False), json.dumps(got, ensure_ascii=False)))


for path in sorted((root / 'spec/fixtures/lines').glob('*.json')) + ios_only:
    current = path in ios_only
    data = json.loads(path.read_text())
    assert data['schema_version'] == 1 and data['domain'] == 'lines'
    assert path.read_text() == json.dumps(data, ensure_ascii=False, indent=2) + '\n'
    for case in data['cases']:
        assert re.fullmatch('[a-z][a-z0-9_]*', case['name'])
        assert case['name'] not in names
        names.add(case['name'])
        if current:
            ios_names.add(case['name'])
        assert bool(case.get('from_test')) != bool(case.get('derived'))
        assert case['description'] and case['source']
        check_source(case['source'])
        if case.get('from_test'):  # a derived case names no test; its sources are still checked above
            assert any(case['from_test'] in (root / s.partition(': ')[0]).read_text() for s in case['source'])
        all_cases[case['name']] = case
        for state in case['expected'].get('checkpoints', [case['expected']]):
            check_lines(state['lines'])
    run = subprocess.run([str(tmp / 'check'), str(path), *zh], env=env, check=True, timeout=60, capture_output=True, text=True)
    actual = [json.loads(line) for line in run.stdout.splitlines()]
    assert [a['name'] for a in actual] == [case['name'] for case in data['cases']]
    for case, got in zip(data['cases'], actual):
        want = copy.deepcopy(case['expected']) if current else ios_expected(case)
        for state in want.get('checkpoints', [want]) + got['actual'].get('checkpoints', [got['actual']]):
            check_lines(state['lines'])
        out = []
        diff(case['name'], want, got['actual'], out)
        if out:
            failed.append(case['name'])
            print('不匹配：%s/%s\n  %s' % (path.name, case['name'], '\n  '.join(out)))
        if case['name'] in patches and not current:
            # An entry must change the case it names. Evaluate this even when the
            # patched comparison passes: an entry that has become unnecessary is stale.
            unpatched = []
            diff(case['name'], ios_expected(case, patched=False), got['actual'], unpatched)
            if not unpatched:
                failed.append(case['name'])
                print('  stale: the case passes without its entry in ios/differences.json')

closed = all_cases['closed_work_identity']['expected']['lines']
opened = all_cases['open_work_identity']['expected']['lines']
assert [line['id'] for line in closed] == [opened[i]['id'] for i in [0, 1, 4]]
assert all_cases['menu_owner_per_message']['expected']['lines'][0]['holds'] == [True, False, False]
assert all_cases['fold_counts_tools_and_thoughts']['expected']['turns'][0]['tool_count'] == 3
assert all_cases['task_notices_find_calls']['expected']['task_index']['calls'] == {'7#0': 'm', '8#0': 'b', '9#0': 'm'}
assert pending <= seen_pending, 'turn_wake_state names no turn of the corpus: %s' % sorted(map(str, pending - seen_pending))
assert set(patches) <= names - ios_names, 'ios/differences.json names no case of the corpus: %s' % sorted(set(patches) - (names - ios_names))
# What the Apple tests the iOS-only cases cite assert, so a case written again from a new run cannot drift from them.
for name, split in {'same_entry_text_image_text_is_one_message': [['1#0'], ['3#0', '3#2', '3#1']],
                    'queued_entry_text_and_images_join_the_running_turn': [['1#0', '3#0', '3#1', '3#2']],
                    'queued_supervision_and_next_genuine_input_are_separate_turns': [['1#0'], ['3#0'], ['5#0', '7#0']],
                    'protected_supervision_text_stays_the_users_queued_input': [['1#0', '3#0', '5#0', '7#0']],
                    'supervision_round_keeps_same_entry_text_and_images': [['1#0'], ['3#0', '3#2', '3#1'], ['5#0']],
                    'image_only_queued_input_after_supervision_opens_new_turn': [['1#0'], ['4#0', '4#1']],
                    'recap_does_not_end_a_supervision_round': [['1#0'], [], ['5#0']]}.items():
    assert [t['user_ids'] for t in all_cases[name]['expected']['turns']] == split, name
for name, gone, index, turn in [('agent_result_wake_opens_its_own_exchange', '5#0', 0, {'wake_ids': [], 'answer_id': '4#0'}),
                                ('agent_result_wake_opens_its_own_exchange', '5#0', 1, {'user_ids': [], 'wake_ids': ['5#0'], 'answer_id': '6#0'}),
                                ('agent_result_wake_pending_keeps_previous_answer', '5#0', 0, {'answer_id': '4#0'}),
                                ('agent_result_wake_pending_keeps_previous_answer', '5#0', 1, {'wake_ids': ['5#0'], 'answer_id': None}),
                                ('agent_result_wake_after_recap_follows_the_recap', '6#0', 0, {'answer_id': '4#0'}),
                                ('agent_result_wake_after_recap_follows_the_recap', '6#0', 1, {'wake_ids': ['6#0'], 'answer_id': '7#0'}),
                                ('orphan_agent_result_wake_draws_no_notice', '10#0', 0, {'user_ids': [], 'wake_ids': ['10#0']}),
                                ('compaction_continuation_joins_its_boundary_mid_turn', '5#0', 0, {'answer_id': '8#0'}),
                                ('idle_compaction_after_answer_is_one_divider', '6#0', 0, {'answer_id': '4#0'}),
                                ('manual_compaction_is_a_divider_after_the_command', '3#0', 0, {'user_ids': ['1#0']})]:
    expected = all_cases[name]['expected']
    assert all(expected['turns'][index][key] == value for key, value in turn.items()), name
    assert all(gone not in [line['id'], line.get('step', {}).get('id')] + [s['id'] for s in line.get('steps', [])] for line in expected['lines']), name
subprocess.run([str(tmp / 'check'), '--properties', *zh], env=env, check=True, timeout=60)
assert not failed, '%d cases do not match: %s' % (len(failed), ', '.join(failed))
print(f'全部通过：{len(names) - len(ios_names)} 个夹具和 {len(ios_names)} 个仅当前 iOS 的用例；临时对照器：{tmp}')
```

临时 Swift 适配片段如下，由上面的入口提取；不单独保存到仓库。

```swift
import Foundation
import PersonaWire

func stamp(_ d: Date?) -> Any { d.map { ISO8601DateFormatter().string(from: $0) } ?? NSNull() }
func nullable<T>(_ x: T?) -> Any { x.map { $0 as Any } ?? NSNull() }

struct Projection {
    let index: TaskIndex
    let builder: ChatLines
    let now: Date
    let menuIDs: [String?]

    func note(_ it: Item) -> [String: Any] {
        guard let n = it.task else { return [:] }
        let call = index.calls[it.id]
        return ["label": TaskNoteText.label(n),
                "subject": TaskNoteText.subject(n, description: call.flatMap { index.descriptions[$0] }),
                "outcome": nullable(TaskNoteText.outcome(n, startedAt: call.flatMap { index.startedAt[$0] }, at: it.ts)),
                "failed": n.failed, "call_id": nullable(call)]
    }
    func row(_ r: ChatRow) -> [String: Any] {
        var type: String
        var it: Item?
        switch r {
        case .user(let i): type = "user"; it = i
        case .assistant(let i): type = "reply"; it = i
        case .thinking(let i): type = "thinking"; it = i
        case .notice(let i): type = i.task != nil && i.wake != true ? "task_note" : "notice"; it = i
        case .image(let i): type = "image"; it = i
        case .recap(let i): type = "recap"; it = i
        case .tools: type = "tools"
        }
        var out: [String: Any] = ["type": type, "id": r.id]
        if let it { out["text"] = nullable(it.text) }
        if let it, type == "task_note" { out["task_note"] = note(it) }
        return out
    }
    func step(_ work: WorkItem) -> [String: Any] {
        switch work {
        case .text(let i): return ["type": "narration", "id": work.id, "item_id": i.id, "text": nullable(i.text)]
        case .step(.call(let c)):
            var out: [String: Any] = ["type": "tool", "id": c.id, "call_id": nullable(c.use.toolUseID),
                "name": c.name, "result_id": nullable(c.result?.id), "result_text": nullable(c.result?.text),
                "running": c.isRunning, "failed": c.isError, "image_count": c.images.count]
            if let id = c.use.toolUseID, index.started.contains(id) || index.notes[id] != nil {
                let badge = TaskNoteText.badge(index.notes[id] ?? [])
                out["background_badge"] = ["text": badge.text, "failed": badge.failed]
            }
            return out
        case .step(.folded(let f)):
            return ["type": "fold", "id": f.id, "tools": f.fold?.tools ?? 0, "thoughts": f.fold?.thoughts ?? 0,
                "items": f.fold?.items ?? 0, "through_seq": nullable(f.fold?.throughSeq),
                "through_id": nullable(f.fold?.throughID), "until": stamp(f.fold?.until)]
        case .step(.thought(let i)):
            var out: [String: Any] = ["type": i.kind == .notice ? (i.task != nil && i.wake != true ? "task_note" : "notice") : "thinking",
                "id": i.id, "text": nullable(i.text)]
            if i.task != nil && i.wake != true { out["task_note"] = note(i) }
            return out
        }
    }
    func line(_ l: ChatLine) -> [String: Any] {
        var out: [String: Any] = ["id": l.id, "is_input": l.isInput]
        if !menuIDs.isEmpty { out["holds"] = menuIDs.map { l.holds($0) } }
        switch l.content {
        case .row(let r): out.merge(row(r)) { _, new in new }
        case .userImages(_, let items): out["type"] = "user_images"; out["item_ids"] = items.map(\.id)
        case .recaps(_, let items): out["type"] = "recaps"; out["item_ids"] = items.map(\.id)
        case .viewed(_, let images): out["type"] = "viewed"; out["image_ids"] = images.map(\.id)
        case .apiError(let item, let info, let count):
            out.merge(["type": "api_error", "item_id": item.id, "count": count, "status": nullable(info.status),
                "reason": info.reason]) { _, new in new }
        case .work(let id, let items, let live, let start, let end):
            let summary = WorkSummary(items)
            let at = live ? now : (end ?? now)
            var tools = 0, thoughts = 0, failures = 0, unresolved = 0, folded = false
            for i in items {
                switch i {
                case .step(let s):
                    tools += s.tools
                    if case .thought(let t) = s { if t.kind != .notice { thoughts += 1 } }
                    if case .folded = s { thoughts += s.thoughts; folded = true }
                    if case .call(let c) = s { if c.isError { failures += 1 }; if c.isRunning { unresolved += 1 } }
                default: break
                }
            }
            out.merge(["type": "work", "live": live, "open": builder.isOpen(id, live: live),
                "start": stamp(start), "end": stamp(end),
                "elapsed_seconds": start.map { max(0, Int(at.timeIntervalSince($0))) } as Any? ?? NSNull(),
                "duration_text": WorkLine.duration(from: start, to: at),
                "tools": tools, "thoughts": thoughts, "known_failed_tools": failures,
                "unresolved_tools": unresolved, "contains_fold": folded,
                "summary_full": summary.full, "summary_short": summary.short,
                "steps": items.map(step)]) { _, new in new }
        case .workStep(let group, let item, let first):
            out.merge(["type": "work_step", "group_id": group, "first": first,
                "separator_before": !first, "step": step(item)]) { _, new in new }
        case .agents(let group, let calls, let first, let alone):
            out.merge(["type": "agents", "group_id": group, "call_ids": calls.map(\.id), "first": first,
                "separator_before": !first, "alone": alone]) { _, new in new }
        case .workEnd(let group, let count, _):
            out.merge(["type": "work_end", "group_id": group, "count": count, "separator_before": true]) { _, new in new }
        case .supervision, .supervisionInput, .agentOut, .toolRun, .feed, .quick, .placeMap, .day, .answerPart:
            fatalError("ChatLines.build made \(l.id), which needs an option the replay leaves off")
        }
        return out
    }
    func turn(_ t: Turn) -> [String: Any] {
        ["id": t.id, "user_ids": t.users.map(\.id), "process_ids": t.process.map(\.id),
         "answer_id": nullable(t.answer?.id), "trailing_ids": t.trailing.map(\.id),
         // A turn has no pending wake and no tail since #9140 (a wake opens its own turn); the keys keep the corpus's shape.
         "wake_ids": t.wakes.map(\.id), "has_pending_wake": false, "tail_ids": [String](),
         "ordered_ids": t.ordered.map(\.id), "tool_count": t.toolCount, "thought_count": t.thoughtCount,
         "interim_count": t.interimCount, "start": stamp(t.startedAt), "end": stamp(t.endedAt)]
    }
}

@main struct Runner {
    static func decode<T: Decodable>(_ value: Any, _ type: T.Type) throws -> T {
        try JSON.decoder.decode(type, from: JSONSerialization.data(withJSONObject: value))
    }
    @MainActor static func snapshot(_ items: [Item], _ input: [String: Any]) throws -> [String: Any] {
        var source = items
        var live = input["live_turn_id"] as? String
        if let f = input["frozen"] as? [String: Any], let seq = f["seq"] as? Int, let part = f["part"] as? Int {
            let cut = ChatView.prefixCount(source) { ($0.seq, $0.part) <= (seq, part) }
            source = Array(source[..<cut]); live = f["live_turn_id"] as? String
        }
        let rows = ChatStore.buildRows(source), blocks = ChatBlock.build(rows)
        if input["check_prefixes"] as? Bool == true {
            var settled: ChatStore.SettledRows?
            for n in 0...source.count {
                let prefix = Array(source.prefix(n))
                let built = ChatStore.rebuild(prefix, settled: settled)
                let full = ChatStore.buildRows(prefix)
                precondition(built.rows == full && built.blocks == ChatBlock.build(full), "incremental mismatch at \(n)")
                settled = built.settled
            }
        }
        // An Agent's chat in a style without BeautifulUI: nothing folds but work lines (`ChatView.lineBuilder`).
        let builder = ChatLines(openWork: Set(input["open_work"] as? [String] ?? []))
        let now = JSON.parseDate(input["now"] as? String ?? "2026-01-01T00:01:00Z")!
        let queries = (input["menu_queries"] as? [Any] ?? []).map { $0 as? String }
        let p = Projection(index: TaskIndex.build(source), builder: builder, now: now, menuIDs: queries)
        let turns = blocks.compactMap { if case .turn(let t) = $0 { t } else { nil } }
        var out: [String: Any] = ["lines": builder.build(blocks, live: live).map(p.line), "turns": turns.map(p.turn)]
        if source.contains(where: { $0.task != nil }) {
            out["task_index"] = ["started": p.index.started.sorted(), "calls": p.index.calls,
                "descriptions": p.index.descriptions, "started_at": p.index.startedAt.mapValues(stamp),
                "statuses": p.index.notes.mapValues { $0.map { nullable($0.status) } }]
        }
        return out
    }
    @MainActor static func run(_ input: [String: Any]) async throws -> [String: Any] {
        let items = try decode(input["items"] ?? [], [Item].self)
        guard let operations = input["operations"] as? [[String: Any]] else { return try snapshot(items, input) }
        let store = ChatStore(key: SessionKey(machineID: "fixture", sessionID: UUID().uuidString))
        store.merge(items)
        var checkpoints: [[String: Any]] = []
        for op in operations {
            let added = try decode(op["items"] ?? [], [Item].self)
            switch op["op"] as? String {
            case "merge": store.merge(added, backlog: true, hasMore: false)
            case "burst_replace":
                for batch in op["batches"] as! [[Any]] {
                    store.merge(try decode(batch, [Item].self))
                    await Task.yield()
                }
                store.replace(with: added, hasMore: false, span: ItemSpan(history: "replacement", to: added.last?.seq))
            case "replace": store.replace(with: added, hasMore: false, span: ItemSpan(history: "replacement", to: added.last?.seq))
            case "unfold":
                let f = store.items.first { $0.id == op["fold_id"] as? String }!
                store.unfold(f, into: added)
            default: fatalError("unknown operation")
            }
            let deadline = ContinuousClock.now + .seconds(5)
            let expectedRows = ChatStore.buildRows(store.items)
            while (store.rows != expectedRows || store.blocks != ChatBlock.build(expectedRows)) && ContinuousClock.now < deadline {
                try await Task.sleep(for: .milliseconds(5))
            }
            precondition(store.rows == expectedRows && store.blocks == ChatBlock.build(expectedRows), "background rebuild timeout")
            var options = input
            for key in ["open_work", "live_turn_id"] { if let value = op[key] { options[key] = value } }
            var state = try snapshot(store.items, options)
            state["item_ids"] = store.items.map(\.id)
            checkpoints.append(state)
        }
        return ["checkpoints": checkpoints]
    }
    /// The pending cards sit in `mainFooter` above the composer, in the order the model lists them while they fit
    /// (`PendingCards.urgentFirst` reorders them once they scroll); the lines are never built from them.
    static func footerOrder() throws {
        func read(_ path: String) throws -> String { try String(contentsOfFile: path, encoding: .utf8) }
        func body(_ text: String, _ start: String, _ end: String) -> String {
            precondition(text.components(separatedBy: start).count == 2, start)
            return String(text.components(separatedBy: start)[1].components(separatedBy: end)[0])
        }
        let view = try read("apple/Persona/Views/Chat/ChatView.swift")
        let footer = body(view, "private var mainFooter: some View {", "private var pendingRecap:")
        let cards = footer.range(of: "PendingCards(cards: pendingCards, room: footerRoom)")!
        precondition(cards.lowerBound < footer.range(of: "Composer(key:")!.lowerBound)
        precondition(body(view, "private var pendingCards: [PendingCards.Card] {", "#if DEBUG").contains("model.interactions(for: key).compactMap"))
        let panel = body(try read("apple/Persona/Views/Chat/PendingCards.swift"), "ForEach(over ? Self.urgentFirst(cards) : cards) { card in", "}")
        precondition(panel.contains("InteractionPanel(interaction: card.interaction"))
        let lines = try read("apple/Persona/Views/Chat/ChatLines.swift")
        precondition(!lines.contains("InteractionPanel") && !lines.contains("interactions(for:"))
    }
    @MainActor static func main() async throws {
        let args = CommandLine.arguments
        if args[1] == "--properties" {
            var prefixes = 0
            for seed in UInt64(1)...40 {
                let all = RebuildTests.session(seed, count: 160)
                var settled: ChatStore.SettledRows?
                for n in 1...all.count {
                    let items = Array(all.prefix(n)), rows = ChatStore.buildRows(items)
                    let built = ChatStore.rebuild(items, settled: settled)
                    precondition(built.rows == rows && built.blocks == ChatBlock.build(rows))
                    settled = built.settled; prefixes += 1
                }
                let older = RebuildTests.session(seed + 1000, count: 30).map { it in
                    var it = it; it.id = "old-" + it.id; it.toolUseID = it.toolUseID.map { "old-" + $0 }; return it
                }
                let built = ChatStore.rebuild(older + all, settled: settled)
                let rows = ChatStore.buildRows(older + all)
                precondition(built.rows == rows && built.blocks == ChatBlock.build(rows))
            }
            var large: [Item] = []
            var seq = 0
            for t in 0..<1000 {
                seq += 1; large.append(RebuildTests.item(seq, "user", .text, text: "第 \(t) 轮"))
                for _ in 0..<6 {
                    seq += 1; large.append(RebuildTests.item(seq, "assistant", .toolUse, tool: "t\(seq)"))
                    seq += 1; large.append(RebuildTests.item(seq, "user", .toolResult, text: "ok", tool: "t\(seq - 1)"))
                }
                seq += 1; large.append(RebuildTests.item(seq, "assistant", .text, text: "答"))
            }
            let settled = ChatStore.rebuild(large, settled: nil).settled
            seq += 1; large.append(RebuildTests.item(seq, "assistant", .thinking, text: "又想"))
            precondition(ChatStore.rebuild(large, settled: settled).rows == ChatStore.buildRows(large))
            let store = ChatStore(key: SessionKey(machineID: "fixture", sessionID: UUID().uuidString))
            let first = RebuildTests.session(8, count: 1800)
            for end in stride(from: 30, through: first.count, by: 30) {
                store.merge(Array(first[(end - 30)..<end])); await Task.yield()
            }
            var replacement = RebuildTests.session(91, count: 400)
            for i in replacement.indices { replacement[i].id = "replacement-" + replacement[i].id }
            store.replace(with: replacement, hasMore: false, span: ItemSpan(history: "new", to: 400))
            let rows = ChatStore.buildRows(store.items)
            let deadline = ContinuousClock.now + .seconds(5)
            while (store.rows != rows || store.blocks != ChatBlock.build(rows)) && ContinuousClock.now < deadline {
                try await Task.sleep(for: .milliseconds(5))
            }
            precondition(store.rows == rows && store.blocks == ChatBlock.build(rows))
            precondition(store.items.allSatisfy { $0.id.hasPrefix("replacement-") })
            print("通过：\(prefixes) 个增量前缀、40 次前插历史、长会话等价和后台替换收敛")
            return
        }
        let fixtures = try JSONSerialization.jsonObject(with: Data(contentsOf: URL(fileURLWithPath: args[1]))) as! [String: Any]
        // One line per case, its name and what the replay drew; the Python entry compares it with `expected`.
        for c in fixtures["cases"] as! [[String: Any]] {
            var actual = try await run(c["input"] as! [String: Any])
            if c["verification"] as? String == "view_source_order" {
                try footerOrder()
                let input = c["input"] as! [String: Any]
                actual["footer"] = ["interaction_ids": input["pending_interaction_ids"]!, "placement": "above_composer", "part_of_transcript": false]
            }
            let line = try JSONSerialization.data(withJSONObject: ["name": c["name"]!, "actual": actual], options: [.sortedKeys])
            print(String(decoding: line, as: UTF8.self))
        }
    }
}
```

## 新增用例

先定位 iOS 函数或现有测试，确定要固定的分支，再往对应主题文件追加用例；新主题可以增加 JSON 文件，保持同一结构。输入只用编造内容，图片、任务和调用标识也如此。

用例名在本领域内唯一；直接移植测试保留 `from_test`，新边界写 `derived: true` 并注明真实函数。先依据原断言或实现填写预期，再运行上面的对照，不要为了让测试通过而自动覆盖整份 `expected`。涉及多次到达的行为用 `operations` 保存每一步快照，展开行为同时检查标题、原行和新增步骤的 ID。

修改后补充规则表，重跑 Swift 对照、JSON 结构与唯一性检查，再执行仓库隐私扫描。需要未来新行为时另定规格版本，保留当前 iOS 差异说明。

A case that current iOS asserts but Android's port does not follow yet goes into an `ios/<topic>.json` file in current iOS's shape, with `check_prefixes` set, not into the corpus. Write its inputs from the Apple test or code it cites, run the replay, and keep the printed object only once it shows what that test asserts; add that claim to the replay's checks of the iOS-only cases.
