# NativeBridge.deliver is called only from Swift, by name, through the class
# JNI_OnLoad caches; its natives are found by their mangled names. R8 sees
# neither, so both keep their names.
-keep class dev.reamde.substrate.kit.NativeBridge {
    public static void deliver(java.lang.String);
    native <methods>;
}
