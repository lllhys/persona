package persona.feature.chat

import android.content.Context
import android.os.Handler
import android.os.Looper
import androidx.compose.runtime.snapshots.ObserverHandle
import androidx.compose.runtime.snapshots.Snapshot
import java.util.WeakHashMap
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch

/**
 * Draft-lifetime cleanup for app-owned drops, independent of whether the large-window wrapper is currently composed.
 * Weak keys do not retain obsolete composer states; values contain only attachment records and the application context.
 */
internal object ComposerDropCleanup {
    private data class Tracked(
        val context: Context,
        val attachments: List<ComposerAttachment>,
        val sources: Collection<String>,
    )

    private val main = Handler(Looper.getMainLooper())
    private val pending = AtomicBoolean(false)
    private val tracked = WeakHashMap<ChatComposerState, Tracked>()
    private val deletions = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private var registration: ObserverHandle? = null

    /** Start watching this draft once a dropped attachment exists. Safe to call again from any recomposition. */
    fun track(state: ChatComposerState, context: Context) {
        if (Looper.myLooper() != Looper.getMainLooper()) {
            main.post { track(state, context.applicationContext) }
            return
        }
        if (tracked.containsKey(state)) return
        tracked[state] = Tracked(context.applicationContext, state.attachments, editSources(state))
        if (registration == null) {
            registration = Snapshot.registerGlobalWriteObserver { scheduleReconcile() }
        }
    }

    private fun scheduleReconcile() {
        if (pending.compareAndSet(false, true)) {
            main.post {
                pending.set(false)
                reconcile()
            }
        }
    }

    /** Every global snapshot write coalesces to one main-thread reconciliation; no composable effect owns this work. */
    private fun reconcile() {
        val current = tracked.entries.toList()
        current.forEach { (state, before) ->
            val attachments = state.attachments
            val sources = editSources(state)
            val gone = leftDrops(before.attachments, attachments, before.context.packageName, before.sources, sources)
            tracked[state] = Tracked(before.context, attachments, sources)
            if (gone.isNotEmpty()) {
                val context = before.context
                deletions.launch { gone.forEach { deleteDrop(context, it) } }
            }
        }
        if (tracked.isEmpty()) {
            registration?.dispose()
            registration = null
        }
    }

    private fun editSources(state: ChatComposerState): Collection<String> =
        state.photoEdits.filterKeys { id -> state.attachments.any { it.id == id } }.values.map { it.sourceUri }
}
