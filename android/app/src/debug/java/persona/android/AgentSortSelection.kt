package persona.android

import android.content.Context
import persona.android.debug.DebugAgentSort
import persona.core.data.AgentListAdapter

internal object AgentSortSelection {
    fun wrap(context: Context, adapter: AgentListAdapter): AgentListAdapter = DebugAgentSort.wrap(context, adapter)
}
