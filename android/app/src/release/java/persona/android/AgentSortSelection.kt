package persona.android

import android.content.Context
import persona.core.data.AgentListAdapter

/** Release never resolves a DEBUG observer or its native dependencies. */
internal object AgentSortSelection {
    fun wrap(context: Context, adapter: AgentListAdapter): AgentListAdapter = adapter
}
