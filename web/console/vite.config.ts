/// <reference types="vitest/config" />
import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

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

/** The entries the guest shell's import map names (src/frame/load.ts). Each
 * keeps a fixed file name because the shell resolves it from a string, not a
 * manifest, and its exports are what the guest imports, which the build
 * would otherwise tree-shake away as unused. The three React entries
 * re-export the console's own React so the guest shares ONE instance with
 * the console's chunk. */
const GUEST_ENTRIES = [
  "app-sdk",
  "app-ui",
  "app-react",
  "app-jsx-runtime",
  "app-react-dom-client",
]

const here = (rel: string) => path.resolve(import.meta.dirname, rel)

export default defineConfig({
  plugins: [react(), tailwindcss()],
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
        "app-frame": here("app-frame.html"),
        "app-sdk": here("src/apps-sdk/index.ts"),
        "app-ui": here("src/apps-sdk/ui/index.ts"),
        "app-react": here("src/apps-sdk/react.ts"),
        "app-jsx-runtime": here("src/apps-sdk/jsx-runtime.ts"),
        "app-react-dom-client": here("src/apps-sdk/react-dom-client.ts"),
      },
      output: {
        entryFileNames: (chunk) =>
          GUEST_ENTRIES.includes(chunk.name)
            ? `assets/${chunk.name}.js`
            : "assets/[name]-[hash].js",
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
