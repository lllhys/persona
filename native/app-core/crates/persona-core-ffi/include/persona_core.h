// The core's C entry points (docs/plans/plan-rust-core.md, "The boundary: a few entry points and bytes").
// Every message is a bincode-encoded wire type; the generated codecs read and write them.
#ifndef PERSONA_CORE_H
#define PERSONA_CORE_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct PersonaCore PersonaCore;

// The client's callbacks. `changed` and `run` are called on the core's worker thread; `release` once, when the core
// is freed. `context` is passed back unchanged and must be safe to use from that thread. Its ownership transfers to
// the core on every persona_core_new call, success or failure; release drops it exactly once. Callback pointers are
// borrowed only for the call and must be copied before returning. Callbacks must not synchronously flush or free the
// same core: the worker cannot join or wait for itself. A handle has one owner and one free. Calls may run concurrently
// with that free: each either leases the live runtime or fails closed. Free removes new leases, requests worker
// shutdown and joins it outside the registry lock. An ordinary call never joins the worker on its final lease; it may
// finish after close starts. Close is an orderly shutdown and cannot be called synchronously from its own callback.
// Handles are never dereferenced by the implementation and are not reused during this process.
typedef struct PersonaCoreHost {
    void *context;
    void (*changed)(void *context, uint64_t version);
    void (*run)(void *context, const uint8_t *effect, size_t length);
    void (*release)(void *context);
} PersonaCoreHost;

// The legacy four-field host layout stays stable. Optional targeted wake is registered separately.
typedef void (*PersonaCoreChatCallback)(void *context, const uint8_t *chat, size_t length, uint64_t version);

// Bytes the core allocated; give them back with persona_core_bytes_free.
typedef struct PersonaCoreBytes {
    uint8_t *data;
    size_t length;
    size_t capacity;
} PersonaCoreBytes;

// Starts a core from an encoded Config. NULL when the config does not decode.
PersonaCore *persona_core_new(PersonaCoreHost host, const uint8_t *config, size_t length);
// Optionally register a chat-targeted wake. Until registered, `changed` remains the version-only callback.
// The callback runs on the worker and must return promptly; it is removed when the core is closed.
bool persona_core_set_changed_chat(PersonaCore *core, PersonaCoreChatCallback callback);
// Queues an encoded Action. Safe on the main thread: it only copies the bytes.
bool persona_core_dispatch(PersonaCore *core, const uint8_t *action, size_t length);
// The chat's encoded ChatPatch since the last drain. `chat` is UTF-8.
PersonaCoreBytes persona_core_drain(PersonaCore *core, const uint8_t *chat, size_t length);
void persona_core_bytes_free(PersonaCoreBytes bytes);
// Waits until queued actions are applied. For tests; never on a main thread. Zero checks once without waiting.
bool persona_core_flush(PersonaCore *core, uint32_t timeout_ms);
// Removes new runtime leases, stops and joins the worker outside the registry lock, then releases the host.
// Ordinary dispatch/drain calls never perform this worker join on their final lease.
void persona_core_free(PersonaCore *core);

// Projects the Agents list from a UTF-8 JSON input (sort::Input). Returns a UTF-8 JSON projection (sort::Projection).
// Pure function: no core handle, no dispatch, no drain. Free the result with persona_core_bytes_free.
PersonaCoreBytes persona_core_project_sort(const uint8_t *input, size_t length);

// Classifies one WebSocket frame through the production transport decoder (#8995). Returns a JSON classification;
// a refusal carries only a reason code and protocol key names. Pure function, no core handle or payload projection.
PersonaCoreBytes persona_core_transport_frame(const uint8_t *input, size_t length);

// The pure projections below share the sort contract: one UTF-8 JSON document in, one UTF-8 JSON document out, no
// core handle, and an empty buffer on any decode, projection or encode failure. The caller frees every returned
// buffer with persona_core_bytes_free, including an empty one.

// Applies one question-answer operation (answer::Input) and returns the complete Transition JSON. An operation that
// cannot produce a transition (invalid page, out-of-range option) returns an empty buffer.
PersonaCoreBytes persona_core_project_answer(const uint8_t *input, size_t length);
// Formats a question option's display text and recommendation flag. The input is a bare JSON string (the option
// label), not an object; the output is the Display JSON.
PersonaCoreBytes persona_core_display_answer(const uint8_t *input, size_t length);
// Splits streamed text into finished blocks and an unfinished tail (stream_chunks::Input). Returns the complete
// {done, tail} JSON.
PersonaCoreBytes persona_core_project_stream_chunks(const uint8_t *input, size_t length);
// Repairs a streaming fence, table or final-block marker (stream_repair::Input). Returns the complete
// {repaired, end} JSON.
PersonaCoreBytes persona_core_project_stream_repair(const uint8_t *input, size_t length);
// Removes the drawn info string of a still-open picture fence (stream_repair::PlainFenceInput). Returns {text} JSON.
PersonaCoreBytes persona_core_project_plain_open_fence(const uint8_t *input, size_t length);
// Projects static transcript rows into turns (turns::Input). Returns a JSON array of complete turn objects. The
// caller applies the checked-in current-iOS overlay (spec/fixtures/lines/ios/differences.json) to the fixture's
// expected turns before comparing; the core never applies it.
PersonaCoreBytes persona_core_project_turns(const uint8_t *input, size_t length);
// Projects the first represented work segment's viewed-image line (viewed_images::Input). Returns the complete
// {id, image_ids, is_input, type} JSON, or an empty buffer when no image produces a viewed line.
PersonaCoreBytes persona_core_project_viewed_images(const uint8_t *input, size_t length);
// Applies one recent-conversation or recent-setup operation (recents::Input). The shown operation supplies its
// persistence predicate as an explicit existing string list. Returns the complete fixture-shaped JSON, or an empty
// buffer for malformed input or an unknown operation. No storage, clock or production adapter is involved.
PersonaCoreBytes persona_core_project_recents(const uint8_t *input, size_t length);
// Projects background-task calls, descriptions and notice status sequences (task_index::Input). Returns the complete
// Index JSON.
PersonaCoreBytes persona_core_project_task_index(const uint8_t *input, size_t length);

#ifdef __cplusplus
}
#endif

#endif
