// Every module applies a convention plugin from build-logic/ and nothing
// else, so the pins, the toolchain and the locks are written once there.
// The root applies one too: the Kotlin formatter, which formats every
// module's sources and the build's own scripts from here.
plugins {
    id("substrate.ktfmt")
}
