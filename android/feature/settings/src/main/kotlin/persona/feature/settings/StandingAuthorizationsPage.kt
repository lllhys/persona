package persona.feature.settings

import android.content.res.Resources
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import persona.core.contracts.Machine
import persona.core.contracts.StandingAuthorization
import persona.core.contracts.api.StandingAuthorizationsSource
import persona.core.designsystem.JAction
import persona.core.designsystem.JActionSheet
import persona.core.designsystem.JButton
import persona.core.designsystem.JButtonKind
import persona.core.designsystem.JDialog
import persona.core.designsystem.JEmptyState
import persona.core.designsystem.JGroupItem
import persona.core.designsystem.JListPage
import persona.core.designsystem.JLoading
import persona.core.designsystem.JNotice
import persona.core.designsystem.JRow
import persona.core.designsystem.JSectionHeader
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Status
import persona.core.designsystem.Tokens
import persona.core.designsystem.failureReasonText
import persona.core.designsystem.icons.PersonaIcons
import persona.core.designsystem.pagePadding
import persona.core.designsystem.text

/** How long "Authorization revoked" stays when the user does not move on. A toast's length. */
private const val REVOKED_NOTICE_MILLIS = 4_000L

/**
 * Settings › Standing Authorizations (the same page as iOS). The list names the folder and the computer, what Persona
 * may do, and the short conditions. The full path, the owner's words and the rest are on the grant's own page.
 * Revoke asks first. A failed revoke keeps the row. There is no Undo.
 */
@Composable
internal fun StandingAuthorizationsPage(
    source: StandingAuthorizationsSource?,
    machines: List<Machine>,
    onBack: () -> Unit,
) {
    if (source == null) {
        BackHandler(onBack = onBack)
        JListPage(title = stringResource(R.string.settings_standing_title), onBack = onBack, modifier = Modifier.testTag("standing-list")) {
            item("demo") { JNotice(stringResource(R.string.settings_standing_demo)) }
        }
        return
    }
    // A new hub drops the previous hub's rows. Revoke uses the client pinned to the hub those rows came from.
    val hubId by source.hubIdentity.collectAsState()
    val pinned = remember(hubId) { source.pinned() }
    val model = remember(pinned) { StandingAuthorizationsModel(pinned) }
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    var selectedId by remember(pinned) { mutableStateOf<String?>(null) }
    var confirming by remember(pinned) { mutableStateOf<StandingAuthorization?>(null) }
    var menu by remember(pinned) { mutableStateOf<StandingAuthorization?>(null) }
    LaunchedEffect(model) { model.load() }
    LaunchedEffect(state.notice) {
        if (state.notice is StandingNotice.Revoked) {
            delay(REVOKED_NOTICE_MILLIS)
            model.dismissRevoked()
        }
    }
    val selected = state.grants?.firstOrNull { it.id == selectedId }
    LaunchedEffect(state.grants, selectedId) {
        if (selectedId != null && state.grants?.any { it.id == selectedId } != true) selectedId = null
    }
    BackHandler(enabled = confirming == null) {
        if (selectedId != null) selectedId = null else onBack()
    }
    val resources = LocalContext.current.resources
    BoxWithConstraints(Modifier.fillMaxSize()) {
        val wide = maxWidth >= Tokens.Android.Size.assistantSplitMin
        fun open(id: String) {
            model.dismissRevoked()
            selectedId = id
        }
        fun refresh() {
            model.dismissRevoked()
            scope.launch { model.load() }
        }
        fun hold(grant: StandingAuthorization) {
            menu = grant
        }
        if (wide && selected != null) {
            Row(Modifier.fillMaxSize()) {
                Box(Modifier.weight(1f).fillMaxHeight()) {
                    GrantList(state, machines, resources, selectedId, ::open, ::hold, ::refresh, onBack)
                }
                Box(Modifier.weight(1f).fillMaxHeight()) {
                    GrantDetail(selected, machines, resources, onBack = { selectedId = null }, onRevoke = { confirming = it })
                }
            }
        } else if (selected != null) {
            GrantDetail(selected, machines, resources, onBack = { selectedId = null }, onRevoke = { confirming = it })
        } else {
            GrantList(state, machines, resources, selectedId, ::open, ::hold, ::refresh, onBack)
        }
    }
    menu?.let { grant ->
        JActionSheet(
            listOf(
                JAction(
                    stringResource(R.string.settings_standing_revoke_authorization_ellipsis),
                    PersonaIcons.delete,
                    destructive = true,
                ) { confirming = grant },
            ),
            { menu = null },
            title = StandingAuthorizationText.actionsTitle(grant, resources),
        )
    }
    confirming?.let { grant ->
        val machine = standingMachineName(grant.machineId, machines)
        JDialog(
            onDismiss = { confirming = null },
            title = stringResource(R.string.settings_standing_revoke_ask),
            text = stringResource(
                R.string.settings_standing_revoke_message,
                grant.folderName,
                machine,
                StandingAuthorizationText.actionsPhrase(grant, resources),
            ),
            confirm = stringResource(R.string.settings_standing_revoke_authorization) to {
                confirming = null
                selectedId = null
                scope.launch { model.revoke(grant) }
            },
            destructive = true,
        )
    }
}

