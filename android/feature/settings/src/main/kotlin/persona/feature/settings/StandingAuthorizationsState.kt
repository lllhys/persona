package persona.feature.settings

import android.content.res.Resources
import java.util.Locale
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import persona.core.contracts.GroupKeys
import persona.core.contracts.Machine
import persona.core.contracts.StandingAuthorization
import persona.core.contracts.api.StandingAuthorizationsSource
import persona.core.contracts.api.hubCodedFailure
import persona.core.contracts.parseTime
import persona.core.designsystem.LocalizedFormats
import persona.core.designsystem.UiText
import persona.core.designsystem.failureReasonText

/** A line the page says after a revoke or a refresh, in place of a toast. */
internal sealed class StandingNotice {
    data object Revoked : StandingNotice()
    data object RevokeFailed : StandingNotice()
    data class DidntRefresh(val reason: UiText) : StandingNotice()
}

internal data class StandingAuthorizationsState(
    /** Null until the first read answers. A later read keeps this until its own answer replaces it. */
    val grants: List<StandingAuthorization>? = null,
    val loading: Boolean = false,
    /** This device has no full access (403). The list is cleared. */
    val forbidden: Boolean = false,
    /** Why the first read failed. A later failure keeps [grants] and uses [notice]. */
    val loadError: Throwable? = null,
    val notice: StandingNotice? = null,
)

/**
 * Settings › Standing Authorizations. Keeps the last list on screen while a refresh runs. A failed revoke puts the
 * row back where it was. There is no undo: the hub has no route that restores a grant.
 */
internal class StandingAuthorizationsModel(private val source: StandingAuthorizationsSource) {
    private val mutable = MutableStateFlow(StandingAuthorizationsState())
    val state = mutable.asStateFlow()
    private val lock = Any()
    private var reads = 0
    private val revoking = mutableSetOf<String>()
    private val revoked = mutableSetOf<String>()

    suspend fun load() {
        val read = synchronized(lock) { ++reads }
        mutable.update { it.copy(loading = true) }
        try {
            val list = source.list()
            if (read != currentRead()) return
            val shown = list.authorizations.filter { !hidden(it.id) }
            // A revoke's "Revoked" stays until the next user action or its lifetime. A refresh that was already
            // running must not wipe it when it succeeds.
            mutable.update { state ->
                val notice = state.notice as? StandingNotice.Revoked
                state.copy(grants = shown, forbidden = false, loadError = null, notice = notice)
            }
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            if (read != currentRead()) return
            if (hubCodedFailure(error)?.status == 403) {
                mutable.update { it.copy(grants = null, forbidden = true, loadError = null, notice = null) }
            } else if (mutable.value.grants != null) {
                mutable.update { it.copy(notice = StandingNotice.DidntRefresh(failureReasonText(error))) }
            } else {
                mutable.update { it.copy(loadError = error, forbidden = false) }
            }
        } finally {
            if (read == currentRead()) mutable.update { it.copy(loading = false) }
        }
    }

    /** The caller has confirmed. The row leaves at once; a failure puts it back and says it is still in effect. */
    suspend fun revoke(grant: StandingAuthorization) {
        val at = synchronized(lock) {
            val list = mutable.value.grants ?: return
            val index = list.indexOfFirst { it.id == grant.id }
            if (index < 0 || grant.id in revoking) return
            revoking += grant.id
            index
        }
        mutable.update { it.copy(grants = it.grants?.filter { row -> row.id != grant.id }, notice = null) }
        try {
            source.revoke(grant.id)
            synchronized(lock) {
                revoked += grant.id
                revoking -= grant.id
            }
            mutable.update { it.copy(notice = StandingNotice.Revoked) }
        } catch (cancelled: CancellationException) {
            synchronized(lock) { revoking -= grant.id }
            restore(grant, at)
            throw cancelled
        } catch (_: Exception) {
            synchronized(lock) { revoking -= grant.id }
            restore(grant, at)
            mutable.update { it.copy(notice = StandingNotice.RevokeFailed) }
        }
    }

