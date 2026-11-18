plugins {
    id("persona.android.library")
    id("persona.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
}

android {
    namespace = "persona.platform.phonetools"
    defaultConfig.testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    testOptions { unitTests { isIncludeAndroidResources = true } }
}

dependencies {
    implementation(project(":core:contracts"))
    implementation(project(":core:navigation"))
    implementation(project(":core:designsystem"))
    implementation(libs.androidx.core)
    implementation(libs.androidx.activity)
    implementation(libs.androidx.lifecycle.compose)
    implementation(libs.compose.material3)
    implementation(libs.compose.preview)
    implementation(libs.coroutines.android)
    implementation(libs.datastore.preferences)
    implementation(libs.androidx.work)
    implementation(libs.androidx.health.connect)
    androidTestImplementation(libs.androidx.test.runner)
    androidTestImplementation(libs.androidx.test.rules)
    androidTestImplementation(libs.androidx.test.ext.junit)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
    testImplementation(platform(libs.compose.bom))
    testImplementation("androidx.compose.ui:ui-test-junit4")
    testImplementation("androidx.compose.ui:ui-test-manifest")
    testImplementation("org.robolectric:robolectric:4.17")
    testImplementation("androidx.test.ext:junit:1.3.0")
}
