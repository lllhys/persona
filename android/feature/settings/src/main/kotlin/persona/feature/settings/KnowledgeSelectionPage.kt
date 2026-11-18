package persona.feature.settings

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.foundation.text.KeyboardOptions
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import persona.core.contracts.api.ConnectorsManagement
import persona.core.contracts.api.HubConnector
import persona.core.contracts.api.KnowledgeSelection
import persona.core.contracts.api.KnowledgeSelectionCandidate
import persona.core.contracts.api.KnowledgeSelectionCandidates
import persona.core.designsystem.GroupPosition
import persona.core.designsystem.JButton
import persona.core.designsystem.JButtonKind
import persona.core.designsystem.JAction
import persona.core.designsystem.JActionSheet
import persona.core.designsystem.JEmptyState
import persona.core.designsystem.JGroupItem
import persona.core.designsystem.JIconButton
import persona.core.designsystem.JListPage
import persona.core.designsystem.JLoading
import persona.core.designsystem.JNotice
import persona.core.designsystem.JRow
import persona.core.designsystem.JSearchField
import persona.core.designsystem.JSectionHeader
import persona.core.designsystem.JSheet
import persona.core.designsystem.JSwitchRow
import persona.core.designsystem.JTextField
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Status
import persona.core.designsystem.Tokens
import persona.core.designsystem.currentAssistant
import persona.core.designsystem.failureReasonText
import persona.core.designsystem.icons.PersonaIcons

private data class RuleEditor(val part: KnowledgeSelectionPart, val name: String, val opened: KnowledgeSelectionRule)
private data class LevelEditor(val part: KnowledgeSelectionPart, val firstRule: String)
private data class ItemEditor(val item: KnowledgeSelectionItem, val isNew: Boolean)

