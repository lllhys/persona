# Source subscriptions

Source subscriptions let the manager pull new information from a connector's read-only tools into the Inbox or knowledge base. The design is tracked in #2036. The optional catalog protocol (#2037) and the manager's storage, validation, scheduler, runner and API (#2038) are implemented; destinations (#2039), model drafting (#2040) and client pages (#2041) are follow-up stages. A catalog alone does not turn on polling or store source content, and until #2039 a subscription's items wait in the manager without reaching the Inbox or the knowledge base.

## Source catalog

A connector may declare `catalog`, without changing connector protocol version 2. It implements `connector.Catalog` in process, or answers `connector.catalog.list` and `connector.catalog.call` through the extension protocol. Schema and SDK types live in `schema/extension/connector.schema.json`, generated into the Go and TypeScript SDKs. `ext.Methods` accepts the generated request and result types like other connector methods.

`catalog.list {}` returns `{sources: [...]}`. Each source has a stable `id`, a display `name`, and `tools`. An id does not change when the name is renamed or translated. Optional fields are:

- `icon`: one of the declarative UI's `NavIcon` values.
- `service`: a `<kind>:<domain>` hint, such as `mail:example.com`, for recognizing a service already read natively. It is a hint, not an account identity or permission.
- `min_interval_seconds`: a positive minimum interval.
- `max_calls_per_hour`: a positive budget shared by all sources of this connector/account, not a separate allowance for each source.
- `recipes`: pull recipes using this source's tools.

Each tool has a short `name`, a `description`, an object `input_schema`, and `read_only: true`. Its optional `title` is for display. Short names are unique within the source; source ids are unique within the connector. Optional `annotations.read_only_hint` and `annotations.destructive_hint` must agree with the read-only declaration: an explicit false read-only hint or true destructive hint refuses the catalog.

`catalog.call {source, tool, args}` returns `{text}`. `args` is an object and defaults to `{}`. The source and tool must still appear in the current read-only catalog before dispatch. A missing or non-read-only tool is refused with `not_read_only`, without invoking it. Upstream `rate_limited` errors retain their `data.retry_after` in seconds, including fractional values. Programs written with the SDK must apply the same refusal in their handler; an `ext.Method` decodes its parameters and does not establish that an arbitrary tool is safe.

The Go SDK's `wire.CheckConnectorCatalogJSON` validates the original wire result, and `wire.CheckConnectorCatalog` validates typed results. Both check schema, identities, limits, read-only declarations, object schemas, and recipe shape/references. Server adapters and `exttest.CheckConnectors` use these shared checks. `connector/conformance.CheckProgram` reads the declared catalog and validates it, without executing catalog tools. A conformance check never polls a real source or invokes a write.

## Existing connectors

Without `catalog`, `connector.ReadCatalog` derives one source from the connector's declared `ReadTools`: its id and name are the connector's, and a tool's `<connector id>_` prefix is removed from the short name. `connector.CallCatalog` calls the existing read tool with its full name through `tools.call`. It never falls back to `actions.do`. A name present in both the read and write lists is refused.

An absent or undeclared catalog does not expose an implementation merely because it has a similarly named method. Existing Inbox/archive capabilities, feed switches, and `poll_seconds` keep their meaning. An older hub filters the unknown `catalog` capability at extension registration and keeps the connector's other capabilities; it can still use its ordinary tools.

## Recipe format

A recipe is a description of one pull, independent of its schedule and enabled destinations:

```json
{
  "mode": "items",
  "tool": "list",
  "args": {"since": "{{since}}"},
  "items_path": "$.items",
  "fields": {
    "id": "$.id",
    "thread": "$.thread",
    "title": "$.title",
    "sender": "$.sender",
    "time": "$.time",
    "body": "$.body",
    "link": "$.link",
    "due_date": "$.due_date"
  },
  "kind": "sub_notice",
  "group_by": "thread"
}
```

Required fields are `mode`, `tool`, `kind`, and `group_by`. The tool must be a short name in this source. `args` defaults to `{}`; it is an object, and strings can contain the runner's `{{since}}` and `{{since_date}}` variables. `items_path` defaults to `$`. Field paths are relative to one item; all are optional at the protocol layer. Supported path syntax is `$`, `.field` (letters/underscore followed by letters, digits, underscore or hyphen), and `[index]`. Wildcards, filters, recursive descent and expressions are not supported.

`mode` is `items` for arriving items or `snapshot` for state such as a balance. Text results can only be snapshots. `group_by` is `thread` or `source`. `kind` is one of `sub_mail`, `sub_message`, `sub_notice`, `sub_deadline`, `sub_event`, `sub_update`, or `sub_digest`.

The catalog stage checks parse, shape and references. The runner must additionally validate filled arguments against the tool schema, enforce the template-variable allowlist, dry-run field mapping, parse titles/times, and prove a repeated result yields nothing new. Missing ids fall back to content hashes and are marked as weak deduplication. A tool changing incompatibly, or repeated missing mapped fields, pauses the subscription for a new recipe.

## Subscriptions in the manager

The core is `server/internal/subscriptions`; the manager wires it to its connectors, database, audit and broadcasts in `server/internal/extmgr/subscriptions.go`. A subscription is one source of one connector, pulled by one recipe.

### Read-only tools

A tool is admitted at save and checked again before every run by one rule, the same as `catalog.list` / `catalog.call`:

- It must be in the connector's current catalog: `catalog.list`, or the `tools.list` read tools of a connector without a catalog. `actions.list` is never used, and the run goes only through `catalog.call` / `tools.call`.
- A catalog whose hints mark a tool as not read-only or destructive is refused as a whole. Hints only refuse: a tool without hints is not refused for that.
- A tool whose name contains a write word (`connector.WriteVerb`: send, create, delete, order, transfer…) is refused, even when it is declared read-only. Subscriptions call it again and again with nobody watching.

A refusal at save answers the error below. Before a run, a tool that left the catalog or is now named as a write pauses the subscription in `needs_recipe`. A catalog refused as a whole is a failed run, retried, without calling anything.

### Validation and dry runs

A dry run calls the tool once and runs five checks in this order: `read_only`, `args` (the arguments with `{{since}}` / `{{since_date}}` filled in, held to the tool's `input_schema` with the JSON Schema subset of `model.complete`, `server/internal/jsonsubset`), `fields` (`items_path` is a list, some item has a title, times parse), `dedup` (the same result again brings nothing new) and `interval`. A text result can only be a snapshot. A recipe that leaves `items_path` empty has its items found: the result itself when it is a list (the protocol's `$`), else its one top-level list. None or several is `no_list`, with the shape's `arrays` naming the top-level lists. The dry run says where it read them (`items_path`), and a saved recipe keeps that path. An item without an `id` field is keyed by a hash of its title, time and sender, marked `weak_dedup`.

A dry run returns up to three items and the first item's leaf paths with examples, for the pages' pickers. These values exist only in that answer. The subscription keeps the result's shape (`json` or `text`, top-level keys, which of them are lists, list length), never a value. Saving a recipe runs the same checks, and a recipe that fails is not saved.

### Storage

Manager migration `0482_subscriptions.sql`:

- `subscriptions`: one row per subscription. The settings, recipe, run state and last 20 runs are a `json` document, with `revision` (a change is written only at the revision the client saw) and `lease_until` beside it.
- `subscription_seen`: dedup keys per subscription, the newest 5000 and none older than 400 days.
- `subscription_items`: new items waiting for the destinations (#2039), bodies cut to 8000 characters.

A run writes its seen keys, its items and the cursor in one transaction; nothing is kept from a run that did not finish.

### Running

- The manager looks for due subscriptions every 30 seconds, and at once after a change or Run now, after waiting a random 0–60 seconds at startup. A run holds a 5-minute lease. A run that died is made again once its lease ends, and the seen keys keep it from adding anything twice.
- Each run reads from an hour before the newest item it has, or a day back the first time. Only one page is read; a full page of new items is reported as `maybe_more` by the dry run.
- Limits are per connector, shared by its subscriptions:
  - one call at a time, at least 20 seconds apart with jitter;
  - at most `max_calls_per_hour` calls an hour (default 120); a run over the budget moves to when the budget allows;
  - `rate_limited` pauses all of the connector's subscriptions until `retry_after`, and a restarted manager keeps the pause;
  - three failed calls in a row pause the connector, 1 minute first, doubling up to 6 hours.
- A subscription's own failure retries after its interval × 2ⁿ, at most 6 hours, and never stops it. Two runs in a row whose items lose their title, or whose list is gone, pause it in `needs_recipe`, as does a tool that changed so that it no longer takes the recipe's arguments; a tool whose description changed while it still fits carries on.
- A connector that is not `ok` or is switched off is `not_connected`: no failure, no run recorded, and it resumes by itself.
- Intervals are at least 5 minutes and the source's `min_interval_seconds`; snapshots at least an hour, by default once a day at `daily_at` (08:00 in the user's time zone). Items default to every 15 minutes, or the source's minimum when longer.
- Logs carry counts and states only. Creating, changing, running now and deleting are recorded in `plugin_audit` (`subscription_created`, `subscription_updated` with the changed field names, `subscription_run_now`, `subscription_deleted`).

### API

Reading is open to every device; changing anything, dry runs included, needs full access (403 `read_only_device`). Errors are the API's usual `{error, code}`, with `dry_run` when checks refused a recipe, `subscription` (the current one) on a 409 `conflict`, and `retry_after` on `rate_limited`.

| Route | Answer |
| --- | --- |
| `GET /api/v1/subscriptions?connector=&extension=` | `{subscriptions, features: {deliver, generate}}`; `deliver` and `generate` turn true with #2039 and #2040 |
| `GET /api/v1/subscriptions/summary` | `{connectors: [{connector, extension, subscribable?, on, failing}]}` from cached catalogs only (never a live call); `subscribable` is absent until the catalog was read |
| `GET /api/v1/subscriptions/catalog?connector=` | the connector's sources and tools read now, each tool with its `input_schema` and `subscribable` or `refused` with a code, the source's own recipes and its subscriptions; `direct` says whether the connector's own Inbox / archive feeds are on. Without `connector`: `{catalogs: [...]}`, every connector's catalog from earlier reads only (never a call; a connector not read yet is left out and read in the background) |
| `GET /api/v1/subscriptions/{id}` | `{subscription, runs}`: the newest 20 runs first, `{at, ms, trigger, new, count, error}` |
| `POST /api/v1/subscriptions/-/dry-run` | `{id}` or `{connector, source}`, with `recipe?` → the dry run; nothing is written |
| `POST /api/v1/subscriptions` | `{connector, source, recipe?, label?}` → 201 and the subscription, both destinations off; without a recipe, the source's first own one (422 `needs_recipe` when it has none) |
| `PUT /api/v1/subscriptions/{id}` | `{revision, label?, recipe?, to_inbox?, to_kb?, interval_seconds?, daily_at?, triage_rules?, fallback_tier?}`; 409 `conflict` at another revision, 422 `interval_too_short` |
| `POST /api/v1/subscriptions/{id}/run` | 202: runs as soon as the connector's limits allow, also while it is off |
| `DELETE /api/v1/subscriptions/{id}?data=1` | 204; removes its seen keys and pending items |
| `POST /api/v1/subscriptions/-/ui` | the pages below, with an extension page's request and answer; every device may draw them, actions need full access |

A subscription's `state` is `off` (both destinations off), `ok`, `failing`, `not_connected`, `rate_limited` or `needs_recipe`, with `error {code, message}`. The message is one English sentence without source content; clients localize the code. The manager reports the `source_subscriptions` feature only once subscriptions deliver (`features.deliver`, #2039), so the apps show their entry rows only then; it broadcasts `subscription` `{id, connector, revision, state, removed}` when one is saved, runs or is deleted.

### Pages

The manager draws the subscription pages (#2041) as declarative UI in `server/internal/subscriptionsui`, served by
`POST /api/v1/subscriptions/-/ui` with the request and answer of an extension page (`context`, `state`, `action`, `values`;
[extensions.md](extensions.md) "Declarative UI"). Every app draws them with the renderer it already has and adds only entry rows.
Unlike an extension page, every device may draw them: a device without full access sees the same pages with a line saying it
can only view, switches disabled, fields shown as values and the buttons that change something left out; an action from it
is 403 `read_only_device`.

| Page (`state.p`) | What it shows |
| --- | --- |
| list (none) | Needs attention (`failing`, `needs_recipe`), on, off, then per connector the sources that can be subscribed: at least one subscribable tool and no subscription yet, from cached catalogs only. `{"ext": id}` or `{"c": id}` narrows it to one extension or connector |
| `source` | The two destination switches, the interval, the state and the last and next pull, the recipe, triage rules, what happens without a triage model, Pull Now, and Turn Off and Delete Stored Content. A failing, paused or unconnected one starts with what is wrong and when it tries again. For a source not subscribed yet (`c`, `s`): the extension's own recipe (the switches create the subscription) or Choose a Tool |
| `recipe` | What it does in plain words (made from the recipe, never by a model), the field table, and on request a dry run with its items and the five checks |
| `tools`, `setup` | The source's subscribable tools and the refused ones with their reason; a tool's arguments drawn from its `input_schema` (`{{since}}` for a date-time), a dry run with `items_path` left empty, field pickers from its `paths` preselected by name, kind and grouping, then Try and Save |
| `runs`, `interval`, `fallback`, `rules`, `raw` | The last runs; pickers that apply at once (the page stays and the choice moves); triage rules; the recipe's JSON |

- A switch that is off carries the confirmation of what turning it on does: what is pulled, how often, that it is kept on the hub,
  and which model triages or summarizes it. Turning one off asks nothing.
- All text is the manager's, in the request's language (en, zh-Hans, ja); the service gives only codes, localized here.
- An app below UI version 6 gets the hub's usual downgrade (a switch becomes a row that turns it on or off); a source icon only
  version 6 has is drawn as an older one, never as none.

## Follow-up behavior

Both destination switches start off. Enabling a destination requires a full-access device and confirmation of what is pulled, how often, where it is stored, and which model triages it. Model recipe drafting separately requires consent before a trimmed sample is sent to the Information processing model; the sample is never stored.

Inbox delivery reuses admission and triage with generic subscription kinds; knowledge-base delivery uses message-id receipts. Initial Inbox catch-up covers one day, combining more than three caught-up items into a digest while retaining separate future deadlines. Knowledge-base catch-up takes the first page. Snapshots append only when content changes. Destination storage, deletion, status pages and scheduling are not enabled by the catalog capability itself.
