package persona.feature.settings

import android.content.res.Resources
import androidx.compose.ui.res.stringResource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.text.style.TextOverflow
import androidx.navigation3.runtime.NavKey
import persona.core.contracts.Machine
import persona.core.contracts.api.HubUpdateRecord
import persona.core.contracts.api.HubUpdateStatus
import persona.core.contracts.api.HubUpdates
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.navigation.MachineDetailRoute
import kotlinx.coroutines.launch

/**
 * Settings › Hub version (docs/operations/hub-update.md "In the app", iOS `HubSettingsPage`, the web's `HubSettings`): the hub's version and
 * channel, checking for and installing a release, what an update waits for (with "Update now"), a Docker hub's commands,
 * auto-update, the personad on each computer, and the history. Follows the hub's `hub_update` pushes.
 */
@Composable
internal fun HubUpdatePage(
    updates: HubUpdates?, machines: List<Machine>, onBack: () -> Unit,
    /** The hub's connection is up: read again each time it comes back (after a restart the pushes may have been missed). */
    online: Boolean = true,
    /** Opens a computer's page ("Redeploy" lives there); null where there is no navigation host. */
    onMachine: ((NavKey) -> Unit)? = null,
) {
    if (updates == null) {
        JListPage(title = stringResource(R.string.settings_hub_update_title), onBack = onBack) { item { JNotice(stringResource(R.string.settings_hub_update_demo), Modifier.padding(top = Tokens.Space.m)) } }
        return
    }
    val model = remember(updates) { HubUpdateModel(updates) }
    val state by model.state.collectAsState()
    HubUpdateContent(state, machines, onBack, onMachine, model)
    LaunchedEffect(model, online) { if (online) model.load() }
    LaunchedEffect(model) { updates.events.collect { model.receive(it) } }
}

@Composable
internal fun HubUpdateContent(
    state: HubUpdateState, machines: List<Machine>, onBack: () -> Unit, onMachine: ((NavKey) -> Unit)?, model: HubUpdateModel?,
) {
    val scope = rememberCoroutineScope()
    val resources = LocalResources.current
    var confirmNow by remember { mutableStateOf(false) }
    val loadable = state.loadable(state.error?.text())
    // The failed check or update is the list's first row: a list scrolled below it keeps its own first row in view, so show it.
    val listState = rememberLazyListState()
    LaunchedEffect(state.actionError) { if (state.actionError != null) listState.animateScrollToItem(0) }
    JListPage(title = stringResource(R.string.settings_hub_update_title), onBack = onBack, state = listState, refreshing = loadable.isRefreshing,
        onRefresh = model?.let { m -> { scope.launch { m.check() } } }) {
        if (state.unsupported) item("unsupported") { JNotice(stringResource(R.string.settings_hub_update_unsupported), Modifier.padding(top = Tokens.Space.m)) }
        else jLoadableContent(loadable, onRetry = model?.let { m -> { scope.launch { m.load() } } }) { s ->
            state.actionError?.let { item("action-error") { JInlineStatus(it.text(), Modifier.padding(top = Tokens.Space.m), tone = InlineTone.Failed) } }
            // Told once that it updates itself (the hub forgets the line once seen or once auto-update was chosen).
            if (s.autoNotice) item("auto-notice") {
                // Like the web's line: "Turn off" turns auto-update off, "Got it" only hides this (both tell the hub it was seen).
                JCard(Modifier.padding(top = Tokens.Space.m)) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                        JIcon(PhIcons.Info, null, tint = PersonaTheme.palette.accent, decorative = true)
                        Text(stringResource(R.string.settings_hub_update_auto_notice), Modifier.weight(1f), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink)
                        JButton(stringResource(R.string.settings_hub_update_turn_off), { model?.let { m -> scope.launch { m.setAuto(false) } } }, kind = JButtonKind.Quiet, size = JButtonSize.Small, enabled = !state.sending)
                        JButton(stringResource(R.string.settings_hub_update_got_it), { model?.let { m -> scope.launch { m.noticeSeen() } } }, kind = JButtonKind.Quiet, size = JButtonSize.Small, enabled = !state.sending)
                    }
                }
            }
            version(s, state, resources, onCheck = { model?.let { m -> scope.launch { m.check() } } })
            update(s, state, resources, onUpdate = { model?.let { m -> scope.launch { m.update(now = false) } } }, onNow = { confirmNow = true })
            if (s.canUpdate) item("auto") {
                Column {
                    JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.section)) {
                        JSwitchRow(stringResource(R.string.settings_hub_update_auto), s.auto, { on -> model?.let { m -> scope.launch { m.setAuto(on) } } }, enabled = !state.sending)
                    }
                    Note(stringResource(R.string.settings_hub_update_auto_footer))
                }
            }
            nodes(machines, onMachine)
            if (state.history.isNotEmpty()) history(state.history)
        }
    }
    if (confirmNow) JDialog(
        onDismiss = { confirmNow = false }, title = stringResource(R.string.settings_hub_update_now_title), text = stringResource(R.string.settings_hub_update_now_warning),
        confirm = stringResource(R.string.settings_hub_update_now) to { confirmNow = false; model?.let { m -> scope.launch { m.update(now = true) } }; Unit },
    )
}

