package persona.feature.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.style.TextOverflow
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import persona.core.contracts.api.GitHubAuth
import persona.core.contracts.api.GitHubInboxOptions
import persona.core.contracts.api.GitHubMachine
import persona.core.contracts.api.HubConnector
import persona.core.designsystem.GroupPosition
import persona.core.designsystem.InlineTone
import persona.core.designsystem.JAction
import persona.core.designsystem.JButton
import persona.core.designsystem.JButtonKind
import persona.core.designsystem.JButtonSize
import persona.core.designsystem.JGroup
import persona.core.designsystem.JGroupItem
import persona.core.designsystem.JIcon
import persona.core.designsystem.JIconButton
import persona.core.designsystem.JInlineStatus
import persona.core.designsystem.JMenu
import persona.core.designsystem.JRow
import persona.core.designsystem.JSectionHeader
import persona.core.designsystem.JSpinner
import persona.core.designsystem.JTextField
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.designsystem.icons.PersonaIcons
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.rowPadding
import persona.core.designsystem.text

/**
 * GitHub's sign-in, in place of the generic one (iOS `GitHubConnectorSections`): where the token comes from, the
 * device-code steps when that is chosen and not signed in, and missing scopes only once signed in with some missing.
 * Switching signs out of the previous method, so there is no Sign out.
 */
internal fun LazyListScope.gitHubSignIn(
    connector: HubConnector,
    session: GitHubAuthSession,
    model: ConnectorSignInModel,
    bind: BindState,
    scope: CoroutineScope,
) {
    item("github-source-header") { JSectionHeader(stringResource(R.string.settings_gh_sign_in_from)) }
    val auth = session.auth
    if (auth == null) {
        item("github-source-loading") {
            if (session.error != null && !session.loading) {
                Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    GitHubErrorText(session.error.orEmpty())
                    JButton(
                        stringResource(R.string.settings_retry_read),
                        { session.refresh() },
                        Modifier.fillMaxWidth().padding(horizontal = rowPadding),
                        kind = JButtonKind.Secondary,
                    )
                }
            } else {
                Row(
                    Modifier.fillMaxWidth().padding(horizontal = rowPadding, vertical = Tokens.Space.s).semantics(mergeDescendants = true) {},
                    horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    JSpinner(stroke = Tokens.Space.Standard.xxs)
                    Text(
                        stringResource(R.string.settings_gh_checking),
                        style = PersonaTheme.type.caption,
                        color = PersonaTheme.palette.ink2,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
        }
    } else {
        val count = auth.machines.size + 1
        auth.machines.forEachIndexed { index, machine ->
            item(githubMachineRowKey(machine.id)) {
                GitHubSourceRow(
                    key = machine.id,
                    position = GroupPosition.of(index, count),
                    icon = PersonaIcons.machine,
                    title = machine.name,
                    subtitle = GitHubConnectorText.machineSubtitle(LocalResources.current, machine),
                    warn = GitHubConnectorText.warns(machine),
                    selected = auth.source == "machine" && auth.machine == machine.id,
                    busy = session.choosing == machine.id,
                    enabled = session.choosing == null,
                    onClick = { session.choose(machine.id) },
                )
            }
        }
        item(GITHUB_DEVICE_ROW_KEY) {
            GitHubSourceRow(
                key = "device",
                position = GroupPosition.of(auth.machines.size, count),
                icon = PersonaIcons.credentials,
                title = stringResource(R.string.settings_gh_device_code),
                subtitle = stringResource(R.string.settings_gh_device_code_subtitle),
                warn = false,
                selected = auth.source == "device",
                busy = session.choosing == "device",
                enabled = session.choosing == null,
                onClick = { session.choose(null) },
            )
        }
        item("github-source-footer") {
            Column {
                session.error?.let { GitHubErrorText(it) }
                SettingsCaption(
                    stringResource(R.string.settings_gh_sign_in_from_footer),
                    Modifier.paddingHorizontalCaption(),
                )
            }
        }
    }
    if (auth?.source == "device" && connector.enabled != false && !ConnectorText.isSignedIn(connector)) {
        val prompt = bind.prompt
        item(githubDeviceStepsKey(prompt?.step, prompt?.kind)) {
            Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                if (prompt == null) {
                    JButton(
                        stringResource(R.string.settings_src_connector_sign_in, connector.name),
                        { scope.launch { model.start() } },
                        Modifier.fillMaxWidth(),
                        icon = PersonaIcons.credentials,
                        loading = bind.sending,
                    )
                    bind.error?.let { JInlineStatus(it.text(), tone = InlineTone.Failed) }
                } else {
                    ConnectorSignInStep(connector.name, prompt, bind, model, pageScope = scope)
                }
                SettingsCaption(stringResource(R.string.settings_gh_device_footer), Modifier.paddingHorizontalCaption())
            }
        }
    }
    val missing = auth?.missing.orEmpty()
    if (auth != null && missing.isNotEmpty() && ConnectorText.isSignedIn(connector)) {
        item("github-missing-header") { JSectionHeader(stringResource(R.string.settings_gh_missing_header)) }
        item("github-missing") {
            val res = LocalResources.current
            Column(Modifier.testTag("github.missingScopes")) {
                JGroupItem(GroupPosition.Single) {
                    JRow {
                        JIcon(
                            PersonaIcons.error,
                            null,
                            Modifier.align(Alignment.Top),
                            size = Tokens.Size.iconS,
                            tint = PersonaTheme.palette.caution,
                            decorative = true,
                        )
                        Spacer(Modifier.width(Tokens.Space.m))
                        Text(
                            GitHubConnectorText.missingAdvice(res, auth, missing),
                            style = PersonaTheme.type.body,
                            color = PersonaTheme.palette.ink,
                        )
                    }
                }
                missing.flatMap { it.affects }.forEach { line ->
                    SettingsCaption(line, Modifier.paddingHorizontalCaption())
                }
            }
        }
    }
}

/** GitHub's Inbox options at the end of the page (iOS `GitHubInboxSettings`): one group, then the explanation. */
internal fun LazyListScope.gitHubInbox(editor: GitHubInboxEditor) {
    item("github-inbox-header") { JSectionHeader(stringResource(R.string.settings_gh_inbox)) }
    item("github-inbox") {
        val res = LocalResources.current
        var open by remember { mutableStateOf(false) }
        val muted = githubMutedRepos(editor.options.mutedRepos)
        val title = GitHubConnectorText.ciTitle(res, editor.options.ci) ?: editor.options.ci
        Box {
            JGroup(rows = buildList<@Composable () -> Unit> {
                add { JRow(stringResource(R.string.settings_gh_ci_failures), value = title, onClick = { open = true }) }
                muted.forEach { repo ->
                    add {
                        JRow(
                            repo,
                            trailing = {
                                JIconButton(PersonaIcons.delete, stringResource(R.string.settings_gh_unmute), { editor.remove(repo) })
                            },
                        )
                    }
                }
                add {
                    Row(
                        Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s),
                    ) {
                        JTextField(
                            editor.draft,
                            { editor.draft = it },
                            Modifier.weight(1f),
                            placeholder = stringResource(R.string.settings_gh_mute_hint),
                            singleLine = true,
                            keyboardOptions = KeyboardOptions(
                                capitalization = KeyboardCapitalization.None,
                                autoCorrectEnabled = false,
                                imeAction = ImeAction.Done,
                            ),
                            keyboardActions = KeyboardActions(onDone = { editor.add() }),
                        )
                        JButton(
                            stringResource(R.string.settings_gh_add),
                            editor::add,
                            kind = JButtonKind.Secondary,
                            size = JButtonSize.Medium,
                            enabled = editor.draft.contains("/"),
                        )
                    }
                }
            })
            JMenu(
                open,
                { open = false },
                GitHubInboxOptions.CI_MODES.map { mode ->
                    JAction(
                        label = GitHubConnectorText.ciTitle(res, mode) ?: mode,
                        checked = editor.options.ci == mode,
                    ) { editor.setCi(mode) }
                },
            )
        }
    }
    item("github-inbox-footer") {
        Column {
            editor.error?.let { GitHubErrorText(it) }
            SettingsCaption(GitHubConnectorText.inboxFooter(LocalResources.current, editor.options), Modifier.paddingHorizontalCaption())
        }
    }
}

