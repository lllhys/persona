package persona.core.designsystem

import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.SpringSpec
import androidx.compose.animation.core.TweenSpec
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class HomeDrawerMotionTest {
    @Test fun glassDrawerUsesTheIPhoneSpring() {
        val spec = drawerSpec<Float>(AppStyle.Glass, reduceMotion = false)
        assertTrue(spec is SpringSpec<*>)
        val spring = spec as SpringSpec<*>
        assertEquals(1f, spring.dampingRatio, 0f)
        assertEquals(385.5f, spring.stiffness, 1f)
    }

    @Test fun briefAndBoardKeepTheirStandardTweens() {
        for ((style, duration) in listOf(AppStyle.Brief to 240, AppStyle.Board to 160)) {
            val spec = drawerSpec<Float>(style, reduceMotion = false)
            assertTrue(spec is TweenSpec<*>)
            val tween = spec as TweenSpec<*>
            assertEquals(duration, tween.durationMillis)
            assertEquals(0, tween.delay)
            assertEquals(FastOutSlowInEasing, tween.easing)
        }
    }

    @Test fun reducedMotionKeepsTheImmediateTweenInEveryStyle() {
        for (style in listOf(AppStyle.Glass, AppStyle.Brief, AppStyle.Board)) {
            val spec = drawerSpec<Float>(style, reduceMotion = true)
            assertTrue(spec is TweenSpec<*>)
            val tween = spec as TweenSpec<*>
            assertEquals(Tokens.Motion.reduced, tween.durationMillis)
            assertEquals(0, tween.delay)
            assertEquals(FastOutSlowInEasing, tween.easing)
        }
    }
}
