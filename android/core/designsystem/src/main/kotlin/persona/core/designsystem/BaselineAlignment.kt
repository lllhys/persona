package persona.core.designsystem

import androidx.compose.foundation.layout.RowScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.AlignmentLine
import androidx.compose.ui.layout.FirstBaseline

/**
 * [modifier] aligned in its Row by the first baseline of its text, as `alignBy(FirstBaseline)`, but a node whose
 * baseline cannot be read at that moment is placed by the Row's own alignment instead of ending the process.
 *
 * The app's pages are laid out under a lookahead pass, and while a window changes size a child can be asked for its
 * baseline when its lookahead state is not there yet: Compose then throws a NullPointerException from inside the
 * measure pass (`LookaheadDelegate.getAlignmentLinesOwner`), which took the process down with a plan card open in an
 * Agents chat on a tablet, and does the same on the current Compose release. The next layout pass reads the baseline
 * as usual, so only that one frame places the child at the Row's default instead of on the baseline. The fallback matches
 * the captured two-frame failure signature; a null-pointer failure from anywhere else is still thrown.
 */
fun RowScope.firstBaselineAligned(modifier: Modifier = Modifier): Modifier =
    modifier.alignBy { measured -> readBaseline { measured[FirstBaseline] } }

/** The baseline [read] returns, or [AlignmentLine.Unspecified] only for the captured Compose lookahead failure. */
internal fun readBaseline(read: () -> Int): Int = try {
    read()
} catch (error: NullPointerException) {
    if (!error.isMissingLookaheadBaseline()) throw error
    AlignmentLine.Unspecified
}

/** The exact class, method and source-file sequence in the tablet captures; line numbers can move with a Compose patch. */
private fun NullPointerException.isMissingLookaheadBaseline(): Boolean {
    val frames = stackTrace
    return frames.size >= 2 &&
        frames[0].className == "androidx.compose.ui.node.LookaheadDelegate" &&
        frames[0].methodName == "getAlignmentLinesOwner" &&
        frames[0].fileName == "LookaheadDelegate.kt" &&
        frames[1].className == "androidx.compose.ui.node.InnerNodeCoordinator\$LookaheadDelegateImpl" &&
        frames[1].methodName == "calculateAlignmentLine" &&
        frames[1].fileName == "InnerNodeCoordinator.kt"
}
