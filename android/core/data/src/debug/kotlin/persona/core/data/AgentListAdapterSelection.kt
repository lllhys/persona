package persona.core.data

/** JVM tests can select the shadow; the app supplies its build-gated observer separately. */
internal object AgentListAdapterSelection {
    fun create(): AgentListAdapter = when (System.getProperty("persona.sort", "kotlin")) {
        "shadow" -> ShadowAgentListAdapter()
        else -> KotlinAgentListAdapter()
    }
}