    private fun restore(grant: StandingAuthorization, at: Int) {
        mutable.update { state ->
            val list = state.grants.orEmpty()
            if (list.any { it.id == grant.id }) state
            else state.copy(grants = list.toMutableList().apply { add(at.coerceIn(0, size), grant) })
        }
    }

    /** The next thing the user does (refresh, open a grant). A refresh that finishes on its own does not call this. */
    fun dismissRevoked() {
        mutable.update { if (it.notice is StandingNotice.Revoked) it.copy(notice = null) else it }
    }

    private fun currentRead(): Int = synchronized(lock) { reads }
    private fun hidden(id: String): Boolean = synchronized(lock) { id in revoking || id in revoked }
}

/**
 * The computer's name. An exact id wins, so two hubs that both have a machine called `mac-mini` do not share a name.
 * A raw id matches only a machine in the same group.
 */
internal fun standingMachineName(id: String, machines: List<Machine>): String {
    if (id.isBlank()) return id
    machines.firstOrNull { it.id == id }?.let { return it.displayName }
    val namespace = GroupKeys.namespaceOf(id)
    val raw = GroupKeys.raw(id)
    return machines.firstOrNull { GroupKeys.namespaceOf(it.id) == namespace && GroupKeys.raw(it.id) == raw }?.displayName ?: id
}

/** The words a grant is shown with. The same sentences as iOS, in the page's language. */
internal object StandingAuthorizationText {
    fun actionLabel(action: String, resources: Resources): String = when (action) {
        "merge" -> resources.getString(R.string.settings_standing_action_merge)
        "push" -> resources.getString(R.string.settings_standing_action_push)
        "deploy-test" -> resources.getString(R.string.settings_standing_action_deploy_test)
        "deploy-production" -> resources.getString(R.string.settings_standing_action_deploy_production)
        "send" -> resources.getString(R.string.settings_standing_action_send)
        "delete" -> resources.getString(R.string.settings_standing_action_delete)
        "spend" -> resources.getString(R.string.settings_standing_action_spend)
        else -> action
    }

    fun phrase(action: String, resources: Resources): String = when (action) {
        "merge" -> resources.getString(R.string.settings_standing_phrase_merge)
        "push" -> resources.getString(R.string.settings_standing_phrase_push)
        "deploy-test" -> resources.getString(R.string.settings_standing_phrase_deploy_test)
        "deploy-production" -> resources.getString(R.string.settings_standing_phrase_deploy_production)
        "send" -> resources.getString(R.string.settings_standing_phrase_send)
        "delete" -> resources.getString(R.string.settings_standing_phrase_delete)
        "spend" -> resources.getString(R.string.settings_standing_phrase_spend)
        else -> resources.getString(R.string.settings_standing_unknown_phrase, action)
    }

    fun condition(condition: String, resources: Resources): String = when (condition) {
        "ci_green" -> resources.getString(R.string.settings_standing_condition_ci)
        "no_conflicts" -> resources.getString(R.string.settings_standing_condition_conflicts)
        else -> condition
    }

    /** "merge changes and push changes", or the no-actions sentence when nothing was recorded. */
    fun actionsPhrase(grant: StandingAuthorization, resources: Resources): String {
        if (grant.actions.isEmpty()) return resources.getString(R.string.settings_standing_no_actions)
        return joinAnd(grant.actions.map { phrase(it, resources) }, resources)
    }

    /** The phrase as a line of its own, its first letter capital. */
    fun actionsTitle(grant: StandingAuthorization, resources: Resources): String {
        val phrase = actionsPhrase(grant, resources)
        if (grant.actions.isEmpty() || phrase.isEmpty()) return phrase
        val locale = resources.configuration.locales[0]
        return phrase.replaceFirstChar { if (it.isLowerCase()) it.titlecase(locale) else it.toString() }
    }