@Composable
private fun GrantList(
    state: StandingAuthorizationsState,
    machines: List<Machine>,
    resources: Resources,
    selectedId: String?,
    onOpen: (String) -> Unit,
    onLongClick: (StandingAuthorization) -> Unit,
    onRefresh: () -> Unit,
    onBack: () -> Unit,
) {
    val grants = state.grants
    JListPage(
        title = stringResource(R.string.settings_standing_title),
        onBack = onBack,
        modifier = Modifier.testTag("standing-list").fillMaxSize(),
        refreshing = state.loading && grants != null,
        onRefresh = if (grants != null) onRefresh else null,
    ) {
        notice(state)
        when {
            grants == null && state.forbidden -> item("forbidden") {
                JEmptyState(
                    stringResource(R.string.settings_standing_needs_access),
                    stringResource(R.string.settings_standing_forbidden),
                    Modifier.testTag("standing-forbidden"),
                    icon = PersonaIcons.locked,
                )
            }
            grants == null && state.loadError != null -> item("error") {
                val error = requireNotNull(state.loadError)
                JEmptyState(
                    stringResource(R.string.settings_standing_load_failed),
                    failureReasonText(error).text(),
                    Modifier.testTag("standing-error"),
                    icon = PersonaIcons.error,
                    action = {
                        JButton(stringResource(R.string.settings_standing_retry), onRefresh, kind = JButtonKind.Secondary)
                    },
                )
            }
            grants == null -> item("loading") { JLoading() }
            grants.isEmpty() -> item("empty") {
                JEmptyState(
                    stringResource(R.string.settings_standing_none),
                    stringResource(R.string.settings_standing_empty),
                    Modifier.testTag("standing-empty"),
                    icon = PersonaIcons.privacy,
                )
            }
            else -> {
                grants.forEachIndexed { index, grant ->
                    item("grant-${grant.id}") {
                        JGroupItem(persona.core.designsystem.GroupPosition.of(index, grants.size), Modifier.padding(top = if (index == 0) Tokens.Space.m else Tokens.Space.flush)) {
                            GrantRow(
                                grant, machines, resources, selected = grant.id == selectedId,
                                onClick = { onOpen(grant.id) }, onLongClick = { onLongClick(grant) },
                            )
                        }
                    }
                }
                item("footer") {
                    SettingsCaption(
                        stringResource(R.string.settings_standing_footer),
                        Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s),
                    )
                }
            }
        }
    }
}

private fun LazyListScope.notice(state: StandingAuthorizationsState) {
    val notice = state.notice ?: return
    item("notice") {
        when (notice) {
            StandingNotice.Revoked -> JNotice(stringResource(R.string.settings_standing_revoked))
            StandingNotice.RevokeFailed -> JNotice(stringResource(R.string.settings_standing_revoke_failed), status = Status.NeedsYou)
            is StandingNotice.DidntRefresh -> JNotice(
                stringResource(R.string.settings_standing_didnt_refresh, notice.reason.text()),
                status = Status.NeedsYou,
            )
        }
    }
}

