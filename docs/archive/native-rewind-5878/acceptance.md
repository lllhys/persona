# Native conversation rewind acceptance

Issue #5878 serves Agents users in the web app and installed PWA. The existing message menu offers conversation rewind for supported Codex and Antigravity sessions. Original text returns to the composer after the runtime confirms success.

## Verification

- Native Codex 0.167.0 against an isolated local Responses fixture preserved the retained prefix, removed the selected turn, continued with the expected context, and rewound to an empty conversation.
- Native Antigravity 1.5.7 verified the same operations on temporary conversations, including replacement transcript exports and plan-mode inputs.
- Runtime and SDK tests cover routing, duplicate messages, moved inputs, busy state, native failures, history identity, restart reconciliation, and original Claude compatibility.
- Go node tests ran through the repository's partitioned runner. Focused Codex, runtime rewind, transcript, capability-key, API and localization checks cover the changed paths.
- Browser checks covered both runtimes in light and dark themes, standard and large text sizes. Each verified the conversation-only choice, request position, original-text restoration, and removal of the abandoned reply. Synthetic completion latency with a 300 ms mock response delay ranged from 565 to 717 ms.

## Design self-check

- Defaults first and progressive disclosure use the existing message-menu action. The chat layout and composer follow the established design system.
- User control retains explicit message selection and runtime refusals. Working and pending-permission states disable rewind.
- Consistency uses existing Menu, inline pending and error feedback, toast and draft restoration.
- Runtime capabilities determine which actions are offered. The integration uses each tool's native conversation semantics.
- History tokens reconcile every subscribed client after success. Native failures preserve the editable draft and remain retryable.
- This is a secondary menu interaction composed of existing components. The screenshots were visually inspected in both themes at large text size.

## Screenshots

| Runtime | Light menu | Dark menu | Light result | Dark result |
| --- | --- | --- | --- | --- |
| Codex | [Menu](codex-light-large-menu.png) | [Menu](codex-dark-large-menu.png) | [Result](codex-light-large-done.png) | [Result](codex-dark-large-done.png) |
| Antigravity | [Menu](agy-light-large-menu.png) | [Menu](agy-dark-large-menu.png) | [Result](agy-light-large-done.png) | [Result](agy-dark-large-done.png) |

Native Apple and Android clients retain their current entry gates. Backend capabilities are available for their future adoption.
