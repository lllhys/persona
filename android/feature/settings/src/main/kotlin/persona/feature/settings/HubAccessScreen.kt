package persona.feature.settings

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import persona.core.contracts.api.HubAccess
import persona.core.contracts.api.HubTunnelSettings
import persona.core.designsystem.*

/**
 * The two values of a Cloudflare Access service token (docs/operations/network.md "Behind Cloudflare Access"): the
 * client id in plain text, the secret masked and outside Autofill.
 */
@Composable
internal fun HubAccessFields(clientId: String, onClientId: (String) -> Unit, secret: String, onSecret: (String) -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
        JTextField(clientId, onClientId, Modifier.fillMaxWidth(), label = stringResource(R.string.settings_access_client_id),
            singleLine = true, autofill = false,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii, autoCorrectEnabled = false))
        JSecretField(secret, onSecret, stringResource(R.string.settings_access_client_secret), Modifier.fillMaxWidth())
    }
}

/** Settings › Hubs › Cloudflare Access of one paired hub: set, change or remove its service token. Back once unpaired. */
@Composable
internal fun HubAccessPage(tunnels: HubTunnelSettings, hubId: String, onBack: () -> Unit) {
    val hubs by tunnels.hubs.collectAsState()
    val info = hubs.firstOrNull { it.hubId == hubId }
    var seen by remember(hubId) { mutableStateOf(info != null) }
    LaunchedEffect(info == null) { if (info == null && seen) onBack() else if (info != null) seen = true }
    val res = LocalResources.current
    val feedback = LocalAppFeedback.current
    val scope = rememberCoroutineScope()
    val saved = info?.access
    var clientId by remember(hubId, saved) { mutableStateOf(saved?.clientId.orEmpty()) }
    var secret by remember(hubId, saved) { mutableStateOf(saved?.clientSecret.orEmpty()) }
    var busy by remember { mutableStateOf(false) }
    var failed by remember { mutableStateOf(false) }
    fun save(value: HubAccess?, @StringRes done: Int) {
        if (busy) return
        busy = true; failed = false
        scope.launch {
            try { tunnels.saveAccess(hubId, value); feedback?.notice(res.getString(done)) }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) { failed = true }
            finally { busy = false }
        }
    }
    val entered = HubAccess.of(clientId, secret)
    JListPage(title = stringResource(R.string.settings_access_title), subtitle = info?.name, onBack = onBack, largeTitle = false) {
        if (info == null) return@JListPage
        item("fields") {
            Column(Modifier.padding(top = Tokens.Space.l), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                HubAccessFields(clientId, { clientId = it }, secret, { secret = it })
                SettingsCaption(stringResource(R.string.settings_access_caption))
            }
        }
        item("save") {
            Column(Modifier.padding(top = Tokens.Space.l, bottom = Tokens.Space.xl), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                if (failed) JNotice(stringResource(R.string.settings_access_failed), status = Status.NeedsYou)
                JButton(stringResource(R.string.settings_access_save), { save(entered, R.string.settings_access_saved) }, Modifier.fillMaxWidth(),
                    loading = busy, enabled = entered != null && entered != saved)
                if (saved != null) JButton(stringResource(R.string.settings_access_clear), { save(null, R.string.settings_access_cleared) },
                    Modifier.fillMaxWidth().padding(top = Tokens.Space.m), kind = JButtonKind.Destructive, enabled = !busy)
            }
        }
    }
}
