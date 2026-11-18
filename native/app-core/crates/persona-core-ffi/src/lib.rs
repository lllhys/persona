//! The core's entry points (docs/plans/plan-rust-core.md, "The boundary: a few entry points and bytes"): C functions
//! that Swift calls directly (`include/persona_core.h`) and JNI functions that Kotlin declares as `external fun`
//! (`jni`). Both carry bincode-encoded wire types. A panic never crosses the boundary: each entry point catches it and
//! returns its failure value.

#[cfg(not(feature = "release-android"))]
mod answer_shadow;
mod jni;

use std::collections::HashMap;
use std::ffi::c_void;
use std::panic::{AssertUnwindSafe, catch_unwind};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex, PoisonError};
use std::time::Duration;

use persona_core_runtime::wire::{self, Config};
use persona_core_runtime::{Host, Runtime};

/// The C client's callbacks (`PersonaCoreHost`).
#[repr(C)]
pub struct PersonaCoreHost {
    pub context: *mut c_void,
    pub changed: Option<unsafe extern "C" fn(*mut c_void, u64)>,
    pub run: Option<unsafe extern "C" fn(*mut c_void, *const u8, usize)>,
    pub release: Option<unsafe extern "C" fn(*mut c_void)>,
}

/// An optional targeted wake callback, registered separately from the stable four-field PersonaCoreHost ABI.
#[derive(Clone, Copy)]
#[repr(transparent)]
pub struct PersonaCoreChatCallback(pub unsafe extern "C" fn(*mut c_void, *const u8, usize, u64));

/// The header requires `changed` and `run` to be callable from any thread with this context.
struct CHost {
    host: PersonaCoreHost,
    handle: u64,
}
unsafe impl Send for CHost {}
unsafe impl Sync for CHost {}

impl Host for CHost {
    fn changed(&self, version: u64) {
        if let Some(changed) = self.host.changed {
            unsafe { changed(self.host.context, version) }
        }
    }
    fn changed_chat(&self, chat: &str, version: u64) {
        let callback = cores()
            .as_ref()
            .and_then(|entries| entries.get(&self.handle).and_then(|entry| entry.changed_chat));
        if let Some(changed) = callback {
            unsafe { (changed.0)(self.host.context, chat.as_ptr(), chat.len(), version) }
        } else {
            self.changed(version);
        }
    }
    fn run(&self, effect: &[u8]) {
        if let Some(run) = self.host.run {
            unsafe { run(self.host.context, effect.as_ptr(), effect.len()) }
        }
    }
}

impl Drop for CHost {
    fn drop(&mut self) {
        if let Some(release) = self.host.release {
            unsafe { release(self.host.context) }
        }
    }
}

/// An opaque handle ID, never dereferenced. A call leases its runtime before releasing the registry lock.
pub struct PersonaCore {
    _private: (),
}

static NEXT_HANDLE: AtomicU64 = AtomicU64::new(1);
struct CoreEntry {
    runtime: Arc<Runtime>,
    changed_chat: Option<PersonaCoreChatCallback>,
}

static CORES: Mutex<Option<HashMap<u64, CoreEntry>>> = Mutex::new(None);
#[cfg(feature = "test-support")]
static DRAIN_STARTED: Mutex<Option<(u64, std::sync::mpsc::Sender<()>)>> = Mutex::new(None);

fn cores() -> std::sync::MutexGuard<'static, Option<HashMap<u64, CoreEntry>>> {
    CORES.lock().unwrap_or_else(PoisonError::into_inner)
}

fn runtime(core: *const PersonaCore) -> Option<Arc<Runtime>> {
    if core.is_null() {
        return None;
    }
    cores()
        .as_ref()?
        .get(&(core.expose_provenance() as u64))
        .map(|entry| entry.runtime.clone())
}

#[cfg(feature = "test-support")]
#[doc(hidden)]
pub fn notify_drain_started_for_test(core: *mut PersonaCore, sender: std::sync::mpsc::Sender<()>) {
    let previous = DRAIN_STARTED
        .lock()
        .unwrap_or_else(PoisonError::into_inner)
        .replace((core.expose_provenance() as u64, sender));
    assert!(previous.is_none(), "only one drain-start observer may be installed");
}

