/// <reference types="vitest/config" />
import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig, type Plugin } from "vite"

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

const SDK_ENTRY = path.resolve(import.meta.dirname, "src/apps-sdk/index.ts")
const SDK_DEV_URL = "/src/apps-sdk/index.ts"

/** `virtual:substrate-sdk-url`: the URL the frame shell's import map gives
 * `substrate/app` (src/frame/main.ts). In dev it is the SDK's source,
 * transformed on request. In a build the SDK is emitted as its own chunk,
 * its exports kept because they are what a guest imports and nothing in the
 * console does, and the shell receives the chunk's CONTENT-HASHED name
 * through `import.meta.ROLLUP_FILE_URL`, resolved by the bundler after
 * hashing, so the shell chunk's own hash moves with it. The name must be
 * hashed: the server serves `/assets/` as `immutable` for a year
 * (internal/api, `spaHandler`), and a fixed name there is a returning
 * browser running last deploy's SDK against this deploy's host, importing a
 * shared chunk the deploy no longer ships. The shell cannot look the name up
 * itself: its origin is opaque and its `connect-src` is `'none'`. */
function sdkUrl(): Plugin {
  const id = "virtual:substrate-sdk-url"
  const resolved = "\0" + id
  let building = false
  let ref: string | undefined
  return {
    name: "substrate-sdk-url",
    configResolved(config) {
      building = config.command === "build"
    },
    buildStart() {
      if (!building) return
      ref = this.emitFile({
        type: "chunk",
        id: SDK_ENTRY,
        name: "app-sdk",
        preserveSignature: "exports-only",
      })
    },
    resolveId(source) {
      return source === id ? resolved : undefined
    },
    load(source) {
      if (source !== resolved) return undefined
      return ref
        ? `export default import.meta.ROLLUP_FILE_URL_${ref}`
        : `export default ${JSON.stringify(SDK_DEV_URL)}`
    },
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss(), sdkUrl()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  server: {
    // A module script is fetched in CORS mode whatever its tag says, and the
    // guest frame (`app-frame.html`) is sandboxed onto an opaque origin whose
    // `Origin` header is the literal `null`; vite's default allowlist (the
    // localhost forms) would refuse it and the SDK import would fail. The
    // frame's own CSP is what keeps this from opening `/api` to it. A build
    // has no CORS at all: the Go server answers none, on `/assets` included,
    // so the built shell cannot load its module or the SDK until it sends
    // `Access-Control-Allow-Origin` on that one path (docs/plans/apps.md);
    // custom views run under `pnpm dev` today, not from the built image.
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
      // Two documents: the console and the guest shell. The SDK is the third
      // chunk, emitted by `sdkUrl` above so the shell learns its hashed name.
      input: {
        main: path.resolve(import.meta.dirname, "index.html"),
        "app-frame": path.resolve(import.meta.dirname, "app-frame.html"),
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
