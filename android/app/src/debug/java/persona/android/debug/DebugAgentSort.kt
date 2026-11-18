package persona.android.debug

import android.content.Context
import android.util.Log
import persona.core.contracts.JsonCodec
import persona.core.data.AgentListAdapter
import persona.core.data.ShadowAgentListAdapter
import persona.core.data.ShadowAgentListDiagnostics

/** Opt-in, read once per process. Kotlin remains authoritative even while the shadow is enabled. */
object DebugAgentSort {
    private const val PREFS = "debug-agent-sort"
    private const val KEY = "shadow"
    private var selected: Boolean? = null

    fun persist(context: Context, enabled: Boolean) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putBoolean(KEY, enabled).apply()
        Log.i("PersonaCoreShadow", "sort selection saved; applies on next process start")
    }

    fun wrap(context: Context, adapter: AgentListAdapter): AgentListAdapter {
        val enabled = synchronized(this) {
            selected ?: context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getBoolean(KEY, false)
                .also { selected = it }
        }
        if (!enabled) return adapter
        return ShadowAgentListAdapter(adapter, sink = {
            Log.w("PersonaCoreShadow", JsonCodec.encodeToString(it))
        }, progress = { compared, mismatches ->
            Log.i("PersonaCoreShadow", "sort compared=$compared mismatches=$mismatches")
        })
    }
}
