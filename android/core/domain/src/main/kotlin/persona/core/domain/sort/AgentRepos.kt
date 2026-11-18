package persona.core.domain.sort

import persona.core.contracts.api.AgentWorkspace
import persona.core.contracts.Session
import java.time.Instant

/** Where a session's cwd sits: the repository it belongs to and the working tree inside it. */
data class RepoPlace(val repo: String, val name: String, val worktree: String, val main: Boolean)

// The layouts a working tree commonly lives in (herdr, Claude Code, a home-level collection), as on web and iOS.
private val WorktreeMarkers = listOf("/.worktrees/", "/.claude/worktrees/", "/.herdr/worktrees/")

private fun baseName(path: String) = path.split('/').lastOrNull { it.isNotEmpty() }.orEmpty()

/**
 * The repository and working tree a cwd belongs to (web's `repoOf`, iOS's `RepoPlace`). The host may name the
 * repository ([repoRoot], the session's `repo_root`, #2840: Paseo keeps its worktrees under its own folder): a cwd
 * inside it is its main checkout, any other one of its trees. Otherwise from the path alone: a tree under one of the
 * known layouts belongs to the directory above the marker, `~/.worktrees/<repo>/<tree>` to `<repo>` under it; any other
 * path is its own repository's main checkout.
 */
fun repoOf(cwd: String, repoRoot: String? = null): RepoPlace {
    val path = cwd.trimEnd('/')
    if (path.isEmpty()) return RepoPlace("", "", "", main = true)
    val root = repoRoot.orEmpty().trimEnd('/')
    if (root.isNotEmpty()) {
        val nestedTree = WorktreeMarkers.firstOrNull { path.startsWith(root + it) }?.let { marker ->
            root + marker + path.removePrefix(root + marker).substringBefore('/')
        }
        if (nestedTree != null) return RepoPlace(root, baseName(root), nestedTree, main = false)
        val inside = path == root || path.startsWith("$root/")
        return RepoPlace(root, baseName(root), if (inside) root else path, main = inside)
    }
    for (marker in WorktreeMarkers) {
        val at = path.indexOf(marker)
        if (at <= 0) continue
        val repo = path.substring(0, at)
        val rest = path.substring(at + marker.length).split('/').filter { it.isNotEmpty() }
        if (rest.size >= 2) return RepoPlace(repo + marker + rest[0], rest[0], path, main = false)
        return RepoPlace(repo, baseName(repo), path, main = false)
    }
    return RepoPlace(path, baseName(path), path, main = true)
}

/**
 * A working tree of a repository on one computer and the sessions in it (in the order they came). [name] is its [branch]
 * when the node says (#1805), else its folder's name (the main checkout: the repository's). A linked tree goes by the
 * name its host gave the place instead, when its sessions agree on one (#4497, [hostPlaceName]) and no Orca session is in
 * it; its [id] does not change with the name.
 */
data class AgentWorktree(
    val id: String, val name: String, val path: String, val main: Boolean, val members: List<AgentSession>, val branch: String? = null,
    /** The node placed its sessions in git (#1805) and named no branch: HEAD is detached there ([worktreeLine]). */
    val detached: Boolean = false,
    /** Exact source workspace, when known; its pin is independent of Persona's session/project pins. */
    val workspace: AgentWorkspace? = null,
    val pinnedAt: Instant? = null,
)

/** One repository on one computer: native pinned workspaces first, then the main checkout and names. */
data class AgentRepo(val id: String, val machineId: String, val name: String, val path: String, val worktrees: List<AgentWorktree>) {
    val members: List<AgentSession> get() = worktrees.flatMap { it.members }
    val pinnedAt: Instant? get() = worktrees.mapNotNull { it.pinnedAt }.maxOrNull()
    /**
     * Only its main checkout and no branch the node named: its sessions sit right under the repository, without a
     * working-tree heading. A named branch keeps its heading (#1805: machine → project → branch → sessions).
     */
    val flat: Boolean get() = worktrees.size == 1 && worktrees[0].main && worktrees[0].branch == null && worktrees[0].workspace == null
}

