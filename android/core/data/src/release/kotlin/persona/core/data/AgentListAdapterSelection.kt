package persona.core.data

/** Release never selects a native implementation, including when JVM properties request one. */
internal object AgentListAdapterSelection {
    fun create(): AgentListAdapter = KotlinAgentListAdapter()
}
