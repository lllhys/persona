package persona.android

import android.content.Context
import persona.core.data.ContinuityAdapter
import persona.core.data.KotlinContinuityAdapter

/** No debug preferences, system properties, Rust or shadow references in the release selection. */
internal object ContinuitySelection {
    fun factory(context: Context): (String, (ContinuityAdapter.Applied) -> Unit) -> ContinuityAdapter =
        { chat, _ -> KotlinContinuityAdapter(chat) }
}
