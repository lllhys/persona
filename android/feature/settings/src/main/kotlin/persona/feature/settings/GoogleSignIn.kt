package persona.feature.settings

import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import persona.core.contracts.api.ConnectorsManagement
import persona.core.contracts.api.GoogleFinish
import persona.core.contracts.api.MailAccount
import persona.core.contracts.api.MicrosoftFinish

/**
 * Gmail's "Sign in with Google" on Android (docs/extensions/mail.md "Android").
 *
 * The hub's OAuth client is an iOS client (no secret, PKCE, reversed client ID as a custom redirect scheme).
 * [OAuthRedirectActivity] declares the scheme from the build (${googleOAuthScheme}).
 * [canReceive] tells whether this build catches the scheme the hub uses; when it does not,
 * the page says so and offers Gmail with an app password.
 */
object GoogleSignIn {
    /** Whose sign-in is waiting: Google's (Gmail) or Microsoft's (Outlook, `persona-mail-oauth://outlook`). */
    enum class Provider { Google, Microsoft }

    sealed interface Result {
        data class Code(val code: String, val state: String?) : Result
        data class Failed(val error: String, val state: String?, val description: String? = null) : Result
        data object Cancelled : Result
    }

    sealed interface Finished {
        data class Added(val account: MailAccount) : Finished
        data class Error(val error: Exception) : Finished
    }

    private val scope by lazy { CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate) }
    @Volatile private var pending: CompletableDeferred<Result>? = null
    @Volatile private var activeConnectors: ConnectorsManagement? = null
    private val _finished = MutableStateFlow<Finished?>(null)
    val finished: StateFlow<Finished?> = _finished.asStateFlow()

    fun takeFinished(): Finished? {
        while (true) {
            val v = _finished.value ?: return null
            if (_finished.compareAndSet(v, null)) return v
        }
    }

    private var expectedState: String? = null
    @Volatile private var provider = Provider.Google

    fun canReceive(ctx: Context, scheme: String, provider: Provider = Provider.Google): Boolean {
        if (scheme.isBlank()) return false
        val uri = if (provider == Provider.Microsoft) "$scheme://outlook" else "$scheme:/oauth2redirect"
        val probe = Intent(Intent.ACTION_VIEW, Uri.parse(uri)).setPackage(ctx.packageName)
            .addCategory(Intent.CATEGORY_BROWSABLE)
        return ctx.packageManager.queryIntentActivities(probe, PackageManager.MATCH_DEFAULT_ONLY).isNotEmpty()
    }

    val isWaiting: Boolean get() = pending?.isActive == true

    fun cancel() {
        pending?.complete(Result.Cancelled)
    }

    fun onRedirect(uri: Uri) {
        val state = uri.getQueryParameter("state")
        val expected = expectedState
        if (state.isNullOrEmpty() || expected == null || state != expected) return
        val r = parse(uri.getQueryParameter("code"), state, uri.getQueryParameter("error"), uri.getQueryParameter("error_description"))
        val d = pending
        if (d != null && d.isActive) {
            d.complete(r)
            return
        }
        val connectors = activeConnectors ?: return
        expectedState = null
        scope.launch { finish(connectors, r, expected) }
    }

    fun parse(code: String?, state: String?, error: String?, description: String? = null): Result = when {
        !error.isNullOrEmpty() -> if (error == "access_denied") Result.Cancelled else Result.Failed(error, state, description)
        !code.isNullOrEmpty() -> Result.Code(code, state)
        else -> Result.Cancelled
    }

    private suspend fun finish(connectors: ConnectorsManagement, r: Result, state: String) {
        val outcome = try {
            when (r) {
                is Result.Cancelled -> null
                // Microsoft's error goes to the hub too: it says what to do about it (an admin has to consent, IMAP is off…).
                is Result.Failed -> if (provider == Provider.Microsoft) connectors.microsoftSignInFinish(MicrosoftFinish(state, null, r.error, r.description))
                    else connectors.googleSignInFinish(GoogleFinish(state, null, r.error))
                is Result.Code -> if (provider == Provider.Microsoft) connectors.microsoftSignInFinish(MicrosoftFinish(state, r.code))
                    else connectors.googleSignInFinish(GoogleFinish(state, r.code))
            }?.let { Finished.Added(it) }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            Finished.Error(e)
        }
        if (outcome != null) _finished.value = outcome
    }

    suspend fun signIn(ctx: Context, connectors: ConnectorsManagement, loginHint: String? = null) =
        run(ctx, connectors, Provider.Google, connectors.googleSignInStart(loginHint))

    /** Outlook's "Sign in with a Microsoft account" (docs/extensions/mail.md "Outlook: Sign In with Microsoft"): the same flow on Microsoft's page. */
    suspend fun signInMicrosoft(ctx: Context, connectors: ConnectorsManagement) =
        run(ctx, connectors, Provider.Microsoft, connectors.microsoftSignInStart())

    private suspend fun run(ctx: Context, connectors: ConnectorsManagement, provider: Provider, start: persona.core.contracts.api.GoogleStart) {
        if (!canReceive(ctx, start.callbackScheme, provider)) throw UnsupportedOperationException(start.callbackScheme)
        pending?.complete(Result.Cancelled)
        this.provider = provider
        val d = CompletableDeferred<Result>()
        pending = d
        activeConnectors = connectors
        expectedState = start.state
        ctx.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(start.authUrl)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        val job = scope.launch {
            try {
                finish(connectors, d.await(), start.state)
            } finally {
                if (pending === d) {
                    pending = null
                    expectedState = null
                    activeConnectors = null
                }
            }
        }
        job.join()
    }
}
