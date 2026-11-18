package persona.android.debug

import android.content.Context
import android.util.Log
import persona.core.contracts.JsonCodec
import persona.core.data.ContinuityAdapter
import persona.core.data.ContinuityMismatch
import persona.core.data.KotlinContinuityAdapter
import persona.core.data.ShadowContinuityAdapter
import persona.core.data.ShadowContinuityDiagnostics

/** The saved selection is read once per process; changing it requires force-stop and relaunch. */
object DebugContinuity {
    private const val PREFS = "debug-continuity"
    private const val KEY = "selection"
    private var selection: String? = null

    fun persist(context: Context, value: String) {
        require(value == "kotlin" || value == "shadow")
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putString(KEY, value).apply()
        Log.i("PersonaCoreShadow", "selection saved; applies on next process start")
    }

    fun factory(context: Context): (String, (ContinuityAdapter.Applied) -> Unit) -> ContinuityAdapter {
        val selected = synchronized(this) {
            selection ?: context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getString(KEY, "kotlin")!!
                .also { selection = it }
        }
        return { chat, _ ->
            if (selected == "shadow") ShadowContinuityAdapter(chat, sink = { mismatch ->
                ShadowContinuityDiagnostics.report(mismatch)
                Log.w("PersonaCoreShadow", JsonCodec.encodeToString(ContinuityMismatch.serializer(), mismatch))
            }, started = { Log.i("PersonaCoreShadow", "shadow started after first frame") },
                progress = { compared, mismatches ->
                    Log.i("PersonaCoreShadow", "compared=$compared mismatches=$mismatches")
                })
            else KotlinContinuityAdapter(chat)
        }
    }
}