fn close(core: *mut PersonaCore) {
    if core.is_null() {
        return;
    }
    let removed = cores()
        .as_mut()
        .and_then(|cores| cores.remove(&(core.expose_provenance() as u64)));
    if let Some(entry) = removed {
        entry.runtime.shutdown();
    }
}

#[repr(C)]
pub struct PersonaCoreBytes {
    pub data: *mut u8,
    pub length: usize,
    pub capacity: usize,
}

impl PersonaCoreBytes {
    fn empty() -> Self {
        PersonaCoreBytes {
            data: std::ptr::null_mut(),
            length: 0,
            capacity: 0,
        }
    }
    fn from_vec(bytes: Vec<u8>) -> Self {
        let mut bytes = std::mem::ManuallyDrop::new(bytes);
        PersonaCoreBytes {
            data: bytes.as_mut_ptr(),
            length: bytes.len(),
            capacity: bytes.capacity(),
        }
    }
}

/// # Safety
/// `pointer` is null with `length` 0, or valid for `length` bytes.
unsafe fn slice<'a>(pointer: *const u8, length: usize) -> &'a [u8] {
    if pointer.is_null() || length == 0 {
        &[]
    } else {
        unsafe { std::slice::from_raw_parts(pointer, length) }
    }
}

pub(crate) fn next_handle() -> u64 {
    NEXT_HANDLE.fetch_add(1, Ordering::Relaxed)
}

pub(crate) fn create(config: &[u8], host: Arc<dyn Host>, handle: u64) -> Option<*mut PersonaCore> {
    let _: Config = wire::decode(config).ok()?;
    let runtime = Arc::new_cyclic(|_| {
        let mut runtime = Runtime::new(host);
        runtime.set_explicit_shutdown();
        runtime
    });
    if handle == u64::MAX {
        // Reserve zero for null and never wrap an ID while a client may still hold it.
        NEXT_HANDLE.store(u64::MAX, Ordering::Relaxed);
        return None;
    }
    cores().get_or_insert_with(HashMap::new).insert(
        handle,
        CoreEntry {
            runtime,
            changed_chat: None,
        },
    );
    Some(handle as *mut PersonaCore)
}

/// # Safety
/// `config` is valid for `length` bytes; the host's callbacks follow `persona_core.h`.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_new(host: PersonaCoreHost, config: *const u8, length: usize) -> *mut PersonaCore {
    let handle = crate::next_handle();
    if handle == u64::MAX {
        NEXT_HANDLE.store(u64::MAX, Ordering::Relaxed);
        return std::ptr::null_mut();
    }
    let host = CHost { host, handle };
    let config = unsafe { slice(config, length) };
    catch_unwind(AssertUnwindSafe(|| create(config, Arc::new(host), handle)))
        .ok()
        .flatten()
        .unwrap_or(std::ptr::null_mut())
}

/// # Safety
/// `core` is live and owned by the caller. `callback` remains valid until the core is closed.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_set_changed_chat(
    core: *mut PersonaCore,
    callback: PersonaCoreChatCallback,
) -> bool {
    let handle = core.expose_provenance() as u64;
    let mut entries = cores();
    let Some(entry) = entries.as_mut().and_then(|entries| entries.get_mut(&handle)) else {
        return false;
    };
    entry.changed_chat = Some(callback);
    true
}

/// # Safety
/// `core` is a live ID returned by `persona_core_new`, or null. Calls racing `persona_core_free` either lease the
/// runtime before closure or fail; an ID is never dereferenced and is not reused while this process runs.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_dispatch(core: *mut PersonaCore, action: *const u8, length: usize) -> bool {
    let action = unsafe { slice(action, length) };
    catch_unwind(AssertUnwindSafe(|| {
        let Some(runtime) = runtime(core) else { return false };
        runtime.dispatch(action)
    }))
    .unwrap_or(false)
}

