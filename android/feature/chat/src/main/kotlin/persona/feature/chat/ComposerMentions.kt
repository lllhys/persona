package persona.feature.chat

import android.content.res.Resources
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.remember
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import persona.core.contracts.Item
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.SessionFileEntry

/**
 * The composer's @ menu (iOS `PromptItems.files` / `sessions`, web `lib/mentions.ts`): files this chat's tools read or
 * wrote (picking one types `@path`, which Claude Code and Codex read as that file), then other agents' sessions
 * (picking one quotes it as a `<persona-context>` before the message, not as text). In Persona's conversations the
 * conversation's open tasks come first (iOS `TaskMentionList`): picking one routes the next message to it. With
 * [folders], the session's own folder lists its entries after the files (relative to it), and a word with a slash lists
 * that folder on the computer instead (iOS `MentionRows.browseTarget`).
 */
@Immutable
data class ComposerMentions(
    /** Absolute paths, newest first ([chatFiles]). */
    val files: List<String>,
    /** The session's folder: a file inside it is typed relative to it. */
    val cwd: String,
    /** Every session the list knows; [otherSessions] picks the ones to offer. */
    val sessions: List<Session>,
    /** This chat, left out of the sessions. */
    val except: SessionKey?,
    /** The conversation's open tasks ([mentionTasks]); null outside Persona's conversations. */
    val tasks: List<MentionTask>? = null,
    /** Where folders are read: the session's own and a typed one; null: no entries, and a slash only narrows the files, as before. */
    val folders: MentionFolders? = null,
)

/**
 * Reads a folder on the computer for the @ list (iOS `MentionSource.machine` and `listDir`, the web's `/fs`): a chat
 * reads its session's computer, New chat's page the assistant's. [dir] is absolute, `~`-based, or under [cwd].
 */
fun interface MentionFolders {
    suspend fun entries(dir: String, cwd: String): List<SessionFileEntry>
}

/** One of the conversation's tasks in the @ menu: its [status] dot, title and where it runs ([place]). */
@Immutable
data class MentionTask(val id: String, val title: String, val place: String, val status: persona.core.designsystem.Status)

/** The `@word` being typed just before the caret: [start] is the `@`, [end] the caret. */
internal data class MentionToken(val start: Int, val end: Int, val query: String)

private val mentionBefore = Regex("(^|\\s)@([^\\s@]*)$")

/** The `@word` just before [caret]: at the start or after whitespace, so an e-mail address is not one. */
internal fun mentionAt(text: String, caret: Int): MentionToken? {
    if (caret < 0 || caret > text.length) return null
    val before = text.take(caret)
    val match = mentionBefore.find(before) ?: return null
    val query = match.groupValues[2]
    return MentionToken(before.length - query.length - 1, caret, query)
}

/** [value] with [token] replaced by [insert], the caret after it; a picked file ends in a space, never doubled. */
internal fun replaceMention(value: TextFieldValue, token: MentionToken, insert: String): TextFieldValue {
    val before = value.text.take(token.start)
    var after = value.text.drop(token.end)
    if (insert.endsWith(" ") && after.startsWith(" ")) after = after.drop(1)
    return TextFieldValue(before + insert + after, TextRange(before.length + insert.length))
}

/** [value] with `@` typed at the caret (a space first when it follows a word): the @ button. */
internal fun typeMention(value: TextFieldValue): TextFieldValue {
    val at = value.selection.min.coerceIn(0, value.text.length)
    val before = value.text.take(at)
    val typed = if (before.isNotEmpty() && !before.last().isWhitespace()) " @" else "@"
    return TextFieldValue(before + typed + value.text.drop(value.selection.max.coerceIn(at, value.text.length)), TextRange(at + typed.length))
}

/** A path as typed after @: relative to the session's folder when inside it. */
internal fun relativeMentionPath(path: String, cwd: String): String {
    val base = cwd.trimEnd('/')
    return if (base.isNotEmpty() && path.startsWith("$base/")) path.drop(base.length + 1) else path
}

/** Files the chat's tools read or wrote, newest first, at most [limit]; absolute paths. */
internal fun chatFiles(items: List<Item>, limit: Int = 12): List<String> {
    val out = LinkedHashSet<String>()
    for (item in items.asReversed()) {
        if (out.size >= limit) break
        if (item.kind != "tool_use") continue
        val input = item.toolInput as? JsonObject ?: continue
        val path = ((input["file_path"] ?: input["notebook_path"]) as? JsonPrimitive)?.takeIf { it.isString }?.content
        if (!path.isNullOrBlank()) out += path
    }
    return out.toList()
}