    /** "Only when: all checks pass and no merge conflicts", null when there are none. */
    fun conditionsLine(grant: StandingAuthorization, resources: Resources): String? {
        if (grant.conditions.isEmpty()) return null
        return resources.getString(R.string.settings_standing_only_when, joinAnd(grant.conditions.map { condition(it, resources) }, resources))
    }

    fun since(grant: StandingAuthorization, resources: Resources): String? {
        val instant = parseTime(grant.grantedAt) ?: return null
        // A normal space lets a narrow column break the date ("Nov" / "11"). Keep the day on one line.
        val day = LocalizedFormats.format(instant, LocalizedFormats.MONTH_DAY, resources.configuration.locales[0])
            .replace(' ', '\u00A0')
        return resources.getString(R.string.settings_standing_since, day)
    }

    fun granted(grant: StandingAuthorization, resources: Resources): String? {
        val instant = parseTime(grant.grantedAt) ?: return null
        return LocalizedFormats.format(instant, LocalizedFormats.MONTH_DAY_TIME, resources.configuration.locales[0])
    }

    fun listCaption(grant: StandingAuthorization, resources: Resources): String? {
        val parts = listOfNotNull(conditionsLine(grant, resources), since(grant, resources))
        return parts.takeIf { it.isNotEmpty() }?.joinToString(" · ")
    }

    /**
     * Where a grant came from, in this language. The hub stores words, including its suggested-rule sentence.
     * Ones this build knows in en, zh-Hans or ja are worded again; anything else stays as written.
     */
    fun source(raw: String, resources: Resources): String {
        val text = raw.trim()
        suggestedCount(text)?.let { return resources.getString(R.string.settings_standing_source_suggested, it) }
        val key = text.lowercase(Locale.ROOT).trimEnd('.', '。', ' ') // i18n-ignore: the words a grant's source may hold
        return when (key) {
            in MAIN_CHAT -> resources.getString(R.string.settings_standing_source_main)
            in A_CHAT -> resources.getString(R.string.settings_standing_source_chat)
            in SETTINGS -> resources.getString(R.string.settings_standing_source_settings)
            else -> text
        }
    }

    private fun joinAnd(parts: List<String>, resources: Resources): String = when (parts.size) {
        0 -> ""
        1 -> parts[0]
        else -> resources.getString(
            R.string.settings_standing_list_and,
            parts.dropLast(1).joinToString(resources.getString(R.string.settings_standing_list_comma)),
            parts.last(),
        )
    }

    private fun suggestedCount(text: String): Int? {
        for (pattern in SUGGESTED) {
            val count = pattern.matchEntire(text)?.groupValues?.getOrNull(1)?.toIntOrNull()
            if (count != null) return count
        }
        return null
    }

    private val MAIN_CHAT = setOf(
        "main conversation", "the main conversation", "main chat", "the main chat",
        "主对话", "主会话", "主聊天", "メインチャット", "メインの会話", "メイン会話", // i18n-ignore: the words a grant's source may hold
    )
    private val A_CHAT = setOf(
        "conversation", "a conversation", "chat", "a chat",
        "对话", "会话", "会話", "チャット", // i18n-ignore: the words a grant's source may hold
    )
    private val SETTINGS = setOf("settings", "设置", "設定") // i18n-ignore: the words a grant's source may hold
    private val SUGGESTED = listOf(
        Regex("""^Suggested rule: the same answer (\d+) times in 14 days, confirmed with Always\.$"""),
        Regex("""^建议的规则：14 天内 (\d+) 次相同的回答，点「总是」确认。$"""), // i18n-ignore: the hub's sentence
        Regex("""^提案されたルール：14 日間に同じ回答が (\d+) 回あり、「常に」で確認されました。$"""), // i18n-ignore: the hub's sentence
    )
}