/// # Safety
/// `core` is a live ID returned by `persona_core_new`, or null. Calls racing `persona_core_free` either lease the
/// runtime before closure or fail; an ID is never dereferenced and is not reused while this process runs.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_drain(
    core: *mut PersonaCore,
    chat: *const u8,
    length: usize,
) -> PersonaCoreBytes {
    let Some(runtime) = runtime(core) else {
        // Android treats a closed handle as no patch. Preserve an empty encoded patch for a live, unchanged chat.
        return PersonaCoreBytes::empty();
    };
    let Ok(chat) = std::str::from_utf8(unsafe { slice(chat, length) }) else {
        return PersonaCoreBytes::empty();
    };
    #[cfg(feature = "test-support")]
    if DRAIN_STARTED
        .lock()
        .unwrap_or_else(PoisonError::into_inner)
        .as_ref()
        .is_some_and(|(handle, _)| *handle == core.expose_provenance() as u64)
        && let Some((_, observer)) = DRAIN_STARTED.lock().unwrap_or_else(PoisonError::into_inner).take()
    {
        let _ = observer.send(());
    }
    catch_unwind(AssertUnwindSafe(|| runtime.drain(chat)))
        .map_or_else(|_| PersonaCoreBytes::empty(), PersonaCoreBytes::from_vec)
}

/// # Safety
/// `bytes` came from `persona_core_drain` and is freed once.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_bytes_free(bytes: PersonaCoreBytes) {
    if !bytes.data.is_null() {
        drop(unsafe { Vec::from_raw_parts(bytes.data, bytes.length, bytes.capacity) });
    }
}

/// # Safety
/// `core` is a live ID returned by `persona_core_new`, or null. Calls racing `persona_core_free` either lease the
/// runtime before closure or fail; an ID is never dereferenced and is not reused while this process runs.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_flush(core: *mut PersonaCore, timeout_ms: u32) -> bool {
    catch_unwind(AssertUnwindSafe(|| {
        let Some(runtime) = runtime(core) else { return false };
        runtime.flush(Duration::from_millis(timeout_ms.into()))
    }))
    .unwrap_or(false)
}

/// # Safety
/// `core` came from `persona_core_new` and is freed once.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_free(core: *mut PersonaCore) {
    if !core.is_null() {
        let _ = catch_unwind(AssertUnwindSafe(|| close(core)));
    }
}

/// Decodes a UTF-8 JSON projection input. Every pure projection entry point shares the sort contract: the input is
/// one JSON document, the output is one JSON document, and any decode, projection or encode failure returns an empty
/// buffer instead of an error string.
fn json_input<T: serde::de::DeserializeOwned>(input: *const u8, length: usize) -> Option<T> {
    let input = unsafe { slice(input, length) };
    let input_str = std::str::from_utf8(input).ok()?;
    serde_json::from_str::<T>(input_str).ok()
}

fn json_output<T: serde::Serialize>(projection: &T) -> PersonaCoreBytes {
    match serde_json::to_string(projection) {
        Ok(json) => PersonaCoreBytes::from_vec(json.into_bytes()),
        Err(_) => PersonaCoreBytes::empty(),
    }
}

/// Decodes one WebSocket frame through the production transport decoder. The result is a JSON classification used by
/// host binding tests; a refusal carries only the same code and protocol keys as `DecodeError`. No core handle.
/// `Frame::Other` is `{"frame":"other"}`; an `items` frame reports its identity and count. This does not decode any
/// other event's associated payload.
///
/// # Safety
/// `input` is valid for `length` bytes of UTF-8 JSON.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_transport_frame(input: *const u8, length: usize) -> PersonaCoreBytes {
    catch_unwind(AssertUnwindSafe(|| {
        let input = unsafe { slice(input, length) };
        let result = match persona_core_domain::transport::decode_frame(input) {
            Ok(persona_core_domain::transport::Frame::Other) => serde_json::json!({"frame": "other"}),
            Ok(persona_core_domain::transport::Frame::Items(frame)) => serde_json::json!({
                "frame": "items", "machine_id": frame.machine_id, "session_id": frame.session_id,
                "item_count": frame.items.len(),
            }),
            Err(error) => serde_json::json!({"error": {"code": error.code(), "detail": error.detail()}}),
        };
        json_output(&result)
    }))
    .unwrap_or_else(|_| PersonaCoreBytes::empty())
}

