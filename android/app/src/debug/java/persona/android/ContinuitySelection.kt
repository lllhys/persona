package persona.android

import android.content.Context
import persona.android.debug.DebugContinuity
import persona.core.data.ContinuityAdapter

internal object ContinuitySelection {
    fun factory(context: Context): (String, (ContinuityAdapter.Applied) -> Unit) -> ContinuityAdapter =
        DebugContinuity.factory(context)
}
