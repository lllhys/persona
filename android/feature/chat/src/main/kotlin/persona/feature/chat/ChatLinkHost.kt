package persona.feature.chat

import persona.core.domain.ports.PortLinks
import android.content.ComponentCallbacks2
import android.content.Intent
import android.content.res.Configuration
import android.net.Uri
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.*
import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.platform.*
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.currentStateAsState
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.feature.inbox.LarkDocumentLink
import persona.feature.inbox.LarkDocumentPrefetch
import persona.feature.inbox.LarkLinkPicks
import persona.feature.inbox.LarkMomentsLink
import persona.feature.inbox.LarkNativeLink
import persona.feature.inbox.LarkTextPick
import persona.feature.inbox.larkNativeLink
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

internal val LocalChatLinkOpening = staticCompositionLocalOf { 0 }

private enum class LinkDetent { Full, Peek, Hidden }

/**
 * Shared URI boundary for Agent, assistant, and other conversation surfaces. http(s) opens in the sheet (the hub's own
 * pages the app has a screen for navigate instead, [LocalChatHubLinks]), `persona://` links navigate ([LocalChatAppLinks]), mailto: / tel: / other apps' links go out after asking, the rest is refused.
 *
 * In a conversation ([onContext], [onEdit]) text picked on a page becomes "Use as context" or "AI edit"; with [onHandToPersona]
 * (pages with no conversation behind them: the app shell, the knowledge base) it is "Hand to Persona" instead.
 */
