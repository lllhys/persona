package persona.feature.settings

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonPrimitive
import persona.core.contracts.api.KnowledgeDetail
import persona.core.contracts.api.KnowledgeEntry
import persona.core.contracts.api.KnowledgeSelection
import persona.core.contracts.api.KnowledgeSelectionCandidate
import persona.core.contracts.api.KnowledgeSelectionCandidates
import persona.core.contracts.api.KnowledgeSelectionUnsupported
import persona.core.designsystem.failureReasonText
import persona.core.hub.HubFailure

internal data class KnowledgeSelectionState(
    val document: KnowledgeSelectionDocument? = null,
    val loading: Boolean = false,
    val saving: Boolean = false,
    val unsupported: Boolean = false,
    val unsupportedDocument: Boolean = false,
    val readOnly: Boolean = false,
    val readError: ResText? = null,
    val saveError: ResText? = null,
    val candidates: KnowledgeSelectionCandidates? = null,
    val candidateSource: String? = null,
    val candidatesLoading: Boolean = false,
    val candidateUnavailable: Boolean = false,
    val candidateError: ResText? = null,
    val documents: List<KnowledgeEntry>? = null,
    val documentsLoading: Boolean = false,
    val documentsError: ResText? = null,
    /** The last removal that was stored, offered as Undo until another removal replaces it. */
    val removedItem: KnowledgeSelectionItem? = null,
) {
    val busy: Boolean get() = loading || saving
    val editable: Boolean get() = document != null && !busy && !readOnly && !unsupported && !unsupportedDocument
}

/** The Settings row's tri-state capability probe: a transient failure never retires a previously available page. */
internal class KnowledgeSelectionAvailability(private val api: KnowledgeSelection) {
    private val mutable = MutableStateFlow<Boolean?>(null)
    val state = mutable.asStateFlow()

    suspend fun probe() {
        try {
            api.selection()
            mutable.value = true
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: KnowledgeSelectionUnsupported) {
            mutable.value = false
        } catch (_: Exception) {
            // Keep the last answer. Initially unknown means hidden, not an apparently broken editor.
        }
    }
}

/**
 * Settings › Knowledge Selection. Every accepted edit is visible first, then applied by identity to a fresh read and
 * written back whole. The hub has no revision/CAS: this preserves changes in that read, not a write after it.
 */
internal class KnowledgeSelectionModel(private val api: KnowledgeSelection) {
    private val mutable = MutableStateFlow(KnowledgeSelectionState())
    val state = mutable.asStateFlow()
    private var generation = 0L
    private var pendingReload = false

    suspend fun load() {
        val before = state.value
        if (before.saving) {
            pendingReload = true
            return
        }
        if (before.loading) return
        val ticket = ++generation
        mutable.update { it.copy(loading = true, readError = null, unsupportedDocument = false) }
        try {
            val fresh = readKnowledgeSelectionDocument(api.selection())
            if (ticket == generation) mutable.update {
                it.copy(document = fresh, unsupported = false, unsupportedDocument = false, readError = null)
            }
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: KnowledgeSelectionUnreadableDocument) {
            if (ticket == generation) mutable.update {
                it.copy(document = null, unsupportedDocument = true, readError = null, saveError = null)
            }
        } catch (_: KnowledgeSelectionUnsupported) {
            if (ticket == generation) mutable.update {
                it.copy(document = null, unsupported = true, readError = ResText.of(R.string.settings_knowledge_selection_old_hub))
            }
        } catch (error: Exception) {
            if (ticket == generation) mutable.update {
                it.copy(readError = ResText.of(R.string.settings_knowledge_selection_read_failed, failureReasonText(error)))
            }
        } finally {
            if (ticket == generation) mutable.update { it.copy(loading = false) }
        }
        drainPendingReload()
    }

    /** An external kb_selection hint. During a save it is coalesced, never turned into a competing read. */
    suspend fun onExternalChange() {
        if (state.value.busy) pendingReload = true else load()
    }

