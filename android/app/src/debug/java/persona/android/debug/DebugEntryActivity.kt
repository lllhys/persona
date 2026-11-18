package persona.android.debug

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import persona.android.PersonaApp
import persona.android.mainActivityIntent
import persona.core.contracts.api.PairingRequest
import persona.core.designsystem.AdaptiveProbe
import java.net.URI
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/** This exported development entry is absent from the release manifest and bytecode. */
class DebugEntryActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        forward(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        forward(intent)
    }

    private fun forward(source: Intent) {
        source.getStringExtra("debug.continuity")?.let {
            DebugContinuity.persist(this, it)
            source.removeExtra("debug.continuity")
        }
        source.getStringExtra("debug.sortShadow")?.let {
            require(it == "on" || it == "off")
            DebugAgentSort.persist(this, it == "on")
            source.removeExtra("debug.sortShadow")
        }
        source.getStringExtra("debug.answerShadow")?.let {
            require(it == "on" || it == "off")
            DebugAnswer.persist(this, it == "on")
            source.removeExtra("debug.answerShadow")
        }
        if (source.getBooleanExtra("debug.automation", false)) AdaptiveProbe.enable(this)
        // Captures of several hub groups: `--es debug.addMockAddress http://<loopback>:<port>` pairs one more mock hub
        // next to the existing ones (settings › hub › "Add hub" does the same with a real pairing link).
        source.getStringExtra("debug.addMockAddress")?.let { address ->
            source.removeExtra("debug.addMockAddress")
            val uri = URI(address)
            val loopback = byteArrayOf(127, 0, 0, 1).joinToString(".") { byte -> byte.toString() }
            require(uri.scheme == "http" && uri.host == loopback && uri.userInfo == null &&
                uri.rawPath in listOf("", "/") && uri.query == null && uri.fragment == null && uri.port in 1..65535) {
                "debug.addMockAddress 只能是本机回环地址上的假 hub"  // i18n-ignore: debug launch arguments, developer-only
            }
            val app = application as PersonaApp
            app.scope.launch {
                try { app.pairingService.addHub(PairingRequest(address, "mock")) { android.util.Log.i("DebugEntry", "add hub: ${it.stage}") } }
                catch (cancelled: CancellationException) { throw cancelled }
                catch (error: Exception) { android.util.Log.w("DebugEntry", "Cannot add mock hub", error) }
            }
        }
        if (source.getStringExtra("debug.automation") == "true") DebugAssistantStackProbe.enable(application)
        startActivity(mainActivityIntent(this).apply {
            source.extras?.let { putExtras(it) }
            data = source.data
            addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        })
        finish()
    }
}
