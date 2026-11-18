package persona.android.debug

import android.content.Context
import android.util.Log
import persona.core.contracts.JsonCodec
import persona.core.domain.answer.AnswerAdapter
import persona.core.domain.answer.ShadowAnswerAdapter

/** Opt-in, read once per process. Kotlin remains authoritative even while the observer is enabled. */
object DebugAnswer {
    private const val PREFS = "debug-answer"
    private const val KEY = "shadow"
    @Volatile private var selected: Boolean? = null
    private val lock = Any()

    fun persist(context: Context, enabled: Boolean) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putBoolean(KEY, enabled).apply()
        Log.i("PersonaCoreShadow", "answer selection saved; applies on next process start")
    }

    fun wrap(context: Context, adapter: AnswerAdapter): AnswerAdapter {
        val enabled = synchronized(lock) {
            selected ?: context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getBoolean(KEY, false)
                .also { selected = it }
        }
        if (!enabled) return adapter
        return observe(adapter)
    }

    internal fun observe(adapter: AnswerAdapter): AnswerAdapter = ShadowAnswerAdapter(adapter, sink = {
        Log.w("PersonaCoreShadow", JsonCodec.encodeToString(it))
    }, progress = { compared, mismatches ->
        Log.i("PersonaCoreShadow", "answer compared=$compared mismatches=$mismatches")
    })

    /** JVM test hook for a fresh process's persisted selection; production never resets this choice mid-process. */
    internal fun resetForTest() {
        synchronized(lock) { selected = null }
    }
}