    /** Returns true when the operation completed (including a fresh read that already contained the edit). */
    suspend fun edit(change: (KnowledgeSelectionDocument) -> KnowledgeSelectionDocument): Boolean {
        val before = state.value
        val original = before.document
        if (!before.editable || original == null) return false
        val optimistic = try {
            change(original)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            mutable.update {
                it.copy(saveError = ResText.of(R.string.settings_knowledge_selection_save_failed, failureReasonText(error)))
            }
            return false
        }
        if (optimistic.root == original.root) return true
        val ticket = ++generation
        mutable.update { it.copy(document = optimistic, saving = true, saveError = null) }
        var putAttempted = false
        try {
            val stored = api.withPinnedConnection {
                val fresh = readKnowledgeSelectionDocument(selection())
                val changed = change(fresh)
                if (changed.root == fresh.root) fresh else {
                    putAttempted = true
                    KnowledgeSelectionDocument(put(changed.root))
                }
            }
            if (ticket == generation) mutable.update {
                it.copy(document = stored, unsupported = false, readError = null)
            }
        } catch (cancelled: CancellationException) {
            if (ticket == generation) mutable.update { it.copy(document = original) }
            throw cancelled
        } catch (_: KnowledgeSelectionUnreadableDocument) {
            if (ticket == generation) mutable.update {
                it.copy(document = null, unsupportedDocument = true, readError = null, saveError = null)
            }
        } catch (_: KnowledgeSelectionUnsupported) {
            if (ticket == generation) mutable.update {
                it.copy(document = original, unsupported = true,
                    saveError = ResText.of(R.string.settings_knowledge_selection_old_hub))
            }
        } catch (error: Exception) {
            if (ticket == generation) mutable.update {
                val refusedReadOnly = putAttempted && error is HubFailure && error.status == 403
                it.copy(
                    document = original,
                    readOnly = refusedReadOnly || it.readOnly,
                    saveError = if (refusedReadOnly) null else ResText.of(
                        R.string.settings_knowledge_selection_save_failed,
                        failureReasonText(error),
                    ),
                )
            }
        } finally {
            if (ticket == generation) mutable.update { it.copy(saving = false) }
        }
        val completed = state.value.saveError == null && !state.value.unsupported && !state.value.unsupportedDocument && !state.value.readOnly
        drainPendingReload()
        return completed
    }

    /** A sheet may stay open through an external reload: reapply only fields changed from the values it opened with. */
    suspend fun saveRule(
        opened: KnowledgeSelectionRule,
        part: KnowledgeSelectionPart,
        name: String,
        on: Boolean,
        days: Int?,
        attachmentDays: Int?,
        includeAll: Boolean?,
        context: Int?,
    ) = edit { document ->
        val current = document.rule(part, name)
        if (current == null) document else document.setRule(
            part, name,
            on = if (on != opened.on) on else current.on,
            days = if (days != opened.days) days else current.days,
            attachmentDays = if (attachmentDays != opened.attachmentDays) attachmentDays else current.attachmentDays,
            includeAll = if (includeAll != opened.includeAll) includeAll else current.includeAll,
            context = if (context != opened.context) context else current.context,
        )
    }

    /** Mode and days are one item-sheet edit; a fresh item keeps its name, unknown fields and optional spellings. */
    suspend fun saveItem(opened: KnowledgeSelectionItem, item: KnowledgeSelectionItem, isNew: Boolean) = edit { document ->
        val current = document.item(item.id)
        when {
            !isNew && item.mode == opened.mode && item.days == opened.days -> document
            current == null -> document.setItem(item)
            else -> document.setItem(current.copy(mode = item.mode, days = item.days))
        }
    }

    suspend fun removeItem(item: KnowledgeSelectionItem): Boolean {
        val wasPresent = state.value.document?.item(item.id) != null
        val removed = edit { it.removeItem(item.id) }
        if (removed && wasPresent) mutable.update { it.copy(removedItem = item) }
        return removed
    }