private fun LazyListScope.version(s: HubUpdateStatus, state: HubUpdateState, resources: Resources, onCheck: () -> Unit) {
    val rows = buildList<@Composable () -> Unit> {
        add { JRow(stringResource(R.string.settings_hub_update_version), value = s.version.ifBlank { stringResource(R.string.settings_hub_update_unknown) }) }
        add { JRow(stringResource(R.string.settings_hub_update_channel), value = HubUpdateText.channel(s.channel, resources)) }
        HubUpdateText.lastCheck(s, resources)?.let { at -> add { JRow(stringResource(R.string.settings_hub_update_last_check), value = at) } }
        add {
            JRow(onClick = onCheck, enabled = !state.checking && !s.working, chevron = false,
                trailing = if (state.checking) ({ JSpinner() }) else null) {
                Text(stringResource(if (state.checking) R.string.settings_hub_update_checking_more else R.string.settings_hub_update_check), style = PersonaTheme.type.body, color = PersonaTheme.palette.accent,
                    maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
    item("version-header") { JSectionHeader(stringResource(R.string.settings_hub_update_version)) }
    itemsIndexed(rows, key = { index, _ -> "version-$index" }) { index, row -> JGroupItem(GroupPosition.of(index, rows.size)) { row() } }
}

private fun LazyListScope.update(s: HubUpdateStatus, state: HubUpdateState, resources: Resources, onUpdate: () -> Unit, onNow: () -> Unit) {
    item("update-header") { JSectionHeader(stringResource(R.string.settings_hub_update_header)) }
    item("update") {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            when (val view = HubUpdateText.view(s, resources)) {
                HubUpdateView.Docker -> DockerRows(s)
                is HubUpdateView.Cannot -> JGroupItem(GroupPosition.Single) { JRow { Callout(view.why) } }
                is HubUpdateView.Newer -> {
                    val latest = s.latest!!
                    JGroupItem(GroupPosition.Single) {
                        JRow {
                            Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                                    Text(stringResource(R.string.settings_hub_update_newer, latest.version), Modifier.weight(1f, fill = false), style = PersonaTheme.type.bodyStrong,
                                        color = PersonaTheme.palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                    HubUpdateText.released(s, resources)?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3, maxLines = 1) }
                                }
                                latest.notes?.takeIf { it.isNotBlank() }?.let { Callout(it) }
                            }
                        }
                    }
                    HubUpdateText.latestWarning(s, resources)?.let { JInlineStatus(it, tone = InlineTone.Caution) }
                    if (view.working) Progress(s, onNow)
                    else JButton(stringResource(R.string.settings_hub_update_update_hub), onUpdate, Modifier.fillMaxWidth(), loading = state.sending, loadingText = stringResource(R.string.settings_hub_update_starting), enabled = !state.sending)
                }
                HubUpdateView.Working -> Progress(s, onNow)
                HubUpdateView.Current -> JGroupItem(GroupPosition.Single) {
                    JRow {
                        JIcon(PhIcons.CheckCircle, null, tint = PersonaTheme.palette.positive, decorative = true)
                        Spacer(Modifier.width(Tokens.Space.m))
                        Text(stringResource(R.string.settings_hub_update_state_current), style = PersonaTheme.type.body, color = PersonaTheme.palette.tone(Tone.Positive).text, maxLines = 1)
                    }
                }
            }
            s.error?.takeIf { it.isNotBlank() && !s.working }?.let {
                JInlineStatus(it, tone = if (s.state == "rolled_back") InlineTone.Caution else InlineTone.Failed)
            }
        }
    }
    if (s.canUpdate) item("update-footer") { Note(stringResource(R.string.settings_hub_update_footer)) }
}

