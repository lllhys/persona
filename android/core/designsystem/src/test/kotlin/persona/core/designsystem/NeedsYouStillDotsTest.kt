package persona.core.designsystem

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.HubManager

/**
 * Where a Needs You dot breathes in Glass and where it is still, place by place as iOS draws it: still in the
 * dense places iOS gives a still dot or icon (the quick filters, a heading's counts, the badge of a button or an
 * avatar, a request's card and panel, the sidebar's rows), breathing where iOS's `StatusDot` breathes (an agent's row,
 * a chat's header, a space's tabs). A lead row's working spinner (#6230) turns in a dense row too, as iOS's does. Each place
 * is drawn alone and caught twice, 400 ms apart: a breath (one a second) or a turn changes its pixels, a still dot doesn't.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h800dp-xhdpi")
class NeedsYouStillDotsTest {
    @get:Rule val rule = createComposeRule()

    private val places: List<Pair<String, @Composable () -> Unit>> = listOf(
        // still, as iOS
        "quick filter chip" to { QuickFilterBar(listOf(Bay("All"), Bay("Needs you", 2, Status.NeedsYou)), 0, onSelect = NoPick) },
        // the Inbox's section bar, its count a badge (#9053)
        "quick filter chip with a toned count" to {
            QuickFilterBar(listOf(Bay("Needs you", 2, Status.NeedsYou, countTone = Status.NeedsYou), Bay("All")), 0, onSelect = NoPick)
        },
        "home filter chip" to {
            CompositionLocalProvider(LocalHomeChrome provides HomeChrome(emptyList(), "agents", {}, {})) {
                HomeAgentFilters(listOf(Bay("All"), Bay("Needs you", 2, Status.NeedsYou)), 0, onSelect = NoPick)
            }
        },
        "heading counts" to { androidx.compose.foundation.layout.Row { StateCounts(listOf(Status.NeedsYou to 1, Status.Working to 2)) } },
        "assistant avatar badge" to { JManagerAvatar(HubManager("m-3fa21c", "Work", online = true), isDefault = false, alert = true) },
        "request card" to { DecisionCard(DecisionKind.Question, "Which branch?") },
        "request panel" to { DecisionDock(DecisionKind.Question, "Which branch?") {} },
        "sidebar row" to { JSidebarHistoryRow("Weekend", Status.NeedsYou, selected = false, onClick = {}, onMenu = {}, menuLabel = "More") },
        "dot asked to keep still" to { JStatusDot(Status.NeedsYou, breathes = false) },
        // a lead whose own row needs you keeps its still dot even with Agents at work under it (#6230, iOS `RowStateMark.spins`)
        "sidebar row needing you over working Agents" to {
            JSidebarHistoryRow("Weekend", Status.NeedsYou, selected = false, onClick = {}, onMenu = {}, menuLabel = "More", working = true)
        },
        // breathing, as iOS's StatusDot
        "state dot" to { JStatusDot(Status.NeedsYou) },
        "row mark" to { JStatusMark(Status.NeedsYou) },
        "chip of a state" to { JChip("Fix the build", status = Status.NeedsYou) },
        // turning, as iOS's `RowStateMark` turns its spinner in a dense row: a lead with Agents at work (#6230); keeping a
        // Needs You dot still does not stop it
        "sidebar row of a lead with Agents at work" to {
            JSidebarHistoryRow("Weekend", Status.Idle, selected = false, onClick = {}, onMenu = {}, menuLabel = "More", working = true)
        },
        "spinning dot asked to keep still" to { JStatusDot(Status.Working, spinning = true, breathes = false) },
    )

    private val breathing = setOf("state dot", "row mark", "chip of a state", "sidebar row of a lead with Agents at work", "spinning dot asked to keep still")

    @Test fun stillWhereIosIsStill() {
        var place by mutableStateOf(places.first())
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = false, style = AppStyle.Glass) {
                Box(Modifier.testTag("place").width(380.dp).background(PersonaTheme.palette.background).padding(16.dp)) {
                    key(place.first) { place.second() }
                }
            }
        }
        val seen = places.associate { p ->
            place = p
            rule.waitForIdle()
            rule.mainClock.advanceTimeBy(1_000)
            val first = shot()
            rule.mainClock.advanceTimeBy(400)
            p.first to !first.sameAs(shot())
        }
        assertEquals(places.associate { it.first to (it.first in breathing) }, seen)
    }

    @Test fun stillWithReducedMotionAndInTheOtherStyles() {
        var case by mutableStateOf(AppStyle.Glass to true)
        rule.mainClock.autoAdvance = false
        rule.setContent {
            key(case) {
                PersonaTheme(darkTheme = false, reduceMotion = case.second, style = case.first) {
                    Box(Modifier.testTag("place").background(PersonaTheme.palette.background).padding(16.dp)) { JStatusDot(Status.NeedsYou) }
                }
            }
        }
        for (c in listOf(AppStyle.Glass to true, AppStyle.Brief to false, AppStyle.Board to false)) {
            case = c
            rule.waitForIdle()
            rule.mainClock.advanceTimeBy(1_000)
            val first = shot()
            rule.mainClock.advanceTimeBy(400)
            assertEquals("$c", true, first.sameAs(shot()))
        }
    }

    private fun shot(): Bitmap = rule.onNodeWithTag("place").captureToImage().asAndroidBitmap()
}

private val NoPick: (Int) -> Unit = {}
