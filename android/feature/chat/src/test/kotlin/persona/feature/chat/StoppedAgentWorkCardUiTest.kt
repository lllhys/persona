package persona.feature.chat

import android.content.Context
import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Interaction
import persona.core.contracts.Question
import persona.core.contracts.QuestionOption
import persona.core.contracts.Resolution
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File

/** A stopped Agent's manager work card retains only answers that do not message the Agent (Web #6753, Android #6839). */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h800dp-xhdpi", application = QuestionTestApplication::class)
class StoppedAgentWorkCardUiTest {
    @get:Rule val rule = createComposeRule()
    private val resources get() = ApplicationProvider.getApplicationContext<Context>().resources
    private val prompt = "The model service failed. What next?"
    private val options = listOf(QuestionOption("Handled, continue"), QuestionOption("Leave it", action = "defer"))
    private val work = Interaction("work_1", kind = "question", toolName = "persona_triage", canCancel = true,
        questions = listOf(Question(prompt, options = options)))
    private val firstPrompt = "First decision?"
    private val secondPrompt = "Second decision?"
    private val multiWork = work.copy(id = "work_multi", questions = listOf(
        Question(firstPrompt, options = options), Question(secondPrompt, options = options),
    ))
    private val close = "Close question without answering"

    @Test fun deferIsTheOnlyAnswerEnabledAndResolves() {
        var request by mutableStateOf<Interaction?>(work)
        val sent = mutableListOf<Pair<String, Resolution>>()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                InteractionPanel(listOfNotNull(request), null, 600.dp, null, emptySet(), { interaction, resolution ->
                    sent += interaction.id to resolution
                    request = null
                }, null, {}, blockedReason = "The Agent has stopped", onDiscardLocal = { fail("Must not discard locally") })
            }
        }
        rule.onNodeWithText("1. Handled, continue").assertIsNotEnabled()
        rule.onNodeWithText("2. Leave it").assertIsEnabled()
        rule.onNodeWithText(resources.getString(R.string.chat_question_other_open)).assertIsNotEnabled()
        rule.onNodeWithText("1. Handled, continue").performClick()
        assertTrue(sent.isEmpty())
        rule.onNodeWithText("2. Leave it").performClick()
        assertEquals(listOf("work_1" to Resolution(answers = mapOf(prompt to "Leave it"))), sent)
    }

    @Test fun closeSendsServerDismissInsteadOfLocalDiscard() {
        var request by mutableStateOf<Interaction?>(work)
        val sent = mutableListOf<Pair<String, Resolution>>()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                InteractionPanel(listOfNotNull(request), null, 600.dp, null, emptySet(), { interaction, resolution ->
                    sent += interaction.id to resolution
                    request = null
                }, null, {}, blockedReason = "The Agent has stopped", onDiscardLocal = { fail("Must not discard locally") })
            }
        }
        rule.onNodeWithContentDescription(close).assertIsEnabled().performClick()
        assertEquals(listOf("work_1" to Resolution(dismiss = true)), sent)
    }

    @Test fun oldHubWithoutActionsKeepsEveryOptionAndCustomAnswerDisabled() {
        val legacy = work.copy(questions = listOf(Question(prompt, options = listOf(
            QuestionOption("Handled, continue"), QuestionOption("Leave it"),
        ))))
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                InteractionPanel(listOf(legacy), null, 600.dp, null, emptySet(), { _, _ -> fail("Must not resolve") },
                    null, {}, blockedReason = "The Agent has stopped")
            }
        }
        rule.onNodeWithText("1. Handled, continue").assertIsNotEnabled()
        rule.onNodeWithText("2. Leave it").assertIsNotEnabled()
        rule.onNodeWithText(resources.getString(R.string.chat_question_other_open)).assertIsNotEnabled()
        rule.onNodeWithText(resources.getString(R.string.chat_request_dismiss)).assertIsEnabled()
    }

    @Test fun permissionCardKeepsAnswersDisabledAndDismissLocalOnly() {
        val permission = Interaction("permission_1", kind = "permission", toolName = "persona_triage", summary = "Run the tests")
        var request by mutableStateOf<Interaction?>(permission)
        val sent = mutableListOf<Resolution>()
        val discarded = mutableListOf<String>()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                InteractionPanel(listOfNotNull(request), null, 600.dp, null, emptySet(), { _, resolution -> sent += resolution },
                    null, {}, blockedReason = "The Agent has stopped", onDiscardLocal = {
                        discarded += it.id
                        request = null
                    })
            }
        }
        rule.onNodeWithText(resources.getString(R.string.chat_request_allow)).assertIsNotEnabled()
        rule.onNodeWithText(resources.getString(R.string.chat_request_deny)).assertIsNotEnabled()
        rule.onNodeWithText(resources.getString(R.string.chat_request_dismiss)).assertIsNotEnabled()
        rule.onNodeWithContentDescription(resources.getString(R.string.chat_card_discard_local)).assertIsEnabled().performClick()
        assertEquals(listOf("permission_1"), discarded)
        assertTrue(sent.isEmpty())
    }

    @Test fun stoppedWorkBlocksRetainedNonDeferAnswersUntilEveryAnswerIsDeferred() {
        var blocked by mutableStateOf<String?>(null)
        val sent = mutableListOf<Resolution>()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                InteractionPanel(listOf(multiWork), null, 600.dp, null, emptySet(), { _, resolution -> sent += resolution },
                    null, {}, blockedReason = blocked)
            }
        }
        val previous = resources.getString(R.string.chat_question_previous)
        val next = resources.getString(R.string.chat_question_next)
        val submit = resources.getString(R.string.chat_question_submit)
        // While the Agent runs, retain a message-sending answer on both pages (RequestEditor keeps this draft).
        rule.onNodeWithText("1. Handled, continue").performClick()
        rule.onNodeWithText("1. Handled, continue").performClick()
        assertTrue(sent.isEmpty())
        blocked = "The Agent has stopped"
        rule.waitForIdle()
        rule.onNodeWithText(previous).performClick()
        rule.onNodeWithText("2. Leave it").assertIsEnabled().performClick()
        assertTrue("A multi-question pick updates the draft without resolving", sent.isEmpty())
        rule.onNodeWithText(submit).assertIsNotEnabled()
        rule.onNodeWithText(previous).performClick()
        // The page indicator has no independent navigation. Next must turn the page without submitting the old answer.
        rule.onNodeWithText("1 / 2").assert(!hasClickAction())
        rule.onNodeWithText(next).assertIsEnabled().performClick()
        rule.onNodeWithText(secondPrompt).assertIsDisplayed()
        assertTrue("Advance on a stopped card only turns the page", sent.isEmpty())
        rule.onNodeWithText(submit).assertIsNotEnabled()
        // Replacing the retained answer with defer makes the complete post-action answer set valid again.
        rule.onNodeWithText("2. Leave it").assertIsEnabled().performClick()
        rule.onNodeWithText(submit).assertIsEnabled().performClick()
        assertEquals(listOf(Resolution(answers = mapOf(firstPrompt to "Leave it", secondPrompt to "Leave it"))), sent)
    }

    @Test fun stoppedWorkNextTurnsPageWithRetainedContinueAnswer() {
        var blocked by mutableStateOf<String?>(null)
        val sent = mutableListOf<Resolution>()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                InteractionPanel(listOf(multiWork), null, 600.dp, null, emptySet(), { _, resolution -> sent += resolution },
                    null, {}, blockedReason = blocked)
            }
        }
        val previous = resources.getString(R.string.chat_question_previous)
        val next = resources.getString(R.string.chat_question_next)
        // Retain a non-defer answer while the Agent runs, then return to that page after it stops.
        rule.onNodeWithText("1. Handled, continue").performClick()
        blocked = "The Agent has stopped"
        rule.waitForIdle()
        rule.onNodeWithText(previous).performClick()
        rule.onNodeWithText(next).assertIsEnabled().performClick()
        rule.onNodeWithText(secondPrompt).assertIsDisplayed()
        assertTrue("Next only turns the page; it does not resolve the retained answer", sent.isEmpty())
    }

    @Test fun stoppedWorkUnansweredPageMovesForwardAfterDeferPick() {
        val sent = mutableListOf<Resolution>()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                InteractionPanel(listOf(multiWork), null, 600.dp, null, emptySet(), { _, resolution -> sent += resolution },
                    null, {}, blockedReason = "The Agent has stopped")
            }
        }
        val next = resources.getString(R.string.chat_question_next)
        val submit = resources.getString(R.string.chat_question_submit)
        rule.onNodeWithText(next).assertIsEnabled()
        rule.onNodeWithText("2. Leave it").assertIsEnabled().performClick()
        rule.onNodeWithText(secondPrompt).assertIsDisplayed()
        assertTrue("A single-choice defer pick moves forward without resolving", sent.isEmpty())
        rule.onNodeWithText("2. Leave it").assertIsEnabled().performClick()
        rule.onNodeWithText(submit).assertIsEnabled().performClick()
        assertEquals(listOf(Resolution(answers = mapOf(firstPrompt to "Leave it", secondPrompt to "Leave it"))), sent)
    }

    @Test fun runningWorkAdvanceStillSubmitsCompleteAnswerSet() {
        val sent = mutableListOf<Resolution>()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                InteractionPanel(listOf(multiWork), null, 600.dp, null, emptySet(), { _, resolution -> sent += resolution },
                    null, {}, blockedReason = null)
            }
        }
        val previous = resources.getString(R.string.chat_question_previous)
        val next = resources.getString(R.string.chat_question_next)
        rule.onNodeWithText("1. Handled, continue").performClick()
        rule.onNodeWithText("1. Handled, continue").performClick()
        rule.onNodeWithText(previous).performClick()
        rule.onNodeWithText(next).assertIsEnabled().performClick()
        assertEquals(listOf(Resolution(answers = mapOf(
            firstPrompt to "Handled, continue", secondPrompt to "Handled, continue",
        ))), sent)
    }

    @Test fun stoppedWorkCardScreenshotsAcrossStyles() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Column(Modifier.width(400.dp).background(PersonaTheme.palette.background).testTag("stopped-work-shot")) {
                    InteractionPanel(listOf(work), null, 600.dp, null, emptySet(), { _, _ -> }, null, {},
                        blockedReason = "The Agent has stopped")
                }
            }
        }
        val output = File("build/stopped-work-card-shots").also { check(it.mkdirs() || it.isDirectory) }
        for (appStyle in AppStyle.entries) for (darkTheme in listOf(false, true)) {
            style = appStyle
            dark = darkTheme
            rule.waitForIdle()
            rule.onNodeWithText("1. Handled, continue").assertIsNotEnabled()
            rule.onNodeWithText("2. Leave it").assertIsEnabled()
            val bitmap = rule.onNodeWithTag("stopped-work-shot").captureToImage().asAndroidBitmap()
            val name = "${appStyle.name.lowercase()}-${if (darkTheme) "dark" else "light"}.png"
            File(output, name).outputStream().use { assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, it)) }
        }
    }
}
