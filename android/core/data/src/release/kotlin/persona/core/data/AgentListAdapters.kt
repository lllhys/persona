package persona.core.data

/** No observer exists in this source set: release cannot schedule a shadow or load JNI through this boundary. */
object AgentListAdapters {
    fun create(): AgentListAdapter = KotlinAgentListAdapter()
}