/** The update under way, and while it waits for idle what it waits for, with "Update now". */
@Composable
private fun Progress(s: HubUpdateStatus, onNow: () -> Unit) {
    val waiting = s.state == "waiting_idle" && s.busy.isNotEmpty()
    val rows = buildList<@Composable () -> Unit> {
        add {
            JRow {
                JSpinner()
                Spacer(Modifier.width(Tokens.Space.m))
                Text(HubUpdateText.state(s, LocalResources.current), Modifier.weight(1f, fill = false), style = PersonaTheme.type.body, color = PersonaTheme.palette.ink,
                    maxLines = 1, overflow = TextOverflow.Ellipsis)
                s.progress?.takeIf { s.state == "downloading" && it.isNotBlank() }?.let {
                    Spacer(Modifier.width(Tokens.Space.s))
                    Text(it, style = PersonaTheme.type.codeSmall, color = PersonaTheme.palette.ink3, maxLines = 1)
                }
            }
        }
        if (waiting) s.busy.forEach { b -> add { JRow(b.title.ifBlank { stringResource(R.string.settings_hub_update_busy) }, subtitle = b.machine.ifBlank { null }, leadingIcon = PhIcons.HourglassMedium) } }
    }
    Column {
        rows.forEachIndexed { index, row -> JGroupItem(GroupPosition.of(index, rows.size)) { row() } }
    }
    if (waiting) JButton(stringResource(R.string.settings_hub_update_now_more), onNow, Modifier.fillMaxWidth(), kind = JButtonKind.Secondary)
}

/** A Docker hub updates by a new image: what to run where, each command with a copy button. */
@Composable
private fun DockerRows(s: HubUpdateStatus) {
    val (headline, steps) = HubUpdateText.dockerSteps(s, LocalResources.current)
    val copy = rememberCopy()
    headline?.let { Text(it, Modifier.padding(horizontal = rowPadding), style = PersonaTheme.type.bodyStrong, color = PersonaTheme.palette.ink, maxLines = 1) }
    steps.forEach { step ->
        Text(step.text, Modifier.padding(horizontal = rowPadding), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
        JGroupItem(GroupPosition.Single) {
            JRow(trailing = {
                JIconButton(PhIcons.Copy, stringResource(R.string.settings_hub_update_copy), { copy(step.copy, null) })
            }) {
                Text(step.copy, style = PersonaTheme.type.codeSmall, color = PersonaTheme.palette.ink, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

private fun LazyListScope.nodes(machines: List<Machine>, onMachine: ((NavKey) -> Unit)?) {
    if (machines.isEmpty()) return
    item("nodes-header") { JSectionHeader(stringResource(R.string.settings_hub_update_nodes_header)) }
    itemsIndexed(machines, key = { _, m -> "node-${m.id}" }) { index, m ->
        JGroupItem(GroupPosition.of(index, machines.size)) {
            JRow(m.displayName, value = m.nodeVersion?.takeIf { it.isNotBlank() } ?: stringResource(if (m.online) R.string.settings_hub_update_unknown else R.string.settings_hub_update_offline),
                onClick = onMachine?.let { go -> { go(MachineDetailRoute(m.id)) } },
                trailing = if (m.nodeMismatch == true) ({ JChip(stringResource(R.string.settings_hub_update_node_mismatch), status = Status.Working) }) else null,
                chevron = onMachine != null)
        }
    }
    item("nodes-footer") { Note(stringResource(R.string.settings_hub_update_nodes_footer)) }
}

private fun LazyListScope.history(records: List<HubUpdateRecord>) {
    val shown = records.take(10)
    item("history-header") { JSectionHeader(stringResource(R.string.settings_hub_update_history_header)) }
    itemsIndexed(shown, key = { index, r -> "record-$index-${r.toBuild}" }) { index, r ->
        JGroupItem(GroupPosition.of(index, shown.size)) { RecordRow(r) }
    }
}

@Composable
private fun RecordRow(r: HubUpdateRecord) {
    val palette = PersonaTheme.palette
    val resources = LocalResources.current
    val tone: Color = when (r.result) { "updated" -> palette.tone(Tone.Positive).text; "rolled_back", "held" -> palette.cautionText; else -> palette.danger.text }
    JRow {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                Text(HubUpdateText.record(r.result, resources), style = PersonaTheme.type.bodyStrong, color = tone, maxLines = 1)
                if (r.auto) JChip(stringResource(R.string.settings_hub_update_auto_chip))
                Spacer(Modifier.weight(1f))
                HubUpdateText.recordTime(r, resources)?.let { Text(it, style = PersonaTheme.type.caption, color = palette.ink3, maxLines = 1) }
            }
            Text("${r.fromVersion} → ${r.toVersion}", style = PersonaTheme.type.codeSmall, color = palette.ink2, maxLines = 1, overflow = TextOverflow.MiddleEllipsis)
            r.reason?.takeIf { it.isNotBlank() }?.let { Text(it, style = PersonaTheme.type.caption, color = palette.ink3) }
        }
    }
}

@Composable
private fun Callout(text: String) = Text(text, style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)

@Composable
private fun Note(text: String) = SettingsCaption(text, Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s))