/** Settings › Knowledge Selection: a whole hub document, edited immediately without a page-level draft or Save button. */
@Composable
internal fun KnowledgeSelectionPage(
    api: KnowledgeSelection?,
    connectors: ConnectorsManagement?,
    onBack: () -> Unit,
) {
    BackHandler(onBack = onBack)
    if (api == null) {
        JListPage(title = stringResource(R.string.settings_knowledge_selection_title), onBack = onBack) {
            item { JNotice(stringResource(R.string.settings_knowledge_selection_demo)) }
        }
        return
    }
    val model = remember(api) { KnowledgeSelectionModel(api) }
    val state by model.state.collectAsState()
    val listState = rememberLazyListState()
    val scope = rememberCoroutineScope()
    val assistant = currentAssistant()
    var sources by remember(connectors, assistant.id) { mutableStateOf<List<HubConnector>?>(null) }
    var sourcesError by remember(connectors, assistant.id) { mutableStateOf<ResText?>(null) }
    var sourcesRetry by remember(connectors, assistant.id) { mutableStateOf(0) }
    var levelEditor by remember { mutableStateOf<LevelEditor?>(null) }
    var ruleEditor by remember { mutableStateOf<RuleEditor?>(null) }
    var itemEditor by remember { mutableStateOf<ItemEditor?>(null) }
    var chatPicker by remember { mutableStateOf(false) }
    var documentPicker by remember { mutableStateOf(false) }

    LaunchedEffect(model) { model.load() }
    LaunchedEffect(model) { api.changes.collect { model.onExternalChange() } }
    LaunchedEffect(state.saveError, state.readOnly) {
        // A refusal can belong to a rule far down the page; bring its one explanation into view.
        if (state.saveError != null || state.readOnly) listState.animateScrollToItem(0)
    }
    LaunchedEffect(state.unsupportedDocument) {
        if (state.unsupportedDocument) {
            levelEditor = null
            ruleEditor = null
            itemEditor = null
            chatPicker = false
            documentPicker = false
        }
    }
    LaunchedEffect(connectors, assistant.id, sourcesRetry) {
        sources = null
        sourcesError = null
        sources = try {
            connectors?.connectors(all = true).orEmpty().filter {
                assistant.owns(it.manager) && it.capabilities?.contains("history") == true
            }
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            sourcesError = ResText.of(R.string.settings_knowledge_selection_picker_failed, failureReasonText(error))
            null
        }
    }

    fun edit(change: (KnowledgeSelectionDocument) -> KnowledgeSelectionDocument) {
        scope.launch { model.edit(change) }
    }

    JListPage(title = stringResource(R.string.settings_knowledge_selection_title), onBack = onBack, state = listState) {
        if (state.readOnly) item("read-only") {
            JNotice(stringResource(R.string.settings_knowledge_selection_read_only), status = Status.NeedsYou)
        }
        state.saveError?.let { error -> item("save-error") { JNotice(error.text(), status = Status.NeedsYou) } }
        state.removedItem?.let { removed -> item("undo") {
            JNotice(stringResource(R.string.settings_knowledge_selection_setting_removed),
                actionLabel = if (state.editable) stringResource(R.string.settings_knowledge_selection_undo) else null,
                onAction = if (state.editable) ({ scope.launch { model.undoRemoval() } }) else null)
        } }
        val document = state.document
        if (document == null && state.loading) item("loading") { JLoading() }
        else if (state.unsupported) item("old-hub") {
            JNotice(stringResource(R.string.settings_knowledge_selection_old_hub), status = Status.NeedsYou)
        } else if (state.unsupportedDocument) item("unsupported-document") {
            JNotice(stringResource(R.string.settings_knowledge_selection_unsupported_document), status = Status.NeedsYou)
        } else if (document == null) {
            item("read-error") {
                JNotice(state.readError?.text() ?: stringResource(R.string.settings_knowledge_selection_old_hub),
                    status = Status.NeedsYou, actionLabel = stringResource(R.string.settings_retry_read),
                    onAction = { scope.launch { model.load() } })
            }
        } else {
            state.readError?.let { error -> item("refresh-error") { JNotice(error.text(), status = Status.NeedsYou) } }
            item("items-header") { JSectionHeader(stringResource(R.string.settings_knowledge_selection_items_title)) }
            val items = document.items()
            items.forEachIndexed { index, item ->
                item("item-${item.id.source}-${item.id.kind}-${item.id.id}") {
                    JGroupItem(GroupPosition.of(index, items.size)) {
                        JRow(
                            title = item.name.ifBlank { item.id.id },
                            subtitle = itemModeTitle(item).let { mode -> "${item.id.source} · $mode" },
                            value = item.days?.let { stringResource(R.string.settings_knowledge_selection_days_value, it) },
                            enabled = state.editable,
                            onClick = { itemEditor = ItemEditor(item, isNew = false) },
                            modifier = Modifier.testTag("knowledge-item-${item.id.id}"),
                        )
                    }
                }
            }
            item("item-add") {
                Column(
                    Modifier.fillMaxWidth().padding(top = Tokens.Space.s),
                    verticalArrangement = Arrangement.spacedBy(Tokens.Space.s),
                ) {
                    JButton(stringResource(R.string.settings_knowledge_selection_add_chat), { chatPicker = true },
                        Modifier.fillMaxWidth(), kind = JButtonKind.Secondary, enabled = state.editable)
                    JButton(stringResource(R.string.settings_knowledge_selection_add_document), { documentPicker = true },
                        Modifier.fillMaxWidth(), kind = JButtonKind.Secondary, enabled = state.editable)
                }
            }
            item("items-about") { SettingsCaption(stringResource(R.string.settings_knowledge_selection_items_about),
                Modifier.padding(horizontal = Tokens.Space.page, vertical = Tokens.Space.s)) }
            KnowledgeSelectionPart.entries.forEach { part ->
                val levels = document.order(part)
                levels.forEachIndexed { index, level ->
                    val key = "${part.wire}-$index"
                    item("header-$key") {
                        KnowledgeSelectionLevelHeader(part, index, levels.size, level.size, state.editable,
                            onActions = { level.firstOrNull()?.let { first -> levelEditor = LevelEditor(part, first) } })
                    }
                    level.forEachIndexed { ruleIndex, name ->
                        val rule = document.rule(part, name) ?: return@forEachIndexed
                        item("rule-${part.wire}-$name") {
                            JGroupItem(GroupPosition.of(ruleIndex, level.size)) {
                                val title = knowledgeRuleTitle(part, name)
                                val up = stringResource(R.string.settings_knowledge_selection_move_up)
                                val down = stringResource(R.string.settings_knowledge_selection_move_down)
                                val merge = stringResource(R.string.settings_knowledge_selection_merge_into, title, index)
                                val split = stringResource(R.string.settings_knowledge_selection_own_level, title)
                                val accessibility = buildList {
                                    if (state.editable && index > 0) add(CustomAccessibilityAction(up) {
                                        edit { it.moveLevel(part, name, -1) }; true
                                    })
                                    if (state.editable && index < levels.lastIndex) add(CustomAccessibilityAction(down) {
                                        edit { it.moveLevel(part, name, 1) }; true
                                    })
                                    if (state.editable && index > 0) add(CustomAccessibilityAction(merge) {
                                        edit { it.regroup(part, name, joinPrevious = true) }; true
                                    })
                                    if (state.editable && level.size > 1) add(CustomAccessibilityAction(split) {
                                        edit { it.regroup(part, name, joinPrevious = false) }; true
                                    })
                                }
                                JRow(
                                    title = title,
                                    subtitle = knowledgeRuleSubtitle(part, name),
                                    subtitleLines = 1,
                                    value = when {
                                        !rule.on -> stringResource(R.string.settings_knowledge_selection_off)
                                        rule.days != null -> stringResource(R.string.settings_knowledge_selection_days_value, rule.days!!)
                                        else -> stringResource(R.string.settings_knowledge_selection_on)
                                    },
                                    enabled = state.editable,
                                    onClick = { ruleEditor = RuleEditor(part, name, rule) },
                                    modifier = Modifier.testTag("knowledge-rule-${part.wire}-$name").semantics {
                                        customActions = accessibility
                                    },
                                )
                            }
                        }
                    }
                }
                item("about-${part.wire}") {
                    SettingsCaption(stringResource(
                        if (part == KnowledgeSelectionPart.Messages) R.string.settings_knowledge_selection_messages_about
                        else R.string.settings_knowledge_selection_documents_about,
                    ), Modifier.padding(horizontal = Tokens.Space.page, vertical = Tokens.Space.s))
                }
            }
        }
    }

    levelEditor?.let { opened ->
        val levels = state.document?.order(opened.part).orEmpty()
        val openedIndex = levels.indexOfFirst { it.firstOrNull() == opened.firstRule }
        if (openedIndex < 0) {
            LaunchedEffect(opened) { levelEditor = null }
        } else {
            val level = levels[openedIndex]
            val actions = buildList {
                add(JAction(stringResource(R.string.settings_knowledge_selection_move_up), PersonaIcons.queueUp,
                    enabled = state.editable && openedIndex > 0,
                    onClick = {
                        edit { it.moveLevel(opened.part, opened.firstRule, -1) }
                        levelEditor = null
                    }))
                add(JAction(stringResource(R.string.settings_knowledge_selection_move_down), PersonaIcons.queueDown,
                    enabled = state.editable && openedIndex < levels.lastIndex,
                    onClick = {
                        edit { it.moveLevel(opened.part, opened.firstRule, 1) }
                        levelEditor = null
                    }))
                level.forEach { name ->
                    val title = knowledgeRuleTitle(opened.part, name)
                    if (openedIndex > 0) add(JAction(
                        stringResource(R.string.settings_knowledge_selection_merge_into, title, openedIndex),
                        enabled = state.editable,
                        onClick = {
                            edit { it.regroup(opened.part, name, joinPrevious = true) }
                            levelEditor = null
                        }))
                    if (level.size > 1) add(JAction(stringResource(R.string.settings_knowledge_selection_own_level, title),
                        enabled = state.editable,
                        onClick = {
                            edit { it.regroup(opened.part, name, joinPrevious = false) }
                            levelEditor = null
                        }))
                }
            }
            JActionSheet(actions, onDismiss = { levelEditor = null },
                title = stringResource(if (opened.part == KnowledgeSelectionPart.Messages) R.string.settings_knowledge_selection_chat_priority
                    else R.string.settings_knowledge_selection_document_priority, openedIndex + 1))
        }
    }

    val editorProblem = state.saveError ?: if (state.readOnly) ResText.of(R.string.settings_knowledge_selection_read_only) else null
    ruleEditor?.let { editor ->
        KnowledgeRuleSheet(editor.part, editor.name, editor.opened, state.saving, state.readOnly, editorProblem,
            onDismiss = { ruleEditor = null },
            onSave = { on, days, attachments, includeAll, context ->
                scope.launch {
                    if (model.saveRule(editor.opened, editor.part, editor.name, on, days, attachments, includeAll, context)) {
                        ruleEditor = null
                    }
                }
            })
    }

    itemEditor?.let { opened ->
        KnowledgeItemSheet(opened.item, opened.isNew, state.saving, state.readOnly, editorProblem,
            onDismiss = { itemEditor = null },
            onSave = { item -> scope.launch { if (model.saveItem(opened.item, item, opened.isNew)) itemEditor = null } },
            onRemove = { item -> scope.launch { if (model.removeItem(item)) itemEditor = null } })
    }

    if (chatPicker) {
        var source by rememberSaveable { mutableStateOf<String?>(null) }
        LaunchedEffect(sources) { if (source == null && sources?.size == 1) source = sources!!.single().id }
        LaunchedEffect(source) { source?.let { model.loadCandidates(it) } }
        JSheet(onDismiss = { chatPicker = false }, title = stringResource(R.string.settings_knowledge_selection_add_chat_title), scrollable = true) {
            KnowledgeChatPickerContent(sources, source, state.candidates, state.candidatesLoading,
                state.candidateUnavailable, state.candidateError, enabled = state.editable,
                sourceError = sourcesError, onRetrySources = { sourcesRetry++ }, onSource = { source = it },
                onCandidate = {
                    val item = model.chatItem(it)
                    itemEditor = ItemEditor(item, isNew = state.document?.item(item.id) == null)
                    chatPicker = false
                }, onRetry = { scope.launch { source?.let { model.loadCandidates(it) } } })
        }
    }

    if (documentPicker) {
        var query by rememberSaveable { mutableStateOf("") }
        var adding by remember { mutableStateOf(false) }
        var documentRequest by remember { mutableStateOf(0L) }
        val pickerScope = rememberCoroutineScope()
        LaunchedEffect(documentPicker) { model.loadDocuments() }
        JSheet(onDismiss = { documentPicker = false; documentRequest++ }, title = stringResource(R.string.settings_knowledge_selection_add_document_title), scrollable = true) {
            Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                JSearchField(query, { query = it }, stringResource(R.string.settings_knowledge_selection_picker_search), Modifier.fillMaxWidth())
                when {
                    state.documentsLoading -> JLoading()
                    state.documentsError != null -> JNotice(state.documentsError!!.text(), status = Status.NeedsYou,
                        actionLabel = stringResource(R.string.settings_retry_read), onAction = { scope.launch { model.loadDocuments() } })
                    state.documents.orEmpty().isEmpty() -> JEmptyState(stringResource(R.string.settings_knowledge_selection_no_candidates),
                        stringResource(R.string.settings_knowledge_selection_no_documents), icon = PersonaIcons.knowledge)
                    else -> state.documents.orEmpty().filter { it.name.contains(query, ignoreCase = true) }.forEach { entry ->
                        JRow(entry.name, subtitle = entry.oneLine, enabled = !adding && state.editable, onClick = {
                            val request = ++documentRequest
                            pickerScope.launch {
                                adding = true
                                val item = model.documentItem(entry)
                                adding = false
                                if (item != null && documentPicker && request == documentRequest) {
                                    itemEditor = ItemEditor(item, isNew = state.document?.item(item.id) == null)
                                    documentPicker = false
                                }
                            }
                        })
                    }
                }
            }
            JButton(stringResource(R.string.settings_cancel), { documentPicker = false; documentRequest++ },
                Modifier.fillMaxWidth(), kind = JButtonKind.Quiet)
        }
    }
}

