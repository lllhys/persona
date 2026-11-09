# Release shrinking only (docs/android/android.md "Release shrinking"): R8 removes what nothing reaches, but obfuscates,
# optimizes and merges nothing, so error reports keep the source's class names (in_app is persona.*), simple names and
# source file names. Two kinds of frame need build/outputs/mapping/release/mapping.txt: a lambda body's method may be
# named $r8$lambda$<hash> (D8 already does this to some in unshrunk builds, R8 to more), and lines inside bodies kotlinc
# inlined from an inline function are numbered by the inlined function. Other method names and lines are the source's.
# Code reached only by name needs a keep rule here and a check in scripts/release/check_android_shrink.py.
-dontobfuscate
-dontoptimize
-keepattributes SourceFile,LineNumberTable

# Looked up by name on a failed tasks call (TasksStore.reloadTodos and isLegacySpacesRoute in core/domain).
-keepclassmembers class persona.core.hub.HubFailure {
    public java.lang.Integer getStatus();
    public java.lang.String getCode();
}
