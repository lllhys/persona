plugins {
    id("persona.jvm.library")
    id("org.jetbrains.kotlin.plugin.serialization")
    // FixtureReader, for other modules' tests replaying the shared fixtures (core:data's chat sync scenarios).
    `java-test-fixtures`
}

val shadowSource = sourceSets.create("shadow") {
    java.srcDir("src/shadow/kotlin")
    compileClasspath += sourceSets.main.get().output
    runtimeClasspath += sourceSets.main.get().output
}
val shadowTestSource = sourceSets.create("shadowTest") {
    java.srcDir("src/testShadow/kotlin")
    compileClasspath += shadowSource.output + sourceSets.test.get().output
    runtimeClasspath += shadowSource.output + sourceSets.test.get().output
}

configurations.named(shadowSource.compileClasspathConfigurationName) {
    extendsFrom(configurations.implementation.get())
}
configurations.named(shadowSource.runtimeClasspathConfigurationName) {
    extendsFrom(configurations.runtimeOnly.get(), configurations.getByName(shadowSource.implementationConfigurationName))
}
configurations.named(shadowTestSource.compileClasspathConfigurationName) {
    extendsFrom(configurations.testImplementation.get(), configurations.testCompileClasspath.get(),
        configurations.getByName(shadowSource.implementationConfigurationName))
    attributes.attribute(
        org.gradle.api.attributes.Usage.USAGE_ATTRIBUTE,
        objects.named(org.gradle.api.attributes.Usage::class.java, org.gradle.api.attributes.Usage.JAVA_RUNTIME),
    )
}
configurations.named(shadowTestSource.runtimeClasspathConfigurationName) {
    extendsFrom(configurations.testRuntimeOnly.get(), configurations.testRuntimeClasspath.get(),
        configurations.getByName(shadowSource.runtimeOnlyConfigurationName))
}

dependencies {
    api(project(":core:contracts"))
    testImplementation(libs.coroutines.test)
    testImplementation(testFixtures(project(":core:domain")))
    add(shadowSource.implementationConfigurationName, project(":core:rustcore"))
    add(shadowSource.implementationConfigurationName, project(path = ":core:rustcore", configuration = "shadowRuntimeElements"))
    add(shadowTestSource.implementationConfigurationName, testFixtures(project(":core:domain")))
    add(shadowTestSource.implementationConfigurationName, project(":core:rustcore"))
    add(shadowTestSource.implementationConfigurationName, project(path = ":core:rustcore", configuration = "shadowRuntimeElements"))
}

// The test compilation is a friend of the separately compiled observer, not a public API relaxation in production.
tasks.named("compileShadowTestKotlin", org.jetbrains.kotlin.gradle.tasks.KotlinJvmCompile::class.java).configure {
    compilerOptions.freeCompilerArgs.add(provider {
        "-Xfriend-paths=" + layout.buildDirectory.dir("classes/kotlin/shadow").get().asFile.absolutePath
    })
}

val shadowTest = tasks.register<Test>("shadowTest") {
    description = "Runs the DEBUG-only answer observer and both Kotlin/JNI fixture replays on the host JVM."
    testClassesDirs = shadowTestSource.output.classesDirs
    classpath = shadowTestSource.runtimeClasspath
    useJUnit()
    maxParallelForks = 1
    forkEvery = 1
    val repositoryRoot = providers.gradleProperty("personaRepoRoot")        .getOrElse(rootProject.layout.projectDirectory.dir("..").asFile.absolutePath)
    systemProperty("persona.repo.root", repositoryRoot)
    val hostLibrary = rootProject.layout.projectDirectory.dir("../native/app-core").file(
        "target/debug/" + when {
            org.gradle.internal.os.OperatingSystem.current().isMacOsX -> "libpersona_core.dylib"
            org.gradle.internal.os.OperatingSystem.current().isWindows -> "persona_core.dll"
            else -> "libpersona_core.so"
        },
    )
    systemProperty("persona.core.library", hostLibrary.asFile.absolutePath)
    dependsOn(":core:rustcore:buildHostCore")
    inputs.files(fileTree(File(repositoryRoot, "spec/fixtures/answer")) { include("*.json") })
        .withPropertyName("answerFixtures").withPathSensitivity(PathSensitivity.RELATIVE)
    testLogging {
        events("failed", "skipped", "passed")
        showStandardStreams = true
    }
    filter { excludeTestsMatching("persona.core.domain.answer.ShadowAnswerGlobalCounterTest") }
}
val shadowCounterTest = tasks.register<Test>("shadowCounterTest") {
    description = "Checks the default global answer mismatch counter in a fresh JVM."
    testClassesDirs = shadowTestSource.output.classesDirs
    classpath = shadowTestSource.runtimeClasspath
    useJUnit()
    maxParallelForks = 1
    forkEvery = 1
    dependsOn(":core:rustcore:buildHostCore")
    systemProperty("persona.repo.root", providers.gradleProperty("personaRepoRoot")
        .getOrElse(rootProject.layout.projectDirectory.dir("..").asFile.absolutePath))
    systemProperty("persona.core.library", rootProject.layout.projectDirectory.dir("../native/app-core")
        .file("target/debug/libpersona_core.so").asFile.absolutePath)
    filter { includeTestsMatching("persona.core.domain.answer.ShadowAnswerGlobalCounterTest") }
    testLogging { events("failed", "skipped", "passed") }
}
tasks.named("test") { dependsOn(shadowCounterTest) }
tasks.named("check") { dependsOn(shadowTest) }
// The repository's standard root `test` and Android CI must run the observer and its real-JNI fixture replay too.
tasks.named("test") { dependsOn(shadowTest) }

// The app's DEBUG variant uses the same observer class as host tests, without putting it in domain.jar. Kotlin's friend
// path grants access to domain internals without making shadow or native types part of the public production contract.
val domainShadowJar = tasks.register<Jar>("domainShadowJar") {
    archiveBaseName.set("domain-shadow")
    from(shadowSource.output)
    dependsOn(shadowSource.classesTaskName)
}
val shadowJarFile = domainShadowJar.flatMap { it.archiveFile }
val shadowRuntimeElements = configurations.create("shadowRuntimeElements") {
    isCanBeConsumed = true
    isCanBeResolved = false
    attributes.attribute(
        org.gradle.api.attributes.Usage.USAGE_ATTRIBUTE,
        objects.named(org.gradle.api.attributes.Usage::class.java, org.gradle.api.attributes.Usage.JAVA_RUNTIME),
    )
    extendsFrom(configurations.getByName(shadowSource.implementationConfigurationName))
}
artifacts.add(shadowRuntimeElements.name, shadowJarFile) {
    builtBy(domainShadowJar)
}
