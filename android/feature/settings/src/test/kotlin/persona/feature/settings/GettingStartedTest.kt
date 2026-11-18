package persona.feature.settings

import android.os.Bundle
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/** The build's getting-started address is used only when it is a plain https address; every other value hides the link. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class GettingStartedTest {
    @Test fun anHttpsAddressIsKeptAsWritten() {
        assertEquals("https://guide.example.com/start", GettingStarted.parse("https://guide.example.com/start"))
        assertEquals("https://guide.example.com/start?lang=en", GettingStarted.parse("  https://guide.example.com/start?lang=en \n"))
        assertEquals("HTTPS://guide.example.com", GettingStarted.parse("HTTPS://guide.example.com"))
    }

    @Test fun noValueMeansNoLink() {
        assertNull(GettingStarted.parse(null))
        assertNull(GettingStarted.parse(""))
        assertNull(GettingStarted.parse("   "))
        // The manifest's marker for a build with no address.
        assertNull(GettingStarted.parse("unset"))
    }

    @Test fun anythingButAPlainHttpsAddressIsRefused() {
        for (value in listOf(
            "http://guide.example.com/start", "ftp://guide.example.com", "javascript:alert(1)", "persona://pair?token=x",
            "guide.example.com/start", "https://", "https:///start", "https://user:secret@guide.example.com/start",
            "https://guide.example.com/a b", "https://guide.example.com/\u0000", "https://[bad",
            "https://guide.example.com/" + "a".repeat(2100),
        )) assertNull(value, GettingStarted.parse(value))
    }

    @Test fun theManifestValueIsReadFromItsMetaDataKey() {
        val meta = Bundle().apply { putString(GettingStarted.MetaKey, "https://guide.example.com/start") }
        assertEquals("https://guide.example.com/start", GettingStarted.fromMetaData(meta))
        assertNull(GettingStarted.fromMetaData(Bundle().apply { putString(GettingStarted.MetaKey, "unset") }))
        assertNull(GettingStarted.fromMetaData(Bundle()))
        assertNull(GettingStarted.fromMetaData(null))
    }
}
