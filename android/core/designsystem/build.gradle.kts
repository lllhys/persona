plugins {
    id("persona.android.library")
    id("persona.compose")
}

android {
    namespace = "persona.core.designsystem"
    testOptions { unitTests { isIncludeAndroidResources = true } }
}

dependencies {
    api(project(":core:domain"))
    implementation(project(":core:hub"))
    implementation(libs.androidx.core)
    // MathImages plugs inline formulas into the Markdown renderer's images.
    implementation(libs.markdown)
    api(libs.compose.material3)
    api(libs.haze)
    implementation(libs.haze.blur)
    implementation(libs.haze.glass)
    implementation(libs.compose.preview)
    debugImplementation(libs.compose.tooling)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
    testImplementation(platform(libs.compose.bom))
    testImplementation("androidx.compose.ui:ui-test-junit4")
    testImplementation("androidx.compose.ui:ui-test-manifest")
    testImplementation("org.robolectric:robolectric:4.17")
    testImplementation("androidx.test.ext:junit:1.3.0")
}
