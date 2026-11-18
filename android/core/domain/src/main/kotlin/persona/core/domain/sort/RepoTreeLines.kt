package persona.core.domain.sort

/**
 * What a working tree's line in By Repository says beside its name (iOS `RepoWorktreeLine` with `treeDetail` and its
 * tally, #4338 A7): the Primary chip on the main checkout, small after the name what the name does not say (the branch,
 * or a detached HEAD as a caution), how many agents at the end (Idle when nothing goes on), and dimmed while its
 * computer is offline.
 *
 * Paseo's source catalog supplies its workspace's branch, pin and availability. Other engines keep the node's git
 * placement (#1805) and their computer's state until their catalogs are connected to this list.
 */
data class WorktreeLine(
    /** The main checkout: ⌂ and the Primary chip. */
    val main: Boolean,
    /** The branch checked out there, when its name is not that branch (a tree its host named, #4497). */
    val branch: String? = null,
    /** In git with no branch checked out: "Detached HEAD", in the caution color. Never beside [branch]. */
    val detached: Boolean = false,
    /** Idle or how many agents; null for none of its own while something goes on under it (iOS leaves it out). */
    val tally: WorktreeTally? = null,
    /** Its computer is out of reach: the name in the secondary color. */
    val offline: Boolean = false,
    /** Native workspace pin. Persona's independent session/project pin controls do not edit it. */
    val pinned: Boolean = false,
)

/** The end of a working tree's line: "Idle" while nothing goes on in it, else "N agents". */
sealed interface WorktreeTally {
    data object Idle : WorktreeTally
    data class Agents(val count: Int) : WorktreeTally
}

/**
 * The line of [tree] (iOS `treeDetail`, `RepoWorktreeLine.tally`): [quiet] when nothing goes on in it (every agent
 * idle, nothing pinned or in the background), [agents] how many of its agents the list shows, [online] its computer.
 */
fun worktreeLine(tree: AgentWorktree, quiet: Boolean, agents: Int, online: Boolean): WorktreeLine = WorktreeLine(
    main = tree.main,
    branch = tree.branch?.takeIf { it != tree.name },
    detached = tree.branch == null && tree.detached,
    tally = when {
        quiet -> WorktreeTally.Idle
        agents > 0 -> WorktreeTally.Agents(agents)
        else -> null
    },
    offline = !online || tree.workspace?.online == false || tree.workspace?.available == false,
    pinned = tree.pinnedAt != null,
)

/**
 * A repository of By Repository with nothing going on (iOS `ChatsView.isQuiet`, #4338 A8): every agent in it idle, none
 * pinned, waiting to be woken or awaiting the user's reply, and no quick filter on. It leaves its card for its computer's
 * Idle Repositories on Glass.
 */
val AgentListGroup.isIdleRepository: Boolean get() = repo == RepoLevel.Repository && parent == "quiet"

/**
 * An idle repository's row in its computer's Idle Repositories (iOS `QuietSpaceRow` in By Repository, #4338 A8): how
 * many [agents], the first three [tabs] they are in (an agent without a tab names none), and when it last moved
 * ([updatedAt], its latest agent's).
 */
data class IdleRepoLine(val agents: Int, val tabs: List<String>, val updatedAt: java.time.Instant?)

/** The row of [group], an idle repository ([isIdleRepository]). */
fun idleRepoLine(group: AgentListGroup): IdleRepoLine = IdleRepoLine(
    agents = group.totalCount ?: group.rows.count { !it.isNestedSubtask },
    tabs = group.rows.mapNotNull { it.session.slotLabel }.take(3),
    updatedAt = group.rows.mapNotNull { it.updatedAt }.maxOrNull(),
)

/**
 * A working tree's idle agents behind one quiet line in By Repository (iOS `RepoIdleLine` and `agentRows`, #4338 A8):
 * [rows] the agents with nothing going on, after the busy ones; "2 idle · review · research" until opened ([open]), then
 * "Hide 2 Idle" after them. None where nothing folds: a quiet tree or repository (it folds whole), a quick filter or a
 * search (every row it leaves shows).
 */
data class IdleFold(val rows: List<AgentSession>, val open: Boolean) {
    /** What the folded line names after the count (iOS `RepoIdleLine.words`): the first three's tab names, else their titles. */
    val names: List<String> get() = rows.take(3).map { it.session.slotLabel ?: it.session.displayTitle }
}
