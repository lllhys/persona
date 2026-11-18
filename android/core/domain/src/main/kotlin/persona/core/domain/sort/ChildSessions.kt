package persona.core.domain.sort

import persona.core.contracts.Session
import persona.core.contracts.GroupKeys
import persona.core.contracts.SessionKey
import persona.core.contracts.parseTime

/**
 * A child is hidden only while this list holds its parent on the same machine (web foldRows). A side session (#7944)
 * folds the same way, and the parent counts the sides this list holds by their states here ([withHeldSides]).
 */
fun foldRows(rows: List<AgentSession>, hierarchy: Boolean = false): List<AgentSession> {
    val listed = rows.mapTo(HashSet()) { it.session.key }
    val knownGroups = rows.filter { it.hierarchyKnown || !it.session.foldedUnder.isNullOrBlank() || it.session.children != null }
        .mapTo(HashSet()) { GroupKeys.namespaceOf(it.session.machineId) }
    val sides = rows.filter { it.session.isFoldedSide }.groupBy { SessionKey(it.session.machineId, it.session.foldedUnder.orEmpty()) }
    return rows.filterNot { row -> row.session.foldedUnder?.takeIf(String::isNotBlank)?.let {
        SessionKey(row.session.machineId, it) in listed
    } == true }.map { row ->
        val known = hierarchy || GroupKeys.namespaceOf(row.session.machineId) in knownGroups
        val rolled = row.session.withHeldSides(sides[row.session.key].orEmpty().map { it.session }.filter { it.key != row.session.key })
        val state = rolled.foldedState
        val session = rolled.let { if (it.agentState == state) it else it.copy(state = state.wire) }
        if (session === row.session && (!known || row.hierarchyKnown)) row else row.copy(session = session, hierarchyKnown = known || row.hierarchyKnown)
    }
}

/** List and dock copies carry the folded state without changing the repository's own session rows. */
fun foldedSessions(rows: List<Session>, hierarchy: Boolean = false): List<Session> = foldRows(rows.map(::AgentSession), hierarchy).map { row ->
    row.session.let { if (it.agentState == row.state) it else it.copy(state = row.state.wire) }
}

/**
 * Every depth is already assigned to its topmost ancestor by the hub. With side folding ([sides], #7944) the sides and
 * the rows folded through one are not the parent's cards: the sides are in its Side sessions, the rest in their side's
 * chat ([sideChildren]).
 */
fun childSessions(rows: List<Session>, parent: SessionKey, sides: Boolean = false): List<Session> = rows
    .filter { it.machineId == parent.machineId && it.foldedUnder == parent.sessionId && it.key != parent && !(sides && it.sideHome != null) }
    .sortedWith(compareBy<Session> { parseTime(it.startedAt ?: it.updatedAt) }.thenBy { it.sessionId })

/** The rows folded through [side] (its own Codex children and workers), the cards of its own chat. */
fun sideChildren(rows: List<Session>, side: SessionKey): List<Session> = rows
    .filter { it.machineId == side.machineId && it.foldedSide == side.sessionId && it.key != side }
    .sortedWith(compareBy<Session> { parseTime(it.startedAt ?: it.updatedAt) }.thenBy { it.sessionId })