/** A machine id of "device" must not share the device-code row's lazy key. */
internal fun githubMachineRowKey(id: String): String = "github-source-m-$id"

internal const val GITHUB_DEVICE_ROW_KEY = "github-source-device"

/** Duplicate muted repositories from the hub are one row. */
internal fun githubMutedRepos(repos: List<String>): List<String> = repos.distinct()

/** A new device-code step gets a new lazy key, so remembered state does not leak across steps. */
internal fun githubDeviceStepsKey(step: String?, kind: String?): String = "github-device-steps-$step-$kind"

@Composable
private fun GitHubErrorText(text: String) {
    Text(
        text,
        Modifier.paddingHorizontalCaption(),
        style = PersonaTheme.type.caption,
        color = PersonaTheme.palette.danger.text,
    )
}

@Composable
private fun GitHubSourceRow(
    key: String,
    position: GroupPosition,
    icon: PhIcon,
    title: String,
    subtitle: String,
    warn: Boolean,
    selected: Boolean,
    busy: Boolean,
    enabled: Boolean,
    onClick: () -> Unit,
) {
    val palette = PersonaTheme.palette
    JGroupItem(position) {
        JRow(
            Modifier.testTag("github.source.$key").semantics { this.selected = selected },
            trailing = {
                if (busy) JSpinner(stroke = Tokens.Space.Standard.xxs)
                else if (selected) JIcon(PersonaIcons.done, null, size = Tokens.Size.iconS, tint = palette.accent)
            },
            onClick = onClick,
            chevron = false,
            enabled = enabled,
            role = Role.RadioButton,
        ) {
            JIcon(icon, null, size = Tokens.Size.iconS, tint = palette.ink2, decorative = true)
            Spacer(Modifier.width(Tokens.Space.m))
            Column(Modifier.weight(1f)) {
                Text(title, style = PersonaTheme.type.body, color = palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(
                    subtitle,
                    style = PersonaTheme.type.caption,
                    color = if (warn) palette.cautionText else palette.ink2,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

/** Lines the section up with the rows' own inset. The list already pads the page. */
@Composable
private fun Modifier.paddingHorizontalCaption(): Modifier =
    padding(horizontal = rowPadding, vertical = Tokens.Space.s)