@Composable
private fun GrantRow(
    grant: StandingAuthorization,
    machines: List<Machine>,
    resources: Resources,
    selected: Boolean,
    onClick: () -> Unit,
    onLongClick: () -> Unit,
) {
    val place = stringResource(R.string.settings_standing_place, grant.folderName, standingMachineName(grant.machineId, machines))
    val more = StandingAuthorizationText.listCaption(grant, resources)
    JRow(onClick = onClick, onLongClick = onLongClick, selected = selected, modifier = Modifier.testTag("standing-row-${grant.id}")) {
        Column(Modifier.weight(1f)) {
            Text(StandingAuthorizationText.actionsTitle(grant, resources), style = PersonaTheme.type.body, color = PersonaTheme.palette.ink)
            Text(place, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
            if (more != null) Text(more, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        }
    }
}

@Composable
private fun GrantDetail(
    grant: StandingAuthorization,
    machines: List<Machine>,
    resources: Resources,
    onBack: () -> Unit,
    onRevoke: (StandingAuthorization) -> Unit,
) {
    val machine = standingMachineName(grant.machineId, machines)
    val actions = grant.actions.ifEmpty { listOf("") }
    val conditions = grant.conditions
    val granted = StandingAuthorizationText.granted(grant, resources)
    val from = grant.source.takeIf { it.isNotBlank() }?.let { StandingAuthorizationText.source(it, resources) }
    val meta = listOfNotNull(granted?.let { "granted" }, "lasts", from?.let { "from" }, "undo")
    JListPage(
        title = stringResource(R.string.settings_standing_one),
        onBack = onBack,
        largeTitle = false,
        modifier = Modifier.testTag("standing-detail").fillMaxSize(),
    ) {
        item("may") { JSectionHeader(stringResource(R.string.settings_standing_may)) }
        actions.forEachIndexed { index, action ->
            item("action-$index") {
                JGroupItem(persona.core.designsystem.GroupPosition.of(index, actions.size)) {
                    JRow(
                        if (action.isEmpty()) stringResource(R.string.settings_standing_no_actions)
                        else StandingAuthorizationText.actionLabel(action, resources),
                    )
                }
            }
        }
        item("where") { JSectionHeader(stringResource(R.string.settings_standing_where)) }
        item("computer") {
            JGroupItem(persona.core.designsystem.GroupPosition.of(0, 2)) {
                JRow(stringResource(R.string.settings_standing_computer), value = machine)
            }
        }
        item("folder") {
            JGroupItem(persona.core.designsystem.GroupPosition.of(1, 2)) {
                JRow {
                    Column(Modifier.weight(1f)) {
                        Text(stringResource(R.string.settings_standing_folder), style = PersonaTheme.type.body, color = PersonaTheme.palette.ink)
                        Text(grant.directory, style = PersonaTheme.type.code, color = PersonaTheme.palette.ink2)
                    }
                }
            }
        }
        item("only") { JSectionHeader(stringResource(R.string.settings_standing_only_when_label)) }
        if (conditions.isEmpty()) {
            item("no-conditions") {
                JGroupItem(persona.core.designsystem.GroupPosition.of(0, 1)) {
                    JRow(stringResource(R.string.settings_standing_no_conditions))
                }
            }
        } else conditions.forEachIndexed { index, condition ->
            item("condition-$index") {
                JGroupItem(persona.core.designsystem.GroupPosition.of(index, conditions.size)) {
                    JRow(StandingAuthorizationText.condition(condition, resources))
                }
            }
        }
        item("checks") {
            SettingsCaption(stringResource(R.string.settings_standing_checks), Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s))
        }
        if (grant.quote.isNotBlank()) {
            item("quote-head") { JSectionHeader(stringResource(R.string.settings_standing_quote)) }
            item("quote") {
                JGroupItem(persona.core.designsystem.GroupPosition.of(0, 1)) {
                    JRow {
                        Column(Modifier.weight(1f)) {
                            Text(grant.quote, style = PersonaTheme.type.body, color = PersonaTheme.palette.ink)
                        }
                    }
                }
            }
        }
        meta.forEachIndexed { index, key ->
            item("meta-$key") {
                // Its own group, as on iOS: a gap under "Your words" (or the conditions note) before Granted.
                JGroupItem(
                    persona.core.designsystem.GroupPosition.of(index, meta.size),
                    Modifier.padding(top = if (index == 0) Tokens.Space.section else Tokens.Space.flush),
                ) {
                    when (key) {
                        "granted" -> JRow(stringResource(R.string.settings_standing_granted), value = granted)
                        "lasts" -> JRow(stringResource(R.string.settings_standing_lasts), value = stringResource(R.string.settings_standing_until))
                        "from" -> JRow {
                            Column(Modifier.weight(1f)) {
                                Text(stringResource(R.string.settings_standing_from), style = PersonaTheme.type.body, color = PersonaTheme.palette.ink)
                                Text(from.orEmpty(), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                            }
                        }
                        else -> JRow(
                            stringResource(R.string.settings_standing_undo_work),
                            value = stringResource(if (grant.reversible) R.string.settings_standing_yes else R.string.settings_standing_no),
                        )
                    }
                }
            }
        }
        item("undo-footer") {
            SettingsCaption(stringResource(R.string.settings_standing_undo_footer), Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s))
        }
        item("revoke") {
            JButton(
                stringResource(R.string.settings_standing_revoke_authorization_ellipsis),
                { onRevoke(grant) },
                Modifier.fillMaxWidth().padding(horizontal = pagePadding, vertical = Tokens.Space.m).testTag("standing-revoke"),
                kind = JButtonKind.Destructive,
            )
        }
    }
}
