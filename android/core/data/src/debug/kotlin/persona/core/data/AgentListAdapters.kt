package persona.core.data

/** Debug's factory has an observer hook; it is not present in the release factory. */
object AgentListAdapters {
    fun create(): AgentListAdapter = AgentListAdapterSelection.create()
    fun create(observer: (AgentListAdapter) -> AgentListAdapter): AgentListAdapter = observer(create())
}