/// Projects the Agents list from a JSON input. The input is a UTF-8 JSON document matching `persona_core::sort::Input`;
/// the returned bytes are a UTF-8 JSON document matching `persona_core::sort::Projection`. This is a pure function: no
/// core handle, no dispatch, no drain. The caller frees the result with `persona_core_bytes_free`.
///
/// # Safety
/// `input` is valid for `length` bytes of UTF-8 JSON.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_project_sort(input: *const u8, length: usize) -> PersonaCoreBytes {
    catch_unwind(AssertUnwindSafe(|| {
        let Some(input) = json_input::<persona_core_domain::sort::Input>(input, length) else {
            return PersonaCoreBytes::empty();
        };
        json_output(&persona_core_domain::sort::project(&input))
    }))
    .unwrap_or_else(|_| PersonaCoreBytes::empty())
}

/// Applies one question-answer operation (`persona_core::answer::Input`). Returns the complete `Transition` JSON, or
/// an empty buffer when the operation is not allowed to produce one (invalid page, out-of-range option). Pure
/// function, same buffer ownership as `persona_core_project_sort`.
///
/// # Safety
/// `input` is valid for `length` bytes of UTF-8 JSON.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_project_answer(input: *const u8, length: usize) -> PersonaCoreBytes {
    catch_unwind(AssertUnwindSafe(|| {
        let Some(input) = json_input::<persona_core_domain::answer::Input>(input, length) else {
            return PersonaCoreBytes::empty();
        };
        let Some(transition) = persona_core_domain::answer::project(&input) else {
            return PersonaCoreBytes::empty();
        };
        json_output(&transition)
    }))
    .unwrap_or_else(|_| PersonaCoreBytes::empty())
}

/// Formats a question option's display text and recommendation flag. Unlike the other projections the input is a
/// bare JSON string (the option label), not an object; the output is the `Display` JSON. Pure function, same buffer
/// ownership as `persona_core_project_sort`.
///
/// # Safety
/// `input` is valid for `length` bytes of UTF-8 JSON.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_display_answer(input: *const u8, length: usize) -> PersonaCoreBytes {
    catch_unwind(AssertUnwindSafe(|| {
        let Some(label) = json_input::<String>(input, length) else {
            return PersonaCoreBytes::empty();
        };
        json_output(&persona_core_domain::answer::display(&label))
    }))
    .unwrap_or_else(|_| PersonaCoreBytes::empty())
}

/// Splits streamed text into finished blocks and an unfinished tail (`persona_core::stream_chunks::Input`). Returns
/// the complete `{done, tail}` JSON. Pure function, same buffer ownership as `persona_core_project_sort`.
///
/// # Safety
/// `input` is valid for `length` bytes of UTF-8 JSON.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_project_stream_chunks(input: *const u8, length: usize) -> PersonaCoreBytes {
    catch_unwind(AssertUnwindSafe(|| {
        let Some(input) = json_input::<persona_core_domain::stream_chunks::Input>(input, length) else {
            return PersonaCoreBytes::empty();
        };
        json_output(&persona_core_domain::stream_chunks::split(&input))
    }))
    .unwrap_or_else(|_| PersonaCoreBytes::empty())
}

/// Repairs a streaming fence, table or final-block marker (`persona_core::stream_repair::Input`). Returns the
/// complete `{repaired, end}` JSON. Pure function, same buffer ownership as `persona_core_project_sort`.
///
/// # Safety
/// `input` is valid for `length` bytes of UTF-8 JSON.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_project_stream_repair(input: *const u8, length: usize) -> PersonaCoreBytes {
    catch_unwind(AssertUnwindSafe(|| {
        let Some(input) = json_input::<persona_core_domain::stream_repair::Input>(input, length) else {
            return PersonaCoreBytes::empty();
        };
        json_output(&persona_core_domain::stream_repair::project(&input))
    }))
    .unwrap_or_else(|_| PersonaCoreBytes::empty())
}