/**
 * What a repository is called when its sessions have no cwd (iOS `RepoTree.unnamed`): sorts after every name; the
 * heading says "Other" (repoTitle).
 */
const val UnnamedRepo = "\uFFFF"

private val Chunks = Regex("\\d+|\\D+")

/**
 * A name's chunks, split once: NaturalOrder is a sort comparator, and splitting both names on every comparison of
 * hundreds of sessions was most of what froze the Agents list. Names are few, so the cache stays small.
 */
private val chunkCache = java.util.concurrent.ConcurrentHashMap<String, List<String>>()
private fun chunks(name: String): List<String> = chunkCache[name] ?: Chunks.findAll(name.lowercase()).map { it.value }.toList().also {
    if (chunkCache.size > 4_096) chunkCache.clear()
    chunkCache[name] = it
}

/** Natural order, like iOS `localizedStandardCompare`: case-insensitive, digit runs by value (repo2 before repo10). */
internal val NaturalOrder = Comparator<String> { a, b ->
    val xs = chunks(a)
    val ys = chunks(b)
    for (i in 0 until minOf(xs.size, ys.size)) {
        val x = xs[i]
        val y = ys[i]
        val c = if (x[0].isDigit() && y[0].isDigit()) {
            val (u, v) = x.trimStart('0') to y.trimStart('0')
            compareValuesBy(u to x, v to y, { it.first.length }, { it.first }, { it.second.length })
        } else x.compareTo(y)
        if (c != 0) return@Comparator c
    }
    xs.size.compareTo(ys.size)
}

private val byName = compareBy<Pair<String, String>, String>(NaturalOrder) { it.first }.thenBy { it.second }

/**
 * Where the node placed a session in git (#1805, web's `gitPlaceOf`, iOS's `RepoPlace.git`): its project (`repo_root`,
 * else the working tree itself) and working tree; null from an older node, which leaves [repoOf]'s inference.
 */
fun gitPlaceOf(repoRoot: String?, worktree: String?): RepoPlace? {
    val tree = worktree.orEmpty().trimEnd('/')
    if (tree.isEmpty()) return null
    val repo = repoRoot.orEmpty().trimEnd('/').ifEmpty { tree }
    return RepoPlace(repo, baseName(repo), tree, main = tree == repo)
}

private val AgentSession.place: RepoPlace
    get() = gitPlaceOf(session.repoRoot, session.worktree) ?: repoOf(session.cwd, session.repoRoot)

/**
 * The name the host gave the place [members] run in (#4497, web's `hostPlaceName`: Paseo sends its workspace's title as
 * `slot_name`, with no `slot_id`), when every session that gives one gives the same; two different names name nothing.
 * A `slot_name` beside a `slot_id` is a tab's label (herdr), which names no working tree.
 */
private fun hostPlaceName(members: List<AgentSession>): String? =
    members.mapNotNullTo(mutableSetOf()) { m -> m.session.slotName?.trim()?.takeIf { it.isNotEmpty() && m.session.slotId.isNullOrBlank() } }
        .singleOrNull()

