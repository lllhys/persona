package persona.core.data

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonArray
import persona.core.contracts.JsonCodec
import persona.core.contracts.Session
import persona.core.domain.sort.AgentGrouping
import persona.core.domain.sort.AgentListSettings
import persona.core.domain.sort.AgentQuickFilter

/** The only production-model to sort-JSON conversion. Content stays inside the shadow worker. */
internal object AgentListCoreCodec {
    fun encode(input: AgentListAdapter.Input): String {
        val supported = input.settings.grouping == AgentGrouping.State ||
            input.settings.grouping == AgentGrouping.Machine || input.settings.grouping == AgentGrouping.Space
        require(supported) { "The core does not expose this grouping" }
        require(input.settings.queryAncestors.isEmpty() && input.todos.isEmpty() && input.directions == null) {
            "The core does not expose this list context"
        }
        require(input.settings.quick == AgentQuickFilter.All || input.settings.quick == AgentQuickFilter.NeedsYou ||
            input.settings.quick == AgentQuickFilter.Unread || input.settings.quick == AgentQuickFilter.Working ||
            input.settings.quick == AgentQuickFilter.BackgroundWaiting) { "The core does not expose this quick filter" }
        return buildJsonObject {
            putJsonArray("sessions") { input.sessions.forEach { add(session(it.session, it)) } }
            putJsonArray("machines") {
                input.machines.forEach { machine ->
                    val (group, id) = machine.id.coreIdentity()
                    add(buildJsonObject {
                        put("id", id)
                        put("name", machine.name)
                        group?.let { put("group", it) }
                    })
                }
            }
            put("pins", buildJsonObject {
                putJsonArray("spaces") { input.pins.spaces.forEach { add(JsonPrimitive(it.corePreference())) } }
                putJsonArray("sessions") { input.pins.sessions.forEach { add(JsonPrimitive(it.corePreference())) } }
            })
            put("mutes", buildJsonObject {
                putJsonArray("spaces") { input.mutes.spaces.forEach { add(JsonPrimitive(it.corePreference())) } }
                putJsonArray("sessions") { input.mutes.sessions.forEach { add(JsonPrimitive(it.corePreference())) } }
                putJsonArray("allowed") { input.mutes.allowed.forEach { add(JsonPrimitive(it.corePreference())) } }
            })
            put("settings", settings(input.settings, input.held, input.now.toString()))
        }.toString()
    }

    private fun session(original: Session, row: persona.core.domain.sort.AgentSession): JsonObject {
        val encoded = JsonCodec.encodeToJsonElement(Session.serializer(), original).jsonObject
        val (group, machine) = original.machineId.coreIdentity()
        return JsonObject(encoded.toMutableMap().apply {
            this["machine_id"] = JsonPrimitive(machine)
            this["state"] = JsonPrimitive(row.state.wire)
            // AgentSession's explicit stateSince differs from Session's on a folded hierarchy.
            this["state_since"] = row.stateSince?.let(::JsonPrimitive) ?: JsonNull
            group?.let { this["app_hub_group"] = JsonPrimitive(it) }
            row.hasRunningSubagent.takeIf { it && original.subagents == null }?.let {
                this["subagents"] = JsonArray(listOf(buildJsonObject { put("running", true) }))
            }
        })
    }

    private fun settings(settings: AgentListSettings, held: String?, now: String) = buildJsonObject {
        put("grouping", settings.grouping.name.lowercase())
        put("quick", when (settings.quick) {
            AgentQuickFilter.All -> "all"
            AgentQuickFilter.NeedsYou -> "needsYou"
            AgentQuickFilter.Unread -> "unread"
            AgentQuickFilter.Working -> "working"
            AgentQuickFilter.BackgroundWaiting -> "backgroundWaiting"
            else -> error("Unsupported quick filter")
        })
        put("sort", settings.sort.name.lowercase())
        put("query", settings.query)
        put("machine_filter", settings.machineFilter.coreIdentity().let { (group, machine) ->
            if (group == null) machine else "$group|$machine"
        })
        putJsonArray("kept") { settings.kept.forEach { add(JsonPrimitive(it.corePreference())) } }
        putJsonArray("folded") { settings.folded.forEach { add(JsonPrimitive(it.coreGroupPreference())) } }
        putJsonArray("expanded_idle") { settings.expandedIdle.forEach { add(JsonPrimitive(it.coreGroupPreference())) } }
        putJsonArray("expanded_quiet") { settings.expandedQuiet.forEach { add(JsonPrimitive(it.coreGroupPreference())) } }
        held?.let { put("held", it.corePreference()) }
        put("now", now)
    }

    private fun String.coreIdentity(): Pair<String?, String> {
        val separator = lastIndexOf('~')
        if (separator <= 0 || separator == length - 1) return null to this
        return substring(separator + 1) to substring(0, separator)
    }

    private fun String.corePreference(): String {
        val separator = lastIndexOf('/')
        if (separator < 0) return this
        val (group, machine) = substring(0, separator).coreIdentity()
        return if (group == null) this else "$group|$machine${substring(separator)}"
    }

    private fun String.coreGroupPreference(): String {
        val separator = indexOf('/')
        if (separator <= 0) return this
        val (group, machine) = substring(0, separator).coreIdentity()
        return if (group == null) this else "$group|$machine${substring(separator)}"
    }
}
