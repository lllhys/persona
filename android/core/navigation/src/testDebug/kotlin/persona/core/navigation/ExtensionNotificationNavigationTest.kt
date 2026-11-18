package persona.core.navigation

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ExtensionNotificationNavigationTest {
    private val router = Router()

    @Test fun notificationWithUiOpensItsPageWithOnlyAnOpaqueActionToken() {
        val route = router.parse(RouteInput.DeepLink("persona://open?target=extension&extension=example-gateway&ui=usage" +
            "&title=Usage&state=range%3D1d&manager=assistant&notification=0123456789abcdef0123456789abcdef"))!!
        assertEquals(ExtensionUiRoute("example-gateway", "usage", "Usage", "range=1d", manager = "assistant",
            notificationActionToken = "0123456789abcdef0123456789abcdef"), route.key)
    }

    @Test fun notificationWithoutUiOpensTheExtensionDetailsInsteadOfInventingAPage() {
        val route = router.parse(RouteInput.DeepLink("persona://open?target=extension&extension=example-gateway&manager=assistant&trigger=content"))!!
        assertEquals(ExtensionRoute("example-gateway", manager = "assistant"), route.key)
    }

    @Test fun ordinaryExtensionLinksAndValidationStayUnchanged() {
        assertEquals(ExtensionUiRoute("example-gateway", "usage", ""),
            router.parse(RouteInput.DeepLink("persona://open?target=extension&extension=example-gateway&ui=usage"))!!.key)
        assertNull(router.parse(RouteInput.DeepLink("persona://open?target=extension&ui=usage")))
        assertNull(router.parse(RouteInput.DeepLink("persona://open?target=extension&extension=&ui=usage")))
    }
}
