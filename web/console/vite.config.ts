/// <reference types="vitest/config" />
import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig, type Plugin, type Rollup } from "vite"

import { GUEST_BUILD_ID, type GuestBuild } from "./src/frame/guest-build"

// The console speaks same-origin paths only, because the server serves it at
// `/` with its own API beside it. Dev serves the console from vite instead, so
// vite forwards three prefixes to the substrate: `/api`, `/healthz` and
// `/.well-known`.
const SUBSTRATE = process.env.VITE_PROXY_SUBSTRATE ?? "http://localhost:8080"

const proxyTarget = { target: SUBSTRATE, changeOrigin: true }

/** Forward the POST to the substrate and let vite serve the SPA for the GET,
 * so the same path is both the console's page and the server's door. */
const doorPost = {
  ...proxyTarget,
  bypass: (req: { method?: string }) =>
    req.method === "POST" ? undefined : "/index.html",
}

/** The guest's import map, one row per specifier: the entry chunk's name,
 * which is also its key in `rollupOptions.input`, and the module behind it.
 * The three React rows re-export the console's own React so the guest shares
 * ONE instance with the console's chunk; `preserveEntrySignatures` keeps the
 * exports the guest imports, which the build would otherwise tree-shake away
 * as unused. The chunks take the default hashed names: the shell learns them
 * from the `#guest-build` block, not from a fixed string. */
const GUEST_ENTRIES = [
  { specifier: "react", name: "app-react", file: "src/apps-sdk/react.ts" },
  {
    specifier: "react/jsx-runtime",
    name: "app-jsx-runtime",
    file: "src/apps-sdk/jsx-runtime.ts",
  },
  {
    specifier: "react-dom/client",
    name: "app-react-dom-client",
    file: "src/apps-sdk/react-dom-client.ts",
  },
  {
    specifier: "substrate/app",
    name: "app-sdk",
    file: "src/apps-sdk/index.ts",
  },
  {
    specifier: "substrate/ui",
    name: "app-ui",
    file: "src/apps-sdk/ui/index.ts",
  },
]

/** The chunk `app-frame.html`'s own module script becomes. */
const SHELL_ENTRY = "app-frame"

/** Where `vite dev` serves from: the sources, the pre-bundled deps, its
 * client, virtual ids and files outside the root. The dev pin is prefixes
 * because the graph is unbundled and moves under HMR; `/api` and the door
 * are outside every one of them. */
const DEV_PREFIXES = [
  "/src/",
  "/node_modules/",
  "/@vite/",
  "/@id/",
  "/@fs/",
  "/@react-refresh",
]

const FONT_FILE = /\.(?:woff2?|ttf|otf)$/

const DEV_BUILD: GuestBuild = {
  imports: Object.fromEntries(
    GUEST_ENTRIES.map((e) => [e.specifier, `/${e.file}`])
  ),
  scripts: DEV_PREFIXES,
  fonts: DEV_PREFIXES,
}

/** The build's answer: the import map names the hashed entry chunks, the
 * pin is the exact import closure of the shell and the five entries, static
 * and dynamic imports both, and the font files the console's CSS emitted. */
function fromBundle(bundle: Rollup.OutputBundle): GuestBuild {
  const chunks = Object.values(bundle).filter(
    (o): o is Rollup.OutputChunk => o.type === "chunk"
  )
  const entry = (name: string): Rollup.OutputChunk => {
    const c = chunks.find((c) => c.isEntry && c.name === name)
    if (!c) throw new Error(`the guest entry ${name} is not in the bundle`)
    return c
  }
  const scripts = new Set<string>()
  const closure = (fileName: string) => {
    if (scripts.has(fileName)) return
    scripts.add(fileName)
    const c = bundle[fileName]
    if (c?.type !== "chunk") return
    for (const f of [...c.imports, ...c.dynamicImports]) closure(f)
  }
  const imports: Record<string, string> = {}
  for (const e of GUEST_ENTRIES)
    imports[e.specifier] = `/${entry(e.name).fileName}`
  for (const name of [SHELL_ENTRY, ...GUEST_ENTRIES.map((e) => e.name)]) {
    closure(entry(name).fileName)
  }
  const fonts = Object.values(bundle)
    .filter((o) => o.type === "asset" && FONT_FILE.test(o.fileName))
    .map((o) => `/${o.fileName}`)
  return { imports, scripts: [...scripts].map((f) => `/${f}`), fonts }
}

/** Writes the `#guest-build` block into `app-frame.html`
 * (src/frame/guest-build.ts says what it carries and why the shell cannot
 * fetch it). A `post` hook, because that is the one the build runs with the
 * bundle in hand; in dev it runs per request with the sources. */
function guestBuild(): Plugin {
  return {
    name: "substrate:guest-build",
    transformIndexHtml: {
      order: "post",
      handler(_html, ctx) {
        if (path.basename(ctx.filename) !== "app-frame.html") return
        const build = ctx.bundle ? fromBundle(ctx.bundle) : DEV_BUILD
        return [
          {
            tag: "script",
            attrs: { type: "application/json", id: GUEST_BUILD_ID },
            children: JSON.stringify(build),
            injectTo: "head-prepend",
          },
        ]
      },
    },
  }
}

const here = (rel: string) => path.resolve(import.meta.dirname, rel)

export default defineConfig({
  plugins: [react(), tailwindcss(), guestBuild()],
  resolve: {
    alias: {
      "@": here("./src"),
    },
  },
  server: {
    // A module script is fetched in CORS mode whatever its tag says, and the
    // guest frame (`app-frame.html`) is sandboxed onto an opaque origin whose
    // `Origin` header is the literal `null`; vite's default allowlist (the
    // localhost forms) would refuse it and the SDK import would fail. The
    // frame's own CSP is what keeps this from opening `/api` to it, and a
    // build has no CORS at all: the server answers none.
    cors: {
      origin: [
        /^https?:\/\/(?:(?:[^:]+\.)?localhost|127\.0\.0\.1|\[::1\])(?::\d+)?$/,
        "null",
      ],
    },
    proxy: {
      "/api": proxyTarget,
      "/healthz": proxyTarget,
      "/.well-known": proxyTarget,
      // The door lives at the server root, beside the SPA: `/login` and
      // `/register` are console PAGES on GET and door endpoints on POST, so
      // only the POST is forwarded; the other three are API-only paths.
      "/login": doorPost,
      "/register": doorPost,
      "/tokens": proxyTarget,
      "/password": proxyTarget,
      "/totp": proxyTarget,
    },
  },
  build: {
    rollupOptions: {
      preserveEntrySignatures: "exports-only",
      input: {
        main: here("index.html"),
        [SHELL_ENTRY]: here("app-frame.html"),
        ...Object.fromEntries(GUEST_ENTRIES.map((e) => [e.name, here(e.file)])),
      },
    },
  },
  test: {
    // `node` is the default because most suites are pure logic: a jsdom window
    // per file costs more than every assertion in the suite put together. A
    // suite that renders declares `// @vitest-environment jsdom` on its first
    // line, and `src/test-setup.ts` installs the browser gaps only there.
    environment: "node",
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["./src/test-setup.ts"],
  },
})
