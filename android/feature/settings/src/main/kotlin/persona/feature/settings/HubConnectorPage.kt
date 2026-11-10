package persona.feature.settings

import persona.core.designsystem.text
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

@Composable
internal fun HubConnectorPage(
    connectors: ConnectorsManagement?,
    connectorId: String,
    onBack: () -> Unit,
    manager: String? = null,
    /** For the source subscriptions' entry row (#2041), the default manager's only; without them it is left out. */
    extensions: persona.core.contracts.api.ExtensionManagement? = null,
    onSubscriptions: (() -> Unit)? = null,
) {
    val owner = manager?.takeIf { it.isNotBlank() } ?: LocalManagerRoster.current?.defaultManagerId.orEmpty()
    val subscriptions = rememberSubscriptionCounts(extensions.takeIf { manager.isNullOrBlank() && onSubscriptions != null }) { it.connector == connectorId }
    key(connectors, connectorId, owner) {
        HubConnectorDetails(connectors, connectorId, onBack, owner, subscriptions, onSubscriptions ?: {})
    }
}

@Composable
private fun HubConnectorDetails(connectors: ConnectorsManagement?, connectorId: String, onBack: () -> Unit, owner: String,
                                subscriptions: Pair<Int, Int>?, onSubscriptions: () -> Unit) {
    val requestManager = remember(owner) { RequestManager(owner) }
    val scope = rememberCoroutineScope { requestManager }
    val feedback = LocalAppFeedback.current
    val res = LocalResources.current
    var connector by remember { mutableStateOf<HubConnector?>(null) }
    var loading by remember { mutableStateOf(true) }
    var error by remember { mutableStateOf<String?>(null) }
    var confirmingDelete by remember { mutableStateOf(false) }
    var confirmingLogout by remember { mutableStateOf(false) }
    val signIn = remember(connectors, connectorId, owner) { connectors?.let { ConnectorSignInModel(it, connectorId) } }
    val idle = remember { kotlinx.coroutines.flow.MutableStateFlow(ConnectorSignIn()) }
    val signInState by (signIn?.state ?: idle).collectAsState()

    fun load() {
        if (connectors == null) {
            loading = false
            return
        }
        scope.launch {
            loading = true
            try {
                val list = connectors.connectors(all = true)
                connector = list.firstOrNull { it.id == connectorId &&
                    (owner.isEmpty() || (it.manager?.takeIf(String::isNotBlank) ?: owner) == owner) }
                error = null
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                error = e.message ?: res.getString(R.string.settings_src_connector_read_failed)
            } finally {
                loading = false
            }
        }
    }

    LaunchedEffect(connectors, connectorId, owner) { load() }
    BackHandler(onBack = onBack)
    // A code on screen: ask the hub again on its own until it is scanned or entered (and the page is open).
    val polled = signInState.bind.prompt?.takeIf { signInState.bind.polled }
    LaunchedEffect(signIn, polled?.step, polled?.kind, polled?.userCode, polled?.url) { if (polled != null) kotlinx.coroutines.withContext(requestManager) { signIn?.poll() } }
    LaunchedEffect(signInState.signedInAs) {
        val who = signInState.signedInAs ?: return@LaunchedEffect
        feedback?.notice(if (who.isBlank()) res.getString(R.string.settings_src_connector_signed_in) else res.getString(R.string.settings_src_connector_signed_in_as, who))
        signIn?.signedInShown()
        load()
    }

    val c = connector
    val title = c?.name ?: connectorId
    val stateLabel = c?.let { ConnectorText.state(res, it) } ?: stringResource(if (loading) R.string.settings_src_checking else R.string.settings_src_state_not_connected)
    val statusDot = c?.let { ConnectorText.status(it) } ?: Status.Offline

    JListPage(title = title, onBack = onBack) {
        if (loading && c == null) {
            item("loading") { JLoading() }
        } else if (c != null) {
            item("status-header") { JSectionHeader(stringResource(R.string.settings_src_status_header)) }
            item("status-row") {
                JGroupItem(GroupPosition.Single) {
                    JRow(
                        title = c.name,
                        subtitle = listOfNotNull(c.account, c.via).joinToString(" · ").ifEmpty { stateLabel },
                        leadingIcon = PhIcons.PlugsConnected,
                        trailing = {
                            Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalAlignment = Alignment.CenterVertically) {
                                JStatusDot(statusDot)
                                if (loading) JSpinner(stroke = Tokens.Space.xxs)
                            }
                        },
                    )
                }
            }
            c.error?.takeIf { it.isNotBlank() }?.let { err ->
                item("connector-error") {
                    JNotice(err, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou)
                }
            }
            c.guide?.takeIf { it.isNotEmpty() }?.let { guide ->
                item("guide") {
                    SettingsCaption(guide.joinToString("\n"), Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s))
                }
            }

            subscriptionsEntry(subscriptions, onSubscriptions)

            if (signIn != null && ConnectorText.signsInHere(c)) {
                connectorSignIn(c, signInState.bind, signIn, scope, onSignOut = { confirmingLogout = true })
            }

            item("settings-header") { JSectionHeader(stringResource(R.string.settings_src_settings_header)) }
            item("enabled-switch") {
                JGroupItem(GroupPosition.Single) {
                    JSwitchRow(stringResource(R.string.settings_src_enable), checked = c.enabled ?: true, onCheckedChange = { want ->
                        scope.launch {
                            try {
                                connector = connectors?.setConnectorEnabled(c.id, want)
                            } catch (e: Exception) {
                                feedback?.notice(res.getString(R.string.settings_src_save_failed, e.message.orEmpty()))
                            }
                        }
                    })
                }
            }
            if (c.archive != null || c.inbox != null) {
                item("feeds-header") { JSectionHeader(stringResource(R.string.settings_src_feeds_header)) }
                val feedCount = listOfNotNull(c.archive, c.inbox).size
                var idx = 0
                if (c.archive != null) {
                    val p = GroupPosition.of(idx++, feedCount)
                    item("feed-archive") {
                        JGroupItem(p) {
                            JSwitchRow(stringResource(R.string.settings_src_feed_archive), checked = c.archive ?: false, onCheckedChange = { want ->
                                scope.launch {
                                    try {
                                        connector = connectors?.setConnectorFeed(c.id, "archive", want)
                                    } catch (e: Exception) {
                                        feedback?.notice(res.getString(R.string.settings_src_save_failed, e.message.orEmpty()))
                                    }
                                }
                            })
                        }
                    }
                }
                if (c.inbox != null) {
                    val p = GroupPosition.of(idx++, feedCount)
                    item("feed-inbox") {
                        JGroupItem(p) {
                            JSwitchRow(stringResource(R.string.settings_src_feed_inbox), checked = c.inbox ?: false, onCheckedChange = { want ->
                                scope.launch {
                                    try {
                                        connector = connectors?.setConnectorFeed(c.id, "inbox", want)
                                    } catch (e: Exception) {
                                        feedback?.notice(res.getString(R.string.settings_src_save_failed, e.message.orEmpty()))
                                    }
                                }
                            })
                        }
                    }
                }
            }

            if (c.capabilities?.contains("archive") == true || c.capabilities?.contains("inbox") == true) {
                item("danger-header") { JSectionHeader(stringResource(R.string.settings_src_data_header)) }
                item("delete-data") {
                    JGroupItem(GroupPosition.Single) {
                        JRow(stringResource(R.string.settings_src_delete_data), leadingIcon = PhIcons.Trash, destructive = true, onClick = { confirmingDelete = true })
                    }
                }
            }
        }
        error?.let { err ->
            item("error") {
                JNotice(err, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou)
            }
        }
    }

    if (confirmingLogout && c != null && signIn != null) {
        JDialog(
            title = stringResource(R.string.settings_src_connector_logout_title, c.name),
            content = {
                Text(stringResource(R.string.settings_src_connector_logout_body), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
            },
            confirm = stringResource(R.string.settings_src_connector_sign_out) to {
                confirmingLogout = false
                scope.launch {
                    val failed = signIn.logout()
                    feedback?.notice(failed?.resolve(res) ?: res.getString(R.string.settings_src_connector_signed_out))
                    load()
                }
            },
            dismiss = stringResource(R.string.settings_src_cancel) to { confirmingLogout = false },
            onDismiss = { confirmingLogout = false },
        )
    }

    if (confirmingDelete && c != null) {
        JDialog(
            title = stringResource(R.string.settings_src_delete_data_title, c.name),
            content = {
                Text(
                    stringResource(R.string.settings_src_delete_data_body),
                    style = PersonaTheme.type.callout,
                    color = PersonaTheme.palette.ink2,
                )
            },
            confirm = stringResource(R.string.settings_src_delete) to {
                confirmingDelete = false
                scope.launch {
                    try {
                        connectors?.deleteConnectorData(c.id)
                        feedback?.notice(res.getString(R.string.settings_src_data_deleted))
                        load()
                    } catch (e: Exception) {
                        feedback?.notice(res.getString(R.string.settings_src_delete_failed, e.message.orEmpty()))
                    }
                }
            },
            dismiss = stringResource(R.string.settings_src_cancel) to { confirmingDelete = false },
            onDismiss = { confirmingDelete = false },
        )
    }
}