/** Each computer's repositories, native pins first, then names in natural order. Source IDs override path inference. */
fun agentRepos(rows: List<AgentSession>, workspaces: List<AgentWorkspace> = emptyList()): List<AgentRepo> {
    if (workspaces.isEmpty()) return inferredAgentRepos(rows)
    // Paseo's project may contain several workspaces at the same folder. Their source IDs, never cwd, own the rows
    // and pins. Other engines retain the existing projection until they expose the same catalog behavior.
    val catalog = workspaces.filter { it.source == "paseo" && it.machineId.isNotEmpty() && it.sourceId.isNotEmpty() }
        .distinctBy { it.machineId to it.sourceId }
    val byID = catalog.associateBy { it.machineId to it.sourceId }
    val assigned = rows.groupBy { row ->
        row.session.paseoWorkspaceKey()?.let(byID::get)
    }
    val native = catalog.filter { assigned[it].orEmpty().isNotEmpty() || it.nativePinTime != null }
        .groupBy { it.machineId to (it.projectId?.takeIf(String::isNotBlank) ?: it.sourceId) }.map { (key, entries) ->
        val first = entries.first()
        val path = first.projectPath?.takeIf(String::isNotBlank) ?: first.cwd
        val name = first.projectName?.takeIf(String::isNotBlank) ?: baseName(path).ifEmpty { UnnamedRepo }
        val trees = entries.map { w ->
            AgentWorktree("tree/catalog/${w.machineId}/${w.source}/${w.sourceId}", w.title.ifBlank { w.branch ?: baseName(w.cwd) },
                w.cwd, w.isMain == true, assigned[w].orEmpty(), w.branch?.takeIf(String::isNotBlank),
                detached = w.workspaceKind == "git" && w.branch.isNullOrBlank(), workspace = w,
                pinnedAt = w.nativePinTime)
        }.sortedWith(compareByDescending<AgentWorktree> { it.pinnedAt }.thenBy { !it.main }
            .thenComparing({ it.name to it.id }, byName))
        AgentRepo("repo/catalog/${key.first}/paseo/${key.second}", key.first, name, path, trees)
    }
    return (inferredAgentRepos(assigned[null].orEmpty()) + native)
        .sortedWith(compareByDescending<AgentRepo> { it.pinnedAt }
            .thenComparing({ it.name to it.path }, byName).thenBy { it.id })
}

/** Invalid or cleared source timestamps are not pins. */
internal val AgentWorkspace.nativePinTime: Instant? get() =
    pinnedAt?.let { runCatching { Instant.parse(it) }.getOrNull() }

/** Exact source attribution; another engine at the same folder or with the same ID is not this workspace. */
internal fun Session.paseoWorkspaceKey(): Pair<String, String>? = workspaceId?.takeIf(String::isNotBlank)?.let {
    if (sourceKind == "runtime" && (sourceInstanceId?.takeIf(String::isNotBlank) ?: spaceKind) == "paseo") machineId to it else null
}

private fun inferredAgentRepos(rows: List<AgentSession>): List<AgentRepo> =
    rows.groupBy { it.session.machineId to it.place.repo }.map { (key, members) ->
        val (machine, repo) = key
        val place = members.first().place
        val name = place.name.ifEmpty { UnnamedRepo }
        val trees = members.groupBy { it.place.worktree }.map { (path, inTree) ->
            val main = inTree.first().place.main
            // Orca's sidebar and git name a working tree by its branch.
            val branch = inTree.firstNotNullOfOrNull { it.session.branch?.takeIf(String::isNotEmpty) }
            // Keep the saved fold key for the repository inferred from the path. If the host puts the same
            // path under a different repository, scope its key to that repository even when filtered on its own.
            val id = "tree/$machine/$path" + if (repo != repoOf(path).repo) "\u0000$repo" else ""
            // A linked tree takes the name its host gave it (#4497), its branch staying beside it. The main checkout keeps
            // its name: several workspaces can share it. So does a tree with an Orca session in it, which Orca names (web `orcaTrees`).
            val hostName = if (main || inTree.any { it.session.spaceKind == "orca" }) null else hostPlaceName(inTree)
            AgentWorktree(id, hostName ?: branch ?: if (path == repo) name else baseName(path), path, main, inTree, branch,
                detached = branch == null && inTree.any { !it.session.worktree.isNullOrBlank() })
        }.sortedWith(compareBy<AgentWorktree> { !it.main }.thenComparing({ it.name to it.path }, byName))
        AgentRepo("repo/$machine/$repo", machine, name, repo, trees)
    }.sortedWith(compareBy<AgentRepo, Pair<String, String>>(byName) { it.name to it.path }.thenBy { it.machineId })