/** Other agents' sessions matching [query] (title or agent · folder), most recently active first, at most [limit]. */
internal fun otherSessions(all: List<Session>, except: SessionKey?, query: String, limit: Int = 8): List<Session> {
    val q = query.lowercase()
    return all.asSequence().filter { !it.assistant && it.key != except }
        .filter { q.isEmpty() || it.displayTitle.lowercase().contains(q) || sessionMentionDetail(it).lowercase().contains(q) }
        .sortedByDescending { it.updated }.take(limit).toList()
}

internal fun sessionMentionDetail(session: Session): String = listOf(session.agentLabel, session.folderName).filter(String::isNotEmpty).joinToString(" · ")

/** One row of the @ menu: a task to route the next message to, a file to type, or a session to quote. */
internal sealed interface MentionRow {
    val key: String
    data class Task(val task: MentionTask, override val key: String = "task:${task.id}") : MentionRow
    data class File(val path: String, val insert: String, override val key: String = "file:$path") : MentionRow
    data class Quote(val session: Session, override val key: String = "session:${session.key.path}") : MentionRow
    /**
     * An entry of a folder: typed as `@<typed><name> ` for a file, `@<typed><name>/` for a folder, where the list then
     * browses on (iOS `MentionRow.Kind.file`). Of the folder being browsed ([typed] is the path so far), or, with [top], of
     * the session's own folder, typed relative to it ([typed] empty).
     */
    data class Entry(val name: String, val folder: Boolean, val typed: String, val top: Boolean = false) : MentionRow {
        override val key = if (top) "top:$name" else "fs:$typed$name"
        val insert get() = "@$typed$name" + if (folder) "/" else " "
    }
}

/** What a word with a slash browses (iOS `MentionRows.browseTarget`): the [dir] read, what stays [typed] before a picked name, the rest [filter]s. */
internal data class MentionBrowse(val dir: String, val typed: String, val filter: String)

/** Where [query] browses: an absolute or `~` path, or one under [cwd]; null without a slash, or for a relative path with no folder to start from. */
internal fun mentionBrowse(query: String, cwd: String): MentionBrowse? {
    val slash = query.lastIndexOf('/')
    if (slash < 0) return null
    val typed = query.take(slash + 1)
    val filter = query.drop(slash + 1)
    val part = typed.trimEnd('/')
    return when {
        typed.startsWith("/") -> MentionBrowse(part.ifEmpty { "/" }, typed, filter)
        typed.startsWith("~") -> MentionBrowse(part.ifEmpty { "~" }, typed, filter)
        cwd.isEmpty() -> null
        else -> MentionBrowse(cwd.trimEnd('/') + "/" + part, typed, filter)
    }
}

/** The folder the @ list reads for [query] (iOS `MentionRows.folder`): the one it browses, else the session's own; null with neither. */
internal fun mentionFolder(query: String, cwd: String): String? = mentionBrowse(query, cwd)?.dir ?: cwd.takeIf(String::isNotEmpty)

/** The most rows a browsed folder lists (iOS `MentionRows.limit`). */
private const val BROWSE_LIMIT = 40

/** The most of the session's own folder's entries the list shows after the chat's files (iOS `MentionRows.rows`). */
private const val TOP_LIMIT = 12

/** A name matches what is typed anywhere in it, ignoring case; hidden ones only when the filter starts with ".". */
private fun nameMatches(name: String, filter: String) =
    (!name.startsWith(".") || filter.startsWith(".")) && (filter.isEmpty() || name.lowercase().contains(filter.lowercase()))

/** Names that start with [filter] first, then the rest, each keeping its order. */
private fun byPrefix(entries: List<SessionFileEntry>, filter: String): List<SessionFileEntry> {
    val f = filter.lowercase()
    return if (f.isEmpty()) entries else entries.partition { it.name.lowercase().startsWith(f) }.let { (first, rest) -> first + rest }
}

