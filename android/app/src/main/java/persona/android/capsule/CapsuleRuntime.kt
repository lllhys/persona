package persona.android.capsule

import persona.android.R
import android.app.Application
import android.app.AppOpsManager
import android.content.Intent
import android.net.Uri
import android.widget.Toast
import androidx.compose.runtime.*
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.platform.UriHandler
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ProcessLifecycleOwner
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import persona.android.MainActivity
import persona.android.PersonaApp
import persona.android.di.ConnectionRuntime
import persona.android.ui.AppServices
import persona.core.contracts.api.ExtensionManagement
import persona.core.contracts.api.FloatingCapsuleSettings
import persona.core.designsystem.*
import persona.core.navigation.*
import persona.feature.chat.ChatWiringSource
import persona.feature.settings.ExtensionCapsuleCards
import persona.platform.capsule.*
import persona.platform.notify.Notify
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class CapsuleRuntime @Inject constructor(
    application: Application,
    private val services: AppServices,
    private val wiring: ChatWiringSource,
    private val extensions: ExtensionManagement,
    private val connections: ConnectionRuntime,
) : FloatingCapsuleSettings {
    private val app = application as PersonaApp
    private val preferences = CapsulePreferences(app)
    override val mode = preferences.mode.map { it.key }.stateIn(app.scope, SharingStarted.Eagerly, preferences.mode.value.key)
    private val permission = MutableStateFlow(CapsulePermission.granted(app))
    override val permissionGranted = permission.asStateFlow()
    private val keeper = MutableStateFlow(CapsuleKeeper.enabled(app))
    override val keeperEnabled = keeper.asStateFlow()
    override fun openKeeperSettings() { app.startActivity(CapsuleKeeper.settingsIntent()) }
    private var host: CapsuleHost? = null
    private val mutablePresentation = MutableStateFlow<CapsulePresentation?>(null)
    internal val presentation = mutablePresentation.asStateFlow()
    private val mutableFullScreen = MutableStateFlow(false)
    internal val fullScreen = mutableFullScreen.asStateFlow()
    private var returnExpanded = false

    private fun expandInApp() {
        if (mutablePresentation.value == null) return
        mutableFullScreen.value = true
        app.startActivity(Intent(app, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP))
    }
    internal fun leaveFullScreen() {
        mutableFullScreen.value = false
        mutablePresentation.value?.source?.fullScreen(false)
    }
    internal fun returnToCapsule() {
        returnExpanded = true
        mutableFullScreen.value = false
        mutablePresentation.value?.source?.fullScreen(false)
    }
    private var started = false
    private val permissionChanged = AppOpsManager.OnOpChangedListener { _, packageName ->
        if (packageName == app.packageName) app.scope.launch(Dispatchers.Main.immediate) { refreshPermission() }
    }
    override fun setMode(mode: String) {
        val chosen = CapsuleMode.entries.single { it.key == mode }
        preferences.setMode(chosen)
        if (chosen == CapsuleMode.Off) CapsuleNoticeMemory.clearPending(app)
        Notify.requireCapsuleConnection(app, chosen != CapsuleMode.Off && CapsulePermission.granted(app), userAction = chosen != CapsuleMode.Off)
        if (chosen != CapsuleMode.Off && !CapsulePermission.granted(app)) requestPermission()
    }
    override fun requestPermission() { app.startActivity(CapsulePermission.intent(app)) }
    override fun refreshPermission() {
        permission.value = CapsulePermission.granted(app)
        keeper.value = CapsuleKeeper.enabled(app)
        if (!permission.value) CapsuleNoticeMemory.clearPending(app)
        Notify.requireCapsuleConnection(app, preferences.mode.value != CapsuleMode.Off && permission.value)
        host?.refreshPermission()
    }
    fun start() {
        if (started) return
        started = true
        // Some systems switch the keeper off when the app updates itself; the update broadcast can come late or not
        // at all, so starting checks too.
        CapsuleKeeper.recover(app)
        app.getSystemService(AppOpsManager::class.java).startWatchingMode(AppOpsManager.OPSTR_SYSTEM_ALERT_WINDOW,
            app.packageName, permissionChanged)
        if (preferences.mode.value == CapsuleMode.Off || !permission.value) CapsuleNoticeMemory.clearPending(app)
        Notify.requireCapsuleConnection(app, preferences.mode.value != CapsuleMode.Off && permission.value)
        app.scope.launch(Dispatchers.Main.immediate) {
            combine(services.session, preferences.mode, permission) { session, mode, granted -> session.takeIf { mode != CapsuleMode.Off && granted } }
                .distinctUntilChanged().collectLatest { session ->
                if (session == null) return@collectLatest
                coroutineScope {
                    val noticeMemory = CapsuleNoticeMemory(app, session.identity)
                    val openInbox: (String?) -> Unit = { item ->
                        launchApp(Uri.Builder().scheme("persona").authority("open")
                            .appendQueryParameter("target", if (item == null) "tab" else "inbox")
                            .apply { if (item == null) appendQueryParameter("tab", "inbox") else appendQueryParameter("item", item) }.build())
                    }
                    val agents = CapsuleSessions(session, wiring, this, openInbox = { item ->
                        launchApp(Uri.Builder().scheme("persona").authority("open")
                            .appendQueryParameter("target", if (item == null) "tab" else "inbox")
                            .apply { if (item == null) appendQueryParameter("tab", "inbox") else appendQueryParameter("item", item) }.build())
                    }, noticeMemory = noticeMemory, currentConversation = { Notify.viewedSession }, openConversation = { key, assistant ->
                        val uri = if (key == null) Uri.parse(if (assistant) AskPersonaLink else "persona://open?target=tab&tab=agents")
                            else NotificationRoute.conversation(key.machineId, key.sessionId, assistant).uri()
                        launchApp(uri)
                    }, openCard = { card -> launchApp(NotificationRoute.card(card.machineId, card.requestId).uri()) },
                        computationContext = Dispatchers.Default)
                    val inboxSource = CapsuleInboxSource(session, this, openInbox, CapsuleNoticeMemory(app, "${session.identity}:inbox"))
                    val source = CapsuleConversationsSource(agents, inboxSource, this,
                        store = app.getSharedPreferences("capsule-passed-${session.identity.hashCode()}", android.content.Context.MODE_PRIVATE),
                        expandInApp = ::expandInApp)
                    val drafts = mutableStateMapOf<Pair<String?, String?>, String>()
                    mutablePresentation.value = CapsulePresentation(source, drafts)
                    val window = CapsuleHost(app, preferences, source, drafts = drafts,
                        interaction = { Notify.userInteraction(app) },
                        turnOff = {
                            setMode(CapsuleMode.Off.key)
                            Toast.makeText(app, app.getString(R.string.app_capsule_turned_off), Toast.LENGTH_LONG).show()
                        },
                        extraContent = { ExtensionCapsuleCards(extensions.takeUnless { session.demo != null }, ::openExtension) },
                        theme = { content -> CapsuleEnvironment(content) }, wake = connections::wake)
                    host = window
                    try {
                        window.start()
                        launch { window.visible.collect { Notify.capsuleVisible(app, it) } }
                        launch { window.attention.collect(connections::setCapsuleAttention) }
                        ProcessLifecycleOwner.get().lifecycle.currentStateFlow.collect { lifecycle ->
                            val foreground = lifecycle.isAtLeast(Lifecycle.State.STARTED)
                            window.setForeground(foreground)
                            if (!foreground && returnExpanded) { returnExpanded = false; window.expand() }
                            refreshPermission()
                        }
                    } finally {
                        mutableFullScreen.value = false
                        if (mutablePresentation.value?.source === source) mutablePresentation.value = null
                        window.close()
                        noticeMemory.clearPending()
                        inboxSource.clearPending()
                        Notify.capsuleVisible(app, false)
                        connections.setCapsuleAttention(false)
                        if (host === window) host = null
                    }
                }
            }
        }
    }
    private fun launchApp(uri: Uri) {
        mutableFullScreen.value = false
        mutablePresentation.value?.source?.fullScreen(false)
        app.startActivity(Intent(app, MainActivity::class.java).setAction(Intent.ACTION_VIEW).setData(uri)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP))
    }
    private fun openExtension(route: ExtensionUiRoute) {
        launchApp(Uri.Builder().scheme("persona").authority("open").appendQueryParameter("target", "extension")
            .appendQueryParameter("extension", route.extensionId).appendQueryParameter("ui", route.uiId)
            .appendQueryParameter("title", route.title).apply { route.state?.let { appendQueryParameter("state", it) } }.build())
    }
    @Composable
    private fun CapsuleEnvironment(content: @Composable () -> Unit) {
        val currentStyle by app.appearance.style.collectAsState()
        val face by app.appearance.face.collectAsState()
        val pack by app.personas.current.collectAsState()
        val feedback = remember { object : AppFeedback {
            override fun failure(error: Throwable, operation: Boolean): Boolean = notice(error.message ?: app.getString(R.string.app_error_failed))
            override fun notice(message: String): Boolean {
                Toast.makeText(app, message, Toast.LENGTH_LONG).show()
                return true
            }
        } }
        val links = remember { object : UriHandler {
            override fun openUri(uri: String) {
                val target = Uri.parse(uri)
                require(target.scheme == "https" && !target.host.isNullOrBlank())
                app.startActivity(Intent(app, MainActivity::class.java).putExtra(MainActivity.EXTRA_CAPSULE_URL, uri)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP))
            }
        } }
        PersonaTheme(style = currentStyle) { CompositionLocalProvider(LocalAppStyleStore provides app.appearance, LocalPersonaDesign provides face,
            LocalPersonaPack provides pack, LocalPersonaName provides (pack?.manifest?.name ?: "Persona"),
            LocalAppFeedback provides feedback, LocalUriHandler provides links, content = content) }
    }
}
