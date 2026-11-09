package persona.feature.settings

import android.content.ClipData
import android.content.ClipboardManager
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import persona.core.contracts.api.ConnectorsManagement
import persona.core.contracts.api.MicrosoftConfig
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

/**
 * The Outlook part of "Add mail account" (iOS `OutlookSignInSection`, docs/extensions/mail.md "Outlook: Sign In with Microsoft"): while the hub has no client
 * ID (or the user changes it), the field for the one they registered in Microsoft Entra with the steps and the redirect URI
 * to register; then "Sign in with a Microsoft account", the same flow as Google's.
 */
internal class OutlookSignInState {
    var config by mutableStateOf<MicrosoftConfig?>(null)
    var clientId by mutableStateOf("")
    var editing by mutableStateOf(false)
    var busy by mutableStateOf(false)
    var loaded by mutableStateOf(false)
    /** Reading the hub's Microsoft settings failed: the part offers to read again instead of a form without its URI. */
    var loadFailed by mutableStateOf<String?>(null)
    var attempt by mutableIntStateOf(0)
    val registering: Boolean get() = config?.configured != true || editing
}

@Composable
internal fun rememberOutlookSignIn(connectors: ConnectorsManagement?, active: Boolean, onError: (String?) -> Unit): OutlookSignInState {
    val state = remember { OutlookSignInState() }
    val res = LocalResources.current
    LaunchedEffect(connectors, active, state.attempt) {
        if (!active || connectors == null || state.loaded) return@LaunchedEffect
        state.loadFailed = null
        try { state.config = connectors.microsoftConfig(); state.loaded = true }
        catch (e: CancellationException) { throw e }
        catch (e: Exception) { state.loadFailed = res.getString(R.string.settings_src_mail_outlook_config_failed, e.message.orEmpty()) }
    }
    return state
}

internal fun LazyListScope.outlookSignIn(state: OutlookSignInState, connectors: ConnectorsManagement?, onError: (String?) -> Unit) {
    // Until the hub's settings are read the form would miss the redirect URI its steps point at.
    if (!state.loaded && connectors != null) item("outlook-loading") {
        val failed = state.loadFailed
        if (failed != null) JNotice(failed, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou,
            actionLabel = stringResource(R.string.settings_src_retry), onAction = { state.attempt++ })
        else JLoading(text = stringResource(R.string.settings_src_loading))
    } else if (!state.registering) item("outlook-sign-in") {
        val context = LocalContext.current
        val scope = rememberCoroutineScope()
        val res = LocalResources.current
        Column(Modifier.fillMaxWidth().padding(top = Tokens.Space.l)) {
            JButton(stringResource(if (state.busy) R.string.settings_src_mail_outlook_connecting else R.string.settings_src_mail_outlook_sign_in),
                onClick = {
                    val api = connectors ?: return@JButton
                    state.busy = true
                    onError(null)
                    scope.launch {
                        try { GoogleSignIn.signInMicrosoft(context, api) }
                        catch (e: CancellationException) { throw e }
                        catch (_: UnsupportedOperationException) { onError(res.getString(R.string.settings_src_mail_outlook_unsupported)) }
                        catch (e: Exception) { onError(e.message ?: res.getString(R.string.settings_src_mail_outlook_start_failed)) }
                        finally { state.busy = false }
                    }
                }, enabled = !state.busy, loading = state.busy, modifier = Modifier.fillMaxWidth())
            SettingsCaption(stringResource(R.string.settings_src_mail_outlook_caption), Modifier.padding(top = Tokens.Space.s))
            JTextButton(stringResource(R.string.settings_src_mail_outlook_change_id), {
                state.clientId = state.config?.clientId.orEmpty(); state.editing = true
            })
        }
    } else item("outlook-register") {
        val context = LocalContext.current
        val scope = rememberCoroutineScope()
        val feedback = LocalAppFeedback.current
        val res = LocalResources.current
        Column(Modifier.fillMaxWidth().padding(top = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            JSectionHeader(stringResource(R.string.settings_src_mail_outlook_register_header))
            JTextField(value = state.clientId, onValueChange = { state.clientId = it.trim() },
                label = stringResource(R.string.settings_src_mail_outlook_client_id), singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii, autoCorrectEnabled = false), modifier = Modifier.fillMaxWidth())
            JButton(stringResource(R.string.settings_src_mail_outlook_save_id), onClick = {
                val api = connectors ?: return@JButton
                state.busy = true
                scope.launch {
                    try { state.config = api.setMicrosoftClientId(state.clientId.trim()); state.editing = false; onError(null) }
                    catch (e: CancellationException) { throw e }
                    catch (e: Exception) { onError(e.message ?: res.getString(R.string.settings_src_mail_outlook_save_failed)) }
                    finally { state.busy = false }
                }
            }, enabled = !state.busy && state.clientId.isNotBlank(), loading = state.busy, kind = JButtonKind.Secondary, modifier = Modifier.fillMaxWidth())
            state.config?.redirectUri?.takeIf { it.isNotBlank() }?.let { uri ->
                JGroupItem(GroupPosition.Single) {
                    JRow(stringResource(R.string.settings_src_mail_outlook_redirect), subtitle = uri, subtitleLines = 1, chevron = false,
                        trailing = {
                            JIconButton(PhIcons.Copy, stringResource(R.string.settings_src_mail_outlook_copy_redirect), {
                                context.getSystemService(ClipboardManager::class.java)?.setPrimaryClip(ClipData.newPlainText(res.getString(R.string.settings_src_mail_outlook_redirect), uri))
                                feedback?.notice(res.getString(R.string.settings_src_mail_outlook_copied))
                            })
                        })
                }
            }
            SettingsCaption(stringResource(R.string.settings_src_mail_outlook_steps))
        }
    }
}
