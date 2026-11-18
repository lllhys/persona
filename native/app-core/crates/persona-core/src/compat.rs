//! Compatibility literals shared by pure protocol readers. New writers use the canonical name; previous-generation
//! values remain confined to this module as they are in Apple's `LegacyNames` / `HubNaming`.

pub(crate) const CONTEXT_TAGS: [&str; 2] = ["persona-context", "jarvis-context"];
