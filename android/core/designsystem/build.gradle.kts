plugins {
    id("persona.android.library")
    id("persona.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
}

android {
    namespace = "persona.core.designsystem"
    defaultConfig { testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner" }
    testOptions {
        unitTests {
            isIncludeAndroidResources = true
            all {
                it.jvmArgs(
                    "--add-opens=java.base/java.io=ALL-UNNAMED",
                    "--add-exports=java.base/jdk.internal.access=ALL-UNNAMED"
                )
            }
        }
    }
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
    debugImplementation("androidx.compose.ui:ui-test-manifest")
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
    testImplementation(platform(libs.compose.bom))
    testImplementation("androidx.compose.ui:ui-test-junit4")
    testImplementation("androidx.compose.ui:ui-test-manifest")
    testImplementation("org.robolectric:robolectric:4.17")
    testImplementation("androidx.test.ext:junit:1.3.0")
    androidTestImplementation(platform(libs.compose.bom))
    androidTestImplementation(libs.androidx.activity)
    androidTestImplementation("androidx.compose.ui:ui-test-junit4")
    androidTestImplementation("androidx.test:runner:1.7.0")
    androidTestImplementation("androidx.test.ext:junit:1.3.0")
}
