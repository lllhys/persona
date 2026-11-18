# 会议里 Persona 的回声

Persona 在会议里把答案说出来时，录音的麦克风也会听到；转写分不清是谁在说。一句转写只有**时间和文字都对上**才算 Persona 的回声：时间上和 Persona 说话的区间重叠，文字是 Persona 在这一段里说的话。规则见 [docs/assistant/meeting-minutes.md「Persona's echo」](../../../docs/assistant/meeting-minutes.md)（#5905）。回声行标记 `echo` 保留，不删除；纪要、录音摘要、会后对话和实时记录都不用它，手机的转写里也不显示。

| 文件 | 覆盖内容 |
| --- | --- |
| `cases.json` | 原样回声；识别的标点、大小写和答案不同；说话期间别人说了别的话（不算）；刚好超出余量；只在余量里重叠；回声和别人的话连成一句，低于阈值（不算）和高于阈值；阈值正好 0.6；中文、日语（半角片假名）、中英混排；开始等于结束的一句；改动前没有文字的区间（旧的时间规则）；归一化后不到 2 个字；只有标点 |

## 规则

1. 时间：一句和区间重叠，区间两边各加 `echoMargin` = 0.5 秒：`line.end >= span.start - 0.5 && line.start <= span.end + 0.5`；开始等于结束（或结束早于开始）的一句，看它的开始是否在加了余量的区间里。覆盖：`just_outside_margin`、`overlap_only_within_margin_after`、`overlap_only_within_margin_before`、`zero_length_line_inside`、`zero_length_line_outside`。
2. 文字：两边都先归一化（Unicode NFKC，逐字转小写，只留字母 L* 和十进制数字 Nd；标点、空格、符号、组合记号都去掉），取这一句的字符二元组集合（去重），其中在答案的二元组里出现的比例 ≥ `echoContainment` = 0.6 就算对上。覆盖：`exact_echo`、`asr_punctuation_and_case`、`participant_inside_span`、`merged_with_participant_below_threshold`、`merged_with_short_interjection_above_threshold`、`containment_exactly_at_threshold`、`containment_below_threshold`，以及中文、日语、中英混排的用例。
3. 归一化后不到 2 个字：只剩 1 个字时，答案里有这个字才算；什么都不剩，不算。覆盖：`single_character_in_answer`、`single_character_not_in_answer`、`punctuation_only_line`。
4. 时间和文字要在**同一个**区间里都对上，几个区间里任一个对上就是回声。覆盖：`words_of_another_span`。
5. 没有文字（`text` 缺省或为空）的区间是改动前存下的：沿用旧的时间规则，至少一半在区间里（不加余量；开始等于结束的一句，开始在区间里）就是回声，不看文字。覆盖：`legacy_span_time_rule`、`legacy_span_less_than_half`、`legacy_span_has_no_margin`。

两个常数都是起始值，要拿标注过的录音校准；改了要同时改两端和这里的用例。

## 结构

- `{"version": 1, "cases": [...]}`；`input.line` 是一句转写 `{start, end, text}`（秒，录音自己的时钟），`input.spans` 是 Persona 说话的区间 `[{start, end, text?}]`，和上传的 `spoken` 同形；`expected.echo` 是这一句算不算回声。
- `text` 缺省和空字符串一样，都按旧的时间规则；其余字段没有缺省值。
- 所有数据都是编造的。

## 验证

- hub：`cd server && go test ./internal/kb -run MeetingEcho`（`server/internal/kb/echo_test.go`，规则在 `server/internal/kb/echo.go`）。
- iPhone：`apple/PersonaTests/MeetingEchoFixtureTests.swift`（规则在 `apple/Shared/Recording/MeetingEcho.swift`），由 Xcode 跑 PersonaTests。
- 新增用例：先按上面的规则手算期望，写明哪几个二元组对上；两端都跑一遍，不要用被测的实现生成期望。