@Composable
internal fun KnowledgeSelectionLevelHeader(
    part: KnowledgeSelectionPart,
    index: Int,
    levelCount: Int,
    ruleCount: Int,
    enabled: Boolean,
    onActions: () -> Unit,
) {
    val title = stringResource(
        if (part == KnowledgeSelectionPart.Messages) R.string.settings_knowledge_selection_chat_priority
        else R.string.settings_knowledge_selection_document_priority,
        index + 1,
    )
    if (levelCount > 1 || ruleCount > 1) {
        JSectionHeader(title, action = {
            JIconButton(PersonaIcons.more, stringResource(R.string.settings_knowledge_selection_level_actions, title), onActions,
                Modifier.testTag("knowledge-priority-${part.wire}-$index"), enabled = enabled)
        })
    } else {
        JSectionHeader(title)
    }
}

@Composable
internal fun KnowledgeChatPickerContent(
    sources: List<HubConnector>?,
    source: String?,
    candidates: KnowledgeSelectionCandidates?,
    loading: Boolean,
    unavailable: Boolean,
    error: ResText?,
    enabled: Boolean = true,
    sourceError: ResText? = null,
    onRetrySources: () -> Unit = {},
    onSource: (String?) -> Unit,
    onCandidate: (KnowledgeSelectionCandidate) -> Unit,
    onRetry: () -> Unit,
) {
    var query by rememberSaveable(source) { mutableStateOf("") }
    Column(Modifier.fillMaxWidth().imePadding(), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
        when {
            sourceError != null -> JNotice(sourceError.text(), status = Status.NeedsYou,
                actionLabel = stringResource(R.string.settings_retry_read), onAction = onRetrySources)
            sources == null -> JLoading()
            sources.isEmpty() -> JNotice(stringResource(R.string.settings_knowledge_selection_no_sources))
            source == null -> sources.forEach { connector ->
                JRow(connector.name, subtitle = connector.account, enabled = enabled, onClick = { onSource(connector.id) })
            }
            else -> {
                if (sources.size > 1) JRow(stringResource(R.string.settings_knowledge_selection_source),
                    value = sources.firstOrNull { it.id == source }?.name ?: source, enabled = enabled, onClick = { onSource(null) })
                JSearchField(query, { query = it }, stringResource(R.string.settings_knowledge_selection_picker_search), Modifier.fillMaxWidth())
                when {
                    loading -> JLoading()
                    unavailable -> JNotice(stringResource(R.string.settings_knowledge_selection_no_picker))
                    error != null -> JNotice(error.text(), status = Status.NeedsYou,
                        actionLabel = stringResource(R.string.settings_retry_read), onAction = onRetry)
                    candidates == null -> JLoading()
                    else -> {
                        if (candidates.cached) JNotice(stringResource(R.string.settings_knowledge_selection_cached))
                        val filtered = candidates.chats.filter { it.name.contains(query, ignoreCase = true) }
                        if (filtered.isEmpty()) JEmptyState(stringResource(R.string.settings_knowledge_selection_no_candidates),
                            stringResource(R.string.settings_knowledge_selection_no_chats), icon = PersonaIcons.knowledge)
                        filtered.forEach { candidate ->
                            val details = buildList {
                                knowledgeChatKindTitle(candidate.kind)?.let(::add)
                                add(stringResource(when (candidate.read) {
                                    "whole" -> R.string.settings_knowledge_selection_read_whole
                                    "personal" -> R.string.settings_knowledge_selection_read_personal
                                    else -> R.string.settings_knowledge_selection_read_none
                                }))
                                if (candidate.estimate > 0) add(stringResource(R.string.settings_knowledge_selection_estimate, candidate.estimate))
                                if (candidate.days > 0) add(stringResource(R.string.settings_knowledge_selection_days_value, candidate.days))
                            }
                            JRow(candidate.name, subtitle = details.joinToString(" · "), subtitleLines = Int.MAX_VALUE, enabled = enabled,
                                onClick = { onCandidate(candidate) }, modifier = Modifier.testTag("knowledge-candidate-${candidate.id}"))
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun KnowledgeRuleSheet(
    part: KnowledgeSelectionPart,
    name: String,
    rule: KnowledgeSelectionRule,
    saving: Boolean,
    readOnly: Boolean,
    problem: ResText?,
    onDismiss: () -> Unit,
    onSave: (Boolean, Int?, Int?, Boolean?, Int?) -> Unit,
) {
    var on by remember { mutableStateOf(rule.on) }
    var days by remember { mutableStateOf(rule.days?.toString().orEmpty()) }
    var attachments by remember { mutableStateOf(rule.attachmentDays?.toString().orEmpty()) }
    var includeAll by remember { mutableStateOf(rule.includeAll) }
    var context by remember { mutableStateOf(rule.context?.toString().orEmpty()) }
    val parsedDays = parseNumber(days, 1..36500)
    val parsedAttachments = parseNumber(attachments, 1..36500)
    val parsedContext = parseNumber(context, 0..200)
    val valid = parsedDays.valid && (part == KnowledgeSelectionPart.Documents || parsedAttachments.valid) &&
        (part == KnowledgeSelectionPart.Documents || name != "mine" || parsedContext.valid)
    val disabled = saving || readOnly
    JSheet(onDismiss, title = knowledgeRuleTitle(part, name), dismissible = !saving, scrollable = true) {
        Column(Modifier.fillMaxWidth().imePadding(), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            problem?.let { JNotice(it.text(), Modifier.testTag("knowledge-rule-problem"), status = Status.NeedsYou) }
            JSwitchRow(stringResource(R.string.settings_knowledge_selection_store_matching), on, { on = it }, enabled = !disabled,
                modifier = Modifier.testTag("knowledge-rule-sheet-on"))
            JTextField(days, { days = it }, Modifier.fillMaxWidth().testTag("knowledge-rule-days"),
                label = stringResource(R.string.settings_knowledge_selection_days_text), placeholder = stringResource(R.string.settings_default),
                supportingText = stringResource(if (part == KnowledgeSelectionPart.Messages) R.string.settings_knowledge_selection_days_hint_messages
                    else R.string.settings_knowledge_selection_days_hint_documents), singleLine = true, enabled = !disabled, isError = !parsedDays.valid,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number))
            if (part == KnowledgeSelectionPart.Messages) {
                JTextField(attachments, { attachments = it }, Modifier.fillMaxWidth().testTag("knowledge-rule-attachment-days"),
                    label = stringResource(R.string.settings_knowledge_selection_days_attachments), placeholder = stringResource(R.string.settings_default),
                    singleLine = true, enabled = !disabled, isError = !parsedAttachments.valid,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number))
                if (name == "mentions") JSwitchRow(stringResource(R.string.settings_knowledge_selection_include_all),
                    includeAll ?: true, { includeAll = it }, enabled = !disabled)
                if (name == "mine") JTextField(context, { context = it }, Modifier.fillMaxWidth().testTag("knowledge-rule-context"),
                    label = stringResource(R.string.settings_knowledge_selection_mine_context), placeholder = "50",
                    supportingText = stringResource(R.string.settings_knowledge_selection_mine_context_hint), singleLine = true,
                    enabled = !disabled, isError = !parsedContext.valid, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number))
            }
            if (!valid) JNotice(stringResource(R.string.settings_knowledge_selection_number_invalid), status = Status.NeedsYou)
            Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                JButton(stringResource(R.string.settings_cancel), onDismiss, Modifier.weight(1f), kind = JButtonKind.Quiet, enabled = !saving)
                JButton(stringResource(R.string.settings_knowledge_selection_save), {
                    if (valid) onSave(on, parsedDays.value, parsedAttachments.value, includeAll, parsedContext.value)
                }, Modifier.weight(1f).testTag("knowledge-rule-save"), enabled = valid && !disabled, loading = saving)
            }
        }
    }
}

@Composable
private fun KnowledgeItemSheet(
    item: KnowledgeSelectionItem,
    isNew: Boolean,
    saving: Boolean,
    readOnly: Boolean,
    problem: ResText?,
    onDismiss: () -> Unit,
    onSave: (KnowledgeSelectionItem) -> Unit,
    onRemove: (KnowledgeSelectionItem) -> Unit,
) {
    var mode by remember { mutableStateOf(item.mode) }
    var days by remember { mutableStateOf(item.days?.toString().orEmpty()) }
    val parsed = parseNumber(days, 1..36500)
    val valid = mode != KnowledgeItemMode.All || parsed.valid
    val changed = isNew || mode != item.mode || (mode == KnowledgeItemMode.All && parsed.value != item.days)
    val disabled = saving || readOnly
    JSheet(onDismiss, title = stringResource(if (item.id.kind == "chat") R.string.settings_knowledge_selection_chat_setting
        else R.string.settings_knowledge_selection_document_setting), dismissible = !saving, scrollable = true) {
        Column(Modifier.fillMaxWidth().imePadding(), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            problem?.let { JNotice(it.text(), Modifier.testTag("knowledge-item-problem"), status = Status.NeedsYou) }
            Text(item.name.ifBlank { item.id.id }, style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink)
            Text(stringResource(R.string.settings_knowledge_selection_source_value, item.id.source),
                style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3)
            Text(stringResource(R.string.settings_knowledge_selection_what_to_store), style = PersonaTheme.type.callout,
                color = PersonaTheme.palette.ink2)
            KnowledgeItemMode.entries.forEach { choice ->
                JRow(itemModeTitle(choice, item.id.kind), subtitle = itemModeDetail(choice, item.id.kind), subtitleLines = Int.MAX_VALUE,
                    selected = mode == choice, enabled = !disabled, onClick = { mode = choice },
                    modifier = Modifier.testTag("knowledge-item-mode-${choice.name}"))
            }
            if (mode == KnowledgeItemMode.All) {
                JTextField(days, { days = it }, Modifier.fillMaxWidth().testTag("knowledge-item-days"),
                    label = stringResource(R.string.settings_knowledge_selection_days_keep), placeholder = stringResource(R.string.settings_default),
                    supportingText = if (!parsed.valid) stringResource(R.string.settings_knowledge_selection_item_days_invalid) else null,
                    singleLine = true, enabled = !disabled, isError = !parsed.valid,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number))
            }
            Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                JButton(stringResource(R.string.settings_cancel), onDismiss, Modifier.weight(1f), kind = JButtonKind.Quiet, enabled = !saving)
                JButton(stringResource(if (isNew) R.string.settings_knowledge_selection_add else R.string.settings_knowledge_selection_save), {
                    if (valid) onSave(item.copy(mode = mode, days = if (mode == KnowledgeItemMode.All) parsed.value else null))
                }, Modifier.weight(1f).testTag("knowledge-item-save"), enabled = valid && changed && !disabled, loading = saving)
            }
            if (!isNew) JButton(stringResource(R.string.settings_knowledge_selection_remove_setting), { onRemove(item) },
                Modifier.fillMaxWidth().testTag("knowledge-item-remove"), kind = JButtonKind.Destructive, enabled = !disabled)
        }
    }
}

private data class ParsedNumber(val value: Int?, val valid: Boolean)
private fun parseNumber(text: String, range: IntRange): ParsedNumber =
    runCatching { knowledgeSelectionNumber(text, range) }.fold({ ParsedNumber(it, true) }, { ParsedNumber(null, false) })

@Composable
private fun knowledgeRuleTitle(part: KnowledgeSelectionPart, name: String): String {
    val title = when {
        name == "mentions" -> R.string.settings_knowledge_selection_rule_mentions
        part == KnowledgeSelectionPart.Documents && name == "comment_mentions" -> R.string.settings_knowledge_selection_rule_comment_mentions
        part == KnowledgeSelectionPart.Documents && name == "authored" -> R.string.settings_knowledge_selection_rule_authored
        part == KnowledgeSelectionPart.Documents && name == "viewed" -> R.string.settings_knowledge_selection_rule_viewed
        part == KnowledgeSelectionPart.Messages && name == "mine" -> R.string.settings_knowledge_selection_rule_mine
        part == KnowledgeSelectionPart.Messages && name == "dm" -> R.string.settings_knowledge_selection_rule_dm
        part == KnowledgeSelectionPart.Messages && name == "group" -> R.string.settings_knowledge_selection_rule_group
        part == KnowledgeSelectionPart.Messages && name == "bot" -> R.string.settings_knowledge_selection_rule_bot
        part == KnowledgeSelectionPart.Messages && name == "official" -> R.string.settings_knowledge_selection_rule_official
        part == KnowledgeSelectionPart.Messages && name == "muted" -> R.string.settings_knowledge_selection_rule_muted
        part == KnowledgeSelectionPart.Messages && name == "collapsed" -> R.string.settings_knowledge_selection_rule_collapsed
        part == KnowledgeSelectionPart.Messages && name == "done" -> R.string.settings_knowledge_selection_rule_done
        else -> null
    }
    return title?.let { stringResource(it) } ?: name
}

@Composable
private fun knowledgeChatKindTitle(kind: String): String? = when (kind) {
    "dm" -> knowledgeRuleTitle(KnowledgeSelectionPart.Messages, "dm")
    "group" -> knowledgeRuleTitle(KnowledgeSelectionPart.Messages, "group")
    "bot" -> knowledgeRuleTitle(KnowledgeSelectionPart.Messages, "bot")
    "official" -> knowledgeRuleTitle(KnowledgeSelectionPart.Messages, "official")
    else -> null
}

@Composable
private fun knowledgeRuleSubtitle(part: KnowledgeSelectionPart, name: String): String? {
    val descriptionResource = when {
        name == "mentions" && part == KnowledgeSelectionPart.Messages -> R.string.settings_knowledge_selection_rule_mentions_about
        name == "mentions" -> R.string.settings_knowledge_selection_rule_doc_mentions_about
        part == KnowledgeSelectionPart.Documents && name == "comment_mentions" -> R.string.settings_knowledge_selection_rule_comment_mentions_about
        part == KnowledgeSelectionPart.Documents && name == "authored" -> R.string.settings_knowledge_selection_rule_authored_about
        part == KnowledgeSelectionPart.Documents && name == "viewed" -> R.string.settings_knowledge_selection_rule_viewed_about
        part == KnowledgeSelectionPart.Messages && name == "mine" -> R.string.settings_knowledge_selection_rule_mine_about
        part == KnowledgeSelectionPart.Messages && name == "dm" -> R.string.settings_knowledge_selection_rule_dm_about
        part == KnowledgeSelectionPart.Messages && name == "group" -> R.string.settings_knowledge_selection_rule_group_about
        part == KnowledgeSelectionPart.Messages && name == "bot" -> R.string.settings_knowledge_selection_rule_bot_about
        part == KnowledgeSelectionPart.Messages && name == "official" -> R.string.settings_knowledge_selection_rule_official_about
        part == KnowledgeSelectionPart.Messages && name == "muted" -> R.string.settings_knowledge_selection_rule_muted_about
        part == KnowledgeSelectionPart.Messages && name == "collapsed" -> R.string.settings_knowledge_selection_rule_collapsed_about
        part == KnowledgeSelectionPart.Messages && name == "done" -> R.string.settings_knowledge_selection_rule_done_about
        else -> null
    }
    return descriptionResource?.let { stringResource(it) }
}

@Composable
private fun itemModeTitle(item: KnowledgeSelectionItem): String = itemModeTitle(item.mode, item.id.kind)

@Composable
private fun itemModeTitle(mode: KnowledgeItemMode, kind: String): String = stringResource(when {
    mode == KnowledgeItemMode.Skip -> R.string.settings_knowledge_selection_mode_skip
    mode == KnowledgeItemMode.All -> R.string.settings_knowledge_selection_mode_all
    kind == "chat" -> R.string.settings_knowledge_selection_mode_rules_chat
    else -> R.string.settings_knowledge_selection_mode_rules_document
})

@Composable
private fun itemModeDetail(mode: KnowledgeItemMode, kind: String): String = stringResource(when {
    mode == KnowledgeItemMode.Skip -> R.string.settings_knowledge_selection_mode_skip_about
    mode == KnowledgeItemMode.Rules && kind == "chat" -> R.string.settings_knowledge_selection_mode_rules_chat_about
    mode == KnowledgeItemMode.Rules -> R.string.settings_knowledge_selection_mode_rules_document_about
    kind == "chat" -> R.string.settings_knowledge_selection_mode_all_chat_about
    else -> R.string.settings_knowledge_selection_mode_all_document_about
})