/// Removes the drawn info string of a still-open picture fence (`persona_core::stream_repair::PlainFenceInput`).
/// Returns the complete `{text}` JSON. Pure function, same buffer ownership as `persona_core_project_stream_repair`.
///
/// # Safety
/// `input` is valid for `length` bytes of UTF-8 JSON.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_project_plain_open_fence(input: *const u8, length: usize) -> PersonaCoreBytes {
    catch_unwind(AssertUnwindSafe(|| {
        let Some(input) = json_input::<persona_core_domain::stream_repair::PlainFenceInput>(input, length) else {
            return PersonaCoreBytes::empty();
        };
        json_output(&persona_core_domain::stream_repair::plain_open_fence(&input))
    }))
    .unwrap_or_else(|_| PersonaCoreBytes::empty())
}

/// Projects static transcript rows into turns (`persona_core::turns::Input`). Returns a JSON array of complete turn
/// objects; the caller applies the checked-in current-iOS overlay to the fixture's expected turns before comparing.
/// Pure function, same buffer ownership as `persona_core_project_sort`.
///
/// # Safety
/// `input` is valid for `length` bytes of UTF-8 JSON.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_project_turns(input: *const u8, length: usize) -> PersonaCoreBytes {
    catch_unwind(AssertUnwindSafe(|| {
        let Some(input) = json_input::<persona_core_domain::turns::Input>(input, length) else {
            return PersonaCoreBytes::empty();
        };
        json_output(&persona_core_domain::turns::project(&input))
    }))
    .unwrap_or_else(|_| PersonaCoreBytes::empty())
}

/// Projects the first represented work segment's viewed-image line (`persona_core::viewed_images::Input`). Returns
/// the complete `{id, image_ids, is_input, type}` JSON, or an empty buffer when no image produces a viewed line.
/// Pure function, same buffer ownership as `persona_core_project_sort`.
///
/// # Safety
/// `input` is valid for `length` bytes of UTF-8 JSON.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_project_viewed_images(input: *const u8, length: usize) -> PersonaCoreBytes {
    catch_unwind(AssertUnwindSafe(|| {
        let Some(input) = json_input::<persona_core_domain::viewed_images::Input>(input, length) else {
            return PersonaCoreBytes::empty();
        };
        let Some(projection) = persona_core_domain::viewed_images::project(&input) else {
            return PersonaCoreBytes::empty();
        };
        json_output(&projection)
    }))
    .unwrap_or_else(|_| PersonaCoreBytes::empty())
}

/// Applies one recent-conversation or recent-setup operation (`persona_core::recents::Input`). The shown operation
/// receives its persistence predicate as an explicit `existing` string list. Returns the complete fixture-shaped JSON,
/// or an empty buffer for malformed input or an unknown operation. No storage, clock or production adapter is involved.
/// Pure function, same buffer ownership as `persona_core_project_sort`.
///
/// # Safety
/// `input` is valid for `length` bytes of UTF-8 JSON.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_project_recents(input: *const u8, length: usize) -> PersonaCoreBytes {
    catch_unwind(AssertUnwindSafe(|| {
        let Some(input) = json_input::<persona_core_domain::recents::Input>(input, length) else {
            return PersonaCoreBytes::empty();
        };
        let Ok(projection) = persona_core_domain::recents::input_fixture(&input) else {
            return PersonaCoreBytes::empty();
        };
        json_output(&projection)
    }))
    .unwrap_or_else(|_| PersonaCoreBytes::empty())
}

/// Projects background-task calls, descriptions and notice status sequences (`persona_core::task_index::Input`).
/// Returns the complete `Index` JSON. Pure function, same buffer ownership as `persona_core_project_sort`.
///
/// # Safety
/// `input` is valid for `length` bytes of UTF-8 JSON.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn persona_core_project_task_index(input: *const u8, length: usize) -> PersonaCoreBytes {
    catch_unwind(AssertUnwindSafe(|| {
        let Some(input) = json_input::<persona_core_domain::task_index::Input>(input, length) else {
            return PersonaCoreBytes::empty();
        };
        json_output(&persona_core_domain::task_index::project(&input))
    }))
    .unwrap_or_else(|_| PersonaCoreBytes::empty())
}
