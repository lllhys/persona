import com.android.build.api.variant.HasUnitTestBuilder

plugins {
    id("persona.android.library")
    alias(libs.plugins.ksp)
    id("org.jetbrains.kotlin.plugin.serialization")
}

android {
    namespace = "persona.core.data"
    defaultConfig { testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner" }
}

dependencies {
    implementation(project(":core:contracts"))
    implementation(project(":core:domain"))
    implementation(project(":core:rustcore"))
    implementation(project(":feature:inbox"))
    implementation(libs.androidx.core)
    implementation(libs.room.runtime)
    implementation(libs.room.ktx)
    implementation(libs.datastore)
    api(libs.datastore.preferences)
    implementation(libs.datastore.tink)
    implementation(libs.tink.android)
    implementation(libs.coroutines.android)
    ksp(libs.room.compiler)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
    testImplementation(project(":core:hub"))
    testImplementation("com.squareup.okhttp3:mockwebserver:${libs.versions.okhttp.get()}")
    // FixtureReader: the shared chat sync scenarios (spec/fixtures/sync, ChatSyncScenariosTest).
    testImplementation(testFixtures(project(":core:domain")))
    // The items cache against a real Room database on the JVM (SessionItemsCacheTest).
    testImplementation("org.robolectric:robolectric:4.17")
    androidTestImplementation("androidx.test:runner:1.7.0")
    androidTestImplementation("androidx.test.ext:junit:1.3.0")
}

androidComponents {
    beforeVariants { variant ->
        // The Gradle script accessor sees the builder's base type; AGP's public setter lives on the library variant.
        if (variant is HasUnitTestBuilder && variant.name == "release") {
            variant.javaClass.getMethod("setEnableUnitTest", Boolean::class.javaPrimitiveType).invoke(variant, true)
        }
    }
}

ksp { arg("room.schemaLocation", "$projectDir/schemas") }

val continuity = providers.gradleProperty("personaContinuity").orElse("kotlin")
val sortShadow = providers.gradleProperty("persona.sort").orElse("kotlin")
val coreWorkspace = rootProject.layout.projectDirectory.dir("../native/app-core")
val hostLibrary = coreWorkspace.file("target/debug/" + when {
    org.gradle.internal.os.OperatingSystem.current().isMacOsX -> "libpersona_core.dylib"
    org.gradle.internal.os.OperatingSystem.current().isWindows -> "persona_core.dll"
    else -> "libpersona_core.so"
})
tasks.withType<Test>().configureEach {
    // Adapter parity always exercises JNI; the switch selects the implementation for every repository test.
    dependsOn(":core:rustcore:buildHostCore")
    inputs.files(project(":core:rustcore").tasks.named("buildHostCore"))
        .withPropertyName("hostCore").withPathSensitivity(PathSensitivity.NONE)
    systemProperty("persona.core.library", hostLibrary.asFile.absolutePath)
    systemProperty("persona.continuity", continuity.get())
    systemProperty("persona.continuity.frameScheduler", "persona.core.data.FlushContinuityFrameScheduler")
    systemProperty("persona.sort", sortShadow.get())
    if (name == "testDebugUnitTest") forkEvery = 1
}