/**
 * "Sign in" on a connector's page: who it is signed in as with "Sign out", or "Sign in", or the step the hub asks for now (the same
 * shapes as a channel's binding: fields, a QR code, a device code).
 */
private fun LazyListScope.connectorSignIn(
    c: HubConnector, bind: BindState, model: ConnectorSignInModel, scope: kotlinx.coroutines.CoroutineScope, onSignOut: () -> Unit,
) {
    val prompt = bind.prompt
    item("signin-header") { JSectionHeader(stringResource(R.string.settings_src_connector_signin_header)) }
    when {
        prompt == null && ConnectorText.isSignedIn(c) -> item("signed-in") {
            Column {
                JGroupItem(GroupPosition.of(0, 2)) {
                    JRow(stringResource(R.string.settings_src_connector_account),
                        value = c.account?.takeIf { it.isNotBlank() } ?: stringResource(R.string.settings_src_connector_signed_in))
                }
                JGroupItem(GroupPosition.of(1, 2)) {
                    JRow(stringResource(R.string.settings_src_connector_sign_out), leadingIcon = PhIcons.X, destructive = true,
                        onClick = onSignOut, chevron = false)
                }
            }
        }
        prompt == null && c.enabled == false -> item("signin-off") {
            SettingsCaption(stringResource(R.string.settings_src_connector_enable_first), Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s))
        }
        prompt == null -> item("signin-start") {
            Column(Modifier.padding(top = Tokens.Space.xs), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                JButton(stringResource(R.string.settings_src_connector_sign_in, c.name), { scope.launch { model.start() } }, Modifier.fillMaxWidth(),
                    icon = PhIcons.Key, loading = bind.sending)
                bind.error?.let { JInlineStatus(it.text(), tone = InlineTone.Failed) }
                SettingsCaption(stringResource(R.string.settings_src_connector_signin_footer, c.name),
                    Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s))
            }
        }
        else -> item("signin-step-${prompt.step}-${prompt.kind}") { ConnectorSignInStep(c.name, prompt, bind, model, pageScope = scope) }
    }
}
