package persona.android.ui

import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.rememberLifecycleOwner

/** Keep navigation and scroll state while stopping hidden tabs' page collectors and pollers. */
@Composable
internal fun ShellTabLifecycle(shown: Boolean, content: @Composable () -> Unit) {
    val owner = rememberLifecycleOwner(maxLifecycle = if (shown) Lifecycle.State.RESUMED else Lifecycle.State.CREATED)
    CompositionLocalProvider(LocalLifecycleOwner provides owner, content = content)
}
