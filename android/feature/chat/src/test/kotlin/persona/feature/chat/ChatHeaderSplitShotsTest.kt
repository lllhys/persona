package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.api.ConnectionState
import persona.core.designsystem.AppStyle
import persona.core.designsystem.LocalSplitDetail
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.SplitDetailContext
import persona.core.designsystem.StatusLineState
import java.io.File

/**
 * JVM screenshots of the Agent chat header across split states: wide with the list beside it (no Back), wide with
 * the list collapsed (sidebar icon), and compact phone width (Back), in all three styles, light and dark.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w900dp-h600dp-mdpi")
class ChatHeaderSplitShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/header-split-shots").also { it.mkdirs() }

    private val machine = Machine("macbook", "MacBook", online = true)
    private val session = Session(
        machineId = "macbook",
        sessionId = "weather",
        title = "Weather widget",
        status = "idle",
        state = "idle",
        agent = "claude",
        paneId = "p1",
        spaceName = "weather",
        slotName = "widget",
    )

    private fun save(bitmap: Bitmap, name: String) =
        File(out(), "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }

    @Test fun wideAndCompactHeadersAcrossStylesAndThemes() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var split by mutableStateOf<SplitDetailContext?>(null)
        var widthDp by mutableStateOf(620)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                val scope = rememberCoroutineScope()
                val resources = LocalResources.current
                val chrome = remember { ChatChromeState(resources, null, scope, {}, {}) }
                CompositionLocalProvider(LocalSplitDetail provides split) {
                    Box(Modifier.width(widthDp.dp).background(PersonaTheme.palette.background).testTag("shot")) {
                        Column(Modifier.fillMaxWidth()) {
                            ChatHeader(
                                session = session,
                                machine = machine,
                                sessions = listOf(session),
                                state = chrome,
                                connection = ConnectionState.Online,
                                readOnly = false,
                                onBack = {},
                                onOpenSession = {},
                                onConnectionDetails = {},
                                onOverlay = {},
                            )
                            ChatSlimHeader(
                                session = session,
                                statusState = StatusLineState(),
                                connection = ConnectionState.Online,
                                onBack = {},
                                onOpen = {},
                            )
                        }
                    }
                }
            }
        }
        val cases = listOf(
            Triple("wide-beside", 620, SplitDetailContext(isFirstPage = true, listHidden = false, onShowList = null)),
            Triple("wide-collapsed", 620, SplitDetailContext(isFirstPage = true, listHidden = true, onShowList = {})),
            Triple("compact", 400, null),
        )
        for (s in AppStyle.entries) {
            for ((name, width, context) in cases) {
                var light: Bitmap? = null
                for (d in listOf(false, true)) {
                    style = s
                    dark = d
                    widthDp = width
                    split = context
                    Snapshot.sendApplyNotifications()
                    rule.waitForIdle()
                    val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
                    if (!d) light = bitmap else assertTrue("$name ${s.name} light and dark must differ", !bitmap.sameAs(light!!))
                    save(bitmap, "$name-${s.name.lowercase()}-${if (d) "dark" else "light"}")
                }
            }
        }
    }
}
