package persona.feature.settings

import android.app.ActivityOptions
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.os.Build

/** PackageInstaller does not grant its sender's background activity launch privileges. */
internal fun installResultCallback(context: Context, sessionId: Int): PendingIntent {
    val result = Intent(context, InstallResultActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        .putExtra(PackageInstaller.EXTRA_SESSION_ID, sessionId)
    val options = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
        ActivityOptions.makeBasic().apply {
            @Suppress("DEPRECATION")
            pendingIntentCreatorBackgroundActivityStartMode =
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.BAKLAVA)
                    ActivityOptions.MODE_BACKGROUND_ACTIVITY_START_ALLOW_IF_VISIBLE
                else ActivityOptions.MODE_BACKGROUND_ACTIVITY_START_ALLOWED
        }.toBundle()
    } else null
    return PendingIntent.getActivity(
        context, sessionId, result, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_MUTABLE, options,
    )
}
