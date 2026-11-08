import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import { defineConfig } from "vite"

// A kit package's published build, run from the package's own folder:
// src/index.ts bundled into one ES module, dist/index.js. One file, not
// tsc's file per module, because the sources import each other without
// extensions, which a bundler resolves and Node does not, and
// @substrate/vite runs in Node. The package's dependencies and peers stay
// imports, resolved by the app that installs it; the declarations come from
// tsc (tsconfig.build.json).
export default defineConfig(() => {
  const root = process.cwd()
  const pkg = JSON.parse(
    readFileSync(resolve(root, "package.json"), "utf8")
  ) as {
    dependencies?: Record<string, string>
    peerDependencies?: Record<string, string>
  }
  const external = [
    ...Object.keys(pkg.dependencies ?? {}),
    ...Object.keys(pkg.peerDependencies ?? {}),
  ]
  return {
    build: {
      lib: {
        entry: resolve(root, "src/index.ts"),
        formats: ["es" as const],
        fileName: "index",
      },
      outDir: resolve(root, "dist"),
      emptyOutDir: true,
      sourcemap: true,
      minify: false,
      target: "es2022",
      rollupOptions: {
        external: (id: string) =>
          id.startsWith("node:") ||
          external.some((d) => id === d || id.startsWith(`${d}/`)),
      },
    },
  }
})
