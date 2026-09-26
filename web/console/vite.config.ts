/// <reference types="vitest/config" />
import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig, searchForWorkspaceRoot } from "vite"

// The console speaks same-origin paths only, because the server serves it at
// `/` with its own API beside it. Dev serves the console from vite instead, so
// vite forwards the API prefixes to the substrate (`/api`, `/healthz`,
// `/.well-known`) and the auth door at the root.
const SUBSTRATE = process.env.VITE_PROXY_SUBSTRATE ?? "http://localhost:8080"

const proxyTarget = { target: SUBSTRATE, changeOrigin: true }

// The auth door lives at the root (`/login`, `/register`, `/password`, `/totp`,
// `/tokens`), and `/login` and `/register` are console pages too: a browser
// navigating there asks for HTML and must get the SPA, while the console's own
// calls ask for JSON and must reach the substrate.
const authTarget = {
  ...proxyTarget,
  bypass: (req: { headers: { accept?: string } }) =>
    req.headers.accept?.includes("text/html") ? "/index.html" : undefined,
}

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  server: {
    // The shipped declarations are the fixtures a rendering suite holds every
    // kind to; a jsdom suite loads them through the same checked file access
    // the dev server uses, so the two directories are named beside the root.
    fs: {
      allow: [
        searchForWorkspaceRoot(process.cwd()),
        path.resolve(import.meta.dirname, "../../kinds"),
        path.resolve(import.meta.dirname, "../../samples"),
      ],
    },
    proxy: {
      "/api": proxyTarget,
      "/healthz": proxyTarget,
      "/.well-known": proxyTarget,
      "/login": authTarget,
      "/register": authTarget,
      "/password": authTarget,
      "/totp": authTarget,
      "/tokens": authTarget,
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