    suspend fun undoRemoval() {
        val item = state.value.removedItem ?: return
        if (edit { document -> if (document.item(item.id) == null) document.setItem(item) else document }) {
            mutable.update { it.copy(removedItem = null) }
        }
    }

    suspend fun loadCandidates(source: String) {
        mutable.update {
            it.copy(candidateSource = source, candidates = null, candidatesLoading = true,
                candidateUnavailable = false, candidateError = null)
        }
        try {
            val result = api.candidates(source)
            // A slower answer for a source whose sheet was replaced cannot populate the new sheet.
            if (state.value.candidateSource == source) mutable.update { it.copy(candidates = result) }
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: KnowledgeSelectionUnsupported) {
            if (state.value.candidateSource == source) mutable.update { it.copy(candidateUnavailable = true) }
        } catch (error: Exception) {
            if (state.value.candidateSource == source) mutable.update {
                it.copy(candidateError = ResText.of(R.string.settings_knowledge_selection_picker_failed, failureReasonText(error)))
            }
        } finally {
            if (state.value.candidateSource == source) mutable.update { it.copy(candidatesLoading = false) }
        }
    }

    suspend fun loadDocuments() {
        if (state.value.documentsLoading) return
        mutable.update { it.copy(documentsLoading = true, documentsError = null) }
        try {
            val result = api.documents()
            mutable.update { it.copy(documents = result.items) }
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            mutable.update {
                it.copy(documentsError = ResText.of(R.string.settings_knowledge_selection_picker_failed, failureReasonText(error)))
            }
        } finally {
            mutable.update { it.copy(documentsLoading = false) }
        }
    }

    /** The candidate's filing source, not the connector whose list was queried, is the stable identity. */
    fun chatItem(candidate: KnowledgeSelectionCandidate): KnowledgeSelectionItem {
        val id = KnowledgeItemId(candidate.source, "chat", candidate.id)
        state.value.document?.item(id)?.let { return it }
        val existing = candidate.item?.let { KnowledgeSelectionItem.fromRaw(it) }
        return existing?.copy(id = id, name = candidate.name) ?: KnowledgeSelectionItem.newChat(candidate.source, candidate.id, candidate.name)
    }

    /** Documents use detail.meta.source + id; the KB detail.id is only the lookup key. */
    suspend fun documentItem(entry: KnowledgeEntry): KnowledgeSelectionItem? {
        val detail = try {
            api.document(entry.id)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            mutable.update {
                it.copy(documentsError = ResText.of(R.string.settings_knowledge_selection_picker_failed, failureReasonText(error)))
            }
            return null
        }
        val picked = storedDocumentItem(detail)
        if (picked == null) {
            mutable.update { it.copy(documentsError = ResText.of(R.string.settings_knowledge_selection_identity_missing)) }
            return null
        }
        return state.value.document?.item(picked.id) ?: picked.copy(name = entry.name)
    }

    private fun storedDocumentItem(detail: KnowledgeDetail): KnowledgeSelectionItem? {
        val source = detail.meta.string("source")
        val id = detail.meta.string("id")
        if (source.isNullOrEmpty() || id.isNullOrEmpty()) return null
        return KnowledgeSelectionItem.newDocument(source, id, detail.path.substringAfterLast('/'))
    }

    private suspend fun drainPendingReload() {
        if (!pendingReload || state.value.busy) return
        pendingReload = false
        load()
    }
}

private class KnowledgeSelectionUnreadableDocument(cause: Throwable) : Exception(cause)

private fun readKnowledgeSelectionDocument(root: JsonObject): KnowledgeSelectionDocument = try {
    KnowledgeSelectionDocument(root)
} catch (error: Exception) {
    throw KnowledgeSelectionUnreadableDocument(error)
}

private fun JsonObject?.string(key: String): String? =
    (this?.get(key) as? kotlinx.serialization.json.JsonPrimitive)?.takeIf { it.isString }?.content