@Composable
fun ChatLinkHost(
    modifier: Modifier = Modifier,
    preloadUrls: List<String> = emptyList(),
    isScrolling: Boolean = false,
    onContext: ((ChatLinkContext) -> Unit)? = null,
    onEdit: (suspend (ChatLinkContext, String) -> Unit)? = null,
    onVisibilityChanged: (Boolean) -> Unit = {},
    /** Sends a whole message to Persona's main conversation (and shows it); the sheet closes once it went. */
    onHandToPersona: (suspend (String) -> Unit)? = null,
    /**
     * The conversation's manager (docs/assistant/managers.md "Clients"): the documents and posts its links open natively, and
     * those read ahead, are read through it, as are links tapped in them; null: none named.
     */
    manager: String? = null,
    content: @Composable (bottomInset: Dp) -> Unit,
) {
    val context = LocalContext.current
    val resources = LocalResources.current
    val pool = remember(context) { ChatWebViewPool(context) }
    val scope = rememberCoroutineScope()
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val lifecycleState by lifecycle.currentStateAsState()
    val feedback = LocalAppFeedback.current
    val appLinks by rememberUpdatedState(LocalChatAppLinks.current)
    val hubLinks by rememberUpdatedState(LocalChatHubLinks.current)
    val lark = LocalLarkFeatures.current
    var url by remember { mutableStateOf<String?>(null) }
    /** "machine name :port" while the sheet shows one of the machine's ports (docs/operations/ports.md "Forwarding"). */
    var forwarded by remember { mutableStateOf<String?>(null) }
    val ports by rememberUpdatedState(LocalChatPorts.current)
    var unsupported by remember { mutableStateOf(false) }
    var external by remember { mutableStateOf<String?>(null) }
    var opening by remember { mutableIntStateOf(0) }
    var closeRequest by remember { mutableIntStateOf(0) }
    val uriHandler = remember { object : UriHandler {
        override fun openUri(uri: String) {
            when (ChatLinks.kind(uri)) {
                ChatLinkKind.Web -> {
                    val target = ChatLinks.webUrl(uri)
                    // A page of the hub's web app that the app shows itself opens there, not in the sheet.
                    if (target != null && hubLinks?.invoke(target) == true) { if (url != null) closeRequest++; return }
                    // A link to this machine's localhost is one of its ports, opened through the hub (docs/operations/ports.md).
                    val machine = ports
                    val port = machine?.let { PortLinks.link(uri, it.hostnames) }
                    when {
                        target == null -> unsupported = true
                        machine != null && port != null -> scope.launch {
                            runCatching { machine.opener.open(machine.machineId, port.port, port.path, port.secure, port.port in machine.listed) }
                                .onSuccess { local -> pool.forget(local); url = local; forwarded = "${machine.name} :${port.port}"; opening++ }
                                .onFailure { error ->
                                    if (error is persona.core.contracts.api.PortGone) machine.presence?.noteGone(port.port)
                                    if (error !is kotlinx.coroutines.CancellationException) feedback?.notice(error.message ?: resources.getString(R.string.chat_link_port_failed, port.port))
                                }
                        }
                        else -> { url = target; forwarded = null; opening++ }
                    }
                }
                ChatLinkKind.App -> {
                    val link = ChatLinks.appLink(uri)
                    val navigate = appLinks
                    if (link == null || navigate == null) unsupported = true
                    else { if (url != null) closeRequest++; navigate(link) }
                }
                ChatLinkKind.External -> { external = uri.trim() }
                ChatLinkKind.Refused -> { unsupported = true }
            }
        }
    } }
    val visibility by rememberUpdatedState(onVisibilityChanged)
    LaunchedEffect(url) { visibility(url != null) }
    DisposableEffect(pool, lifecycle) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_STOP) pool.pause()
            if (event == Lifecycle.Event.ON_START) pool.resume()
        }
        lifecycle.addObserver(observer)
        onDispose { lifecycle.removeObserver(observer); pool.destroy() }
    }
    // Memory pressure (the app in the background, or the system asks): pages not on screen go; each is a renderer.
    DisposableEffect(pool, context) {
        val callbacks = object : ComponentCallbacks2 {
            override fun onTrimMemory(level: Int) { if (level >= ComponentCallbacks2.TRIM_MEMORY_UI_HIDDEN) pool.trimMemory() }
            override fun onConfigurationChanged(newConfig: Configuration) = Unit
            @Deprecated("Superseded by onTrimMemory")
            override fun onLowMemory() = pool.trimMemory()
        }
        context.applicationContext.registerComponentCallbacks(callbacks)
        onDispose { context.applicationContext.unregisterComponentCallbacks(callbacks) }
    }
    LaunchedEffect(preloadUrls, isScrolling, lifecycleState) {
        if (!isScrolling && lifecycleState.isAtLeast(Lifecycle.State.STARTED)) {
            delay(600)
            pool.warmUp()
            // A machine's port is never preloaded: it would load the phone's own localhost (docs/operations/ports.md). Nor is a
            // Feishu link the card shows natively: its page loads only on "Switch to web page" (its document is read ahead below).
            preloadUrls.mapNotNull { ChatLinks.webUrl(it) }
                .filter { PortLinks.link(it, ports?.hostnames.orEmpty()) == null && larkNativeLink(it, lark) == null }
                .take(3).forEach { pool.preload(it) }
        }
    }
    // Feishu documents among the links are read ahead into the reader's cache, as on the web (iOS skips them too).
    val documentsAhead = remember(preloadUrls, lark) {
        preloadUrls.mapNotNull { ChatLinks.webUrl(it) }.filter { larkNativeLink(it, lark) is LarkNativeLink.Document }.take(3)
    }
    if (documentsAhead.isNotEmpty() && !isScrolling && lifecycleState.isAtLeast(Lifecycle.State.STARTED)) LarkDocumentPrefetch(documentsAhead, manager)
    val picks = if (onHandToPersona != null) listOf(ChatLinkPick.HandToPersona)
        else listOfNotNull(ChatLinkPick.Edit.takeIf { onEdit != null }, ChatLinkPick.Context.takeIf { onContext != null })
    // A web page is over the page: the page's glass goes opaque and the dock makes way (LocalWebSheets).
    WebSheetShown(url != null)
    // A conversation embedded under its host's bar (Persona's page) has the card start below that bar, not under it.
    val hostBar = LocalChromeInsets.current.top
    CompositionLocalProvider(LocalUriHandler provides uriHandler, LocalChatLinkOpening provides opening) {
        BoxWithConstraints(modifier.fillMaxSize()) {
            val height = maxHeight
            val density = LocalDensity.current
            val pixels = with(density) { height.toPx() }
            val above = with(density) { hostBar.toPx() }.coerceIn(0f, pixels)
            val drag = remember { AnchoredDraggableState(LinkDetent.Hidden) }
            SideEffect {
                drag.updateAnchors(DraggableAnchors {
                    LinkDetent.Full at above
                    LinkDetent.Peek at above + (pixels - above) * 0.4f
                    LinkDetent.Hidden at pixels
                })
            }
            LaunchedEffect(opening) { if (url != null) drag.animateTo(LinkDetent.Full) }
            LaunchedEffect(closeRequest) { if (closeRequest > 0 && url != null) drag.animateTo(LinkDetent.Hidden) }
            LaunchedEffect(drag.settledValue) { if (drag.settledValue == LinkDetent.Hidden && !drag.isAnimationRunning) { url = null; forwarded = null } }
            content(if (url != null && drag.currentValue == LinkDetent.Peek) (height - hostBar.coerceAtMost(height)) * 0.6f else 0.dp)
            url?.let { target ->
                // A page whose renderer went away is loaded anew (a new view) when the user asks.
                var generation by remember(target) { mutableIntStateOf(0) }
                // A Feishu link the hub shows natively (the Lark extension, #1530): a document's reader or a Moments post.
                val native = remember(target, lark) { larkNativeLink(target, lark) }
                // Whether that native view switched to the web page ("Switch to web page", or a read that failed): then the card's
                // "Refresh", progress and crash row are the page's again.
                var nativeWebpage by remember(target) { mutableStateOf(false) }
                val nativeShown = native != null && !nativeWebpage
                // The WebView is only taken (and loaded) once a page shows it: a native view reads without it.
                val view = remember(pool, target, generation, nativeShown) { if (nativeShown) null else pool.acquire(target) }
                var editing by remember(target) { mutableStateOf<ChatLinkContext?>(null) }
                var handing by remember(target) { mutableStateOf<ChatLinkContext?>(null) }
                var menu by remember { mutableStateOf(false) }
                val pick by rememberUpdatedState(onContext)
                val picked: (ChatLinkPick, ChatLinkContext) -> Unit = { kind, selected ->
                    when (kind) {
                        ChatLinkPick.Edit -> { editing = selected }
                        ChatLinkPick.Context -> { pick?.invoke(selected); scope.launch { drag.animateTo(LinkDetent.Peek) } }
                        ChatLinkPick.HandToPersona -> { handing = selected }
                    }
                }
                // What the native view read: its title and the text "Use as context" sends for the whole of it (G184).
                var nativeTitle by remember(target) { mutableStateOf<String?>(null) }
                var nativeWhole by remember(target) { mutableStateOf<String?>(null) }
                var nativeRefresh by remember(target) { mutableIntStateOf(0) }
                val nativeFallback = when (native) {
                    is LarkNativeLink.Document -> stringResource(R.string.chat_link_lark_doc)
                    is LarkNativeLink.MomentsPost -> stringResource(R.string.chat_link_moments)
                    null -> ""
                }
                val pickLabels = picks.map { stringResource(it.label) }
                val nativePicks = remember(picks, pickLabels, target) {
                    LarkLinkPicks(picks.zip(pickLabels) { kind, label ->
                        LarkTextPick(label) { text ->
                            picked(kind, ChatLinkContext(if (kind == ChatLinkPick.Edit) "edit" else "context",
                                nativeTitle ?: nativeFallback, target, text))
                        }
                    }, onWebpage = { nativeWebpage = it }) { title, whole -> nativeTitle = title; nativeWhole = whole }
                }
                if (view != null) DisposableEffect(view, picks) {
                    view.picks = picks
                    view.onPick = picked
                    view.onExternal = { external = it }
                    onDispose { pool.release(view) }
                }
                fun close() { scope.launch { drag.animateTo(LinkDetent.Hidden) } }
                fun back() { if (view != null && !view.gone && view.canGoBack()) view.goBack() else close() }
                BackHandler { back() }
                val offset = drag.offset.takeIf { it.isFinite() } ?: pixels
                val panelHeight = with(density) { (pixels - offset).coerceIn(0f, pixels).toDp() }
                val palette = PersonaTheme.palette
                val shape = PersonaTheme.shapes.sheetShape
                // No glass inside the card either: its page would put the WebView in a glass source (LocalWebSheets).
                // The card sits below the host's bar and over the dock, so its page keeps no room for them.
                CompositionLocalProvider(LocalGlassLevel provides GlassLevel.Solid, LocalChromeInsets provides ChromeInsets()) {
                Column(Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(panelHeight)
                    .shadow(Tokens.Space.s, shape).clip(shape).background(palette.surfaceOpaque)) {
                    Column(Modifier.fillMaxWidth().anchoredDraggable(drag, Orientation.Vertical)) {
                        Box(Modifier.align(Alignment.CenterHorizontally).padding(top = Tokens.Space.s).size(Tokens.Space.xxxl, Tokens.Space.xs)
                            .clip(PersonaTheme.shapes.controlShape).background(palette.ink3))
                        Row(Modifier.fillMaxWidth().padding(end = Tokens.Space.xs), verticalAlignment = Alignment.CenterVertically) {
                            JIconButton(PhIcons.CaretLeft, stringResource(R.string.chat_link_back), { back() })
                            Column(Modifier.weight(1f)) {
                                Text((if (native != null && (nativeShown || view?.pageTitle.isNullOrBlank())) nativeTitle ?: nativeFallback else view?.pageTitle.orEmpty()).ifBlank { stringResource(R.string.chat_link_page) }, style = PersonaTheme.type.bodyStrong, color = palette.ink,
                                    maxLines = 1, overflow = TextOverflow.Ellipsis)
                                Text(forwarded ?: view?.currentUrl.orEmpty().ifBlank { target }, style = PersonaTheme.type.micro, color = palette.ink3,
                                    maxLines = 1, overflow = TextOverflow.Ellipsis)
                            }
                            // The whole of a native page as context (iOS LinkSheet, web ContextButton): a document goes
                            // by its link and the whole-document marker (the agent reads it), a post with its text once it is read.
                            val whole = ChatLinkContext.whole(native, nativeWhole)
                            if (whole != null && onContext != null) JIconButton(PhIcons.Quotes,
                                stringResource(if (native is LarkNativeLink.Document) R.string.chat_link_context_doc else R.string.chat_link_context_post), {
                                    picked(ChatLinkPick.Context, ChatLinkContext("context", nativeTitle ?: nativeFallback, target, whole))
                                })
                            JIconButton(if (drag.currentValue == LinkDetent.Full) PhIcons.CaretDown else PhIcons.CaretUp,
                                stringResource(if (drag.currentValue == LinkDetent.Full) R.string.chat_link_peek else R.string.chat_link_full),
                                { scope.launch { drag.animateTo(if (drag.currentValue == LinkDetent.Full) LinkDetent.Peek else LinkDetent.Full) } })
                            Box {
                                val clipboard = LocalClipboardManager.current
                                val page = { view?.currentUrl?.let { ChatLinks.webUrl(it) } ?: target }
                                JIconButton(PhIcons.DotsThree, stringResource(R.string.chat_link_actions), { menu = true })
                                val copied = stringResource(R.string.chat_link_copied)
                                val noBrowser = stringResource(R.string.chat_link_no_browser)
                                // A forwarded port's address is the phone's own loopback: no other app could open it.
                                JMenu(menu, { menu = false }, listOfNotNull(
                                    JAction(stringResource(R.string.chat_link_copy), PhIcons.LinkSimple) {
                                        clipboard.setText(AnnotatedString(page())); feedback?.notice(copied)
                                    }.takeIf { forwarded == null },
                                    JAction(stringResource(R.string.chat_link_reload), PhIcons.ArrowClockwise, enabled = nativeShown || view?.gone == false) {
                                        if (nativeShown) nativeRefresh++ else view?.reload()
                                    },
                                    if (forwarded != null) null else JAction(stringResource(R.string.chat_link_open_browser), PhIcons.ArrowSquareOut) {
                                        val opened = runCatching {
                                            context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(page())).addCategory(Intent.CATEGORY_BROWSABLE))
                                        }.isSuccess
                                        if (!opened) feedback?.notice(noBrowser)
                                    },
                                    JAction(stringResource(R.string.chat_link_share), PhIcons.ShareNetwork) {
                                        val send = Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, page())
                                        runCatching { context.startActivity(Intent.createChooser(send, null)) }
                                    }.takeIf { forwarded == null },
                                ))
                            }
                            JIconButton(PhIcons.X, stringResource(R.string.chat_link_close), ::close)
                        }
                    }
                    if (view != null && !view.gone && view.loadProgress < 100 && view.failure == null)
                        JLinearProgress(view.loadProgress / 100f, Modifier.padding(horizontal = pagePadding))
                    if (view != null) (if (view.gone) stringResource(R.string.chat_link_crashed) else view.failure)?.let { error ->
                        Row(Modifier.padding(horizontal = pagePadding), verticalAlignment = Alignment.CenterVertically) {
                            Text(error, Modifier.weight(1f).padding(vertical = Tokens.Space.s), style = PersonaTheme.type.callout,
                                color = palette.danger.text)
                            JTextButton(stringResource(if (view.gone) R.string.chat_link_reopen else R.string.chat_link_retry), { if (view.gone) generation++ else view.reload() })
                        }
                    }
                    if (native != null) {
                        // Feishu documents and Moments posts in their native views while the hub has them (#1530); each can
                        // still switch to the page, which takes the WebView then (a crashed one is gone: "Reopen" above).
                        val page: @Composable () -> Unit = {
                            if (view != null && !view.gone) key(view) {
                                AndroidView(factory = { view }, modifier = Modifier.fillMaxSize(), onRelease = { pool.release(it) })
                            }
                        }
                        if (native is LarkNativeLink.Document)
                            LarkDocumentLink(target, Modifier.fillMaxWidth().weight(1f), nativePicks, nativeRefresh, manager, page)
                        else if (native is LarkNativeLink.MomentsPost)
                            LarkMomentsLink(native.postId, target, Modifier.fillMaxWidth().weight(1f), nativePicks, nativeRefresh, manager, page)
                    } else if (view != null && !view.gone) key(view) {
                        JPage(title = stringResource(R.string.chat_link_page), modifier = Modifier.fillMaxWidth().weight(1f), topBar = {}) { padding ->
                            AndroidView(factory = { view },
                                modifier = Modifier.fillMaxSize().padding(padding).padding(horizontal = pagePadding),
                                onRelease = { pool.release(it) })
                        }
                    } else Spacer(Modifier.fillMaxWidth().weight(1f))
                }
                }
                editing?.let { selection ->
                    LinkPickDialog(stringResource(R.string.chat_link_edit_title), selection, stringResource(R.string.chat_link_edit_hint), onDismiss = { editing = null }) { instruction ->
                        onEdit?.invoke(selection, instruction)
                        editing = null
                        scope.launch { drag.animateTo(LinkDetent.Peek) }
                    }
                }
                handing?.let { selection ->
                    HandToPersonaDialog(selection, onDismiss = { handing = null }) { message ->
                        onHandToPersona?.invoke(message)
                        handing = null
                        close()
                    }
                }
            }
        }
        if (unsupported) JDialog(onDismiss = { unsupported = false }, title = stringResource(R.string.chat_link_unsupported_title),
            text = stringResource(R.string.chat_link_unsupported_text), confirm = stringResource(R.string.chat_link_ok) to { unsupported = false }, dismiss = null)
        external?.let { link ->
            val prompt = remember(link, resources) { ChatLinks.externalPrompt(link, resources) }
            val noApp = stringResource(R.string.chat_link_no_app)
            JDialog(onDismiss = { external = null }, title = prompt.title, text = prompt.text, confirm = stringResource(R.string.chat_link_open) to {
                external = null
                if (!openExternal(context, link)) feedback?.notice(noApp)
            })
        }
    }
}

/**
 * The machine a conversation runs on, for its port links (docs/operations/ports.md "Recognizing links in replies"): its id and name, the names
 * it goes by in links, the ports its agents list (the hub allows those without asking), and how to open one.
 */
internal data class ChatPorts(
    val machineId: String,
    val name: String,
    val hostnames: List<String>,
    /** The ports its agents list, with their pages' titles. */
    val listed: Map<Int, String?>,
    val opener: persona.core.contracts.api.PortOpener,
    /** What the other ports' badges know (null where nothing asks, e.g. a preview). */
    val presence: PortPresence? = null,
)

internal val LocalChatPorts = androidx.compose.runtime.compositionLocalOf<ChatPorts?> { null }
