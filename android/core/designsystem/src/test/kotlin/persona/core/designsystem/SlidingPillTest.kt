package persona.core.designsystem

import android.graphics.Color as AndroidColor
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assertIsNotSelected
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.filter
import androidx.compose.ui.test.getUnclippedBoundsInRoot
import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.DpRect
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.designsystem.icons.PhIcons
import kotlin.math.abs

private val DpRect.width: Dp get() = right - left
private val DpRect.height: Dp get() = bottom - top

/**
 * Glass draws one pill and moves it on `Theme.Motion.state` (`ShellComponents.swift` `SwitchMotion`).
 * The first frame already sits on the selected segment. Brief and Board draw no pill.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-mdpi")
class SlidingPillTest {
    @get:Rule val rule = createComposeRule()

    @Test fun pillFrameLerpsBetweenSegments() {
        assertEquals(PillFrame(0, 100, 36), pillFrame(0f, listOf(0, 100), listOf(100, 100), 36))
        assertEquals(PillFrame(50, 100, 36), pillFrame(0.5f, listOf(0, 100), listOf(100, 100), 36))
        assertEquals(PillFrame(100, 100, 36), pillFrame(1f, listOf(0, 100), listOf(100, 100), 36))
        assertEquals(PillFrame(150, 100, 36), pillFrame(1.5f, listOf(0, 100, 200), listOf(100, 100, 100), 36))
        assertEquals(PillFrame(20, 60, 36), pillFrame(0.5f, listOf(0, 40), listOf(40, 80), 36))
        assertEquals(PillFrame(0, 100, 36), pillFrame(-2f, listOf(0, 100), listOf(100, 100), 36))
        assertEquals(PillFrame(100, 100, 36), pillFrame(9f, listOf(0, 100), listOf(100, 100), 36))
        assertEquals(PillFrame(0, 0, 10), pillFrame(0.4f, emptyList(), emptyList(), 10))
        assertEquals(PillFrame(7, 12, 4), pillFrame(3f, listOf(7), listOf(12), 4))
        assertEquals(PillFrame(20, 60, 30), pillFrame(0.5f, listOf(0, 40), listOf(40, 80), listOf(20, 40)))
        assertEquals(PillFrame(0, 40, 20), pillFrame(0f, listOf(0, 40), listOf(40, 80), listOf(20, 40)))
    }

    @Test fun aTapSpringsTheGlassSegmentPill() {
        rule.mainClock.autoAdvance = false
        var selected by mutableIntStateOf(0)
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = false) {
                JSegmented(listOf(JSegment("Day"), JSegment("This week")), selected, { selected = it })
            }
        }
        rule.mainClock.advanceTimeBy(300)
        val day = bounds("Day")
        val week = bounds("This week")
        val start = pill()
        assertNear(day.left, start.left)
        assertNear(day.width, start.width)
        assertNear(day.top, start.top)
        assertEquals(1, pillCount())
        rule.onNodeWithText("This week").performClick()
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeBy(80)
        val mid = pill()
        val at80 = progress(day.left, week.left, mid.left)
        assertTrue("80ms left progress $at80", at80 in 0.35f..0.85f)
        val width80 = progress(day.width, week.width, mid.width)
        assertTrue("80ms width progress $width80 vs left $at80", abs(width80 - at80) < 0.2f)
        rule.mainClock.advanceTimeBy(120)
        val at200 = progress(day.left, week.left, pill().left)
        // State spring (0.25s, bounce 0.15) is home by 200ms. The layout spring (0.3s, no bounce) is still ~0.90.
        assertTrue("200ms left progress $at200", at200 >= 0.96f)
        rule.mainClock.advanceTimeBy(800)
        val end = pill()
        assertNear(week.left, end.left)
        assertNear(week.width, end.width)
        assertNear(week.height, end.height)
        assertEquals(1, selected)
    }

    @Test fun aJumpSpringsTheIndexAcrossTheSegmentsBetween() {
        rule.mainClock.autoAdvance = false
        var selected by mutableIntStateOf(0)
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = false) {
                JSegmented(
                    listOf(JSegment("Go"), JSegment("A much longer middle"), JSegment("End")),
                    selected, { selected = it },
                )
            }
        }
        rule.mainClock.advanceTimeBy(300)
        val go = bounds("Go")
        val middle = bounds("A much longer middle")
        val end = bounds("End")
        rule.runOnIdle { selected = 2 }
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeBy(80)
        val mid = pill()
        // The index springs through the segment between, so the left edge is not halfway across the row yet.
        val widest = maxOf(go.width, middle.width, end.width)
        assertTrue(
            "jump left ${mid.left} width ${mid.width} widest $widest",
            mid.left > go.left && mid.left < end.left && mid.width <= widest + 1.dp,
        )
        rule.mainClock.advanceTimeBy(920)
        assertNear(end.left, pill().left)
        assertNear(end.width, pill().width)
    }

    @Test fun reduceMotionPutsThePillOnTheChoiceAtOnce() {
        rule.mainClock.autoAdvance = false
        var selected by mutableIntStateOf(0)
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = true, reduceMotion = true) {
                JSegmented(listOf(JSegment("Day"), JSegment("This week")), selected, { selected = it })
            }
        }
        rule.mainClock.advanceTimeBy(100)
        rule.onNodeWithText("This week").performClick()
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeBy(16)
        val week = bounds("This week")
        assertNear(week.left, pill().left)
        assertNear(week.width, pill().width)
        rule.mainClock.advanceTimeBy(104)
        assertNear(week.left, pill().left)
        assertNear(week.width, pill().width)
    }

    @Test fun theHomeSwitchUsesTheSameSpring() {
        rule.mainClock.autoAdvance = false
        var selected by mutableStateOf("assistant")
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = false) {
                HomeNavigation(
                    items = listOf(
                        NavItem("assistant", "Assistant", PhIcons.ChatCircle),
                        NavItem("agents", "Agents", PhIcons.Robot),
                    ),
                    selected = selected, onSelect = { selected = it }, leading = {},
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }
        rule.mainClock.advanceTimeBy(300)
        val from = bounds("Assistant")
        val to = bounds("Agents")
        assertNear(from.left, pill().left)
        assertNear(from.width, pill().width)
        assertEquals(1, pillCount())
        rule.onNodeWithTag("home-switcher").assertExists()
        rule.onNodeWithText("Agents").performClick()
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeBy(80)
        val at80 = progress(from.left, to.left, pill().left)
        assertTrue("home 80ms $at80", at80 in 0.35f..0.85f)
        rule.mainClock.advanceTimeBy(120)
        val at200 = progress(from.left, to.left, pill().left)
        assertTrue("home 200ms $at200", at200 >= 0.96f)
        rule.mainClock.advanceTimeBy(800)
        assertNear(to.left, pill().left)
        assertNear(to.width, pill().width)
        assertEquals("agents", selected)
    }

    @Test fun theFirstFrameShowsThePillOnTheSelectedSegment() {
        rule.mainClock.autoAdvance = false
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = false) {
                GlassSlidingPill(selected = 1, count = 2, color = Color.Black) { index ->
                    Box(Modifier.size(if (index == 0) 40.dp else 70.dp, 36.dp).testTag("seg-$index"))
                }
            }
        }
        val chosen = bounds("seg-1")
        assertNear(chosen.left, pill().left)
        assertNear(chosen.width, pill().width)
        assertNear(chosen.height, pill().height)
        assertEquals(1, pillCount())
        assertTrue("the second segment is not the leading edge", chosen.left > bounds("seg-0").left)
        rule.mainClock.advanceTimeBy(48)
        assertNear(chosen.left, pill().left)
        assertNear(chosen.width, pill().width)
    }

    @Test fun theFirstFrameShowsThePillOnTheSelectedSegmentInRtl() {
        rule.mainClock.autoAdvance = false
        rule.setContent {
            CompositionLocalProvider(LocalLayoutDirection provides LayoutDirection.Rtl) {
                PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = false) {
                    GlassSlidingPill(selected = 0, count = 2, color = Color.Black) { index ->
                        Box(Modifier.size(if (index == 0) 50.dp else 90.dp, 36.dp).testTag("seg-$index"))
                    }
                }
            }
        }
        val chosen = bounds("seg-0")
        assertNear(chosen.left, pill().left)
        assertNear(chosen.width, pill().width)
        assertTrue("first segment should sit on the right", chosen.left > bounds("seg-1").left)
        rule.mainClock.advanceTimeBy(48)
        assertNear(chosen.left, pill().left)
        assertNear(chosen.width, pill().width)
    }

    @Test fun fillMaxWidthPacksSegmentsAtTheStart() {
        rule.mainClock.autoAdvance = false
        var rtl by mutableStateOf(false)
        var selected by mutableIntStateOf(0)
        rule.setContent {
            CompositionLocalProvider(LocalLayoutDirection provides if (rtl) LayoutDirection.Rtl else LayoutDirection.Ltr) {
                PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = true) {
                    Box(Modifier.width(360.dp)) {
                        JSegmented(
                            listOf(JSegment("Day"), JSegment("This week")),
                            selected,
                            { selected = it },
                            Modifier.fillMaxWidth().testTag("segmented"),
                        )
                    }
                }
            }
        }
        val control = bounds("segmented")
        assertNear(360.dp, control.width)
        val day = bounds("Day")
        val week = bounds("This week")
        assertNear(day.left, pill().left)
        assertNear(day.width, pill().width)
        assertTrue("LTR start ${day.left - control.left}", day.left - control.left < 32.dp)
        assertTrue("LTR tail ${control.right - week.right}", control.right - week.right > 80.dp)
        rule.runOnIdle { selected = 1 }
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeBy(16)
        val weekOn = bounds("This week")
        assertNear(weekOn.left, pill().left)
        assertTrue("second segment stays in the leading half", weekOn.right < control.left + control.width / 2)
        rule.runOnIdle { rtl = true; selected = 0 }
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeBy(16)
        val controlRtl = bounds("segmented")
        val dayRtl = bounds("Day")
        val weekRtl = bounds("This week")
        assertNear(360.dp, controlRtl.width)
        assertNear(dayRtl.left, pill().left)
        assertNear(dayRtl.width, pill().width)
        assertTrue("RTL end ${controlRtl.right - dayRtl.right}", controlRtl.right - dayRtl.right < 32.dp)
        assertTrue("RTL lead ${weekRtl.left - controlRtl.left}", weekRtl.left - controlRtl.left > 80.dp)
        assertTrue(dayRtl.left > weekRtl.left)
    }

    @Test fun thePillHeightFollowsTheSelectedSegment() {
        rule.mainClock.autoAdvance = false
        var selected by mutableIntStateOf(0)
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = false) {
                GlassSlidingPill(selected = selected, count = 2, color = Color.Black) { index ->
                    Box(Modifier.size(48.dp, if (index == 0) 20.dp else 50.dp).testTag("seg-$index"))
                }
            }
        }
        assertNear(20.dp, pill().height)
        assertNear(bounds("seg-0").top, pill().top)
        assertTrue(pill().height < bounds("seg-1").height)
        rule.runOnIdle { selected = 1 }
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeBy(80)
        val mid = pill().height
        assertTrue("mid height $mid", mid > 20.dp && mid < 50.dp)
        rule.mainClock.advanceTimeBy(920)
        assertNear(50.dp, pill().height)
        assertNear(bounds("seg-1").top, pill().top)
    }

    @Test fun nothingSelectedDrawsNoPill() {
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = false) {
                JSegmented(listOf(JSegment("Day"), JSegment("This week")), -1, {})
            }
        }
        rule.waitForIdle()
        assertEquals(0, pillCount())
        rule.onNodeWithText("Day").assertIsNotSelected()
        rule.onNodeWithText("This week").assertIsNotSelected()
    }

    @Test fun aChoiceAfterNothingSelectedAppearsThere() {
        rule.mainClock.autoAdvance = false
        var selected by mutableIntStateOf(-1)
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = false) {
                JSegmented(listOf(JSegment("Day"), JSegment("This week")), selected, { selected = it })
            }
        }
        assertEquals(0, pillCount())
        rule.runOnIdle { selected = 1 }
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeBy(16)
        assertNear(bounds("This week").left, pill().left)
        rule.mainClock.advanceTimeBy(80)
        assertNear(bounds("This week").left, pill().left)
        assertNear(bounds("This week").width, pill().width)
    }

    @Test fun theHomeSwitchDrawsNoPillWhenTheKeyIsMissing() {
        rule.mainClock.autoAdvance = false
        var selected by mutableStateOf("none")
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = true, reduceMotion = false) {
                HomeNavigation(
                    items = listOf(
                        NavItem("assistant", "Assistant", PhIcons.ChatCircle),
                        NavItem("agents", "Agents", PhIcons.Robot),
                    ),
                    selected = selected, onSelect = { selected = it }, leading = {},
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }
        assertEquals(0, pillCount())
        rule.onNodeWithText("Assistant").assertIsNotSelected()
        rule.runOnIdle { selected = "agents" }
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeBy(16)
        assertNear(bounds("Agents").left, pill().left)
        rule.mainClock.advanceTimeBy(80)
        assertNear(bounds("Agents").left, pill().left)
    }

    @Test fun aNarrowParentKeepsLaterSegmentsInside() {
        rule.setContent {
            CompositionLocalProvider(LocalLayoutDirection provides LayoutDirection.Ltr) {
                Box(Modifier.width(100.dp).testTag("narrow")) {
                    GlassSlidingPill(selected = 0, count = 2, color = Color.Black) { index ->
                        Box(Modifier.size(if (index == 0) 40.dp else 80.dp, 24.dp).testTag("seg-$index"))
                    }
                }
            }
        }
        val box = bounds("narrow")
        val second = bounds("seg-1")
        assertTrue("second ${second.width} should be cut down from 80", second.width < 70.dp)
        assertTrue("second right ${second.right} box ${box.right}", second.right <= box.right + 1.dp)
        assertTrue(second.left >= box.left - 1.dp)
        assertNear(bounds("seg-0").width, 40.dp)
    }

    @GraphicsMode(GraphicsMode.Mode.NATIVE)
    @Config(sdk = [35], qualifiers = "en-w1200dp-h900dp-mdpi")
    @Test fun aNarrowRowGivesLaterSegmentsTheWidthLeft() {
        rule.mainClock.autoAdvance = false
        var rtl by mutableStateOf(false)
        var selected by mutableIntStateOf(0)
        rule.setContent {
            val density = LocalDensity.current
            CompositionLocalProvider(
                LocalLayoutDirection provides if (rtl) LayoutDirection.Rtl else LayoutDirection.Ltr,
                LocalDensity provides Density(density.density, fontScale = 2f),
            ) {
                PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = true) {
                    Column {
                        Box(Modifier.width(320.dp).testTag("narrow")) {
                            JSegmented(
                                listOf(JSegment("September"), JSegment("Last year's meetings")),
                                selected, { selected = it }, Modifier.fillMaxWidth(),
                            )
                        }
                        Box(Modifier.width(800.dp).testTag("wrap")) {
                            JSegmented(
                                listOf(JSegment("September"), JSegment("Last year's meetings")),
                                0, {}, Modifier.testTag("wrap-control"),
                            )
                        }
                    }
                }
            }
        }
        fun narrowPill(): DpRect = rule.onAllNodesWithTag("sliding-pill", useUnmergedTree = true)
            .filter(hasAnyAncestor(hasTestTag("narrow")))
            .onFirst()
            .getUnclippedBoundsInRoot()
        fun labelIn(hostTag: String, text: String): DpRect {
            val host = bounds(hostTag)
            val all = rule.onAllNodesWithText(text)
            return listOf(all[0].getUnclippedBoundsInRoot(), all[1].getUnclippedBoundsInRoot()).first {
                it.top >= host.top - 1.dp && it.bottom <= host.bottom + 1.dp
            }
        }
        val box = bounds("narrow")
        assertNear(320.dp, box.width)
        val wrapControl = bounds("wrap-control")
        val narrowText = labelIn("narrow", "Last year's meetings")
        val wrapText = labelIn("wrap", "Last year's meetings")
        val sepNarrow = labelIn("narrow", "September")
        val sepWrap = labelIn("wrap", "September")
        assertTrue(
            "narrow ${narrowText.width} wrap ${wrapText.width} sepN ${sepNarrow.width} sepW ${sepWrap.width} box ${box.width} wrapControl ${wrapControl.width}",
            narrowText.width < wrapText.width - 4.dp,
        )
        assertTrue("ellipsized text still has a width ${narrowText.width}", narrowText.width > 8.dp)
        assertTrue(narrowText.right <= box.right + 1.dp)
        rule.runOnIdle { selected = 1 }
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeBy(16)
        val chosen = labelIn("narrow", "Last year's meetings")
        assertNear(chosen.left, narrowPill().left)
        assertTrue(chosen.right <= box.right + 1.dp)
        rule.runOnIdle { rtl = true; selected = 0 }
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeBy(16)
        val boxRtl = bounds("narrow")
        val longRtl = labelIn("narrow", "Last year's meetings")
        assertTrue("RTL start ${longRtl.left} vs ${boxRtl.left}", longRtl.left >= boxRtl.left - 1.dp)
        assertTrue(longRtl.right <= boxRtl.right + 1.dp)
        assertTrue(longRtl.width < wrapText.width - 4.dp)
        assertNear(labelIn("narrow", "September").left, narrowPill().left)
    }

    @Test fun aTapDoesNotRecomposeTheSegmentsEveryFrame() {
        rule.mainClock.autoAdvance = false
        var selected by mutableIntStateOf(0)
        var frames = 0
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = false) {
                GlassSlidingPill(selected = selected, count = 2, color = Color.Black) { index ->
                    SideEffect { if (index == 0) frames++ }
                    Box(Modifier.size(40.dp, 36.dp))
                }
            }
        }
        rule.mainClock.advanceTimeBy(300)
        val settled = frames
        rule.runOnIdle { selected = 1 }
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeBy(200)
        assertTrue("segment recomposed ${frames - settled} times", frames - settled in 1..3)
    }

    @Test fun aTapDoesNotRecomposeTheLabelEveryFrame() {
        rule.mainClock.autoAdvance = false
        var selected by mutableStateOf(true)
        var frames = 0
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = false) {
                GlassSegmentLabel(
                    "This week", selected = selected, selectedColor = Color.White, idleColor = Color.Gray,
                    style = PersonaTheme.type.labelSmall, onComposed = { frames++ },
                )
            }
        }
        rule.mainClock.advanceTimeBy(300)
        val settled = frames
        rule.runOnIdle { selected = false }
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeBy(200)
        assertTrue("label recomposed ${frames - settled} times", frames - settled in 1..3)
    }

    @GraphicsMode(GraphicsMode.Mode.NATIVE)
    @Test fun theLabelColourFadesWithThePill() {
        rule.mainClock.autoAdvance = false
        var selected by mutableIntStateOf(0)
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass, darkTheme = true, reduceMotion = false) {
                JSegmented(listOf(JSegment("Day"), JSegment("This week")), selected, { selected = it })
            }
        }
        rule.mainClock.advanceTimeBy(300)
        rule.onNodeWithText("This week").performClick()
        Snapshot.sendApplyNotifications()
        rule.mainClock.advanceTimeBy(16)
        val image = rule.onNodeWithText("This week").captureToImage().asAndroidBitmap()
        var maxChannel = 0
        for (y in 0 until image.height) {
            for (x in 0 until image.width) {
                val pixel = image.getPixel(x, y)
                maxChannel = maxOf(maxChannel, AndroidColor.red(pixel), AndroidColor.green(pixel), AndroidColor.blue(pixel))
            }
        }
        assertTrue("dark label went black while the pill was still on Day (channel $maxChannel)", maxChannel > 80)
    }

    @Test fun aMeasuringCopyDoesNotSpring() {
        rule.mainClock.autoAdvance = false
        var selected by mutableIntStateOf(0)
        rule.setContent {
            CompositionLocalProvider(LocalSlidingPillAnimate provides false) {
                PersonaTheme(style = AppStyle.Glass, darkTheme = false, reduceMotion = false) {
                    JSegmented(listOf(JSegment("Day"), JSegment("This week")), selected, { selected = it })
                }
            }
        }
        rule.runOnIdle { selected = 1 }
        Snapshot.sendApplyNotifications()
        // One frame applies the new choice. A spring would still be near the old segment here.
        rule.mainClock.advanceTimeBy(16)
        assertNear(bounds("This week").left, pill().left)
        assertNear(bounds("This week").width, pill().width)
    }

    @Test fun briefAndBoardKeepTheirOwnMarks() {
        var selected by mutableIntStateOf(0)
        var style by mutableStateOf(AppStyle.Brief)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = false, reduceMotion = false) {
                JSegmented(listOf(JSegment("Day"), JSegment("Week")), selected, { selected = it })
            }
        }
        for (next in listOf(AppStyle.Brief, AppStyle.Board)) {
            rule.runOnIdle { style = next; selected = 0 }
            rule.waitForIdle()
            assertEquals(0, pillCount())
            rule.onNodeWithText("Week").performClick()
            rule.waitForIdle()
            assertEquals("$next selects", 1, selected)
            assertEquals(0, pillCount())
        }
    }

    /** The placed switch is under `home-level-*`. Measurement copies are not, and carry no tag of their own. */
    private fun placedPills() = run {
        val all = rule.onAllNodesWithTag("sliding-pill", useUnmergedTree = true)
        val onTheSwitch = all.filter(
            hasAnyAncestor(
                SemanticsMatcher("home level") { node ->
                    node.config.getOrNull(SemanticsProperties.TestTag)?.startsWith("home-level") == true
                },
            ),
        )
        if (onTheSwitch.fetchSemanticsNodes().isNotEmpty()) onTheSwitch else all
    }

    private fun pill(): DpRect = placedPills().onFirst().getUnclippedBoundsInRoot()

    private fun pillCount(): Int = placedPills().fetchSemanticsNodes().size

    private fun bounds(tagOrText: String): DpRect {
        val tagged = rule.onAllNodesWithTag(tagOrText, useUnmergedTree = true).fetchSemanticsNodes()
        return if (tagged.isNotEmpty()) rule.onNodeWithTag(tagOrText, useUnmergedTree = true).getUnclippedBoundsInRoot()
        else rule.onNodeWithText(tagOrText).getUnclippedBoundsInRoot()
    }

    private fun progress(from: Dp, to: Dp, at: Dp): Float {
        val span = (to - from).value
        return if (span == 0f) 1f else (at - from).value / span
    }

    private fun assertNear(expected: Dp, actual: Dp, tolerance: Dp = 1.dp) {
        assertTrue("expected $expected ± $tolerance, was $actual", abs((actual - expected).value) <= tolerance.value)
    }
}