/**
 * The @ menu for [query]. A word with a slash, where [ComposerMentions.folders] lists folders, shows that folder's
 * [listing] alone (null until read): its folders, then its files, each with the names that start with the filter first.
 * Otherwise the tasks whose title contains it, the files whose relative path does, the session folder's own entries
 * ([listing] is the session's folder then, as iOS lists it: folders first, at most twelve, none the chat used already),
 * then the sessions it matches. iOS lists every task whatever follows the @ in a panel of its own; here they share the
 * files' list, so the typed word narrows them too.
 */
internal fun mentionRows(mentions: ComposerMentions, query: String, listing: List<SessionFileEntry>? = null): List<MentionRow> {
    val browse = mentions.folders?.let { mentionBrowse(query, mentions.cwd) }
    if (browse != null) {
        val hits = listing.orEmpty().filter { nameMatches(it.name, browse.filter) }
        return (byPrefix(hits.filter { it.dir }, browse.filter) + byPrefix(hits.filterNot { it.dir }, browse.filter))
            .take(BROWSE_LIMIT).map { MentionRow.Entry(it.name, it.dir, browse.typed) }
    }
    val q = query.lowercase()
    val tasks = mentions.tasks.orEmpty().filter { q.isEmpty() || it.title.lowercase().contains(q) }.map { MentionRow.Task(it) }
    val used = mentions.files.map { relativeMentionPath(it, mentions.cwd) to it }
        .filter { (relative, _) -> q.isEmpty() || relative.lowercase().contains(q) }
    val files = used.map { (relative, path) -> MentionRow.File(path, "@$relative ") }
    // The session folder's entries (iOS `top`), in the folder's own names: a file the chat used is not listed again.
    val listed = used.mapTo(HashSet()) { it.first }
    val own = if (mentions.folders == null || mentions.cwd.isEmpty()) emptyList() else listing.orEmpty().filter { nameMatches(it.name, q) && it.name !in listed }
    val top = (byPrefix(own.filter { it.dir }, q) + byPrefix(own.filterNot { it.dir }, q)).take(TOP_LIMIT)
        .map { MentionRow.Entry(it.name, it.dir, "", top = true) }
    return tasks + files + top + otherSessions(mentions.sessions, mentions.except, query).map { MentionRow.Quote(it) }
}

/**
 * The folder the @ list is reading ([dir], [mentionFolder]) and what was read of it: [listing] is null while it is read.
 * [browse] is set when the word names a folder, else [dir] is the session's own.
 */
internal data class MentionListing(val browse: MentionBrowse?, val dir: String?, val listing: List<SessionFileEntry>?) {
    /** A folder is to be read and is not yet: "Reading folder…" (iOS `ComposerMentionMenu`'s `.reading`). */
    val reading get() = dir != null && listing == null
}

/**
 * Reads the folder [query] browses, else the session's own, each folder once while the list is open (iOS
 * `MentionFolders`): after a short pause, so typing through a path asks only for where it stops; one that can't be read
 * lists nothing.
 */
@Composable
internal fun rememberMentionListing(mentions: ComposerMentions?, query: String?): MentionListing {
    val folders = mentions?.folders
    val cwd = mentions?.cwd.orEmpty()
    val open = folders != null && query != null
    val browse = if (open) mentionBrowse(query.orEmpty(), cwd) else null
    val dir = if (open) mentionFolder(query.orEmpty(), cwd) else null
    val listings = remember(folders, query != null) { mutableStateMapOf<String, List<SessionFileEntry>>() }
    LaunchedEffect(listings, dir) {
        if (folders == null || dir == null || dir in listings) return@LaunchedEffect
        delay(120)
        listings[dir] = try { folders.entries(dir, cwd) } catch (e: CancellationException) { throw e } catch (_: Exception) { emptyList() }
    }
    return MentionListing(browse, dir, dir?.let { listings[it] })
}

/** The quote for a picked session (iOS `PromptItems.quote`): its title, agent, folder and last words, as context. */
internal fun sessionQuote(session: Session, resources: Resources): ChatLinkContext {
    val lines = buildList {
        add(resources.getString(R.string.chat_mention_quote_session, session.displayTitle, session.agentLabel))
        add(resources.getString(R.string.chat_mention_quote_cwd, session.cwd))
        session.lastText?.trim()?.takeIf(String::isNotEmpty)?.let { add(resources.getString(R.string.chat_mention_quote_last, it.take(600))) }
    }
    return ChatLinkContext("context", session.displayTitle, "persona://session/${session.machineId}/${session.sessionId}", lines.joinToString("\n"))
}
