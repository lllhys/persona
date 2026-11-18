package persona.feature.chat

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.DialogOption
import persona.core.contracts.Interaction
import persona.core.contracts.Resolution
import persona.core.contracts.ScreenDialog
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme

/** Android's plain-words command prompt (#5506), using the same rules as iOS and web. Images are synthetic Robolectric
 * native captures in `build/command-ask-shots/`, not emulator screenshots. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h1200dp-xhdpi", application = QuestionTestApplication::class)
class CommandAskUiTest {
    @get:Rule val rule = createComposeRule()
    private val sent = mutableListOf<Resolution>()
    private val yesNo = listOf(DialogOption(key = "1", label = "Yes"), DialogOption(key = "2", label = "No"))
    private val longCommand = "rm -rf ./build ./dist && git status --short\n" +
        "./gradlew :feature:chat:testDebugUnitTest --tests '*CommandAsk*'\n" +
        "git add android/feature/chat android/core/contracts\n" +
        "git commit -m 'Fix Android command permission cards'\n" +
        "git push origin fix/5506-android-command-ask"

    private fun dialog(
        command: String = longCommand,
        warning: String? = "Dangerous rm operation not ruled out — the shell may read this command differently",
        risk: String? = "delete",
        options: List<DialogOption> = yesNo,
        multi: Boolean = false,
        deadlineAction: String? = "deny",
        deadlineAt: String? = Instant.now().plusSeconds(104).toString(),
    ) = ScreenDialog(
        question = "Do you want to proceed?", rev = "44597c89094b", command = command,
        description = "Remove generated files and verify the Android command card",
        warning = warning, risk = risk, options = options, multi = multi,
        deadline = "Claude Code will automatically block this request in 1:44, to avoid holding up an unattended session",
        deadlineAction = deadlineAction, deadlineAt = deadlineAt,
    )

    private fun card(dialog: ScreenDialog = dialog()) = Interaction("ix_command", machineId = "macbook", sessionId = "s1", kind = "screen", dialog = dialog)

    private fun show(interaction: Interaction = card()) {
        sent.clear()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                InteractionPanel(listOf(interaction), null, 1_000.dp, null, emptySet(), { _, resolution -> sent += resolution }, null, {})
            }
        }
    }

    @Test fun plainWordsOriginalWarningCountdownAndAllow() {
        show()
        rule.onNodeWithText("Claude Wants to Run a Command").assertExists()
        rule.onNodeWithText("Do you want to proceed?").assertDoesNotExist()
        rule.onNodeWithText("Remove generated files and verify the Android command card").assertExists()
        rule.onNodeWithText("Can't confirm this command won't delete files").assertExists()
        rule.onAllNodes(hasText("Dangerous rm operation", substring = true)).assertCountEquals(0)
        rule.onNodeWithTag("screen-command-countdown").assert(hasText("Denied automatically in", substring = true))
        rule.onNodeWithTag("screen-command-original").performClick()
        rule.onAllNodes(hasText("Dangerous rm operation", substring = true)).assertCountEquals(1)
        rule.onAllNodes(hasText("avoid holding up an unattended session", substring = true)).assertCountEquals(1)
        rule.onNodeWithText("Yes").assertDoesNotExist()
        rule.onNodeWithText("No").assertDoesNotExist()
        rule.onNodeWithText("Allow").assertIsEnabled().performClick()
        assertEquals(listOf(Resolution(option = "1")), sent)
    }

    @Test fun denyUsesTheOriginalNoOption() {
        show()
        rule.onNodeWithText("Deny").assertIsEnabled().performClick()
        assertEquals(listOf(Resolution(option = "2")), sent)
    }

    @Test fun longCommandFoldsToThreeRowsAndExpands() {
        show()
        rule.onNodeWithTag("screen-command-all").assert(hasText("Show All")).performClick()
        rule.onNodeWithTag("screen-command-all").assert(hasText("Collapse")).performClick()
        rule.onNodeWithTag("screen-command-all").assert(hasText("Show All"))
    }

    @Test fun shortCommandHasNoShowAll() {
        show(card(dialog(command = "ls -la", warning = null, deadlineAt = null)))
        rule.onNodeWithTag("screen-command-all").assertDoesNotExist()
    }

    @Test fun moreChoicesKeepTheirOwnLabels() {
        val options = listOf(
            DialogOption(key = "1", label = "Yes"),
            DialogOption(key = "2", label = "Yes, and don't ask again for rm commands"),
            DialogOption(key = "3", label = "No"),
        )
        show(card(dialog(options = options)))
        rule.onAllNodes(hasText("Yes, and don't ask again for rm commands", substring = true)).assertCountEquals(1)
        rule.onAllNodes(hasText("3. No", substring = true)).assertCountEquals(1)
        rule.onNodeWithText("Allow").assertDoesNotExist()
        rule.onNodeWithText("Deny").assertDoesNotExist()
    }

    @Test fun expiredAllowDeadlineSaysWhatClaudeCodeDoes() {
        show(card(dialog(deadlineAction = "allow", deadlineAt = Instant.now().minusSeconds(1).toString())))
        rule.onNodeWithText("Time's up: Claude Code allows it").assertExists()
    }

    @Test fun commandAskParsingAndCountdownMatchTheOtherClients() {
        val end = Instant.parse("2026-11-12T03:00:00Z")
        val ask = dialog(deadlineAt = end.toString()).commandAsk!!
        assertEquals(longCommand, ask.command)
        assertTrue(ask.folds)
        assertFalse(dialog(command = "ls -la").commandAsk!!.folds)
        assertEquals("Remove generated files and verify the Android command card", ask.description)
        assertEquals(104L, ask.countdownSeconds(end.minusMillis(104_000)))
        assertEquals(1L, ask.countdownSeconds(end.minusMillis(500)))
        assertEquals(0L, ask.countdownSeconds(end))
        assertEquals(0L, ask.countdownSeconds(end.plusSeconds(1)))
        assertTrue(ask.original!!.startsWith("Dangerous rm operation"))
        assertTrue(ask.original!!.contains("\nClaude Code will automatically block"))
        assertEquals(true, dialog(deadlineAction = "allow").commandAsk!!.deadlineAllows)
        assertNull(dialog(deadlineAt = "not a time").commandAsk!!.deadlineAt)
        assertNull(dialog(command = "  ").commandAsk)
    }

    @Test fun allowDenyRequiresExactlyPlainYesThenNo() {
        assertEquals("1", dialog().commandAllowDeny()!!.first.key)
        assertEquals("2", dialog().commandAllowDeny()!!.second.key)
        assertNull(dialog(multi = true).commandAllowDeny())
        assertNull(dialog(options = yesNo.reversed()).commandAllowDeny())
        assertNull(dialog(options = yesNo + DialogOption(key = "3", label = "Other")).commandAllowDeny())
        assertNull(dialog(options = listOf(yesNo[0], yesNo[1].copy(kind = "chat"))).commandAllowDeny())
        assertNull(dialog(options = listOf(yesNo[0], yesNo[1].copy(label = "Cancel"))).commandAllowDeny())
    }

    @Test fun drawsEveryStyleLightDarkAndLargeText() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var large by mutableStateOf(false)
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                val density = LocalDensity.current
                CompositionLocalProvider(LocalDensity provides Density(density.density, if (large) 2f else density.fontScale)) {
                    Column(Modifier.width(400.dp).background(PersonaTheme.palette.background).testTag("command-ask-shot")) {
                        InteractionPanel(listOf(card()), null, 1_000.dp, null, emptySet(), { _, _ -> }, null, {})
                    }
                }
            }
        }
        val output = File("build/command-ask-shots").also { it.mkdirs() }
        val defaultPixels = mutableMapOf<String, IntArray>()
        for (isLarge in listOf(false, true)) {
            for (s in AppStyle.entries) {
                val appearances = mutableListOf<IntArray>()
                for (d in listOf(false, true)) {
                    style = s; dark = d; large = isLarge
                    Snapshot.sendApplyNotifications(); rule.mainClock.advanceTimeBy(1_000); rule.waitForIdle()
                    val bitmap = rule.onNodeWithTag("command-ask-shot").captureToImage().asAndroidBitmap()
                    val name = "${if (isLarge) "large" else "default"}-${s.name.lowercase()}-${if (d) "dark" else "light"}"
                    File(output, "$name.png").outputStream().use { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) }
                    val pixels = IntArray(bitmap.width * bitmap.height).also { bitmap.getPixels(it, 0, bitmap.width, 0, 0, bitmap.width, bitmap.height) }
                    appearances += pixels
                    if (!isLarge) defaultPixels["${s.name}-$d"] = pixels
                    else assertFalse("$name must differ from default text", defaultPixels["${s.name}-$d"]!!.contentEquals(pixels))
                }
                assertFalse("${s.name}: light and dark must differ", appearances[0].contentEquals(appearances[1]))
            }
        }
        assertEquals(12, output.listFiles()!!.count { it.extension == "png" })
    }
}
