plugins {
    id("persona.android.library")
    id("persona.compose")
    id("persona.hilt")
    id("org.jetbrains.kotlin.plugin.serialization")
}

android {
    namespace = "persona.feature.inbox"
    // The translation views and the triage banner are rendered on the JVM (Robolectric's native graphics) for their
    // screenshots.
    testOptions { unitTests { isIncludeAndroidResources = true } }
}

dependencies {
    implementation(project(":core:chatlist"))
    implementation(project(":core:contracts"))
    implementation(project(":core:hub"))
    implementation(project(":core:domain"))
    implementation(project(":core:designsystem"))
    implementation(project(":core:navigation"))
    implementation(libs.androidx.lifecycle.compose)
    implementation(libs.androidx.activity)
    implementation(libs.coroutines.android)
    implementation(libs.serialization.json)
    implementation(libs.coil.compose)
    implementation(libs.compose.preview)
    implementation("org.jsoup:jsoup:1.18.3")
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
    testImplementation(platform(libs.compose.bom))
    testImplementation("androidx.compose.ui:ui-test-junit4")
    testImplementation("androidx.compose.ui:ui-test-manifest")
    testImplementation("org.robolectric:robolectric:4.17")
    testImplementation("androidx.test.ext:junit:1.3.0")
}
